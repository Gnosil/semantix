package main

import (
	"math"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
)

// softGraph is frozen at the session cutoff. A directional link carries the
// partial credit a neighbor can supply for a missing query term, in BM25 units.
type softGraph map[string]map[string]float64

func (p prefixPairs) softGraph() (softGraph, int) {
	g := make(softGraph)
	n := float64(p.turnCount)
	edges := 0
	for _, edge := range p.edges {
		a, b := edge.From, edge.To
		da, db := float64(p.termTurns[a]), float64(p.termTurns[b])
		// Subtract the overlap expected by chance before cosine normalization.
		overlap := float64(p.pairTurns[edge.Triple]) - da*db/n
		if overlap <= 0 {
			continue
		}
		support := float64(len(edge.Sessions))
		affinity := overlap / math.Sqrt(da*db) * support / (support + 1)
		for _, seed := range []string{a, b} {
			if g[seed] == nil {
				g[seed] = make(map[string]float64)
			}
		}
		// Credit is bounded by half the prefix IDF of the *original* term.
		g[a][b] = 0.5 * math.Log1p((n-da+0.5)/(da+0.5)) * affinity
		g[b][a] = 0.5 * math.Log1p((n-db+0.5)/(db+0.5)) * affinity
		edges++
	}
	return g, edges
}

type termDocument struct {
	doc   *slice.Slice
	words map[string]bool
}

func newTermDocument(doc *slice.Slice) termDocument {
	words := make(map[string]bool)
	for _, word := range bm25.Tokenize(string(doc.Content)) {
		words[word] = true
	}
	return termDocument{doc, words}
}

func (g softGraph) rerank(query string, primary []slice.Hit, docs []termDocument) ([]slice.Hit, bool) {
	var seeds []string
	inQuery := make(map[string]bool)
	for _, word := range terms(query) {
		inQuery[word] = true
		if len(g[word]) > 0 {
			seeds = append(seeds, word)
		}
	}
	if len(seeds) == 0 {
		return primary, false
	}

	byID := make(map[string]slice.Hit, len(primary))
	for _, h := range primary {
		byID[h.Slice.ID] = h
	}
	changed := false
	for _, d := range docs {
		var bonus float64
		for _, seed := range seeds {
			if d.words[seed] {
				continue // Exact match already has its full lexical score.
			}
			var best float64
			for word := range d.words {
				if !inQuery[word] {
					best = max(best, g[seed][word])
				}
			}
			bonus += best
		}
		if bonus > 0 {
			h := byID[d.doc.ID]
			h.Slice = d.doc
			h.Score += bonus
			byID[d.doc.ID] = h
			changed = true
		}
	}
	if !changed {
		return primary, false
	}
	out := make([]slice.Hit, 0, len(byID))
	for _, h := range byID {
		out = append(out, h)
	}
	sortHits(out)
	return out, true
}
