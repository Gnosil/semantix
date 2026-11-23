package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
)

func TestInjectHandoffPreviewIsExplicitAndReadOnly(t *testing.T) {
	db := filepath.Join(t.TempDir(), "slices.jsonl")
	store := openTestStore(t, db)
	const body = "Task outcome (task=bugfix): Resolve parser regression. Status unresolved.\nEdited:\n- Src/Parser.go\nVerified-by: go test ./Src -run '^TestParser$'"
	if err := store.Put(&slice.Slice{ID: "outcome", Type: slice.Memory, Scope: slice.Project, Content: []byte(body),
		Meta: slice.SliceMeta{Historical: true, Origin: slice.OriginSessionAuto, ProjectSlug: "fixture", SourceSession: "session", BaseCommit: "commit"}}); err != nil {
		t.Fatal(err)
	}
	deps := dependencies{openStore: slice.NewFileStore, newIndex: func() slice.Index { return bm25.New() }}
	args := []string{"--db", db, "--query", "resolve parser regression"}
	var standard, preview, stderr bytes.Buffer
	if err := runInject(args, &standard, &stderr, deps); err != nil {
		t.Fatal(err)
	}
	if err := runInject(append(args, "--preview-handoff"), &preview, &stderr, deps); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(standard.String(), "[semantix-reuse]") || strings.Contains(standard.String(), "Historical handoff preview") {
		t.Fatalf("default changed: %q", standard.String())
	}
	if !strings.Contains(preview.String(), "Historical handoff preview") || !strings.Contains(preview.String(), body) || preview.Len() >= standard.Len() {
		t.Fatalf("want shorter preview with verbatim body; default=%q, preview=%q", standard.String(), preview.String())
	}
	reopened := openTestStore(t, db)
	still, err := reopened.Get("outcome")
	if err != nil || string(still.Content) != body || still.Stats.Hits != 0 || still.Stats.Injected != 0 {
		t.Fatalf("offline preview changed the source or delivery stats: %+v %v", still, err)
	}
}
