package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"semantix/kernel/embed"
	"semantix/kernel/slice"
)

type searchResult struct {
	ID      string  `json:"id"`
	Type    int     `json:"type"`
	Scope   string  `json:"scope"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
	Zone    string  `json:"zone"`
}

func runSearch(args []string, stdout, stderr io.Writer, deps dependencies) error {
	cfgPath, cfgExplicit := explicitConfigPath(args, defaultGetenv)
	cfg, err := loadConfigFor(cfgPath, cfgExplicit, defaultGetenv)
	if err != nil {
		return err
	}

	flags := flag.NewFlagSet("search", flag.ContinueOnError)
	flags.SetOutput(stderr)
	queryFlag := flags.String("query", "", "search query (alternatively pass it as positional text)")
	scopeValue := flags.String("scope", cfg.Store.Scope, "slice scope: session, project, or user")
	limit := flags.Int("limit", cfg.Retrieval.SearchLimit, "maximum number of results")
	dbOverride := flags.String("db", "", "database path override")
	projectDB := flags.String("project-db", cfg.Store.DB, "project/session database path")
	userDB := flags.String("user-db", defaultUserDB(), "user database path")
	jsonOutput := flags.Bool("json", false, "write JSON results")
	retriever := flags.String("retriever", cfg.Retrieval.Retriever, "retriever: bm25 (default) | vector (hash-embedding) | hybrid (RRF fusion)")
	embedder := flags.String("embedder", "hash", "embedder: hash (default, zero-dependency) | model (remote OpenAI-compatible API; see SEMANTIX_EMBED_* env)")
	_ = flags.String("config", cfgPath, "config file path (default ./semantix.toml)")
	zf := addZoneFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := zf.validate(); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than zero")
	}

	query := strings.TrimSpace(*queryFlag)
	if query == "" {
		query = strings.TrimSpace(strings.Join(flags.Args(), " "))
	} else if flags.NArg() != 0 {
		return errors.New("use either --query or positional query text, not both")
	}
	if query == "" {
		return errors.New("query is required")
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
		return fmt.Errorf("invalid --retriever %q (want bm25, vector, or hybrid)", *retriever)
	}
	var hits []slice.Hit
	switch *retriever {
	case "vector", "hybrid":
		emb, err := buildEmbedder(*embedder, stderr)
		if err != nil {
			return err
		}
		texts := make([]string, len(items))
		for i, item := range items {
			texts[i] = string(item.Content)
		}
		vecs, err := emb.Embed(texts)
		if err != nil {
			return fmt.Errorf("embed slices: %w", err)
		}
		vi := embed.NewVectorIndex()
		for i, item := range items {
			vi.Insert(item.ID, vecs[i])
		}
		qvecs, err := emb.Embed([]string{query})
		if err != nil {
			return fmt.Errorf("embed query: %w", err)
		}
		vhits := vi.Search(qvecs[0], *limit)
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
			hits = rrfFuse(bm25hits, vhits, items, *limit)
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
			ID:      hit.Slice.ID,
			Type:    int(hit.Slice.Type),
			Scope:   scopeName(hit.Slice.Scope),
			Content: string(hit.Slice.Content),
			Score:   hit.Score,
			Zone:    zones.Classify(hit.Score, top1).String(),
		}
	}

	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(results)
	}
	for i, result := range results {
		content := stripESC(strings.Join(strings.Fields(result.Content), " "))
		fmt.Fprintf(stdout, "%d. score=%.6f zone=%s id=%s scope=%s\n   %s\n", i+1, result.Score, result.Zone, result.ID, result.Scope, content)
	}
	return nil
}

// rrfFuse merges BM25 and vector rankings via Reciprocal Rank Fusion.
// Constant 60 follows the standard RRF formulation; higher ranks dominate.
func rrfFuse(bm25 []slice.Hit, vec []embed.Hit, items []*slice.Slice, k int) []slice.Hit {
	byID := map[string]*slice.Slice{}
	for _, it := range items {
		byID[it.ID] = it
	}
	scores := map[string]float64{}
	for i, h := range bm25 {
		scores[h.Slice.ID] += 1.0 / (60 + float64(i))
	}
	for i, h := range vec {
		scores[h.ID] += 1.0 / (60 + float64(i))
	}
	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return scores[ids[i]] > scores[ids[j]] })
	out := make([]slice.Hit, 0, k)
	for _, id := range ids {
		if sl := byID[id]; sl != nil {
			out = append(out, slice.Hit{Slice: sl, Score: scores[id]})
			if len(out) >= k {
				break
			}
		}
	}
	return out
}
