package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"semantix/kernel/embed"
	"semantix/kernel/fuse"
	"semantix/kernel/slice"
)

type searchResult struct {
	ID      string  `json:"id"`
	Type    int     `json:"type"`
	Scope   string  `json:"scope"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
	Zone    string  `json:"zone"`
	// SourceSession is provenance shown in the text output only (U30);
	// json:"-" keeps search --json output byte-identical (backward compat).
	SourceSession string `json:"-"`
}

func runSearch(args []string, stdout, stderr io.Writer, deps dependencies) error {
	flags := flag.NewFlagSet("search", flag.ContinueOnError)
	flags.SetOutput(stderr)
	queryFlag := flags.String("query", "", "search query (alternatively pass it as positional text)")
	scopeValue := flags.String("scope", cfgString(deps.resolved, "store.scope", "project"), "slice scope: session, project, or user")
	limit := flags.Int("limit", cfgInt(deps.resolved, "retrieval.limit", 10), "maximum number of results")
	dbOverride := flags.String("db", "", "database path override")
	projectDB := flags.String("project-db", cfgString(deps.resolved, "store.db", defaultProjectDB()), "project/session database path")
	userDB := flags.String("user-db", defaultUserDB(), "user database path")
	jsonOutput := flags.Bool("json", false, "write JSON envelope")
	retriever := flags.String("retriever", cfgString(deps.resolved, "retrieval.retriever", "bm25"), "retriever: bm25 (default) | vector (hash-embedding) | hybrid (fusion)")
	fusion := flags.String("fusion", cfgString(deps.resolved, "retrieval.fusion", "weighted"), "hybrid fusion strategy: weighted (score average) | rrf (reciprocal rank fusion)")
	rrfK := flags.Int("rrf-k", cfgInt(deps.resolved, "retrieval.rrf_k", fuse.DefaultRrfK), "RRF constant (default 60)")
	bwFlag := flags.Float64("bm25-weight", 0, "weighted-mode BM25 route share in [0,1] (default 0.5; explicit 0 = pure vector route)")
	embedder := flags.String("embedder", "hash", "embedder: hash (default, zero-dependency) | model (remote OpenAI-compatible API; see SEMANTIX_EMBED_* env)")
	zf := addZoneFlags(flags)
	if err := flags.Parse(args); err != nil {
		return usageWrap(err)
	}
	if err := zf.applyConfigZones(flags, deps.resolved); err != nil {
		return usagef("%v", err)
	}
	if err := zf.validate(); err != nil {
		return usagef("%v", err)
	}
	if *limit <= 0 {
		return usagef("--limit must be greater than zero")
	}
	fusionCfg := fuse.Config{RrfK: *rrfK}
	switch *fusion {
	case "rrf":
		fusionCfg.Strategy = fuse.RRF
	case "", "weighted":
	default:
		return usagef("invalid --fusion %q (want weighted or rrf)", *fusion)
	}
	if *rrfK <= 0 {
		return usagef("--rrf-k must be positive")
	}
	// --bm25-weight: an explicit flag value (or a configured
	// retrieval.bm25_weight) selects the BM25 route share, including an
	// explicit 0 = pure vector route; absent → nil (fuse default 0.5).
	bwSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "bm25-weight" {
			bwSet = true
		}
	})
	if !bwSet {
		if v, _, ok := deps.resolved.Get("retrieval.bm25_weight"); ok {
			if f, ok := v.(float64); ok {
				*bwFlag = f
				bwSet = true
			}
		}
	}
	if bwSet {
		w := *bwFlag
		if w < 0 || w > 1 {
			return usagef("--bm25-weight must be in [0,1]")
		}
		fusionCfg.BM25Weight = &w
	}

	query := strings.TrimSpace(*queryFlag)
	if query == "" {
		query = strings.TrimSpace(strings.Join(flags.Args(), " "))
	} else if flags.NArg() != 0 {
		return usagef("use either --query or positional query text, not both")
	}
	if query == "" {
		return usagef("query is required")
	}

	scope, err := parseScope(*scopeValue)
	if err != nil {
		return err
	}
	dbPath := selectDB(scope, *dbOverride, *projectDB, *userDB)
	store, err := deps.openStore(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	if store == nil {
		return errors.New("slice store is unavailable; merge the U4 implementation first")
	}
	defer closeStore(store)

	items, err := store.List(scope)
	if err != nil {
		return fmt.Errorf("list %s slices: %w", scopeName(scope), err)
	}
	index := deps.newIndex()
	if index == nil {
		return errors.New("search index is unavailable")
	}
	for i, item := range items {
		if item == nil {
			return fmt.Errorf("store returned nil slice at position %d", i)
		}
		if err := index.Insert(item); err != nil {
			return fmt.Errorf("index slice %q: %w", item.ID, err)
		}
	}

	switch *retriever {
	case "bm25", "vector", "hybrid":
	default:
		return usagef("invalid --retriever %q (want bm25, vector, or hybrid)", *retriever)
	}
	var hits []slice.Hit
	switch *retriever {
	case "vector", "hybrid":
		emb, err := buildEmbedder(*embedder, stderr)
		if err != nil {
			return err
		}
		vecs, qvec, err := searchVectors(emb, store, items, query, stderr)
		if err != nil {
			return err
		}
		vi := embed.NewVectorIndex()
		for i, item := range items {
			vi.Insert(item.ID, vecs[i])
		}
		vhits := vi.Search(qvec, *limit)
		if *retriever == "vector" {
			byID := map[string]*slice.Slice{}
			for _, item := range items {
				byID[item.ID] = item
			}
			for _, vh := range vhits {
				if sl := byID[vh.ID]; sl != nil {
					hits = append(hits, slice.Hit{Slice: sl, Score: float64(vh.Score)})
				}
			}
		} else {
			bm25hits, err := index.Search(query, 20, scope)
			if err != nil {
				return fmt.Errorf("search index: %w", err)
			}
			// Map the vector route (embed.Hit) onto slice.Hit for the shared
			// fusion (Issue #274 single source of truth).
			byID := map[string]*slice.Slice{}
			for _, item := range items {
				byID[item.ID] = item
			}
			vecHits := make([]slice.Hit, 0, len(vhits))
			for _, vh := range vhits {
				if sl := byID[vh.ID]; sl != nil {
					vecHits = append(vecHits, slice.Hit{Slice: sl, Score: float64(vh.Score)})
				}
			}
			hits = fuse.Fuse(bm25hits, vecHits, query, *limit, fusionCfg)
		}
	default: // bm25
		hits, err = index.Search(query, *limit, scope)
		if err != nil {
			return fmt.Errorf("search index: %w", err)
		}
	}
	results := make([]searchResult, len(hits))
	top1 := 0.0
	if len(hits) > 0 {
		top1 = hits[0].Score
	}
	zones := zf.zones()
	for i, hit := range hits {
		results[i] = searchResult{
			ID:            hit.Slice.ID,
			Type:          int(hit.Slice.Type),
			Scope:         scopeName(hit.Slice.Scope),
			Content:       string(hit.Slice.Content),
			Score:         hit.Score,
			Zone:          zones.Classify(hit.Score, top1).String(),
			SourceSession: hit.Slice.Meta.SourceSession,
		}
	}

	if *jsonOutput {
		return writeJSON(stdout, okEnvelope("search", results))
	}
	rows := make([]hitRow, len(results))
	for i, result := range results {
		content := stripESC(strings.Join(strings.Fields(result.Content), " "))
		fmt.Fprintf(stdout, "%s\n   %s\n", formatHitLine(i+1, verdictIcon(result.Zone), fmt.Sprintf("%.6f", result.Score), result.Zone, result.ID, result.Scope, result.SourceSession), content)
		rows[i] = hitRow{Zone: result.Zone, SourceSession: result.SourceSession}
	}
	writeHitsSummary(stdout, rows)
	return nil
}
