package bm25

import (
	"testing"

	"semantix/kernel/slice"
)

func TestInsertSkipsTokenlessContent(t *testing.T) {
	ix := New()
	mustInsert(t, ix, &slice.Slice{ID: "valid", Scope: slice.User, Content: []byte("正常中文视频渲染")})
	before := mustSearch(t, ix, "视频渲染", 5, slice.User)
	for _, content := range []string{"", " \n\t", "? ? ?", "!!!", "\ufffd\ufffd", "🎬🔧"} {
		if err := ix.Insert(&slice.Slice{ID: "bad", Scope: slice.User, Content: []byte(content)}); err != nil {
			t.Fatalf("tokenless content %q poisoned indexing: %v", content, err)
		}
	}
	after := mustSearch(t, ix, "视频渲染", 5, slice.User)
	if len(before) != 1 || len(after) != 1 || after[0].Slice.ID != "valid" || after[0].Score != before[0].Score {
		t.Fatalf("tokenless rows changed valid retrieval: before=%+v after=%+v", before, after)
	}
	// Replacing an indexed row with unsearchable content must not leave stale terms.
	if err := ix.Insert(&slice.Slice{ID: "valid", Scope: slice.User, Content: []byte("???")}); err != nil {
		t.Fatal(err)
	}
	if hits := mustSearch(t, ix, "视频渲染", 5, slice.User); len(hits) != 0 {
		t.Fatalf("stale indexed content survived replacement: %+v", hits)
	}
}
