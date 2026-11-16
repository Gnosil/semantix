package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"semantix/kernel/embed"
	"semantix/kernel/slice"
)

func TestVectorIndexInsertBatchRespectsEmbeddingIndices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if len(req.Input) == 2 {
			_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[0,1]},{"index":0,"embedding":[1,0]}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,0]}]}`))
	}))
	defer srv.Close()
	emb, err := embed.NewModelEmbedder(embed.ModelEmbedderConfig{
		BaseURL: srv.URL, Model: "test-model", APIKey: "test-key", Dim: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	idx := newVectorIndex(2, emb, false)
	if err := idx.InsertBatch([]*slice.Slice{projectSlice("first", "first input"), projectSlice("second", "second input")}); err != nil {
		t.Fatal(err)
	}
	hits, err := idx.Search("first input", 2, slice.Project)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("search returned %d hits, want 2", len(hits))
	}
	if hits[0].Slice.ID != "first" || hits[0].Score != 1 {
		t.Fatalf("top slice = %q, score = %v; want first with score 1", hits[0].Slice.ID, hits[0].Score)
	}
}
