package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"semantix/gateway"
	"semantix/kernel/fuse"
	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

// runProbe is the W0 cross-instance hit-rate probe (efficiency research
// plan, docs/specs/swebench-efficiency-research-plan.md §3 W0): given an
// ORDERED list of session transcripts, it replays them as if instance i
// starts with the slice library accumulated from instances 0..i-1, then
// measures for every user turn of instance i which zones its retrievals
// land in and whose slices serve them.
//
// This is the number that decides the whole SWE-bench plan: if slices from
// earlier sessions of the same project family never reach zone.Hit for
// later sessions, cross-instance reuse has no value and the plan's
// expected payoff collapses. All of it is offline — no model calls.
//
// Output: per-turn TSV (stdout) plus a summary block (--json supported).
func runProbe(args []string, stdout, stderr io.Writer, deps dependencies) error {
	flags := flag.NewFlagSet("probe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	sessionsFlag := flags.String("sessions", "", "comma-separated ordered session JSONL paths")
	dirFlag := flags.String("dir", "", "directory of session JSONLs (ordered by name)")
	topK := flags.Int("topk", 5, "top-k retrieval per turn")
	minQueryLen := flags.Int("min-query-len", 8, "skip user turns shorter than this many bytes")
	// QueryMode selects what each turn is probed with. "user" (default)
	// probes with the user message text — content-level reuse. "tools"
	// probes with the turn's tool-name sequence — the structure-level mode
	// for sanitized loads like TraceLab (#263) where content is stripped
	// but tool trajectories are intact: it measures whether T-slices from
	// earlier sessions retrieve for later ones.
	queryMode := flags.String("query-mode", "user", "what to probe each turn with: user | tools")
	retrieverKind := flags.String("retriever", "bm25", "retrieval index: bm25 | hybrid (bm25+vector fused; vector route per --embed-*)")
	embedBackend := flags.String("embed-backend", "hash", "vector-route embedder backend: hash | model (OpenAI-compatible API; SEMANTIX_EMBED_* env)")
	embedBaseURL := flags.String("embed-base-url", os.Getenv("SEMANTIX_EMBED_BASE_URL"), "embeddings API base URL (model backend)")
	embedModel := flags.String("embed-model", os.Getenv("SEMANTIX_EMBED_MODEL"), "embeddings model name (model backend)")
	tStepSplit := flags.Bool("t-step-split", false, "extract T-slices at subtask granularity (see extract --t-step-split)")
	jsonOutput := flags.Bool("json", false, "write JSON envelope output")
	if err := flags.Parse(args); err != nil {
		return usageWrap(err)
	}
	if flags.NArg() != 0 {
		return usageErrf("probe: unexpected arguments: %v", flags.Args())
	}
	paths, err := probeSessionPaths(*sessionsFlag, *dirFlag)
	if err != nil {
		return err
	}
	if len(paths) < 2 {
		return usageErrf("probe: need at least 2 sessions for a cross-session measurement, got %d", len(paths))
	}

	// Parse all transcripts up front: queries per session.
	type sessionQueries struct {
		name    string
		queries []string
	}
	sessions := make([]sessionQueries, len(paths))
	for i, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("probe: read %s: %w", p, err)
		}
		var queries []string
		switch *queryMode {
		case "tools":
			queries = probeToolQueries(data)
		default:
			queries = probeUserQueries(data, *minQueryLen)
		}
		sessions[i] = sessionQueries{name: filepath.Base(p), queries: queries}
	}

	extractor := slice.NewExtractorWithOptions(slice.ExtractOptions{TStepSplit: *tStepSplit})
	store, err := slice.NewFileStore(filepath.Join(probeWorkDir(), "probe.db"))
	if err != nil {
		return fmt.Errorf("probe: open store: %w", err)
	}
	defer closeStore(store)

	z := zone.Default()
	type turnRecord struct {
		session   int
		query     string
		zone      string
		score     float64 // top-1 retrieval score (0 when no hits)
		topType   string
		topSource string
		cross     bool // top-1 slice came from a PRIOR session
	}
	var turns []turnRecord
	perSession := make([]zoneCounters, len(paths))
	crossHits, sameHits := 0, 0

	for i := 0; i < len(paths); i++ {
		// Library = sessions 0..i-1 (session 0 starts from nothing).
		for j := 0; j < i; j++ {
			data, err := os.ReadFile(paths[j])
			if err != nil {
				return fmt.Errorf("probe: read %s: %w", paths[j], err)
			}
			items, err := extractor.Extract(data, slice.SliceMeta{SourceSession: sessions[j].name})
			if err != nil {
				return fmt.Errorf("probe: extract %s: %w", paths[j], err)
			}
			for _, s := range items {
				if err := store.Put(s); err != nil {
					return fmt.Errorf("probe: store: %w", err)
				}
			}
		}
		idx := gateway.NewFusedIndex(*retrieverKind, 0, fuse.Config{}, gateway.EmbedSettings{
			Backend: *embedBackend,
			BaseURL: *embedBaseURL,
			Model:   *embedModel,
		})
		for _, scope := range []slice.Scope{slice.Session, slice.Project, slice.User} {
			items, err := store.List(scope)
			if err != nil {
				return err
			}
			for _, s := range items {
				if err := idx.Insert(s); err != nil {
					return err
				}
			}
		}
		for _, q := range sessions[i].queries {
			hits, err := idx.Search(q, *topK, slice.Project)
			if err != nil {
				return fmt.Errorf("probe: search: %w", err)
			}
			top1 := 0.0
			if len(hits) > 0 {
				top1 = hits[0].Score
			}
			rec := turnRecord{session: i, query: q, zone: "miss"}
			if len(hits) > 0 {
				rec.score = hits[0].Score
				switch z.ClassifyHit(hits[0], top1) {
				case zone.Hit:
					rec.zone = "hit"
				case zone.Grey:
					rec.zone = "grey"
				}
				if hits[0].Slice != nil {
					rec.topType = hits[0].Slice.Type.String()
					rec.topSource = hits[0].Slice.Meta.SourceSession
					rec.cross = rec.topSource != "" && rec.topSource != sessions[i].name
				}
			}
			switch rec.zone {
			case "hit":
				perSession[i].hit++
				if rec.cross {
					crossHits++
				} else {
					sameHits++
				}
			case "grey":
				perSession[i].grey++
			default:
				perSession[i].miss++
			}
			turns = append(turns, rec)
		}
	}

	// Summary aggregation.
	type typeCount struct {
		typ    string
		hit    int
		grey   int
		miss   int
		cross  int
	}
	types := map[string]*typeCount{}
	for _, t := range turns {
		key := t.topType
		if key == "" {
			key = "none"
		}
		tc, ok := types[key]
		if !ok {
			tc = &typeCount{typ: key}
			types[key] = tc
		}
		switch t.zone {
		case "hit":
			tc.hit++
		case "grey":
			tc.grey++
		default:
			tc.miss++
		}
		if t.cross {
			tc.cross++
		}
	}
	var typeKeys []string
	for k := range types {
		typeKeys = append(typeKeys, k)
	}
	sort.Strings(typeKeys)

	total := len(turns)
	hits := crossHits + sameHits
	if *jsonOutput {
		var turnRows []map[string]interface{}
		for _, t := range turns {
			turnRows = append(turnRows, map[string]interface{}{
				"session":     t.session,
				"query":       t.query,
				"zone":        t.zone,
				"score":       t.score,
				"top_type":    t.topType,
				"top_source":  t.topSource,
				"cross":       t.cross,
			})
		}
		var typeRows []map[string]interface{}
		for _, k := range typeKeys {
			tc := types[k]
			typeRows = append(typeRows, map[string]interface{}{
				"type": k, "hit": tc.hit, "grey": tc.grey, "miss": tc.miss, "cross": tc.cross,
			})
		}
		data := map[string]interface{}{
			"sessions":    len(paths),
			"turns":       total,
			"hit":         hits,
			"grey":        total - hits - probeMisses(perSession),
			"miss":        probeMisses(perSession),
			"hit_rate":    probeRatio(hits, total),
			"grey_ratio":  probeRatio(total-hits-probeMisses(perSession), total),
			"cross_hits":  crossHits,
			"same_hits":   sameHits,
			"per_type":    typeRows,
			"per_session": probeSessionRows(perSession),
			"turns_tsv":   turnRows,
		}
		return writeEnvelope(stdout, "probe", data)
	}

	fmt.Fprintf(stdout, "probe: %d sessions, %d turns\n", len(paths), total)
	fmt.Fprintf(stdout, "  hit  %4d (%.1f%%)  [cross-session %d / same-session %d]\n", hits, probeRatio(hits, total)*100, crossHits, sameHits)
	fmt.Fprintf(stdout, "  grey %4d (%.1f%%)\n", total-hits-probeMisses(perSession), probeRatio(total-hits-probeMisses(perSession), total)*100)
	fmt.Fprintf(stdout, "  miss %4d (%.1f%%)\n", probeMisses(perSession), probeRatio(probeMisses(perSession), total)*100)
	fmt.Fprintf(stdout, "  per type:\n")
	for _, k := range typeKeys {
		tc := types[k]
		fmt.Fprintf(stdout, "    %-12s hit=%d grey=%d miss=%d cross=%d\n", k, tc.hit, tc.grey, tc.miss, tc.cross)
	}
	fmt.Fprintf(stdout, "  session\tturns are cumulative-library; per-session hit/grey/miss:\n")
	for i, c := range perSession {
		fmt.Fprintf(stdout, "    %d\t%s\thit=%d grey=%d miss=%d\n", i, sessions[i].name, c.hit, c.grey, c.miss)
	}
	fmt.Fprintf(stdout, "  turn detail:\n")
	for _, t := range turns {
		fmt.Fprintf(stdout, "    %d\t%s\t%-12s\t%s\n", t.session, t.zone, t.topType, probeTrunc(t.query, 60))
	}
	return nil
}

func probeSessionPaths(sessionsFlag, dirFlag string) ([]string, error) {
	var paths []string
	if sessionsFlag != "" {
		for _, p := range strings.Split(sessionsFlag, ",") {
			if p = strings.TrimSpace(p); p != "" {
				paths = append(paths, p)
			}
		}
	}
	if dirFlag != "" {
		entries, err := os.ReadDir(dirFlag)
		if err != nil {
			return nil, fmt.Errorf("probe: read dir: %w", err)
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
				paths = append(paths, filepath.Join(dirFlag, e.Name()))
			}
		}
	}
	if len(paths) == 0 {
		return nil, usageErrf("probe: --sessions or --dir is required")
	}
	sort.Strings(paths)
	return paths, nil
}

// probeUserQueries extracts user-turn queries from a session transcript —
// the same turn boundaries the extractor uses (each user message starts a
// turn).
func probeUserQueries(data []byte, minLen int) []string {
	var queries []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var tl struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(line), &tl); err != nil {
			continue
		}
		if tl.Role != "user" {
			continue
		}
		q := strings.TrimSpace(tl.Content)
		if len(q) >= minLen {
			queries = append(queries, q)
		}
	}
	return queries
}

// probeToolQueries returns each turn's tool-name sequence as the probe
// query (structure-level mode; see the --query-mode flag comment).
func probeToolQueries(data []byte) []string {
	var queries []string
	var turn []string
	flush := func() {
		if len(turn) >= 2 {
			queries = append(queries, strings.Join(turn, " "))
		}
		turn = turn[:0]
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var tl struct {
			Role      string `json:"role"`
			ToolCalls []struct {
				Name string `json:"name"`
			} `json:"tool_calls"`
		}
		if err := json.Unmarshal([]byte(line), &tl); err != nil {
			continue
		}
		if tl.Role == "user" && len(turn) > 0 {
			flush()
			continue
		}
		for _, tc := range tl.ToolCalls {
			if tc.Name != "" {
				turn = append(turn, tc.Name)
			}
		}
	}
	flush()
	return queries
}

func probeWorkDir() string {
	dir, err := os.MkdirTemp("", "semantix-probe-*")
	if err != nil {
		return "."
	}
	// Best-effort cleanup at process exit is unnecessary: the store is a
	// temp artifact and small. Leave it for OS tmp reaping, but note the
	// path in the summary when verbose debugging is needed.
	return dir
}

// zoneCounters tallies zone verdicts for one session.
type zoneCounters struct{ hit, grey, miss int }

func probeMisses(per []zoneCounters) int {
	n := 0
	for _, c := range per {
		n += c.miss
	}
	return n
}

func probeRatio(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total)
}

func probeSessionRows(per []zoneCounters) []map[string]interface{} {
	var rows []map[string]interface{}
	for i, c := range per {
		rows = append(rows, map[string]interface{}{
			"session": i, "hit": c.hit, "grey": c.grey, "miss": c.miss,
		})
	}
	return rows
}

func probeTrunc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
