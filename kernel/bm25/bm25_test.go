package bm25

import (
	"math"
	"sync"
	"testing"

	"semantix/kernel/slice"
)

func TestSearchRanksMatchingDocument(t *testing.T) {
	ix := New()
	mustInsert(t, ix, &slice.Slice{ID: "relevant", Scope: slice.Project, Content: []byte("prompt cache cache stability")})
	mustInsert(t, ix, &slice.Slice{ID: "unrelated", Scope: slice.Project, Content: []byte("dashboard colors")})

	hits := mustSearch(t, ix, "prompt cache", 5, slice.Project)
	if len(hits) != 1 || hits[0].Slice.ID != "relevant" {
		t.Fatalf("Search() = %#v, want relevant document", hitIDs(hits))
	}
}

func TestSearchRanksCJKDocument(t *testing.T) {
	ix := New()
	mustInsert(t, ix, &slice.Slice{ID: "retrieval", Scope: slice.Project, Content: []byte("使用 BM25 实现中文检索")})
	mustInsert(t, ix, &slice.Slice{ID: "frontend", Scope: slice.Project, Content: []byte("修改网页背景颜色")})

	hits := mustSearch(t, ix, "中文检索", 5, slice.Project)
	if len(hits) == 0 || hits[0].Slice.ID != "retrieval" {
		t.Fatalf("top hit = %#v, want retrieval", hitIDs(hits))
	}
}

func TestSearchFiltersScope(t *testing.T) {
	ix := New()
	mustInsert(t, ix, &slice.Slice{ID: "project", Scope: slice.Project, Content: []byte("project test command")})
	mustInsert(t, ix, &slice.Slice{ID: "user", Scope: slice.User, Content: []byte("user test preference")})

	projectHits := mustSearch(t, ix, "test", 10, slice.Project)
	if got := hitIDs(projectHits); len(got) != 1 || got[0] != "project" {
		t.Fatalf("project Search() = %#v, want [project]", got)
	}
	userHits := mustSearch(t, ix, "test", 10, slice.User)
	if got := hitIDs(userHits); len(got) != 1 || got[0] != "user" {
		t.Fatalf("user Search() = %#v, want [user]", got)
	}
}

func TestInsertReplacesExistingDocument(t *testing.T) {
	ix := New()
	mustInsert(t, ix, &slice.Slice{ID: "doc", Scope: slice.Project, Content: []byte("cache")})
	mustInsert(t, ix, &slice.Slice{ID: "doc", Scope: slice.User, Content: []byte("dashboard")})

	if hits := mustSearch(t, ix, "cache", 10, slice.Project); len(hits) != 0 {
		t.Fatalf("old document still searchable: %#v", hitIDs(hits))
	}
	hits := mustSearch(t, ix, "dashboard", 10, slice.User)
	if got := hitIDs(hits); len(got) != 1 || got[0] != "doc" {
		t.Fatalf("replacement Search() = %#v, want [doc]", got)
	}
}

func TestInsertCopiesInput(t *testing.T) {
	ix := New()
	doc := &slice.Slice{ID: "doc", Scope: slice.Project, Content: []byte("stable cache")}
	mustInsert(t, ix, doc)
	doc.Scope = slice.User
	copy(doc.Content, "broken")

	hits := mustSearch(t, ix, "stable", 10, slice.Project)
	if len(hits) != 1 || string(hits[0].Slice.Content) != "stable cache" {
		t.Fatalf("indexed document changed through caller mutation: %#v", hits)
	}
}

func TestRemoveIsCompleteAndIdempotent(t *testing.T) {
	ix := New()
	mustInsert(t, ix, &slice.Slice{ID: "doc", Scope: slice.Project, Content: []byte("cache cache")})
	if err := ix.Remove("doc"); err != nil {
		t.Fatal(err)
	}
	if err := ix.Remove("doc"); err != nil {
		t.Fatalf("second Remove() error = %v", err)
	}
	if hits := mustSearch(t, ix, "cache", 10, slice.Project); len(hits) != 0 {
		t.Fatalf("removed document still searchable: %#v", hitIDs(hits))
	}
}

func TestSearchLimitsAndStablySortsEqualScores(t *testing.T) {
	ix := New()
	for _, id := range []string{"c", "a", "b"} {
		mustInsert(t, ix, &slice.Slice{ID: id, Scope: slice.Project, Content: []byte("same content")})
	}

	hits := mustSearch(t, ix, "same", 2, slice.Project)
	got := hitIDs(hits)
	want := []string{"a", "b"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Search() = %#v, want %#v", got, want)
	}
}

func TestSearchUsesScopeLocalStatistics(t *testing.T) {
	ix := New()
	mustInsert(t, ix, &slice.Slice{ID: "short", Scope: slice.Project, Content: []byte("cache")})
	mustInsert(t, ix, &slice.Slice{ID: "long", Scope: slice.Project, Content: []byte("cache filler filler filler filler")})
	for i := 0; i < 20; i++ {
		mustInsert(t, ix, &slice.Slice{ID: string(rune('A' + i)), Scope: slice.User, Content: []byte("cache user")})
	}

	hits := mustSearch(t, ix, "cache", 10, slice.Project)
	if len(hits) != 2 || hits[0].Slice.ID != "short" {
		t.Fatalf("Search() = %#v, want short document first", hitIDs(hits))
	}

	wantIDF := math.Log(1 + (2.0-2.0+0.5)/(2.0+0.5))
	wantScore := wantIDF * (1 * (defaultK1 + 1)) / (1 + defaultK1*(1-defaultB+defaultB*1/3))
	if math.Abs(hits[0].Score-wantScore) > 1e-12 {
		t.Fatalf("score = %.12f, want %.12f", hits[0].Score, wantScore)
	}
}

func TestInvalidInputs(t *testing.T) {
	ix := New()
	if err := ix.Insert(nil); err == nil {
		t.Fatal("Insert(nil) succeeded, want error")
	}
	if err := ix.Insert(&slice.Slice{Content: []byte("content")}); err == nil {
		t.Fatal("Insert(empty ID) succeeded, want error")
	}
	if err := ix.Remove(""); err == nil {
		t.Fatal("Remove(empty ID) succeeded, want error")
	}
	if _, err := ix.Search("!!!", 5, slice.Project); err == nil {
		t.Fatal("Search(punctuation) succeeded, want error")
	}
	if hits, err := ix.Search("valid", 0, slice.Project); err != nil || len(hits) != 0 {
		t.Fatalf("Search(k=0) = %#v, %v; want empty result", hits, err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	ix := New()
	const workers = 20
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		id := string(rune('a' + i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 50; n++ {
				if err := ix.Insert(&slice.Slice{ID: id, Scope: slice.Project, Content: []byte("concurrent cache")}); err != nil {
					t.Errorf("Insert() error = %v", err)
					return
				}
				if _, err := ix.Search("cache", 5, slice.Project); err != nil {
					t.Errorf("Search() error = %v", err)
					return
				}
			}
			if err := ix.Remove(id); err != nil {
				t.Errorf("Remove() error = %v", err)
			}
		}()
	}
	wg.Wait()
}

func mustInsert(t *testing.T, ix *Index, s *slice.Slice) {
	t.Helper()
	if err := ix.Insert(s); err != nil {
		t.Fatalf("Insert(%q) error = %v", s.ID, err)
	}
}

func mustSearch(t *testing.T, ix *Index, query string, k int, scope slice.Scope) []slice.Hit {
	t.Helper()
	hits, err := ix.Search(query, k, scope)
	if err != nil {
		t.Fatalf("Search(%q) error = %v", query, err)
	}
	return hits
}

func hitIDs(hits []slice.Hit) []string {
	ids := make([]string, len(hits))
	for i, hit := range hits {
		ids[i] = hit.Slice.ID
	}
	return ids
}
