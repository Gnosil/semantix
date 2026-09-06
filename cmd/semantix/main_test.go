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

// openTestStore opens a real file store and registers Close so the journal
// handle is released before t.TempDir() teardown — on Windows the .journal
// file stays locked while any handle is open, which fails RemoveAll cleanup
// (CI on Linux can unlink open files, so this only bites local Windows runs).
func openTestStore(t *testing.T, path string) slice.Store {
	t.Helper()
	st, err := slice.NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeStore(st) })
	return st
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

func (f *fakeStore) ListAll() ([]*slice.Slice, error) {
	items := make([]*slice.Slice, 0, len(f.items))
	for _, item := range f.items {
		items = append(items, item)
	}
	return items, nil
}

func (f *fakeStore) Delete(id string) error {
	delete(f.items, id)
	return nil
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
		"--base-commit", "abc123",
	}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	if openedPath != dbPath {
		t.Fatalf("opened path = %q, want %q", openedPath, dbPath)
	}
	if extractor.meta.SourceSession != "session-1" || extractor.meta.ProjectSlug != "semantix" || extractor.meta.Language != "zh-CN" || extractor.meta.BaseCommit != "abc123" {
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

func TestExtractDoesNotStoreProjectContextInUserScope(t *testing.T) {
	input := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(input, []byte("{\"role\":\"user\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	extractor := &fakeExtractor{items: []*slice.Slice{
		{ID: "prompt", Type: slice.Prompt, Scope: slice.Project, Content: []byte("question")},
		{ID: "context", Type: slice.Context, Scope: slice.Project, Content: []byte("project paths")},
	}}
	store := newFakeStore()
	deps := dependencies{
		newExtractor: func() slice.Extractor { return extractor },
		openStore:    func(string) (slice.Store, error) { return store, nil },
		newIndex:     func() slice.Index { return bm25.New() },
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"extract", "--input", input, "--scope", "user", "--db", filepath.Join(t.TempDir(), "user.db")}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	if store.items["context"] != nil {
		t.Fatal("project Context slice was stored in user scope")
	}
	if store.items["prompt"] == nil || store.items["prompt"].Scope != slice.User {
		t.Fatalf("prompt slice = %#v, want user scope", store.items["prompt"])
	}
	if !strings.Contains(stdout.String(), "extracted=2 stored=1 scope=user") {
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
	store := openTestStore(t, db)
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

// TestDispatchExitCodeContract locks the U19 exit-code contract
// (docs/reports/cli-v2-architecture.md §4.3) at the dispatch layer:
// 0 ok · 1 runtime error · 2 usage error · 3 gate not met.
func TestDispatchExitCodeContract(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.jsonl")
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"no args → usage", nil, 2},
		{"help → ok", []string{"help"}, 0},
		{"--help → ok", []string{"--help"}, 0},
		{"-h → ok", []string{"-h"}, 0},
		{"unknown command → usage", []string{"nope"}, 2},
		{"extract missing --input → usage", []string{"extract"}, 2},
		{"extract missing input file → runtime", []string{"extract", "--input", missing}, 1},
		{"extract unknown flag → usage", []string{"extract", "--bogus"}, 2},
		{"search missing query → usage", []string{"search"}, 2},
		{"search unknown flag → usage", []string{"search", "--bogus"}, 2},
		{"verify missing --session → usage", []string{"verify"}, 2},
		{"verify unknown flag → usage", []string{"verify", "--bogus"}, 2},
		{"eval missing --set → usage", []string{"eval"}, 2},
		{"eval unknown flag → usage", []string{"eval", "--bogus"}, 2},
		{"eval-judge missing backend → usage", []string{"eval-judge"}, 2},
		{"eval-judge missing audit → runtime", []string{"eval-judge", "--stub", "yes", "--audit", missing}, 1},
		{"usage missing db → runtime", []string{"usage", "--db", filepath.Join(dir, "nope.jsonl")}, 1},
		{"usage unknown flag → usage", []string{"usage", "--bogus"}, 2},
		{"lookup missing query → usage", []string{"lookup"}, 2},
		{"inject missing query → usage", []string{"inject"}, 2},
		{"install missing --target → usage", []string{"install"}, 2},
		{"install invalid target → usage", []string{"install", "--target", "vscode"}, 2},
		{"install missing source → runtime", []string{"install", "--target", "custom", "--dir", filepath.Join(dir, "d"), "--source", filepath.Join(dir, "nope")}, 1},
	}
	for _, c := range cases {
		var stdout, stderr bytes.Buffer
		if got := run(c.args, &stdout, &stderr, productionDependencies()); got != c.want {
			t.Errorf("%s: run(%v) = %d, want %d (stderr %q)", c.name, c.args, got, c.want, stderr.String())
		}
	}
}

// TestEveryCommandHelpExitsZero: per the U19 help contract every registered
// subcommand must accept --help and exit 0.
func TestEveryCommandHelpExitsZero(t *testing.T) {
	for _, cmd := range commands {
		var stdout, stderr bytes.Buffer
		if code := run([]string{cmd.name, "--help"}, &stdout, &stderr, productionDependencies()); code != 0 {
			t.Errorf("%s --help: code = %d, want 0; stderr = %q", cmd.name, code, stderr.String())
		}
	}
}

// TestHelpListsAllCommandsByGroup: `semantix help` must list every
// registered command grouped under the four command-tree branches.
func TestHelpListsAllCommandsByGroup(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help"}, &stdout, &stderr, productionDependencies()); code != 0 {
		t.Fatalf("help: code = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()
	// Service mode was removed in the tool-set minimization (serve/watch were
	// never implemented); only groups with registered commands are shown.
	for _, group := range []string{"Kernel operations", "Product & management", "Maintenance"} {
		if !strings.Contains(out, group) {
			t.Errorf("help missing group %q:\n%s", group, out)
		}
	}
	for _, cmd := range commands {
		if !strings.Contains(out, cmd.name) {
			t.Errorf("help missing command %q:\n%s", cmd.name, out)
		}
	}
}

// TestHelpCommandShowsSynopsis: `semantix help <command>` prints the
// command's synopsis; an unknown command is a usage error.
func TestHelpCommandShowsSynopsis(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help", "extract"}, &stdout, &stderr, productionDependencies()); code != 0 {
		t.Fatalf("help extract: code = %d", code)
	}
	if !strings.Contains(stdout.String(), "semantix extract --input") {
		t.Fatalf("help extract missing synopsis:\n%s", stdout.String())
	}
	var stdout2, stderr2 bytes.Buffer
	if code := run([]string{"help", "nope"}, &stdout2, &stderr2, productionDependencies()); code != 2 {
		t.Fatalf("help nope: code = %d, want 2", code)
	}
}

// TestHelpHasNoEmptyOrPlannedGroups: after the tool-set minimization the
// command tree has no placeholder groups — help must not render an empty
// "Service mode" header or any "(planned: ...)" suffix, and every shown group
// must carry at least one real command.
func TestHelpHasNoEmptyOrPlannedGroups(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help"}, &stdout, &stderr, productionDependencies()); code != 0 {
		t.Fatalf("help: code = %d", code)
	}
	out := stdout.String()
	for _, unwanted := range []string{"Service mode", "(planned:"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("help must not render %q after minimization:\n%s", unwanted, out)
		}
	}
	if !strings.Contains(out, "Maintenance") {
		t.Errorf("Maintenance group (gc) must still be shown:\n%s", out)
	}
}

// TestVerifyGateExitCodeThree: the gate contract (exit 3) survives the
// dispatch layer for verify --strict.
func TestVerifyGateExitCodeThree(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "s1.jsonl", []string{"修复 go 测试失败", "配置 CI"})
	writeSession(t, dir, "s2.jsonl", []string{"修复 go 测试失败", "配置 CI"})
	var stdout, stderr bytes.Buffer
	args := []string{"verify", "--session", dir, "--db", filepath.Join(t.TempDir(), "v.db"),
		"--holdout", "0.5", "--abs-high", "100", "--grey-target", "10", "--strict"}
	if code := run(args, &stdout, &stderr, productionDependencies()); code != 3 {
		t.Fatalf("verify --strict gate: code = %d, want 3; out:\n%s", code, stdout.String())
	}
}
