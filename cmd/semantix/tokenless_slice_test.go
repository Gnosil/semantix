package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

func TestLegacyTokenlessSliceDoesNotPoisonCommands(t *testing.T) {
	db := filepath.Join(t.TempDir(), "legacy.db")
	store := openTestStore(t, db)
	for _, sl := range []*slice.Slice{
		{ID: "bad", Type: slice.Context, Scope: slice.User, Content: []byte("? ? ?")},
		{ID: "good", Type: slice.Context, Scope: slice.User, Content: []byte("正常中文切片关于视频渲染"), Meta: slice.SliceMeta{Origin: slice.OriginSessionAuto}},
	} {
		if err := store.Put(sl); err != nil {
			t.Fatal(err)
		}
	}
	closeStore(store)
	for _, args := range [][]string{
		{"inject", "--db", db, "--scope", "user", "--query", "视频渲染"},
		{"search", "--db", db, "--scope", "user", "--retriever", "bm25", "--query", "视频渲染"},
		{"search", "--db", db, "--scope", "user", "--retriever", "hybrid", "--query", "视频渲染"},
		{"search", "--db", db, "--scope", "user", "--retriever", "bm25", "--query", "unrelated"},
	} {
		var out, stderr bytes.Buffer
		if code := run(args, &out, &stderr, productionDependencies()); code != 0 {
			t.Fatalf("%v: exit=%d stderr=%s", args, code, stderr.String())
		}
		if args[len(args)-1] != "unrelated" && !strings.Contains(out.String(), "视频渲染") {
			t.Fatalf("%v lost the valid Chinese slice: %s", args, out.String())
		}
	}
	// Recovery only changes the in-memory index; the user's stored rows stay intact.
	store = openTestStore(t, db)
	if bad, err := store.Get("bad"); err != nil || bad == nil || string(bad.Content) != "? ? ?" {
		t.Fatalf("legacy store was rewritten: bad=%+v err=%v", bad, err)
	}
}
