package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"semantix/kernel/embed"
	"semantix/kernel/slice"
)

func TestSearchVectorsCacheColdWarmAndInvalidation(t *testing.T) {
	calls, texts := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		calls++
		texts += len(body.Input)
		if len(body.Input) > embedBatch {
			t.Errorf("unbounded batch: %d", len(body.Input))
		}
		data := make([]map[string]any, len(body.Input))
		for i := range data {
			v := []float32{0, 1}
			if body.Input[i] == "query" || body.Input[i] == "document 0" || body.Input[i] == "changed content" {
				v = []float32{1, 0}
			}
			data[i] = map[string]any{"embedding": v}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()
	emb, err := embed.NewModelEmbedder(embed.ModelEmbedderConfig{BaseURL: srv.URL, Model: "m", APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(t.TempDir(), "cache.db")
	store := openTestStore(t, db)
	for i := 0; i < embedBatch+1; i++ {
		if err := store.Put(&slice.Slice{ID: fmt.Sprint(i), Content: []byte(fmt.Sprintf("document %d", i)), Scope: slice.Project, Type: slice.Prompt}); err != nil {
			t.Fatal(err)
		}
	}
	search := func() {
		t.Helper()
		items, err := store.List(slice.Project)
		if err != nil {
			t.Fatal(err)
		}
		var warn bytes.Buffer
		vs, q, err := searchVectors(emb, store, items, "query", &warn)
		if err != nil || warn.Len() != 0 {
			t.Fatalf("search: %v %s", err, &warn)
		}
		if len(vs) != embedBatch+1 || len(q) != 2 {
			t.Fatalf("bad vectors")
		}
		index := embed.NewVectorIndex()
		for i, item := range items {
			index.Insert(item.ID, vs[i])
		}
		hits := index.Search(q, 1)
		if len(hits) != 1 || hits[0].ID != "0" || hits[0].Score != 1 {
			t.Fatalf("cold/warm ranking changed: %+v", hits)
		}
	}
	search()
	if calls != 3 || texts != embedBatch+2 {
		t.Fatalf("cold calls=%d texts=%d", calls, texts)
	}
	calls, texts = 0, 0
	// Reopen proves actual persistence rather than a process-only cache.
	closeStore(store)
	store = openTestStore(t, db)
	search()
	if calls != 1 || texts != 1 {
		t.Fatalf("warm calls=%d texts=%d", calls, texts)
	}

	sl, err := store.Get("0")
	if err != nil {
		t.Fatal(err)
	}
	sl.Content = []byte("changed content")
	if err := store.Put(sl); err != nil {
		t.Fatal(err)
	}
	calls, texts = 0, 0
	search()
	if calls != 2 || texts != 2 {
		t.Fatalf("content invalidation calls=%d texts=%d", calls, texts)
	}
	// The same model name at another endpoint is a different vector space.
	emb, err = embed.NewModelEmbedder(embed.ModelEmbedderConfig{BaseURL: srv.URL + "/other", Model: "m", APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	calls, texts = 0, 0
	search()
	if calls != 3 || texts != embedBatch+2 {
		t.Fatalf("endpoint invalidation calls=%d texts=%d", calls, texts)
	}
	emb, err = embed.NewModelEmbedder(embed.ModelEmbedderConfig{BaseURL: srv.URL, Model: "m2", APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	calls, texts = 0, 0
	search()
	if calls != 3 || texts != embedBatch+2 {
		t.Fatalf("model invalidation calls=%d texts=%d", calls, texts)
	}
	// Poison the disposable cache, not the primary slice metadata.
	cache := store.(slice.EmbeddingCache)
	for _, v := range [][]float32{{1}, {0, 0}} {
		if err := cache.CacheEmbedding(sl.ID, sl.Content, v, emb.Model(), emb.CacheKey(), embeddingContentHash(sl.Content)); err != nil {
			t.Fatal(err)
		}
		calls, texts = 0, 0
		search()
		if calls != 2 || texts != 2 {
			t.Fatalf("corrupt vector invalidation calls=%d texts=%d", calls, texts)
		}
	}

}

func TestSearchVectorsFallbackNeverMixesSpaces(t *testing.T) {
	for _, failOn := range []int{1, 3} {
		t.Run(fmt.Sprint(failOn), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == failOn {
					http.Error(w, "unavailable", 500)
					return
				}
				var body struct {
					Input []string `json:"input"`
				}
				json.NewDecoder(r.Body).Decode(&body)
				data := make([]map[string]any, len(body.Input))
				for i := range data {
					data[i] = map[string]any{"embedding": []float32{1, 0}}
				}
				json.NewEncoder(w).Encode(map[string]any{"data": data})
			}))
			defer srv.Close()
			emb, err := embed.NewModelEmbedder(embed.ModelEmbedderConfig{BaseURL: srv.URL, Model: "m", APIKey: "test"})
			if err != nil {
				t.Fatal(err)
			}
			store := openTestStore(t, filepath.Join(t.TempDir(), "cache.db"))
			texts := make([]string, embedBatch+1)
			for i := range texts {
				texts[i] = fmt.Sprintf("document %d", i)
				store.Put(&slice.Slice{ID: fmt.Sprint(i), Content: []byte(texts[i]), Scope: slice.Project, Type: slice.Prompt})
			}
			items, _ := store.List(slice.Project)
			// List order is not the insertion order; compare in exactly that order.
			for i, item := range items {
				texts[i] = string(item.Content)
			}
			var warn bytes.Buffer
			vs, q, err := searchVectors(emb, store, items, "query", &warn)
			if err != nil {
				t.Fatal(err)
			}
			want, _ := (embed.HashEmbedder{}).Embed(texts)
			wantQ, _ := (embed.HashEmbedder{}).Embed([]string{"query"})
			if !reflect.DeepEqual(vs, want) || !reflect.DeepEqual(q, wantQ[0]) {
				t.Fatal("mixed model/hash spaces")
			}
			if !strings.Contains(warn.String(), "entire query") {
				t.Fatalf("no degradation warning: %s", &warn)
			}
			updated, _ := store.List(slice.Project)
			for _, item := range updated {
				if item.Meta.EmbedModel == "hash" {
					t.Fatal("persisted fallback")
				}
			}
		})
	}
}

func TestValidSearchVectorRejectsInvalid(t *testing.T) {
	for _, v := range [][]float32{nil, {}, {0, 0}, {1}} {
		if validSearchVector(v, 2) {
			t.Fatalf("accepted %v", v)
		}
	}
	v := []float32{3, 4}
	if !validSearchVector(v, 2) || v[0] != 0.6 || v[1] != 0.8 {
		t.Fatalf("not normalized: %v", v)
	}
}

func TestExtractHashNamedModelFallbackHasNoModelCacheProvenance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unavailable", 500) }))
	defer srv.Close()
	emb, err := embed.NewModelEmbedder(embed.ModelEmbedderConfig{BaseURL: srv.URL, Model: "hash", APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	items := []*slice.Slice{{ID: "x", Content: []byte("document")}}
	if err := embedItems(emb, items, "model"); err != nil {
		t.Fatal(err)
	}
	if items[0].Meta.EmbedSource != "" || items[0].Meta.EmbedContentHash != "" {
		t.Fatal("fallback stamped as model cache")
	}
}
