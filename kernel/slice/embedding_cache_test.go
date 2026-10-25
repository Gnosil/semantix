package slice

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func cacheContentHash(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

func TestEmbeddingCachePreservesConcurrentSliceState(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cache.db")
	a, err := NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	sl := &Slice{ID: "a", Type: Prompt, Scope: Project, Content: []byte("original"), Weight: 2, Meta: SliceMeta{SourceSession: "session"}}
	if err := a.Put(sl); err != nil {
		t.Fatal(err)
	}
	b, err := NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	cache := b.(EmbeddingCache)
	if err := a.UpdateStats("a", SliceStats{Hits: 3}); err != nil {
		t.Fatal(err)
	}
	hash := cacheContentHash(sl.Content)
	if err := cache.CacheEmbedding("a", sl.Content, []float32{1, 0}, "model", "source", hash); err != nil {
		t.Fatal(err)
	}
	raw, err := cache.ReadCachedEmbedding("a", "source", hash)
	if err != nil {
		t.Fatal(err)
	}
	var v []float32
	if json.Unmarshal(raw, &v) != nil || len(v) != 2 {
		t.Fatalf("bad cache: %s", raw)
	}
	got, _ := a.Get("a")
	if got.Embedding != nil || got.Stats.Hits != 3 || got.Weight != 2 || got.Meta.SourceSession != "session" {
		t.Fatalf("lost state: %+v", got)
	}
	// Content and deletion changes on A must survive stale writes on B.
	got.Content = []byte("changed")
	if err := a.Put(got); err != nil {
		t.Fatal(err)
	}
	if err := cache.CacheEmbedding("a", sl.Content, []float32{0, 1}, "model", "source", hash); err != nil {
		t.Fatal(err)
	}
	b.(*fileStore).Close()
	a.(*fileStore).Close()
	a, err = NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = a.Get("a")
	if string(got.Content) != "changed" || got.Stats.Hits != 3 {
		t.Fatalf("stale cache overwrote slice: %+v", got)
	}
	b, err = NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if err := b.(EmbeddingCache).CacheEmbedding("a", sl.Content, []float32{1, 0}, "model", "source", hash); err != nil {
		t.Fatal(err)
	}
	b.(*fileStore).Close()
	a.(*fileStore).Close()
	a, err = NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer a.(*fileStore).Close()
	if got, _ := a.Get("a"); got != nil {
		t.Fatal("resurrected deleted record")
	}
}

func TestEmbeddingCacheRawReadsAndSymlinkSafety(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cache.db")
	store, err := NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.(*fileStore).Close()
	item := &Slice{ID: "a", Content: []byte("content"), Type: Prompt, Scope: Project, Embedding: []float32{1, 0}}
	if err := store.Put(item); err != nil {
		t.Fatal(err)
	}
	cache := store.(EmbeddingCache)
	raw, err := cache.ReadEmbedding("a")
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = '!'
	again, _ := cache.ReadEmbedding("a")
	if again[0] != '[' {
		t.Fatal("raw alias")
	}
	got, _ := store.Get("a")
	if got.Embedding != nil {
		t.Fatal("broke lazy reads")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, db+".embeddings"); err != nil {
		t.Skip(err)
	}
	hash := cacheContentHash(item.Content)
	if err := cache.CacheEmbedding("a", item.Content, []float32{1, 0}, "model", "source", hash); err == nil {
		t.Fatal("followed cache directory symlink")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatal("wrote outside cache")
	}
}
