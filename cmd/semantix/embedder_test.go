package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/embed"
	"semantix/kernel/slice"
)

// emptyStderr is defined in main_test.go; keep a local buffer writer helper
// instead of depending on test file ordering.

func TestBuildEmbedderHash(t *testing.T) {
	emb, err := buildEmbedder("hash", &emptyStderr{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := emb.(embed.HashEmbedder); !ok {
		t.Fatalf("got %T, want HashEmbedder", emb)
	}
}

func TestBuildEmbedderInvalid(t *testing.T) {
	if _, err := buildEmbedder("onnx", &emptyStderr{}); err == nil {
		t.Fatal("want error for invalid embedder")
	}
}

func TestBuildEmbedderModelMissingEnv(t *testing.T) {
	t.Setenv("SEMANTIX_EMBED_BASE_URL", "")
	t.Setenv("SEMANTIX_EMBED_MODEL", "")
	t.Setenv("SEMANTIX_EMBED_API_KEY", "")
	if _, err := buildEmbedder("model", &emptyStderr{}); err == nil {
		t.Fatal("want error when env is missing")
	}
}

func TestBuildEmbedderModelEnvSet(t *testing.T) {
	t.Setenv("SEMANTIX_EMBED_BASE_URL", "http://127.0.0.1:9")
	t.Setenv("SEMANTIX_EMBED_MODEL", "test-model")
	t.Setenv("SEMANTIX_EMBED_API_KEY", "sk-test")
	emb, err := buildEmbedder("model", &emptyStderr{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := emb.(*embed.ModelEmbedder); !ok {
		t.Fatalf("got %T, want *ModelEmbedder", emb)
	}
}

// TestSearchVectorModelEmbedder runs the full search path against a mock
// embeddings API: request goes to the endpoint, results come back scored.
func TestSearchVectorModelEmbedder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("path = %q, want /embeddings", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		// deterministic mock vectors: first text gets [1,0], others [0,1]
		var body struct {
			Input []string `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		var data []map[string]any
		for _, text := range body.Input {
			v := []float32{0, 1}
			if strings.Contains(text, "修复") {
				v = []float32{1, 0}
			}
			data = append(data, map[string]any{"embedding": v})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	t.Setenv("SEMANTIX_EMBED_BASE_URL", srv.URL)
	t.Setenv("SEMANTIX_EMBED_MODEL", "test-model")
	t.Setenv("SEMANTIX_EMBED_API_KEY", "sk-test")

	dir := t.TempDir()
	db := filepath.Join(dir, "lib.db")
	store := openTestStore(t, db)
	for _, c := range []string{"修复 go 测试失败", "配置 CI 流水线"} {
		sl := &slice.Slice{ID: "m-" + c[:4], Type: slice.Prompt, Scope: slice.Project, Content: []byte(c)}
		if err := store.Put(sl); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"search", "--query", "修复测试失败", "--db", db, "--retriever", "vector", "--embedder", "model", "--limit", "2"}, &out, &errOut, productionDependencies())
	if code != 0 {
		t.Fatalf("search code = %d, stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "修复 go 测试失败") {
		t.Fatalf("search missed the repair slice:\n%s", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr (no fallback should happen): %s", errOut.String())
	}
}

// TestSearchVectorModelEmbedderFallback verifies the fail-soft path end to
// end: a 500 from the API degrades to hash vectors, search still works, and
// a warning lands on stderr.
func TestSearchVectorModelEmbedderFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("SEMANTIX_EMBED_BASE_URL", srv.URL)
	t.Setenv("SEMANTIX_EMBED_MODEL", "test-model")
	t.Setenv("SEMANTIX_EMBED_API_KEY", "sk-test")

	dir := t.TempDir()
	db := filepath.Join(dir, "lib.db")
	store := openTestStore(t, db)
	for _, c := range []string{"修复 go 测试失败", "配置 CI 流水线"} {
		sl := &slice.Slice{ID: "f-" + c[:4], Type: slice.Prompt, Scope: slice.Project, Content: []byte(c)}
		if err := store.Put(sl); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"search", "--query", "修复测试失败", "--db", db, "--retriever", "vector", "--embedder", "model", "--limit", "2"}, &out, &errOut, productionDependencies())
	if code != 0 {
		t.Fatalf("search code = %d (fail-soft must not crash), stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "修复 go 测试失败") {
		t.Fatalf("fallback search missed the repair slice:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "fell back to hash embedding") {
		t.Fatalf("stderr lacks fallback warning: %s", errOut.String())
	}
}

// TestEmbedItemsHashNotPersisted: hash vectors are deterministic functions
// of Content with zero persisted readers, so extract must not store them —
// no Embedding, no provenance meta.
func TestEmbedItemsHashNotPersisted(t *testing.T) {
	items := []*slice.Slice{
		{ID: "a", Content: []byte("hello")},
		{ID: "b", Content: []byte("world")},
	}
	if err := embedItems(embed.HashEmbedder{}, items, "hash"); err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Embedding != nil {
			t.Fatalf("%s: hash embedding persisted (%d floats), want none", item.ID, len(item.Embedding))
		}
		if item.Meta.EmbedModel != "" || item.Meta.EmbedDim != 0 {
			t.Fatalf("%s: provenance written for unpersisted vector: %+v", item.ID, item.Meta)
		}
	}
}

// TestEmbedItemsBatching verifies more than embedBatch slices produce
// multiple remote calls.
func TestEmbedItemsBatching(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Input []string `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if len(body.Input) > embedBatch {
			t.Errorf("batch size %d exceeds %d", len(body.Input), embedBatch)
		}
		var data []map[string]any
		for range body.Input {
			data = append(data, map[string]any{"embedding": []float32{1, 0}})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	emb, err := embed.NewModelEmbedder(embed.ModelEmbedderConfig{
		BaseURL: srv.URL, Model: "m", APIKey: "k",
	})
	if err != nil {
		t.Fatal(err)
	}
	items := make([]*slice.Slice, embedBatch*2+3)
	for i := range items {
		items[i] = &slice.Slice{ID: "s", Content: []byte("x")}
	}
	if err := embedItems(emb, items, "model"); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("server called %d times, want 3 (2 full batches + 1 remainder)", calls)
	}
	for i, item := range items {
		if len(item.Embedding) != 2 || item.Meta.EmbedDim != 2 || item.Meta.EmbedModel != "m" {
			t.Fatalf("item %d: embedding=%v meta=%+v", i, item.Embedding, item.Meta)
		}
	}
}

// TestEmbedItemsRecordsModelName: provenance must name the model that made
// the vector, not the --embedder flag value (Issue #512). Two different
// models must leave different labels, so later retrieval can tell them apart.
func TestEmbedItemsRecordsModelName(t *testing.T) {
	for _, model := range []string{"text-embedding-3-small", "text-embedding-3-large"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Input []string `json:"input"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			var data []map[string]any
			for range body.Input {
				data = append(data, map[string]any{"embedding": []float32{1, 0}})
			}
			json.NewEncoder(w).Encode(map[string]any{"data": data})
		}))
		emb, err := embed.NewModelEmbedder(embed.ModelEmbedderConfig{
			BaseURL: srv.URL, Model: model, APIKey: "k",
		})
		if err != nil {
			t.Fatal(err)
		}
		items := []*slice.Slice{{ID: "a", Content: []byte("x")}}
		if err := embedItems(emb, items, "model"); err != nil {
			t.Fatal(err)
		}
		srv.Close()
		if got := items[0].Meta.EmbedModel; got != model {
			t.Fatalf("EmbedModel = %q, want %q", got, model)
		}
	}
}

// TestEmbedItemsFallbackRecordsHash: when the remote call fails, the batch
// holds hash vectors. Provenance must say "hash", not the remote model name,
// and the dimension must match the vector actually stored.
func TestEmbedItemsFallbackRecordsHash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	emb, err := embed.NewModelEmbedder(embed.ModelEmbedderConfig{
		BaseURL: srv.URL, Model: "text-embedding-3-small", APIKey: "k",
	})
	if err != nil {
		t.Fatal(err)
	}
	items := []*slice.Slice{{ID: "a", Content: []byte("hello")}}
	if err := embedItems(emb, items, "model"); err != nil {
		t.Fatal(err)
	}
	item := items[0]
	if item.Meta.EmbedModel != "hash" {
		t.Fatalf("EmbedModel = %q, want \"hash\" for a fallback batch", item.Meta.EmbedModel)
	}
	if item.Meta.EmbedDim != len(item.Embedding) || item.Meta.EmbedDim == 0 {
		t.Fatalf("EmbedDim = %d, stored vector has %d floats", item.Meta.EmbedDim, len(item.Embedding))
	}
}

// TestEmbedItemsEmpty verifies a nil/empty item list is a no-op.
func TestEmbedItemsEmpty(t *testing.T) {
	if err := embedItems(embed.HashEmbedder{}, nil, "hash"); err != nil {
		t.Fatal(err)
	}
	if err := embedItems(embed.HashEmbedder{}, []*slice.Slice{}, "hash"); err != nil {
		t.Fatal(err)
	}
}
