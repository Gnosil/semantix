package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"semantix/kernel/config"
	"semantix/kernel/judge"
	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

// verify implements offline replay validation for the M0-2 hypothesis:
// "when using a coding agent, repeated intermediate work appears across
// sessions and can be reused". It splits sessions by time (holdout fraction
// reserved as the replay stream), indexes the earlier turns, then replays
// every user turn of the later sessions and prints a TSV evaluation table
// (query → top-1 hit + score) for human relevance marking.
//
// Output columns (tab-separated, first line is a header):
//   session turn score top1_content query
// After marking each row ✅/❌, relevance rate = marked-correct / total.
// Target for M0-Gate: ≥70% of replayed turns find a "previously done similar"
// top-1 hit.

type verifyOptions struct {
	sessions []string // files or directories (scanned for *.jsonl)
	db       string
	project  string
	holdout  float64 // fraction of sessions reserved as replay stream
	scope    slice.Scope
}

type verifyTurn struct {
	Session string
	Turn    int
	Query   string
}

// transcriptLine mirrors kernel/slice's tolerant JSONL parsing (unknown
// fields ignored) so verify stays independent of extractor internals.
type verifyLine struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func collectSessionFiles(paths []string) ([]string, error) {
	var files []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			entries, err := os.ReadDir(p)
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
					files = append(files, filepath.Join(p, e.Name()))
				}
			}
			continue
		}
		files = append(files, p)
	}
	// Time order: a coding agent names session files with timestamps, but
	// mtime is the reliable proxy for "earlier vs later". Stat failures
	// (file removed between walk and sort) surface as errors instead of a
	// nil-pointer panic.
	sort.Slice(files, func(i, j int) bool {
		a, aerr := os.Stat(files[i])
		b, berr := os.Stat(files[j])
		if aerr != nil || berr != nil {
			return false // keep relative order; the replay loop reports the error
		}
		return a.ModTime().Before(b.ModTime())
	})
	return files, nil
}

// parseTurns extracts user turns (in order) from one session JSONL.
func parseTurns(path string) ([]verifyTurn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var turns []verifyTurn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	n := 0
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var l verifyLine
		if err := json.Unmarshal(line, &l); err != nil {
			continue // tolerant: skip corrupt lines
		}
		if l.Role != "user" {
			continue
		}
		if strings.TrimSpace(l.Content) == "" {
			continue // skip empty steering lines
		}
		n++
		turns = append(turns, verifyTurn{Session: filepath.Base(path), Turn: n, Query: strings.TrimSpace(l.Content)})
	}
	return turns, sc.Err()
}

func runVerify(args []string, stdout io.Writer, deps dependencies) int {
	cfgPath, cfgExplicit := explicitConfigPath(args, defaultGetenv)
	cfg, err := loadConfigFor(cfgPath, cfgExplicit, defaultGetenv)
	if err != nil {
		if _, ok := config.IsError(err); ok {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Fprintln(os.Stderr, "verify:", err)
		return 1
	}

	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	var opt verifyOptions
	fs.Var(stringListFlag{&opt.sessions}, "session", "session JSONL file or directory (repeatable)")
	fs.StringVar(&opt.db, "db", "", "database path override (default: .semantix/verify.db)")
	fs.StringVar(&opt.project, "project", cfg.Project.Name, "project slug")
	fs.Float64Var(&opt.holdout, "holdout", cfg.Verify.Holdout, "fraction of latest sessions reserved as replay stream (0-1)")
	var scopeName string
	fs.StringVar(&scopeName, "scope", cfg.Store.Scope, "scope: project|user|session")
	greyTarget := fs.Float64("grey-target", 30.0, "grey-zone traffic ratio alarm threshold in percent (0 disables the alarm)")
	strict := fs.Bool("strict", false, "return exit code 3 when the grey-zone ratio exceeds --grey-target")
	zf := addZoneFlags(fs)
	judgeProtocol := fs.String("judge-protocol", "", "LLM judge protocol: openai|anthropic (empty = rules only)")
	judgeBaseURL := fs.String("judge-base-url", "", "LLM judge endpoint base URL (e.g. https://api.openai.com/v1)")
	judgeModel := fs.String("judge-model", "", "LLM judge model name")
	_ = fs.String("config", cfgPath, "config file path (default ./semantix.toml)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := zf.validate(); err != nil {
		fmt.Fprintf(stdout, "verify: %v\n", err)
		return 2
	}
	if len(opt.sessions) == 0 || fs.NArg() > 0 {
		fmt.Fprintln(stdout, "Usage: semantix verify --session <file|dir> [--session ...] [--holdout 0.3]")
		return 2
	}
	scope, err := parseScope(scopeName)
	if err != nil {
		fmt.Fprintf(stdout, "verify: %v\n", err)
		return 2
	}
	opt.scope = scope
	if opt.holdout < 0 || opt.holdout > 1 {
		fmt.Fprintln(stdout, "--holdout must be in [0,1]")
		return 2
	}
	if opt.db == "" {
		// verify must not pollute the extract/search store with v- prefixed
		// training slices; keep a dedicated replay database.
		opt.db = ".semantix/verify.db"
	}

	files, err := collectSessionFiles(opt.sessions)
	if err != nil {
		fmt.Fprintf(stdout, "verify: %v\n", err)
		return 1
	}
	if len(files) < 2 {
		fmt.Fprintf(stdout, "verify: need at least 2 session files (have %d)\n", len(files))
		return 1
	}

	split := len(files) - int(float64(len(files))*opt.holdout)
	if split < 1 {
		split = 1
	}
	if split >= len(files) {
		split = len(files) - 1
	}
	train := files[:split]
	replay := files[split:]

	store, err := deps.openStore(opt.db)
	if err != nil {
		// ensure the store directory exists (mirrors runExtract behavior)
		if mkErr := os.MkdirAll(filepath.Dir(opt.db), 0o700); mkErr == nil {
			store, err = deps.openStore(opt.db)
		}
		if err != nil {
			fmt.Fprintf(stdout, "verify: open store: %v\n", err)
			return 1
		}
	}
	idx := deps.newIndex()

	// Train: index every user turn of earlier sessions as a P-slice.
	trained := 0
	for _, path := range train {
		turns, err := parseTurns(path)
		if err != nil {
			fmt.Fprintf(stdout, "verify: %s: %v\n", path, err)
			return 1
		}
		for _, t := range turns {
			sl := &slice.Slice{
				ID:    turnSliceID(t.Query),
				Type:  slice.Prompt,
				Scope: opt.scope,
				Content: []byte(t.Query),
				Meta: slice.SliceMeta{ProjectSlug: opt.project, SourceSession: t.Session},
			}
			if err := store.Put(sl); err != nil {
				fmt.Fprintf(stdout, "verify: put: %v\n", err)
				return 1
			}
			if err := idx.Insert(sl); err != nil {
				fmt.Fprintf(stdout, "verify: index: %v\n", err)
				return 1
			}
			trained++
		}
	}

	// Replay: for every user turn of later sessions, top-1 hit.
	fmt.Fprintf(stdout, "# verify: %d sessions, %d trained turns, %d replay sessions\n", len(files), trained, len(replay))
	fmt.Fprintln(stdout, "# mark each row ✅ (top-1 is a 'previously done similar' turn) or ❌; relevance = marked / total ≥ 0.7")
	fmt.Fprintln(stdout, "# zone distribution (Issue #7): top-1 grey ratio should stay ≤ 30%")
	fmt.Fprintln(stdout, "session\tturn\tscore\tzone\ttop1_content\tquery")

	// LLM judge (Issue #8 stage ②): user picks the protocol and endpoint;
	// the API key comes from SEMANTIX_JUDGE_API_KEY, never from flags.
	var jg judge.Judge
	if *judgeProtocol != "" {
		apiKey := os.Getenv("SEMANTIX_JUDGE_API_KEY")
		lj, err := judge.NewLLMJudge(judge.LLMConfig{
			Protocol: *judgeProtocol, BaseURL: *judgeBaseURL, Model: *judgeModel, APIKey: apiKey,
		})
		if err != nil {
			fmt.Fprintf(stdout, "verify: judge: %v\n", err)
			return 2
		}
		jg = lj
	}
	var jstats judge.Stats
	gate := judge.RuleGate{Judge: jg, Stats: &jstats}

	replayed := 0
	zones := zf.zones()
	var zoneCount [3]int // [0]=miss [1]=grey [2]=hit
	for _, path := range replay {
		turns, err := parseTurns(path)
		if err != nil {
			fmt.Fprintf(stdout, "verify: %s: %v\n", path, err)
			return 1
		}
		for _, t := range turns {
			hits, err := idx.Search(t.Query, 2, opt.scope) // k=2: grey needs the runner-up
			if err != nil {
				fmt.Fprintf(stdout, "verify: search: %v\n", err)
				return 1
			}
			replayed++
			top1 := ""
			score := 0.0
			z := zone.Miss
			if len(hits) > 0 {
				top1 = string(hits[0].Slice.Content)
				score = hits[0].Score
				top2 := 0.0
				if len(hits) > 1 {
					top2 = hits[1].Score
				}
				z = classifyTop1(zones, score, top2)
				if z == zone.Grey && jg != nil {
					// Grey zone reaches the async LLM judge (off the critical path
					// in production; here inline). Verdict only affects stats.
					v, _, cerr := gate.Chain(context.Background(), judge.Candidate{
						Query: t.Query, SliceID: hits[0].Slice.ID, Content: top1,
						Scope: opt.scope, Type: hits[0].Slice.Type, Zone: z,
					})
					if cerr != nil {
						fmt.Fprintf(stdout, "verify: judge: %v\n", cerr)
					}
					_ = v
				}
			}
			zoneCount[int(z)]++
			fmt.Fprintf(stdout, "%s\t%d\t%.4f\t%s\t%s\t%s\n",
				tabSafe(t.Session), t.Turn, score, z.String(), tabSafe(top1), tabSafe(t.Query))
		}
	}
	greyRatio := 0.0
	if replayed > 0 {
		greyRatio = 100 * float64(zoneCount[int(zone.Grey)]) / float64(replayed)
	}
	fmt.Fprintf(stdout, "# done: %d replayed turns; zones hit=%d grey=%d miss=%d grey_ratio=%.1f%% (target %.1f%%)\n",
		replayed, zoneCount[int(zone.Hit)], zoneCount[int(zone.Grey)], zoneCount[int(zone.Miss)], greyRatio, *greyTarget)
	if *judgeProtocol != "" {
		fmt.Fprintf(stdout, "# judge: confirmed=%d rules_reject=%d fingerprint=%d judge_reject=%d judge_approved=%d waste=%d\n",
			jstats.Confirmed, jstats.RulesReject, jstats.Fingerprint, jstats.JudgeReject, jstats.JudgeApproved,
			jstats.JudgeReject+jstats.Fingerprint+jstats.RulesReject)
	}
	if *greyTarget > 0 && greyRatio > *greyTarget {
		// Issue #7 acceptance: the grey-zone share is an observability
		// metric with a hard alarm — the threshold can be tuned but a
		// runaway grey zone must be visible to the caller (CI can gate on
		// --strict, which surfaces as exit code 3).
		fmt.Fprintf(stdout, "# WARN: grey_ratio=%.1f%% exceeds target %.1f%% (Issue #7 alarm; retune --tau-* or accept the grey zone)\n",
			greyRatio, *greyTarget)
		if *strict {
			return 3
		}
	}
	return 0
}

// classifyTop1 maps the top-1/top-2 scores to a zone for the replay table.
// Unlike plain Classify (relative conf of the top-1 is trivially 1, which
// would make the grey zone unreachable under BM25's unbounded scores), the
// grey zone here is the "ambiguous winner" region (Krites §3.1): the top-1
// is absolutely weak, or the runner-up competes closely — reuse only when
// the winner is confident AND separated from the runner-up.
func classifyTop1(z zone.Zones, top1, top2 float64) zone.Zone {
	if math.IsNaN(top1) || math.IsNaN(top2) || math.IsInf(top1, 0) || math.IsInf(top2, 0) {
		return zone.Miss // failure-safe: NaN/Inf can never be a clear hit
	}
	switch {
	case top1 <= 0 || top1 < z.AbsLow:
		return zone.Miss
	case top1 >= z.AbsHigh && top1-top2 >= z.TauLow*top1:
		return zone.Hit
	default:
		return zone.Grey
	}
}

func turnSliceID(q string) string {
	h := sha256.Sum256([]byte(q))
	return "v-" + hex.EncodeToString(h[:8])
}

// tabSafe sanitizes TSV cells: control chars stripped and spreadsheet
// formula-prefix neutralized (= + - @ at cell start -> prefixed with ').
func tabSafe(s string) string {
	s = stripESC(s)
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	if strings.HasPrefix(s, "=") || strings.HasPrefix(s, "+") ||
		strings.HasPrefix(s, "-") || strings.HasPrefix(s, "@") {
		s = "'" + s
	}
	return s
}

// stringListFlag collects repeated --session flags.
type stringListFlag struct{ p *[]string }

func (f stringListFlag) String() string { return strings.Join(*f.p, ",") }
func (f stringListFlag) Set(v string) error {
	*f.p = append(*f.p, v)
	return nil
}
