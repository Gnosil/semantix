package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
)

func buildLookupStatsDeps(t *testing.T) (dependencies, string) {
	t.Helper()
	db := filepath.Join(t.TempDir(), "library.jsonl")
	deps := dependencies{
		newExtractor: func() slice.Extractor { return nil },
		openStore:    func(path string) (slice.Store, error) { return slice.NewFileStore(path) },
		newIndex:     func() slice.Index { return bm25.New() },
	}
	return deps, db
}

func seedLookupCorpus(t *testing.T, db string) {
	t.Helper()
	st, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	put := func(id, content string) {
		t.Helper()
		if err := st.Put(&slice.Slice{ID: id, Type: slice.Result, Scope: slice.Project,
			Content: []byte(content), Weight: 1.0}); err != nil {
			t.Fatal(err)
		}
	}
	put("target", "gateway streaming passthrough flush done marker termination")
	for _, c := range []string{"alpha filler", "bravo filler", "charlie filler", "delta filler"} {
		put("distractor-"+strings.Fields(c)[0], c)
	}
	if c, ok := st.(interface{ Close() error }); ok {
		_ = c.Close()
	}
}

// A lookup that classifies a result as zone=hit must write Hits+1 and
// LastUsed onto that slice — and only that slice (grey/miss earn nothing).
func TestLookupHitWritesStats(t *testing.T) {
	deps, db := buildLookupStatsDeps(t)
	seedLookupCorpus(t, db)

	var stdout, stderr bytes.Buffer
	code := run([]string{"lookup", "--query", "gateway streaming passthrough flush done marker",
		"--scope", "project", "--db", db, "--json"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("lookup code = %d, stderr %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"zone": "hit"`) {
		t.Fatalf("fixture must produce a hit-zone result:\n%s", stdout.String())
	}

	st, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get("target")
	if got == nil || got.Stats.Hits != 1 || got.Stats.LastUsed == 0 {
		t.Fatalf("hit not recorded on target: %+v", got)
	}
	d, _ := st.Get("distractor-alpha")
	if d == nil || d.Stats.Hits != 0 {
		t.Fatalf("non-hit slice wrongly credited: %+v", d)
	}
}

// inject credits Injected+1 to exactly the slices in the assembled block.
func TestInjectWritesStats(t *testing.T) {
	deps, db := buildLookupStatsDeps(t)
	seedLookupCorpus(t, db)

	var stdout, stderr bytes.Buffer
	code := run([]string{"inject", "--query", "gateway streaming passthrough flush done marker",
		"--scope", "project", "--db", db}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("inject code = %d, stderr %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "[semantix-reuse]") {
		t.Fatalf("no injection block produced:\n%s", stdout.String())
	}

	st, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get("target")
	if got == nil || got.Stats.Injected != 1 || got.Stats.LastUsed == 0 {
		t.Fatalf("injection not recorded on target: %+v", got)
	}
	if got.Stats.Hits != 0 {
		t.Fatalf("inject must not count as lookup hit: %+v", got.Stats)
	}
}
