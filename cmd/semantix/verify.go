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
	"strconv"
	"strings"

	"semantix/kernel/adapt"
	"semantix/kernel/config"
	"semantix/kernel/judge"
	"semantix/kernel/slice"
	"semantix/kernel/usage"
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
	Sessions       int                `json:"sessions"`
	TrainedTurns   int                `json:"trained_turns"`
	ReplaySessions int                `json:"replay_sessions"`
	ReplayedTurns  int                `json:"replayed_turns"`
	ZonesHit       int                `json:"zones_hit"`
	ZonesGrey      int                `json:"zones_grey"`
	ZonesMiss      int                `json:"zones_miss"`
	GreyRatioPct   float64            `json:"grey_ratio_pct"`
	GreyTargetPct  float64            `json:"grey_target_pct"`
	RelevancePct   float64            `json:"relevance_pct"`
	Icon           string             `json:"icon"`
	WarnGrey       bool               `json:"warn_grey"`
	Judge          *verifyJudgeJSON   `json:"judge,omitempty"`
	Calibration    *verifyCalibration `json:"calibration,omitempty"`
	Rows           []verifyRow        `json:"rows"`
}

// verifyJudgeJSON is the CLI-side wire shape for judge.Stats. The kernel type
// carries no json tags, so marshaling it directly published PascalCase keys
// ("NeedJudge", and — once Issue #245 landed — "JudgeError") into an envelope
// that is snake_case everywhere else. Keeping the mapping here follows the
// house rule already stated in search.go: the CLI owns its wire format and the
// kernel type stays policy-free.
type verifyJudgeJSON struct {
	Confirmed     int `json:"confirmed"`
	RulesReject   int `json:"rules_reject"`
	Fingerprint   int `json:"fingerprint"`
	JudgeReject   int `json:"judge_reject"`
	JudgeError    int `json:"judge_error"`
	JudgeApproved int `json:"judge_approved"`
	NeedJudge     int `json:"need_judge"`
}

func newVerifyJudgeJSON(s judge.Stats) *verifyJudgeJSON {
	return &verifyJudgeJSON{
		Confirmed:     s.Confirmed,
		RulesReject:   s.RulesReject,
		Fingerprint:   s.Fingerprint,
		JudgeReject:   s.JudgeReject,
		JudgeError:    s.JudgeError,
		JudgeApproved: s.JudgeApproved,
		NeedJudge:     s.NeedJudge,
	}
}

// verifyCalibration is the Issue #262 calibration report: the replay stream
// bucketed by relative confidence r = top2/top1 — the axis the three-region
// gate actually classifies on (classifyTop1) — showing per-bin zone shares
// under the current thresholds plus the three-region drift against
// zone.Default(). A mistuned tau/abs threshold shows up as a shift in the
// per-bin hit/grey/miss distribution and in the drift block (Issue #262
// acceptance: "人为调坏 TauHigh 后校准报告能显示失配"). With --labels, the
// oracle marks per-turn relevance and the report adds per-bin false
// positives (hits the oracle rejects) and precision — the P-CHR simplified
// distribution (GPT Semantic Cache, arXiv:2411.05276).
type verifyCalibration struct {
	Bins    []verifyBin              `json:"bins"`
	Current [3]int                   `json:"current"`           // zone counts under the current thresholds
	Default [3]int                   `json:"default"`           // zone counts under zone.Default()
	ByType  map[string]verifyTypeCal `json:"by_type,omitempty"` // Issue #259 阶段 2: per-type zone distribution
	Labeled bool                     `json:"labeled"`           // --labels was provided
	Marks   map[string]bool          `json:"-"`                 // oracle relevance marks (session\x00turn -> relevant)
}

// verifyTypeCal is the per-slice-type zone distribution (Issue #259
// 阶段 2): how each type's top-1 candidates land under the current
// (possibly per-type) thresholds vs the default thresholds — the input
// for deciding whether a type deserves its own tau.
type verifyTypeCal struct {
	N          int `json:"n"`
	Hit        int `json:"hit"`
	Grey       int `json:"grey"`
	Miss       int `json:"miss"`
	DefaultHit int `json:"default_hit"` // hit count under zone.Default(), for drift comparison
}

// verifyBin is one relative-confidence bucket [Lo,Hi) of the replay stream.
type verifyBin struct {
	Bin       string   `json:"bin"` // e.g. "[0.00,0.10)"
	N         int      `json:"n"`
	Hit       int      `json:"hit"`
	Grey      int      `json:"grey"`
	Miss      int      `json:"miss"`
	HitPct    float64  `json:"hit_pct,omitempty"`
	Top1Sum   float64  `json:"-"`
	Top1Avg   float64  `json:"top1_avg,omitempty"`
	FP        int      `json:"fp,omitempty"`        // labeled irrelevant but judged hit
	Precision *float64 `json:"precision,omitempty"` // labeled: (hit-FP)/hit, nil when unlabeled
}

// verifyCalibBuckets is the fixed bucket plan: ten 0.1-wide relative-
// confidence bins over [0,1]. r == 1 (exact tie, top2 == top1) lands in the
// last bin.
const verifyCalibBuckets = 10

// calibBinIndex maps a relative confidence r ∈ [0,1] to a bucket index.
func calibBinIndex(r float64) int {
	if math.IsNaN(r) || r < 0 {
		return 0
	}
	if r >= 1 {
		return verifyCalibBuckets - 1
	}
	return int(r * verifyCalibBuckets)
}

// calibBinLabel renders the bucket range as "[lo,hi)" (last bucket "[..,1]").
func calibBinLabel(i int) string {
	lo := float64(i) / verifyCalibBuckets
	hi := float64(i+1) / verifyCalibBuckets
	if i == verifyCalibBuckets-1 {
		return fmt.Sprintf("[%.2f,1.00]", lo)
	}
	return fmt.Sprintf("[%.2f,%.2f)", lo, hi)
}

// readVerifyLabels parses the --labels TSV (session<TAB>turn<TAB>1|0; '#'
// comments and blank lines skipped). Turn relevance marks the oracle side of
// the calibration report: a hit the oracle marks 0 is a false positive.
func readVerifyLabels(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	marks := make(map[string]bool)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			return nil, fmt.Errorf("labels: malformed line %q (want session<TAB>turn<TAB>1|0)", line)
		}
		rel := false
		switch parts[2] {
		case "1":
			rel = true
		case "0":
		default:
			return nil, fmt.Errorf("labels: malformed relevance %q in line %q (want 1|0)", parts[2], line)
		}
		turn, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("labels: malformed turn %q in line %q", parts[1], line)
		}
		marks[fmt.Sprintf("%s\x00%d", parts[0], turn)] = rel
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return marks, nil
}

// printVerifyCalibration renders the calibration report (text mode) after
// the replay table: per-bin distribution plus the three-region drift block.
func printVerifyCalibration(stdout io.Writer, cal *verifyCalibration) {
	if cal == nil {
		return
	}
	fmt.Fprintln(stdout, "# calibration: 分桶命中分布——相对置信度 r = top2/top1（classifyTop1 实际判定轴）")
	hdr := "#   bin        n   hit grey miss  hit%   top1_avg"
	if cal.Labeled {
		hdr += "   fp  precision"
	}
	fmt.Fprintln(stdout, hdr)
	for i := range cal.Bins {
		b := &cal.Bins[i]
		if b.N == 0 {
			continue
		}
		hitPct := 100 * float64(b.Hit) / float64(b.N)
		line := fmt.Sprintf("#   %-10s %4d %4d %4d %4d  %5.1f%%  %7.3f",
			calibBinLabel(i), b.N, b.Hit, b.Grey, b.Miss, hitPct, b.Top1Avg)
		if cal.Labeled {
			if b.Precision != nil {
				line += fmt.Sprintf("  %3d  %5.1f%%", b.FP, *b.Precision*100)
			} else {
				line += "    -        -"
			}
		}
		fmt.Fprintln(stdout, line)
	}
	// Three-region drift: current thresholds vs zone.Default().
	def := zone.Default()
	fmt.Fprintf(stdout, "# drift vs default thresholds (tau_high=%.2f tau_low=%.2f abs_high=%.2f abs_low=%.2f)\n",
		def.TauHigh, def.TauLow, def.AbsHigh, def.AbsLow)
	fmt.Fprintln(stdout, "#   zone    current  default   delta")
	total := cal.Current[0] + cal.Current[1] + cal.Current[2]
	if total == 0 {
		total = 1 // avoid div-by-zero on an empty replay stream
	}
	defTotal := cal.Default[0] + cal.Default[1] + cal.Default[2]
	if defTotal == 0 {
		defTotal = 1
	}
	names := []string{"miss", "grey", "hit"}
	for i := range names {
		cur := 100 * float64(cal.Current[i]) / float64(total)
		dft := 100 * float64(cal.Default[i]) / float64(defTotal)
		fmt.Fprintf(stdout, "#   %-6s %7.1f%% %7.1f%%  %+5.1fpt\n", names[i], cur, dft, cur-dft)
	}
	// Per-type distribution (Issue #259 阶段 2): how each slice type lands
	// under the current (possibly per-type) thresholds vs the default ones
	// — the calibration input for per-type tau decisions.
	if len(cal.ByType) > 0 {
		fmt.Fprintln(stdout, "# by_type: 类型分型命中分布 (current 阈值 vs default 阈值; Issue #259 阶段 2)")
		fmt.Fprintln(stdout, "#   type          n   hit grey miss  hit%  default_hit")
		typeNames := make([]string, 0, len(cal.ByType))
		for name := range cal.ByType {
			typeNames = append(typeNames, name)
		}
		sort.Strings(typeNames)
		for _, name := range typeNames {
			tc := cal.ByType[name]
			hitPct := 0.0
			if tc.N > 0 {
				hitPct = 100 * float64(tc.Hit) / float64(tc.N)
			}
			fmt.Fprintf(stdout, "#   %-12s %4d %4d %4d %4d  %5.1f%%  %6d\n",
				name, tc.N, tc.Hit, tc.Grey, tc.Miss, hitPct, tc.DefaultHit)
		}
	}
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
	fs.StringVar(&opt.db, "db", cfgString(deps.resolved, "store.db", ""), "database path override (default: configured store.db or .semantix/verify.db)")
	fs.StringVar(&opt.project, "project", "", "project slug")
	fs.Float64Var(&opt.holdout, "holdout", cfgFloat(deps.resolved, "verify.holdout", 0.3), "fraction of latest sessions reserved as replay stream (0-1)")
	var scopeName string
	fs.StringVar(&scopeName, "scope", cfgString(deps.resolved, "store.scope", "project"), "scope: project|user|session")
	greyTarget := fs.Float64("grey-target", 30.0, "grey-zone traffic ratio alarm threshold in percent (0 disables the alarm)")
	strict := fs.Bool("strict", false, "return exit code 3 when the grey-zone ratio exceeds --grey-target")
	usageDB := fs.String("usage-db", filepath.Join(".semantix", "usage.jsonl"), "usage log for the L1 hit-rate regression footer (missing = skipped)")
	jsonOut := fs.Bool("json", false, "output as JSON envelope (summary + rows)")
	calibrate := fs.Bool("calibrate", false, "append the Issue #262 calibration report: per-bin hit/miss distribution + three-region drift vs default thresholds")
	labelsPath := fs.String("labels", "", "optional oracle relevance marks: session<TAB>turn<TAB>1|0 (implies --calibrate; adds per-bin fp/precision)")
	adaptDB := fs.String("adapt-db", "", "per-entry adaptive state file (Issue #259 阶段 3); empty = <db 同目录>/l3-adapt.json, 缺失则不报告")
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
	if err := zf.applyConfigZones(fs, deps.resolved); err != nil {
		fmt.Fprintf(stdout, "verify: %v\n", err)
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
	opt.db = config.ExpandHome(opt.db)

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
	// Release the store journal before returning: runVerify is
	// called in-process (tests, embedding), so the journal file
	// handle must not outlive the replay (Windows TempDir cleanup).
	defer func() {
		if c, ok := store.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}()
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
				Meta: slice.SliceMeta{ProjectSlug: opt.project, SourceSession: t.Session,
					Origin: slice.OriginSessionAuto}, // Issue #279
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

	// Issue #262 calibration collection: per-bin relative-confidence
	// distribution under the current vs default thresholds, plus optional
	// oracle labels for per-bin fp/precision (P-CHR simplified).
	calMode := *calibrate || *labelsPath != ""
	var cal *verifyCalibration
	if calMode {
		cal = &verifyCalibration{Bins: make([]verifyBin, verifyCalibBuckets), ByType: map[string]verifyTypeCal{}}
		for i := range cal.Bins {
			cal.Bins[i].Bin = calibBinLabel(i)
		}
		if *labelsPath != "" {
			marks, err := readVerifyLabels(*labelsPath)
			if err != nil {
				fmt.Fprintf(stdout, "verify: labels: %v\n", err)
				return 2
			}
			cal.Marks = marks
			cal.Labeled = true
		}
	}

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
			top2 := 0.0
			z := zone.Miss
			if len(hits) > 0 {
				top1 = string(hits[0].Slice.Content)
				score = hits[0].Score
				if len(hits) > 1 {
					top2 = hits[1].Score
				}
				// Per-type thresholds (Issue #259 阶段 2): the top-1
				// candidate is classified under its own type's override
				// (falling back to the global baseline).
				tz := zones
				if hits[0].Slice != nil {
					tz = zones.ForType(hits[0].Slice.Type.String())
				}
				z = classifyTop1(tz, score, top2)
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
			if cal != nil {
				// Calibration: relative confidence r = top2/top1 is the axis
				// classifyTop1 decides on; bucket it and classify under both
				// the current and the default thresholds (Issue #262).
				r := 0.0
				if score > 0 {
					r = top2 / score
				}
				bi := calibBinIndex(r)
				b := &cal.Bins[bi]
				b.N++
				switch z {
				case zone.Hit:
					b.Hit++
				case zone.Grey:
					b.Grey++
				default:
					b.Miss++
				}
				b.Top1Sum += score
				cal.Current[int(z)]++
				cal.Default[int(classifyTop1(zone.Default(), score, top2))]++
				// Per-type distribution (Issue #259 阶段 2): where each
				// slice type's candidates land under the current (possibly
				// per-type) thresholds vs the defaults — the calibration
				// input for deciding whether a type needs its own tau.
				if len(hits) > 0 && hits[0].Slice != nil {
					name := hits[0].Slice.Type.String()
					tc := cal.ByType[name]
					tc.N++
					switch z {
					case zone.Hit:
						tc.Hit++
					case zone.Grey:
						tc.Grey++
					default:
						tc.Miss++
					}
					if classifyTop1(zone.Default(), score, top2) == zone.Hit {
						tc.DefaultHit++
					}
					cal.ByType[name] = tc
				}
				if cal.Marks != nil {
					if rel, ok := cal.Marks[fmt.Sprintf("%s\x00%d", t.Session, t.Turn)]; ok && z == zone.Hit && !rel {
						b.FP++ // oracle says irrelevant but the gate would reuse
					}
				}
			}
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
	// Finalize the calibration bins: averages and labeled precision.
	if cal != nil {
		for i := range cal.Bins {
			b := &cal.Bins[i]
			if b.N == 0 {
				continue
			}
			b.HitPct = 100 * float64(b.Hit) / float64(b.N)
			b.Top1Avg = b.Top1Sum / float64(b.N)
			if cal.Labeled && b.Hit > 0 {
				p := float64(b.Hit-b.FP) / float64(b.Hit)
				b.Precision = &p
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
		if cal != nil {
			summary.Calibration = cal
		}
		if *judgeProtocol != "" {
			summary.Judge = newVerifyJudgeJSON(jstats)
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
		// waste keeps its pre-#245 meaning. Judge errors used to arrive inside
		// RulesReject, so JudgeError must be added back into the total or this
		// published number would silently shrink as a side effect of the split.
		fmt.Fprintf(stdout, "# judge: confirmed=%d rules_reject=%d fingerprint=%d judge_reject=%d judge_error=%d judge_approved=%d waste=%d\n",
			jstats.Confirmed, jstats.RulesReject, jstats.Fingerprint, jstats.JudgeReject, jstats.JudgeError, jstats.JudgeApproved,
			jstats.JudgeReject+jstats.Fingerprint+jstats.RulesReject+jstats.JudgeError)
	}
	printVerifyCalibration(stdout, cal)
	printAdaptSummary(stdout, *adaptDB, opt.db)
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
	printVerifyL1Footer(stdout, *usageDB)
	return 0
}

// printAdaptSummary appends the per-entry adaptive state summary (Issue
// #259 阶段 3) to the replay report: entry count, learned-tau distribution
// and adjustment totals, so an operator can see whether vCache-style
// per-entry learning is active and how far entries drifted from the
// global prior. adaptPath empty → derive from the store db directory; a
// missing state file prints nothing (adaptation simply has no entries
// yet). The engine is opened read-only: no observations are folded.
func printAdaptSummary(stdout io.Writer, adaptPath, db string) {
	if adaptPath == "" {
		if db == "" {
			return
		}
		adaptPath = filepath.Join(filepath.Dir(db), "l3-adapt.json")
	}
	e := adapt.New(adapt.Config{}, adaptPath)
	snap := e.Snapshot()
	if len(snap) == 0 {
		return
	}
	var minTau, maxTau float64
	var relaxed int
	taus := make([]float64, 0, len(snap))
	for i := range snap {
		ent := &snap[i]
		if ent.TauLow <= 0 {
			continue // cold-start entries keep the global prior
		}
		if ent.Relaxed {
			relaxed++
		}
		taus = append(taus, ent.TauLow)
		if minTau == 0 || ent.TauLow < minTau {
			minTau = ent.TauLow
		}
		if ent.TauLow > maxTau {
			maxTau = ent.TauLow
		}
	}
	if len(taus) == 0 {
		return
	}
	sort.Float64s(taus)
	median := taus[len(taus)/2]
	fmt.Fprintf(stdout, "# adapt: per-entry 自适应状态 (Issue #259 阶段 3) — %s\n", adaptPath)
	fmt.Fprintf(stdout, "#   entries=%d learned=%d relaxed=%d adjustments=%d epoch=%d tau_low min=%.2f median=%.2f max=%.2f\n",
		len(snap), len(taus), relaxed, e.Adjustments(), e.Epoch(), minTau, median, maxTau)
}

// printVerifyL1Footer appends the L1 hit-rate regression rows to the replay
// report (GLM-P0-4, Issue #292): per-provider exact-metered rates from the
// usage log, flagged against the 85% prefix-pollution floor. Purely
// informational — replay verdicts and exit codes are untouched. A missing
// or unreadable log prints nothing (verify stays usable without traffic).
func printVerifyL1Footer(stdout io.Writer, usagePath string) {
	if usagePath == "" {
		return
	}
	if _, err := os.Stat(usagePath); err != nil {
		return
	}
	s, err := usage.Summarize(usagePath, 0, 0)
	if err != nil {
		return
	}
	names := make([]string, 0, len(s.ByProvider))
	for name, p := range s.ByProvider {
		if p.ExactEvents > 0 {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return
	}
	sort.Strings(names)
	for _, name := range names {
		p := s.ByProvider[name]
		label := name
		if label == "" {
			label = "(unattributed)"
		}
		flag := ""
		if p.L1HitRate() < l1WarnThreshold {
			flag = fmt.Sprintf("  ⚠ below %.0f%% — prefix-pollution checklist: docs/reports/glm-p0-1-prefix-audit.md", l1WarnThreshold*100)
		}
		fmt.Fprintf(stdout, "# l1: %s hit=%.1f%% (exact %d/%d)%s\n", label, p.L1HitRate()*100, p.ExactEvents, p.Events, flag)
	}
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
