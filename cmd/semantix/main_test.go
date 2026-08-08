package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
)

type fakeExtractor struct {
	items []*slice.Slice
	meta  slice.SliceMeta
}

func (f *fakeExtractor) Extract(_ []byte, meta slice.SliceMeta) ([]*slice.Slice, error) {
	f.meta = meta
	return f.items, nil
}

type fakeStore struct {
	items  map[string]*slice.Slice
	closed bool
}

func newFakeStore(items ...*slice.Slice) *fakeStore {
	store := &fakeStore{items: make(map[string]*slice.Slice)}
	for _, item := range items {
		store.items[item.ID] = item
	}
	return store
}

func (f *fakeStore) Put(item *slice.Slice) error {
	if item == nil {
		return errors.New("nil slice")
	}
	f.items[item.ID] = item
	return nil
}

func (f *fakeStore) Get(id string) (*slice.Slice, error) {
	item := f.items[id]
	if item == nil {
		return nil, errors.New("not found")
	}
	return item, nil
}

func (f *fakeStore) List(scope slice.Scope) ([]*slice.Slice, error) {
	var items []*slice.Slice
	for _, item := range f.items {
		if item.Scope == scope {
			items = append(items, item)
		}
	}
	return items, nil
}

func (f *fakeStore) UpdateStats(string, slice.SliceStats) error { return nil }

func (f *fakeStore) Close() error {
	f.closed = true
	return nil
}

func TestExtractStoresSlicesInSelectedScope(t *testing.T) {
	input := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(input, []byte("{\"role\":\"user\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	extractor := &fakeExtractor{items: []*slice.Slice{
		{ID: "one", Scope: slice.Session, Content: []byte("first")},
		{ID: "two", Scope: slice.Session, Content: []byte("second")},
	}}
	store := newFakeStore()
	var openedPath string
	deps := dependencies{
		newExtractor: func() slice.Extractor { return extractor },
		openStore: func(path string) (slice.Store, error) {
			openedPath = path
			return store, nil
		},
		newIndex: func() slice.Index { return bm25.New() },
	}

	var stdout, stderr bytes.Buffer
	dbPath := filepath.Join(t.TempDir(), "user.db")
	code := run([]string{
		"extract", "--input", input, "--scope", "user", "--db", dbPath,
		"--session", "session-1", "--project", "semantix", "--language", "zh-CN",
	}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	if openedPath != dbPath {
		t.Fatalf("opened path = %q, want %q", openedPath, dbPath)
	}
	if extractor.meta.SourceSession != "session-1" || extractor.meta.ProjectSlug != "semantix" || extractor.meta.Language != "zh-CN" {
		t.Fatalf("extractor meta = %#v", extractor.meta)
	}
	for _, id := range []string{"one", "two"} {
		if store.items[id] == nil || store.items[id].Scope != slice.User {
			t.Fatalf("stored slice %q = %#v, want user scope", id, store.items[id])
		}
	}
	if !store.closed {
		t.Fatal("store was not closed")
	}
	if !strings.Contains(stdout.String(), "extracted=2 stored=2 scope=user") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestSearchLoadsStoreAndRanksResults(t *testing.T) {
	store := newFakeStore(
		&slice.Slice{ID: "relevant", Scope: slice.Project, Content: []byte("BM25 cache retrieval")},
		&slice.Slice{ID: "unrelated", Scope: slice.Project, Content: []byte("dashboard colors")},
		&slice.Slice{ID: "user", Scope: slice.User, Content: []byte("cache preference")},
	)
	deps := dependencies{
		newExtractor: func() slice.Extractor { return nil },
		openStore:    func(string) (slice.Store, error) { return store, nil },
		newIndex:     func() slice.Index { return bm25.New() },
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"search", "--db", "test.db", "--scope", "project", "--limit", "1", "cache retrieval"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "id=relevant") || strings.Contains(stdout.String(), "id=user") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !store.closed {
		t.Fatal("store was not closed")
	}
}

func TestSearchJSONOutput(t *testing.T) {
	store := newFakeStore(&slice.Slice{ID: "doc", Scope: slice.Project, Content: []byte("中文检索")})
	deps := dependencies{
		newExtractor: func() slice.Extractor { return nil },
		openStore:    func(string) (slice.Store, error) { return store, nil },
		newIndex:     func() slice.Index { return bm25.New() },
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"search", "--db", "test.db", "--json", "中文"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"id": "doc"`) || !strings.Contains(stdout.String(), `"content": "中文检索"`) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestExtractWithProductionDependencies(t *testing.T) {
	input := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(input, []byte(`{"role":"user","content":"查一下"}
{"role":"assistant","content":"","tool_calls":[{"id":"c1","name":"readFile"},{"id":"c2","name":"editFile"}]}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	db := filepath.Join(t.TempDir(), "slices.jsonl")
	var stdout, stderr bytes.Buffer
	code := run([]string{"extract", "--input", input, "--db", db}, &stdout, &stderr, productionDependencies())
	if code != 0 {
		t.Fatalf("run() code = %d, want 0; stderr = %q", code, stderr.String())
	}
	store, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.List(slice.Project)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("extract with production dependencies stored no slices")
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"unknown"}, &stdout, &stderr, dependencies{})
	if code != 2 || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
}
