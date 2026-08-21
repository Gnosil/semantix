package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"semantix/kernel/judge"
	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

// verifyRow is one replayed turn in the --json envelope.
type verifyRow struct {
	Session string  `json:"session"`
	Turn    int     `json:"turn"`
	Score   float64 `json:"score"`
	Zone    string  `json:"zone"`
	Top1    string  `json:"top1_content"`
	Query   string  `json:"query"`
}

// verifySummary is the --json envelope payload (M2-U22 §4.2).
type verifySummary struct {
	Sessions       int          `json:"sessions"`
	TrainedTurns   int          `json:"trained_turns"`
	ReplaySessions int          `json:"replay_sessions"`
	ReplayedTurns  int          `json:"replayed_turns"`
	ZonesHit       int          `json:"zones_hit"`
	ZonesGrey      int          `json:"zones_grey"`
	ZonesMiss      int          `json:"zones_miss"`
	GreyRatioPct   float64      `json:"grey_ratio_pct"`
	GreyTargetPct  float64      `json:"grey_target_pct"`
	RelevancePct   float64      `json:"relevance_pct"`
	Icon           string       `json:"icon"`
	WarnGrey       bool         `json:"warn_grey"`
	Judge          *judge.Stats `json:"judge,omitempty"`
	Rows           []verifyRow  `json:"rows"`
}

// verifyVerdict returns the one-line gate verdict icon (Issue #153 / U29):
// "warn" when the grey-zone alarm fires, "fail" when relevance is under the
// M0-Gate 70% bar, otherwise "pass".
func verifyVerdict(relevance, greyRatio, greyTarget float64) string {
	if greyTarget > 0 && greyRatio > greyTarget {
		return "warn"
	}
	if relevance < 0.7 {
		return "fail"
	}
	return "pass"
}

// verifyZoneIcon prefixes a zone with a human-readable glyph for the replay
// table. Named verifyZoneIcon (not zoneIcon) to stay clear of hitviz.go's
// zoneIcon(string) — both live in package main (U29 × U30).
func verifyZoneIcon(z zone.Zone) string {
	switch z {
	case zone.Hit:
		return "✅hit"
	case zone.Grey:
		return "🟡grey"
	default:
		return "❌miss"
	}
}

// verifyBar renders an ASCII bar (█ filled, ░ empty) for a ratio in [0,1].
// Kept local to verify so U29 stays file-disjoint from U28 (usage.go owns
// its own barChart helper).
func verifyBar(ratio float64, width int) string {
	if width <= 0 {
		return ""
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio*float64(width) + 0.5)
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// verifyExit maps the grey-zone alarm to the exit code contract (3 = gate).
func verifyExit(greyTarget, greyRatio float64, strict bool) int {
	if strict && greyTarget > 0 && greyRatio > greyTarget {
		return 3
	}
	return 0
}

// verify implements offline replay validation for the M0-2 hypothesis:
// "when using a coding agent, repeated intermediate work appears across
// sessions and can be reused". It splits sessions by time (holdout fraction
// reserved as the replay stream), indexes the earlier turns, then replays
// every user turn of the later sessions and prints a TSV evaluation table
// (query → top-1 hit + score) for human relevance marking.
//
// Output columns (tab-separated, first line is a header):
//   session turn score zone top1_content query
// The zone cell is iconized (✅hit / 🟡grey / ❌miss) and the tail appends a
// zone-distribution bar plus a gate verdict line. The verdict's relevance is
// the AUTO-classified hit share (hit / replayed) — NOT the human-marked
// relevance of #58, which requires offline labeling and is not computed here.
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
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	var opt verifyOptions
	fs.Var(stringListFlag{&opt.sessions}, "session", "session JSONL file or directory (repeatable)")
	fs.StringVar(&opt.db, "db", cfgString(deps.resolved, "store.db", ""), "database path override (default: .semantix/project.db)")
	fs.StringVar(&opt.project, "project", "", "project slug")
	fs.Float64Var(&opt.holdout, "holdout", cfgFloat(deps.resolved, "verify.holdout", 0.3), "fraction of latest sessions reserved as replay stream (0-1)")
	var scopeName string
	fs.StringVar(&scopeName, "scope", cfgString(deps.resolved, "store.scope", "project"), "scope: project|user|session")
	greyTarget := fs.Float64("grey-target", 30.0, "grey-zone traffic ratio alarm threshold in percent (0 disables the alarm)")
	strict := fs.Bool("strict", false, "return exit code 3 when the grey-zone ratio exceeds --grey-target")
	jsonOut := fs.Bool("json", false, "output as JSON envelope (summary + rows)")
	zf := addZoneFlags(fs)
	judgeProtocol := fs.String("judge-protocol", "", "LLM judge protocol: openai|anthropic (empty = rules only)")
	judgeBaseURL := fs.String("judge-base-url", "", "LLM judge endpoint base URL (e.g. https://api.openai.com/v1)")
	judgeModel := fs.String("judge-model", "", "LLM judge model name")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0 // --help is a successful request
		}
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
				ID:      turnSliceID(t.Query),
				Type:    slice.Prompt,
				Scope:   opt.scope,
				Content: []byte(t.Query),
				Meta:    slice.SliceMeta{ProjectSlug: opt.project, SourceSession: t.Session},
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
	if !*jsonOut {
		fmt.Fprintf(stdout, "# verify: %d sessions, %d trained turns, %d replay sessions\n", len(files), trained, len(replay))
		fmt.Fprintln(stdout, "# mark each row ✅ (top-1 is a 'previously done similar' turn) or ❌; relevance = marked / total ≥ 0.7")
		fmt.Fprintln(stdout, "# zone distribution (Issue #7): top-1 grey ratio should stay ≤ 30%")
		fmt.Fprintln(stdout, "session\tturn\tscore\tzone\ttop1_content\tquery")
	}
	var rows []verifyRow

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
			if *jsonOut {
				rows = append(rows, verifyRow{
					Session: t.Session, Turn: t.Turn, Score: score, Zone: z.String(), Top1: top1, Query: t.Query,
				})
			} else {
				fmt.Fprintf(stdout, "%s\t%d\t%.4f\t%s\t%s\t%s\n",
					tabSafe(t.Session), t.Turn, score, verifyZoneIcon(z), tabSafe(top1), tabSafe(t.Query))
			}
		}
	}
	greyRatio := 0.0
	relevance := 0.0
	if replayed > 0 {
		greyRatio = 100 * float64(zoneCount[int(zone.Grey)]) / float64(replayed)
		// relevance is the auto-classified hit share (hit / replayed), not the
		// human-marked relevance of #58 — see the runVerify doc comment.
		relevance = float64(zoneCount[int(zone.Hit)]) / float64(replayed)
	}
	icon := verifyVerdict(relevance, greyRatio, *greyTarget)
	if *jsonOut {
		summary := verifySummary{
			Sessions: len(files), TrainedTurns: trained, ReplaySessions: len(replay), ReplayedTurns: replayed,
			ZonesHit: zoneCount[int(zone.Hit)], ZonesGrey: zoneCount[int(zone.Grey)], ZonesMiss: zoneCount[int(zone.Miss)],
			GreyRatioPct: greyRatio, GreyTargetPct: *greyTarget,
			RelevancePct: relevance * 100, Icon: icon,
			WarnGrey: *greyTarget > 0 && greyRatio > *greyTarget,
			Rows:     rows,
		}
		if *judgeProtocol != "" {
			summary.Judge = &jstats
		}
		if err := writeJSON(stdout, okEnvelope("verify", summary)); err != nil {
			fmt.Fprintf(stdout, "verify: %v\n", err)
			return 1
		}
		return verifyExit(*greyTarget, greyRatio, *strict)
	}
	fmt.Fprintf(stdout, "# done: %d replayed turns; zones hit=%d grey=%d miss=%d grey_ratio=%.1f%% (target %.1f%%)\n",
		replayed, zoneCount[int(zone.Hit)], zoneCount[int(zone.Grey)], zoneCount[int(zone.Miss)], greyRatio, *greyTarget)
	// zone distribution bar (Issue #153 / U29).
	if replayed > 0 {
		fmt.Fprintf(stdout, "# zones: hit %s grey %s miss %s\n",
			verifyBar(float64(zoneCount[int(zone.Hit)])/float64(replayed), 8),
			verifyBar(float64(zoneCount[int(zone.Grey)])/float64(replayed), 8),
			verifyBar(float64(zoneCount[int(zone.Miss)])/float64(replayed), 8))
	}
	// gate verdict line (Issue #153 / U29).
	switch icon {
	case "pass":
		fmt.Fprintf(stdout, "# ✅ PASS relevance=%.1f%% (≥70%%)\n", relevance*100)
	case "fail":
		fmt.Fprintf(stdout, "# ❌ FAIL relevance=%.1f%% (<70%%)\n", relevance*100)
	default:
		fmt.Fprintf(stdout, "# ⚠ WARN grey_ratio=%.1f%% exceeds target %.1f%%\n", greyRatio, *greyTarget)
	}
	if *judgeProtocol != "" {
		fmt.Fprintf(stdout, "# judge: confirmed=%d rules_reject=%d fingerprint=%d judge_reject=%d judge_approved=%d judge_error=%d waste=%d\n",
			jstats.Confirmed, jstats.RulesReject, jstats.Fingerprint, jstats.JudgeReject, jstats.JudgeApproved, jstats.JudgeError,
			jstats.JudgeReject+jstats.Fingerprint+jstats.RulesReject+jstats.JudgeError)
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

func (f stringListFlag) String() string {
	if f.p == nil {
		// Go's flag package calls String on a zero value to decide
		// whether to print "(default ...)"; a nil target must not panic.
		return ""
	}
	return strings.Join(*f.p, ",")
}
func (f stringListFlag) Set(v string) error {
	*f.p = append(*f.p, v)
	return nil
}
