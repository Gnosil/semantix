package gateway

import (
	"testing"

	"semantix/kernel/embed"
	"semantix/kernel/slice"
)

// TestNewGatewayEmbedderHashDefault: empty/hash kind returns nil, so the
// retriever falls back to the zero-dependency HashEmbedder (no behavior change).
func TestNewGatewayEmbedderHashDefault(t *testing.T) {
	if emb := newGatewayEmbedder(EmbedderConfig{}); emb != nil {
		t.Fatalf("empty embedder config = %T, want nil (hash default)", emb)
	}
	if emb := newGatewayEmbedder(EmbedderConfig{Kind: "hash"}); emb != nil {
		t.Fatalf("kind=hash = %T, want nil (hash default)", emb)
	}
}

// TestNewGatewayEmbedderModelWiresModelEmbedder: kind=model with the env API key
// builds a *embed.ModelEmbedder without calling the remote (assembly only).
func TestNewGatewayEmbedderModelWiresModelEmbedder(t *testing.T) {
	t.Setenv("SEMANTIX_EMBED_API_KEY", "test-key")
	emb := newGatewayEmbedder(EmbedderConfig{Kind: "model", BaseURL: "https://example.invalid/v1", Model: "text-emb"})
	if emb == nil {
		t.Fatal("kind=model: expected a non-nil embedder")
	}
	if _, ok := emb.(*embed.ModelEmbedder); !ok {
		t.Fatalf("kind=model built %T, want *embed.ModelEmbedder", emb)
	}
}

// TestVectorIndexCompatibilityGuard: slices stored in a different embedding
// space (different model or dimension) are not returned by Search, so mixed
// spaces never collide (Issue #275).
func TestVectorIndexCompatibilityGuard(t *testing.T) {
	// Index embeds with model "hash" / dim 256 (default).
	idx := newRetriever("vector", 0, nil, "", 0)
	seedRetriever(t, idx,
		projectSlice("a", "golang grpc binary build deploy linux systemd"),
		&slice.Slice{ID: "b", Type: slice.Result, Scope: slice.Project, Content: []byte("golang grpc binary build deploy linux systemd"),
			Meta: slice.SliceMeta{EmbedModel: "text-emb", EmbedDim: 768}},
		&slice.Slice{ID: "c", Type: slice.Result, Scope: slice.Project, Content: []byte("golang grpc binary build deploy linux systemd"),
			Meta: slice.SliceMeta{EmbedModel: "hash", EmbedDim: 512}},
	)

	got, err := idx.Search("golang grpc binary build linux systemd", 5, slice.Project)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range got {
		if h.Slice.ID == "b" || h.Slice.ID == "c" {
			t.Fatalf("incompatible slice %q leaked into search: meta=%+v", h.Slice.ID, h.Slice.Meta)
		}
	}
	// Slice "a" (no provenance, hash space) is the expected hit.
	found := false
	for _, h := range got {
		if h.Slice.ID == "a" {
			found = true
		}
	}
	if !found {
		t.Fatalf("compatible slice a not returned; got %d hits", len(got))
	}
}
