package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"semantix/kernel/slice"
)

// stubIndex is a fixed-order slice.Index: Search returns the seeded hits
// verbatim, so tests can tell "reranked" apart from "passthrough".
type stubIndex struct{ hits []slice.Hit }

func (s *stubIndex) Insert(*slice.Slice) error { return nil }
func (s *stubIndex) Remove(string) error       { return nil }
func (s *stubIndex) Search(query string, k int, scope slice.Scope) ([]slice.Hit, error) {
	if k > len(s.hits) {
		k = len(s.hits)
	}
	out := make([]slice.Hit, k)
	copy(out, s.hits[:k])
	return out, nil
}

func stubHits() []slice.Hit {
	mk := func(id, content string, score float64) slice.Hit {
		return slice.Hit{
			Slice: &slice.Slice{
				ID: id, Type: slice.Prompt, Scope: slice.Project,
				Content: []byte(content),
				// Poison-pill stats/weight: if any of this leaks into the
				// rerank request the feature-boundary assertion fires.
				Stats:  slice.SliceStats{Hits: 99, Injected: 42},
				Weight: 0.777,
			},
			Score: score, Lexical: 0.5, LexicalValid: true,
		}
	}
	return []slice.Hit{
		mk("first", "alpha content", 9.0),
		mk("second", "bravo content", 8.0),
		mk("third", "charlie content", 7.0),
	}
}

// rerankStub serves POST /rerank reversing the document order with fixed
// descending scores. It records the raw request body for the I-5 feature
// boundary check and counts calls.
func rerankStub(t *testing.T, calls *atomic.Int64, lastBody *atomic.Value, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/rerank") {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		var req struct {
			Query     string `json:"query"`
			Documents []struct {
				ID   string `json:"id"`
				Text string `json:"text"`
			} `json:"documents"`
		}
		raw := make([]byte, 0)
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			raw = append(raw, buf[:n]...)
			if err != nil {
				break
			}
		}
		lastBody.Store(string(raw))
		if err := json.Unmarshal(raw, &req); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		if status != http.StatusOK {
			http.Error(w, "boom", status)
			return
		}
		type result struct {
			ID    string  `json:"id"`
			Score float64 `json:"score"`
		}
		out := struct {
			Results []result `json:"results"`
		}{}
		score := 0.95
		for i := len(req.Documents) - 1; i >= 0; i-- {
			out.Results = append(out.Results, result{ID: req.Documents[i].ID, Score: score})
			score -= 0.2
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
}

// TestRerankIndexReorders: the decorator must return hits in the reranker's
// order with the reranker's bounded [0,1] scores, preserving each hit's
// original Lexical fields — and the request body must carry ONLY id+text
// per document (I-5 feature boundary: no stats, no weight).
func TestRerankIndexReorders(t *testing.T) {
	var calls atomic.Int64
	var lastBody atomic.Value
	srv := rerankStub(t, &calls, &lastBody, http.StatusOK)
	defer srv.Close()

	idx := newRerankIndex(&stubIndex{hits: stubHits()}, rerankSettings{
		BaseURL: srv.URL, TopN: 3, TimeoutMs: 2000,
	})
	hits, err := idx.Search("some query", 3, slice.Project)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("rerank calls = %d, want 1", calls.Load())
	}
	if len(hits) != 3 {
		t.Fatalf("len(hits) = %d, want 3", len(hits))
	}
	if hits[0].Slice.ID != "third" || hits[2].Slice.ID != "first" {
		t.Errorf("order = [%s %s %s], want reversed [third second first]",
			hits[0].Slice.ID, hits[1].Slice.ID, hits[2].Slice.ID)
	}
	if hits[0].Score != 0.95 {
		t.Errorf("top score = %v, want 0.95 (reranker's bounded score)", hits[0].Score)
	}
	if !hits[0].LexicalValid || hits[0].Lexical != 0.5 {
		t.Errorf("lexical fields not preserved: %+v", hits[0])
	}

	body, _ := lastBody.Load().(string)
	for _, poison := range []string{"777", "\"Hits\"", "\"hits\"", "weight", "Stats", "stats"} {
		if strings.Contains(body, poison) {
			t.Errorf("rerank request leaks slice metadata (%q found): %s", poison, body)
		}
	}
	if !strings.Contains(body, "alpha content") {
		t.Errorf("rerank request missing document text: %s", body)
	}
}

// TestRerankIndexFailSoft: an erroring rerank service must degrade to the
// inner index's untouched order and scores — after actually attempting the
// call (same fail-soft contract as ModelEmbedder).
func TestRerankIndexFailSoft(t *testing.T) {
	var calls atomic.Int64
	var lastBody atomic.Value
	srv := rerankStub(t, &calls, &lastBody, http.StatusInternalServerError)
	defer srv.Close()

	idx := newRerankIndex(&stubIndex{hits: stubHits()}, rerankSettings{
		BaseURL: srv.URL, TopN: 3, TimeoutMs: 2000,
	})
	hits, err := idx.Search("some query", 3, slice.Project)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("rerank calls = %d, want 1 (must attempt before degrading)", calls.Load())
	}
	if hits[0].Slice.ID != "first" || hits[0].Score != 9.0 {
		t.Errorf("fail-soft must return inner order/scores untouched, got %s score %v",
			hits[0].Slice.ID, hits[0].Score)
	}
}

// TestRerankWiredThroughConfig: with [retrieval] rerank_base_url set, a
// real chat turn must hit the rerank sidecar — proof that New() wraps the
// retriever with the decorator (not just that the decorator works).
func TestRerankWiredThroughConfig(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	var calls atomic.Int64
	var lastBody atomic.Value
	rrSrv := rerankStub(t, &calls, &lastBody, http.StatusOK)
	defer rrSrv.Close()

	g := newTestGateway(t, upSrv.URL)
	_ = g.Close() // rebuild with rerank config: the decorator wraps at New time
	cfg := g.cfg
	cfg.Retrieval.RerankBaseURL = "http://127.0.0.1:1" // placeholder; replaced below
	// rerankStub listens on 127.0.0.1 with a random port — loopback, so the
	// config validation path stays honest.
	cfg.Retrieval.RerankBaseURL = rrSrv.URL
	cfg.Retrieval.RerankTimeoutMs = 2000
	g2, err := New(cfg)
	if err != nil {
		t.Fatalf("New with rerank config: %v", err)
	}
	defer g2.Close()
	srv := httptest.NewServer(g2)
	defer srv.Close()

	// Two documents must match the query: a single-candidate retrieval
	// short-circuits before the rerank call (nothing to reorder).
	seed(t, g2, &slice.Slice{
		ID: "rr-a", Type: slice.Prompt, Scope: slice.Project,
		Content: []byte("prior knowledge about widgets"),
	})
	seed(t, g2, &slice.Slice{
		ID: "rr-b", Type: slice.Prompt, Scope: slice.Project,
		Content: []byte("widgets assembly notes"),
	})
	for i, content := range []string{"alpha", "bravo", "charlie"} {
		seed(t, g2, &slice.Slice{
			ID: fmt.Sprintf("rr-distractor-%d", i), Type: slice.Prompt, Scope: slice.Project,
			Content: []byte(content),
		})
	}
	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "widgets", false))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	if calls.Load() == 0 {
		t.Fatal("rerank sidecar never called: decorator not wired through config")
	}
}

// TestRerankConfigLoopbackOnly: [retrieval] rerank_base_url outside
// 127.0.0.1/localhost fails validation — the rerank protocol is
// unauthenticated plaintext HTTP, lab-loopback only (spec §8).
func TestRerankConfigLoopbackOnly(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.GatewayKey = "k"
	cfg.Upstreams = []UpstreamConfig{{
		Name: "up", BaseURL: "https://example.com/v1", APIKey: "x",
		ModelAlias: []string{"m"}, UpstreamModel: "m", Vendor: "openai",
	}}
	cfg.Retrieval.RerankBaseURL = "http://evil.example.com:8080"
	if err := cfg.validate(); err == nil {
		t.Fatal("validate accepted a non-loopback rerank_base_url")
	}
	cfg.Retrieval.RerankBaseURL = "http://127.0.0.1:18080"
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate rejected loopback rerank_base_url: %v", err)
	}
	cfg.Retrieval.RerankBaseURL = "http://localhost:18080"
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate rejected localhost rerank_base_url: %v", err)
	}
}
