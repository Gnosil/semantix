package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
)

// distillSession is a minimal transcript in the harness mirror shape: one
// task turn with a paired edit receipt and a passing test run.
const distillSession = `{"role":"user","content":"Issue:\nFix the crash in the Rational constructor.\n"}
{"role":"assistant","content":"on it","tool_calls":[{"id":"c1","name":"edit_file","arguments":{"path":"core/numbers.py"}},{"id":"c2","name":"bash","arguments":{"command":"python -m pytest core/tests -x"}}]}
{"type":"tool","tool_call_id":"c1","name":"edit_file","content":"edited core/numbers.py"}
{"type":"tool","tool_call_id":"c2","name":"bash","content":"3 passed"}
`

func TestExtractDistillFlagAppendsCards(t *testing.T) {
	input := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(input, []byte(distillSession), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore()
	deps := dependencies{
		newExtractor: func() slice.Extractor { return &fakeExtractor{} },
		openStore:    func(string) (slice.Store, error) { return store, nil },
		newIndex:     func() slice.Index { return bm25.New() },
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"extract", "--input", input, "--distill",
		"--db", filepath.Join(t.TempDir(), "slices.db")}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}

	var sawOps, sawOutcome bool
	for _, item := range store.items {
		switch {
		case strings.HasPrefix(string(item.Content), "Repo operations"):
			sawOps = true
		case strings.HasPrefix(string(item.Content), "Task outcome"):
			sawOutcome = true
			if item.Meta.TaskType != slice.TaskBugfix {
				t.Errorf("outcome TaskType = %q, want bugfix", item.Meta.TaskType)
			}
		}
	}
	if !sawOps || !sawOutcome {
		t.Fatalf("distilled cards missing: repo-ops=%v outcome=%v", sawOps, sawOutcome)
	}
}

func TestExtractWithoutDistillFlagStoresNoCards(t *testing.T) {
	input := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(input, []byte(distillSession), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore()
	deps := dependencies{
		newExtractor: func() slice.Extractor { return &fakeExtractor{} },
		openStore:    func(string) (slice.Store, error) { return store, nil },
		newIndex:     func() slice.Index { return bm25.New() },
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"extract", "--input", input,
		"--db", filepath.Join(t.TempDir(), "slices.db")}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	for _, item := range store.items {
		t.Errorf("unexpected stored slice without --distill: %s", item.Content)
	}
}

func TestExtractConsolidateFlagMergesContextCards(t *testing.T) {
	input := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(input, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Two near-duplicate Context cards (same token set, differing counts)
	// already in the library from earlier sessions.
	c1 := &slice.Slice{ID: "ctx-1", Type: slice.Context, Scope: slice.Project,
		Content: []byte("Frequent paths:\n- src/core/numbers.py (3)\n- src/core/tests.py (2)")}
	c2 := &slice.Slice{ID: "ctx-2", Type: slice.Context, Scope: slice.Project,
		Content: []byte("Frequent paths:\n- src/core/numbers.py (4)\n- src/core/tests.py (1)")}
	store := newFakeStore(c1, c2)
	deps := dependencies{
		newExtractor: func() slice.Extractor { return &fakeExtractor{} },
		openStore:    func(string) (slice.Store, error) { return store, nil },
		newIndex:     func() slice.Index { return bm25.New() },
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"extract", "--input", input, "--consolidate",
		"--db", filepath.Join(t.TempDir(), "slices.db")}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "consolidated checked=2 groups=1 merged=1") {
		t.Fatalf("stdout = %q, missing consolidation summary", stdout.String())
	}
	var contexts int
	for _, item := range store.items {
		if item.Type == slice.Context {
			contexts++
		}
	}
	if contexts != 1 {
		t.Fatalf("Context cards after consolidate = %d, want 1 merged card", contexts)
	}
}
