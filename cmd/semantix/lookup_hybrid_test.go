package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

// lookupIDs runs `lookup --json` and returns the ordered slice IDs.
func lookupIDs(t *testing.T, db string, extra ...string) []string {
	t.Helper()
	args := append([]string{"lookup", "--query", "when did the trip happen", "--db", db, "--json", "--limit", "5"}, extra...)
	var out bytes.Buffer
	var stderr bytes.Buffer
	if code := run(args, &out, &stderr, productionDependencies()); code != 0 {
		t.Fatalf("lookup %v code = %d, stderr=%s", extra, code, stderr.String())
	}
	var env struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out.String())
	}
	ids := make([]string, 0, len(env.Data))
	for _, d := range env.Data {
		ids = append(ids, d.ID)
	}
	return ids
}

// embedServer serves deterministic 2-d vectors: the query and every content
// containing "journey" point at [1,0]; everything else points at [0,1].
func embedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		data := make([]map[string]any, len(body.Input))
		for i, text := range body.Input {
			v := []float32{0, 1}
			if strings.Contains(text, "journey") || strings.Contains(text, "trip happen") {
				v = []float32{1, 0}
			}
			data[i] = map[string]any{"embedding": v}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func seedHybridLookupCorpus(t *testing.T, db string) {
	t.Helper()
	store := openTestStore(t, db)
	for _, sl := range []struct {
		id, content string
		scope       slice.Scope
	}{
		// Lexical match, semantically unrelated to the trip question.
		{"lexical", "trip", slice.Project},
		// No shared query tokens, but the embedding route puts it on the query.
		{"semantic", "the journey date", slice.Project},
		{"other", "deploy the server", slice.Project},
		// Same text, different scope: the vector route must not leak it.
		{"semantic-user", "the journey date", slice.User},
	} {
		if err := store.Put(&slice.Slice{ID: sl.id, Type: slice.Context, Scope: sl.scope, Content: []byte(sl.content)}); err != nil {
			t.Fatal(err)
		}
	}
}

// TestLookupDefaultIsBM25 pins the pre-Issue-#553 contract: without an
// explicit --retriever, lookup ordering is exactly the BM25 route.
func TestLookupDefaultIsBM25(t *testing.T) {
	db := filepath.Join(t.TempDir(), "lib.db")
	seedHybridLookupCorpus(t, db)
	def := lookupIDs(t, db)
	explicit := lookupIDs(t, db, "--retriever", "bm25")
	if strings.Join(def, ",") != strings.Join(explicit, ",") {
		t.Fatalf("default lookup diverged from bm25: %v vs %v", def, explicit)
	}
	if len(def) == 0 || def[0] != "lexical" {
		t.Fatalf("bm25 route should rank the lexical match first, got %v", def)
	}
}

// TestLookupHybridFusesVectorRoute covers the Issue #553 candidate path: the
// lookup surface can now discover a slice that shares no query token, because
// the vector route contributes to the fused ranking.
func TestLookupHybridFusesVectorRoute(t *testing.T) {
	srv := embedServer(t)
	t.Setenv("SEMANTIX_EMBED_BASE_URL", srv.URL)
	t.Setenv("SEMANTIX_EMBED_MODEL", "test-embed")
	t.Setenv("SEMANTIX_EMBED_API_KEY", "test")

	db := filepath.Join(t.TempDir(), "lib.db")
	seedHybridLookupCorpus(t, db)
	bm25 := lookupIDs(t, db)
	hybrid := lookupIDs(t, db, "--retriever", "hybrid", "--embedder", "model", "--bm25-weight", "0.2")
	if len(hybrid) == 0 || hybrid[0] != "semantic" {
		t.Fatalf("hybrid should surface the embedding match first, got %v (bm25 was %v)", hybrid, bm25)
	}
	if bm25[0] == "semantic" {
		t.Fatalf("bm25 unexpectedly ranked the embedding-only match first: %v", bm25)
	}
}

// The healthy hybrid path must give both routes enough candidates, not just
// return enough final rows: RRF can otherwise mask an undersized BM25 pool.
func TestSharedHybridRespectsLimitAboveBM25Pool(t *testing.T) {
	srv := embedServer(t)
	t.Setenv("SEMANTIX_EMBED_BASE_URL", srv.URL)
	t.Setenv("SEMANTIX_EMBED_MODEL", "test-embed")
	t.Setenv("SEMANTIX_EMBED_API_KEY", "test")

	db := filepath.Join(t.TempDir(), "lib.db")
	store := openTestStore(t, db)
	for i := range 30 {
		id := fmt.Sprintf("hybrid-limit-%02d", i)
		if err := store.Put(&slice.Slice{
			ID: id, Type: slice.Context, Scope: slice.Project,
			Content: []byte("trip happen journey " + id),
		}); err != nil {
			t.Fatal(err)
		}
	}

	for _, verb := range []string{"search", "lookup"} {
		for _, mode := range []struct {
			name string
			args []string
		}{
			{"pure-weighted-bm25", []string{"--bm25-weight", "1"}},
			{"dual-route-rrf", []string{"--fusion", "rrf", "--rrf-k", "60"}},
		} {
			t.Run(verb+"/"+mode.name, func(t *testing.T) {
				args := []string{verb, "--scope", "project", "--query", "when did the trip happen",
					"--db", db, "--json", "--limit", "30", "--retriever", "hybrid", "--embedder", "model"}
				var out, stderr bytes.Buffer
				if code := run(append(args, mode.args...), &out, &stderr, productionDependencies()); code != 0 {
					t.Fatalf("exit %d: %s", code, stderr.String())
				}
				if strings.Contains(stderr.String(), "degraded to bm25") {
					t.Fatalf("expected healthy hybrid routes: %s", stderr.String())
				}
				var response struct {
					OK   bool `json:"ok"`
					Data []struct {
						ID    string  `json:"id"`
						Score float64 `json:"score"`
					} `json:"data"`
				}
				if err := json.Unmarshal(out.Bytes(), &response); err != nil || !response.OK {
					t.Fatalf("unexpected response: %s; %v", out.String(), err)
				}
				if got := len(response.Data); got != 30 {
					t.Fatalf("got %d candidates, want 30", got)
				}
				if mode.name == "dual-route-rrf" {
					for _, hit := range response.Data {
						if hit.Score <= 0.5 {
							t.Errorf("%s scored %.3f: expected both contributors, not at most one", hit.ID, hit.Score)
						}
					}
				}
			})
		}
	}
}

// TestLookupHybridDegradesToBM25: an unusable embedding backend must not fail
// the read path — hybrid falls back to BM25 and says so on stderr.
func TestLookupHybridDegradesToBM25(t *testing.T) {
	// Base URL/model set but no API key: buildEmbedder refuses to construct
	// the model embedder, which is the "backend not configured" failure mode.
	t.Setenv("SEMANTIX_EMBED_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("SEMANTIX_EMBED_MODEL", "test-embed")
	t.Setenv("SEMANTIX_EMBED_API_KEY", "")

	db := filepath.Join(t.TempDir(), "lib.db")
	seedHybridLookupCorpus(t, db)
	bm25 := lookupIDs(t, db)

	args := []string{"lookup", "--query", "when did the trip happen", "--db", db, "--json", "--limit", "5", "--retriever", "hybrid", "--embedder", "model"}
	var out, stderr bytes.Buffer
	if code := run(args, &out, &stderr, productionDependencies()); code != 0 {
		t.Fatalf("hybrid fallback code = %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "degraded to bm25") {
		t.Fatalf("fallback was silent; stderr=%q", stderr.String())
	}
	var env struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	ids := make([]string, 0, len(env.Data))
	for _, d := range env.Data {
		ids = append(ids, d.ID)
	}
	if strings.Join(ids, ",") != strings.Join(bm25, ",") {
		t.Fatalf("fallback ordering %v != bm25 ordering %v", ids, bm25)
	}
}

// TestLookupHybridRejectsHashFallback: a reachable-but-failing embedding API
// makes ModelEmbedder fall back to hash vectors. Fusing hash space measured
// worse than BM25 alone, so hybrid must degrade to BM25 rather than present
// hash cosine as semantic retrieval.
func TestLookupHybridRejectsHashFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SEMANTIX_EMBED_BASE_URL", srv.URL)
	t.Setenv("SEMANTIX_EMBED_MODEL", "test-embed")
	t.Setenv("SEMANTIX_EMBED_API_KEY", "test")

	db := filepath.Join(t.TempDir(), "lib.db")
	seedHybridLookupCorpus(t, db)
	bm25 := lookupIDs(t, db)

	args := []string{"lookup", "--query", "when did the trip happen", "--db", db, "--json", "--limit", "5", "--retriever", "hybrid", "--embedder", "model"}
	var out, stderr bytes.Buffer
	if code := run(args, &out, &stderr, productionDependencies()); code != 0 {
		t.Fatalf("hybrid fallback code = %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "degraded to bm25") {
		t.Fatalf("hash fallback not reported as bm25 degradation; stderr=%q", stderr.String())
	}
	var env struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	ids := make([]string, 0, len(env.Data))
	for _, d := range env.Data {
		ids = append(ids, d.ID)
	}
	if strings.Join(ids, ",") != strings.Join(bm25, ",") {
		t.Fatalf("hash-fallback ordering %v != bm25 ordering %v", ids, bm25)
	}
}

// TestLookupHybridRespectsScope: the vector route must apply the same scope
// boundary as the BM25 route instead of leaking cross-scope candidates.
func TestLookupHybridRespectsScope(t *testing.T) {
	srv := embedServer(t)
	t.Setenv("SEMANTIX_EMBED_BASE_URL", srv.URL)
	t.Setenv("SEMANTIX_EMBED_MODEL", "test-embed")
	t.Setenv("SEMANTIX_EMBED_API_KEY", "test")

	db := filepath.Join(t.TempDir(), "lib.db")
	seedHybridLookupCorpus(t, db)
	ids := lookupIDs(t, db, "--scope", "user", "--retriever", "hybrid", "--embedder", "model")
	if len(ids) != 1 || ids[0] != "semantic-user" {
		t.Fatalf("user-scope hybrid leaked or dropped candidates: %v", ids)
	}
}
