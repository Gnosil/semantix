package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"

	"semantix/kernel/embed"
	"semantix/kernel/slice"
)

func embeddingContentHash(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

// validSearchVector rejects corrupt, empty, non-finite and zero vectors, then
// normalizes a detached copy so imported magnitudes cannot skew cosine scores.
func validSearchVector(v []float32, dim int) bool {
	if len(v) == 0 || len(v) != dim {
		return false
	}
	var norm float64
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return false
		}
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return false
	}
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
	return true
}

// searchVectors uses the query's model dimension to validate cached vectors.
// On any fallback, ALL vectors (including the query) use hash space. Mixing
// cached model vectors with fallback hashes would silently corrupt ranking.
func searchVectors(emb embed.Embedder, store slice.Store, items []*slice.Slice, query string, warn io.Writer) ([][]float32, []float32, error) {
	texts := make([]string, len(items))
	for i, item := range items {
		texts[i] = string(item.Content)
	}
	hashAll := func() ([][]float32, []float32, error) {
		h := embed.HashEmbedder{}
		vs, err := h.Embed(texts)
		if err != nil {
			return nil, nil, err
		}
		q, err := h.Embed([]string{query})
		if err != nil {
			return nil, nil, err
		}
		return vs, q[0], nil
	}
	remote, ok := emb.(*embed.ModelEmbedder)
	if !ok {
		return hashAll()
	}
	fallback := func() ([][]float32, []float32, error) {
		fmt.Fprintln(warn, "semantix: vector search degraded to hash for the entire query; model vectors were not mixed with fallback vectors")
		return hashAll()
	}
	q, fellBack, err := remote.EmbedReport([]string{query})
	if err != nil {
		return nil, nil, fmt.Errorf("embed query: %w", err)
	}
	if fellBack || len(q) != 1 || !validSearchVector(q[0], len(q[0])) {
		return fallback()
	}
	dim := len(q[0])
	cache, canCache := store.(slice.EmbeddingCache)
	vecs := make([][]float32, len(items))
	missing := make([]int, 0)
	for i, item := range items {
		meta := item.Meta
		if canCache {
			raw, readErr := cache.ReadCachedEmbedding(item.ID, remote.CacheKey(), embeddingContentHash(item.Content))
			if readErr != nil {
				fmt.Fprintf(warn, "semantix: embedding cache read failed for %q: %v\n", item.ID, readErr)
			}
			if readErr == nil && json.Unmarshal(raw, &vecs[i]) == nil && validSearchVector(vecs[i], dim) {
				continue
			}
		}
		if canCache && meta.EmbedModel == remote.Model() && meta.EmbedDim == dim && meta.EmbedSource == remote.CacheKey() && meta.EmbedContentHash == embeddingContentHash(item.Content) {
			raw, readErr := cache.ReadEmbedding(item.ID)
			if readErr != nil {
				fmt.Fprintf(warn, "semantix: embedding cache read failed for %q: %v\n", item.ID, readErr)
			}
			if readErr == nil && json.Unmarshal(raw, &vecs[i]) == nil && validSearchVector(vecs[i], dim) {
				continue
			}
		}
		vecs[i] = nil
		missing = append(missing, i)
	}
	for start := 0; start < len(missing); start += embedBatch {
		batch := missing[start:min(start+embedBatch, len(missing))]
		input := make([]string, len(batch))
		for j, i := range batch {
			input[j] = texts[i]
		}
		vs, fellBack, err := remote.EmbedReport(input)
		if err != nil {
			return nil, nil, fmt.Errorf("embed slices: %w", err)
		}
		if fellBack || len(vs) != len(batch) {
			return fallback()
		}
		for _, v := range vs {
			if !validSearchVector(v, dim) {
				return fallback()
			}
		}
		for j, i := range batch {
			vecs[i] = vs[j]
			if canCache {
				item := items[i]
				if err := cache.CacheEmbedding(item.ID, item.Content, vs[j], remote.Model(), remote.CacheKey(), embeddingContentHash(item.Content)); err != nil {
					fmt.Fprintf(warn, "semantix: embedding cache write failed for %q: %v\n", item.ID, err)
				}
			}
		}
	}
	return vecs, q[0], nil
}
