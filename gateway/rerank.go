package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"semantix/kernel/slice"
)

// This file implements the optional local reranker decorator
// (docs/specs/local-retrieval-model.md §3 B). The decorator over-fetches
// candidates from the inner index, POSTs {query, documents:[{id,text}]} to
// the loopback rerank service, and returns the hits in the reranker's order
// with its bounded [0,1] scores — the same bounded scale the zone floors
// were tuned for (retriever.go score scale contract).
//
// I-5 feature boundary (spec §3.2): the request carries ONLY the query and
// each candidate's id+content. Stats, Weight and Meta never leave the
// process, so the "ranked higher → injected → ranked higher" loop is
// structurally cut at the wire format.
//
// Fail-soft contract (same as ModelEmbedder): any error — connect, timeout,
// non-200, malformed reply, unknown ids — degrades to the inner results,
// untouched and truncated to k. Retrieval never breaks because the lab
// sidecar is down.

// rerankSettings configures the decorator, mapped from [retrieval]
// rerank_base_url / rerank_top_n / rerank_timeout_ms.
type rerankSettings struct {
	BaseURL   string
	TopN      int
	TimeoutMs int
}

const (
	defaultRerankTopN      = 20
	defaultRerankTimeoutMs = 300
)

func (s rerankSettings) topN() int {
	if s.TopN <= 0 {
		return defaultRerankTopN
	}
	return s.TopN
}

func (s rerankSettings) timeout() time.Duration {
	if s.TimeoutMs <= 0 {
		return defaultRerankTimeoutMs * time.Millisecond
	}
	return time.Duration(s.TimeoutMs) * time.Millisecond
}

// rerankIndex decorates a slice.Index with the rerank pass.
type rerankIndex struct {
	inner  slice.Index
	cfg    rerankSettings
	client *http.Client
}

func newRerankIndex(inner slice.Index, cfg rerankSettings) *rerankIndex {
	return &rerankIndex{
		inner:  inner,
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.timeout()},
	}
}

func (r *rerankIndex) Insert(s *slice.Slice) error { return r.inner.Insert(s) }
func (r *rerankIndex) Remove(id string) error      { return r.inner.Remove(id) }

func (r *rerankIndex) Search(query string, k int, scope slice.Scope) ([]slice.Hit, error) {
	if k <= 0 {
		return []slice.Hit{}, nil
	}
	over := r.cfg.topN()
	if over < k {
		over = k
	}
	hits, err := r.inner.Search(query, over, scope)
	if err != nil {
		return nil, err
	}
	if len(hits) <= 1 {
		return truncate(hits, k), nil
	}
	reranked, err := r.rerank(query, hits)
	if err != nil {
		log.Printf("gateway: rerank fallback: %v", err)
		return truncate(hits, k), nil
	}
	return truncate(reranked, k), nil
}

type rerankRequest struct {
	Query     string           `json:"query"`
	Documents []rerankDocument `json:"documents"`
	TopN      int              `json:"top_n,omitempty"`
}

// rerankDocument is the entire per-candidate payload: id + content only
// (I-5 feature boundary — never add stats/weight/meta fields here).
type rerankDocument struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type rerankResponse struct {
	Results []struct {
		ID    string  `json:"id"`
		Score float64 `json:"score"`
	} `json:"results"`
}

// rerank calls the sidecar and maps its (id, score) order back onto the
// retrieved hits. Scores are clamped to [0,1]; Lexical fields carry over
// from the original hit so the lexical support gate is unaffected.
func (r *rerankIndex) rerank(query string, hits []slice.Hit) ([]slice.Hit, error) {
	byID := make(map[string]slice.Hit, len(hits))
	docs := make([]rerankDocument, 0, len(hits))
	for _, h := range hits {
		if h.Slice == nil {
			continue
		}
		byID[h.Slice.ID] = h
		docs = append(docs, rerankDocument{ID: h.Slice.ID, Text: string(h.Slice.Content)})
	}
	if len(docs) == 0 {
		return hits, nil
	}
	body, err := json.Marshal(rerankRequest{Query: query, Documents: docs, TopN: len(docs)})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.timeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.cfg.BaseURL+"/rerank", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rerank: status %d", resp.StatusCode)
	}
	var parsed rerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("rerank: decode: %w", err)
	}
	if len(parsed.Results) == 0 {
		return nil, fmt.Errorf("rerank: empty results")
	}
	out := make([]slice.Hit, 0, len(parsed.Results))
	seen := make(map[string]bool, len(parsed.Results))
	for _, res := range parsed.Results {
		h, ok := byID[res.ID]
		if !ok || seen[res.ID] {
			return nil, fmt.Errorf("rerank: unknown or duplicate id %q", res.ID)
		}
		seen[res.ID] = true
		score := res.Score
		if score < 0 {
			score = 0
		}
		if score > 1 {
			score = 1
		}
		h.Score = score
		out = append(out, h)
	}
	return out, nil
}

func truncate(hits []slice.Hit, k int) []slice.Hit {
	if len(hits) > k {
		return hits[:k]
	}
	return hits
}
