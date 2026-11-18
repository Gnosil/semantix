package main

import (
	"flag"
	"fmt"
	"io"

	"semantix/kernel/embed"
	"semantix/kernel/fuse"
	"semantix/kernel/slice"
)

// hybridBM25Candidates bounds the BM25 route of a hybrid retrieval before
// fusion: the vector route can only rescue candidates that the lexical route
// also surfaced (Issue #274 single source of truth, matching search).
const hybridBM25Candidates = 20

// retrieveOptions selects the candidate route for one query. The zero value
// (empty Retriever) is BM25, which must stay byte-identical to the historical
// index.Search call.
type retrieveOptions struct {
	Retriever string // bm25 | vector | hybrid
	Embedder  string // hash | model (vector routes only)
	Fusion    fuse.Config
}

// retrieverFlags registers the candidate-route flags shared by `search` and
// `lookup` so both memory-read surfaces accept exactly the same knobs
// (Issue #553: candidate discovery must not have two divergent dialects).
type retrieverFlags struct {
	retriever  *string
	fusion     *string
	rrfK       *int
	bm25Weight *float64
	embedder   *string
}

func addRetrieverFlags(fs *flag.FlagSet, deps dependencies, retrieverKey, retrieverDefault string) retrieverFlags {
	return retrieverFlags{
		retriever: fs.String("retriever", cfgString(deps.resolved, retrieverKey, retrieverDefault),
			"retriever: bm25 | vector (embedding) | hybrid (fusion)"),
		fusion: fs.String("fusion", cfgString(deps.resolved, "retrieval.fusion", "weighted"),
			"hybrid fusion strategy: weighted (score average) | rrf (reciprocal rank fusion)"),
		rrfK: fs.Int("rrf-k", cfgInt(deps.resolved, "retrieval.rrf_k", fuse.DefaultRrfK),
			"RRF constant (default 60)"),
		bm25Weight: fs.Float64("bm25-weight", 0,
			"weighted-mode BM25 route share in [0,1] (default 0.5; explicit 0 = pure vector route)"),
		embedder: fs.String("embedder", "hash",
			"embedder: hash (default, zero-dependency) | model (remote OpenAI-compatible API; see SEMANTIX_EMBED_* env)"),
	}
}

// options validates the parsed flags and resolves the configured BM25 weight.
// An explicit --bm25-weight (including 0) wins over retrieval.bm25_weight;
// absent both, the fusion default (0.5) applies.
func (rf retrieverFlags) options(fs *flag.FlagSet, deps dependencies) (retrieveOptions, error) {
	opt := retrieveOptions{Retriever: *rf.retriever, Embedder: *rf.embedder}
	switch opt.Retriever {
	case "bm25", "vector", "hybrid":
	default:
		return opt, usagef("invalid --retriever %q (want bm25, vector, or hybrid)", opt.Retriever)
	}
	switch *rf.embedder {
	case "hash", "model":
	default:
		return opt, usagef("invalid --embedder %q (want hash or model)", opt.Embedder)
	}
	opt.Fusion = fuse.Config{RrfK: *rf.rrfK}
	switch *rf.fusion {
	case "rrf":
		opt.Fusion.Strategy = fuse.RRF
	case "", "weighted":
	default:
		return opt, usagef("invalid --fusion %q (want weighted or rrf)", *rf.fusion)
	}
	if *rf.rrfK <= 0 {
		return opt, usagef("--rrf-k must be positive")
	}
	bwSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "bm25-weight" {
			bwSet = true
		}
	})
	if !bwSet && deps.resolved != nil {
		if v, _, ok := deps.resolved.Get("retrieval.bm25_weight"); ok {
			if f, ok := v.(float64); ok {
				*rf.bm25Weight = f
				bwSet = true
			}
		}
	}
	if bwSet {
		w := *rf.bm25Weight
		if w < 0 || w > 1 {
			return opt, usagef("--bm25-weight must be in [0,1]")
		}
		opt.Fusion.BM25Weight = &w
	}
	return opt, nil
}

// retrieveHits runs the selected candidate route over an already-built index.
//
// BM25 returns index.Search unchanged. vector returns cosine hits only. hybrid
// fuses the BM25 route with the vector route through kernel/fuse: both scores
// are already normalized onto the bounded [0,1] scale, so the zone package's
// absolute floors keep their meaning (unlike raw BM25 scores).
//
// A hybrid run whose vector route is unavailable degrades to BM25 with a
// visible warning instead of failing: discovering existing memory is a read
// path in the middle of a session, and an embedding endpoint being down must
// not break lookups. Hybrid also degrades when the model embedder falls back
// to hash vectors (hash-space fusion measured worse than BM25 alone), while an
// explicit vector request keeps the documented hash fallback and only warns.
// The degradation is reported on warn, never silent.
func retrieveHits(store slice.Store, items []*slice.Slice, index slice.Index, query string, limit int, scope slice.Scope, opt retrieveOptions, warn io.Writer) ([]slice.Hit, error) {
	switch opt.Retriever {
	case "", "bm25":
		hits, err := index.Search(query, limit, scope)
		if err != nil {
			return nil, fmt.Errorf("search index: %w", err)
		}
		return hits, nil

	case "vector":
		hits, _, err := vectorRouteHits(store, items, query, limit, scope, opt.Embedder, warn)
		return hits, err

	case "hybrid":
		bm25hits, err := index.Search(query, hybridBM25Candidates, scope)
		if err != nil {
			return nil, fmt.Errorf("search index: %w", err)
		}
		vecHits, degraded, err := vectorRouteHits(store, items, query, limit, scope, opt.Embedder, warn)
		if err != nil || degraded {
			// Hybrid is the only route where a degraded embedding backend is
			// strictly worse than not using it: fusing hash-space cosine over
			// BM25 measured below BM25 alone (-13.6pp hit@10 on LoCoMo), and
			// the issue forbids presenting hash fallback as semantic retrieval.
			reason := fmt.Sprintf("%v", err)
			if degraded {
				reason = "embedding backend fell back to hash vectors"
			}
			fmt.Fprintf(warn, "semantix: hybrid retrieval degraded to bm25 (%s)\n", reason)
			hits, bmErr := index.Search(query, limit, scope)
			if bmErr != nil {
				return nil, fmt.Errorf("search index: %w", bmErr)
			}
			return hits, nil
		}
		return fuse.Fuse(bm25hits, vecHits, query, limit, opt.Fusion), nil

	default:
		return nil, fmt.Errorf("invalid retriever %q (want bm25, vector, or hybrid)", opt.Retriever)
	}
}

// vectorRouteHits embeds the candidate library plus the query and returns the
// cosine top-N. Only slices in scope are embedded: the BM25 index filters scope
// inside Search, the vector route must apply the same boundary or it would
// leak candidates across scopes (project/user/session isolation). Vectors are
// cached through the store's embedding cache, so repeated queries pay one
// query embedding only.
func vectorRouteHits(store slice.Store, items []*slice.Slice, query string, limit int, scope slice.Scope, embedderKind string, warn io.Writer) ([]slice.Hit, bool, error) {
	emb, err := buildEmbedder(embedderKind, warn)
	if err != nil {
		return nil, false, err
	}
	scoped := make([]*slice.Slice, 0, len(items))
	for _, item := range items {
		if item.Scope == scope {
			scoped = append(scoped, item)
		}
	}
	vecs, qvec, degraded, err := searchVectors(emb, store, scoped, query, warn)
	if err != nil {
		return nil, degraded, err
	}
	vi := embed.NewVectorIndex()
	for i, item := range scoped {
		vi.Insert(item.ID, vecs[i])
	}
	byID := make(map[string]*slice.Slice, len(scoped))
	for _, item := range scoped {
		byID[item.ID] = item
	}
	vhits := vi.Search(qvec, limit)
	out := make([]slice.Hit, 0, len(vhits))
	for _, vh := range vhits {
		if sl := byID[vh.ID]; sl != nil {
			out = append(out, slice.Hit{Slice: sl, Score: float64(vh.Score)})
		}
	}
	return out, degraded, nil
}
