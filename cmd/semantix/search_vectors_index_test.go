package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"semantix/kernel/embed"
	"semantix/kernel/slice"
)

func TestSearchVectorsRefreshesLegacyPositionalEmbeddings(t *testing.T) {
	for _, persisted := range []bool{false, true} {
		name := "disposable cache"
		if persisted {
			name = "persisted embedding"
		}
		t.Run(name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,0]}]}`))
			}))
			defer srv.Close()
			remote, err := embed.NewModelEmbedder(embed.ModelEmbedderConfig{BaseURL: srv.URL, Model: "m", APIKey: "test-key"})
			if err != nil {
				t.Fatal(err)
			}
			legacyHash := sha256.Sum256([]byte("l2-normalized-v1\x00" + srv.URL + "\x00m"))
			legacySource := hex.EncodeToString(legacyHash[:])
			store := openTestStore(t, filepath.Join(t.TempDir(), "cache.db"))
			item := &slice.Slice{ID: "doc", Type: slice.Prompt, Scope: slice.Project, Content: []byte("document")}
			if persisted {
				item.Embedding = []float32{0, 1}
				item.Meta.EmbedModel, item.Meta.EmbedDim = "m", 2
				item.Meta.EmbedSource, item.Meta.EmbedContentHash = legacySource, embeddingContentHash(item.Content)
			}
			if err := store.Put(item); err != nil {
				t.Fatal(err)
			}
			if !persisted {
				if err := store.(slice.EmbeddingCache).CacheEmbedding(item.ID, item.Content, []float32{0, 1}, "m", legacySource, embeddingContentHash(item.Content)); err != nil {
					t.Fatal(err)
				}
			}
			items, err := store.List(slice.Project)
			if err != nil {
				t.Fatal(err)
			}
			var warn bytes.Buffer
			vecs, _, err := searchVectors(remote, store, items, "query", &warn)
			if err != nil || warn.Len() > 0 {
				t.Fatalf("search: %v %s", err, &warn)
			}
			if calls != 2 || !reflect.DeepEqual(vecs, [][]float32{{1, 0}}) {
				t.Fatalf("calls = %d, vectors = %v; want query plus refreshed document", calls, vecs)
			}
		})
	}
}
