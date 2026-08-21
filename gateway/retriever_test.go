package gateway

import (
	"testing"

	"semantix/kernel/slice"
)

// seedRetriever inserts slices into any slice.Index (bm25 / vector / hybrid).
func seedRetriever(t *testing.T, idx slice.Index, slices ...*slice.Slice) {
	t.Helper()
	for _, s := range slices {
		if err := idx.Insert(s); err != nil {
			t.Fatalf("Insert(%s): %v", s.ID, err)
		}
	}
}

func projectSlice(id, content string) *slice.Slice {
	return &slice.Slice{ID: id, Type: slice.Result, Scope: slice.Project, Content: []byte(content)}
}

func TestNewRetrieverKindsSearch(t *testing.T) {
	for _, kind := range []string{"bm25", "vector", "hybrid"} {
		t.Run(kind, func(t *testing.T) {
			idx := newRetriever(kind, 0, nil, "", 0)
			seedRetriever(t, idx, projectSlice("a", "deploy golang binary to linux server with systemd"))
			got, err := idx.Search("deploy golang binary linux", 5, slice.Project)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) == 0 {
				t.Fatalf("%s: expected a hit for a matching query", kind)
			}
			if got[0].Slice.ID != "a" {
				t.Fatalf("%s: top hit = %s, want a", kind, got[0].Slice.ID)
			}
		})
	}
}

func TestNewRetrieverDefaultFallsBackToBM25(t *testing.T) {
	idx := newRetriever("", 0, nil, "", 0)
	seedRetriever(t, idx, projectSlice("a", "hello world"))
	got, err := idx.Search("hello", 5, slice.Project)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Slice.ID != "a" {
		t.Fatalf("bm25 fallback search = %+v, want 1 hit", got)
	}
}

func TestVectorIndexScopesFilter(t *testing.T) {
	idx := newRetriever("vector", 0, nil, "", 0)
	seedRetriever(t, idx,
		projectSlice("proj", "deploy golang binary to linux server"),
		&slice.Slice{ID: "sess", Type: slice.Result, Scope: slice.Session, Content: []byte("deploy golang binary to linux server")},
	)
	got, err := idx.Search("deploy golang binary linux", 5, slice.Project)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Slice.ID != "proj" {
		t.Fatalf("project-scope search = %+v, want only proj slice", got)
	}
}

func TestVectorIndexRanksSimilarAboveUnrelated(t *testing.T) {
	idx := newRetriever("vector", 0, nil, "", 0)
	seedRetriever(t, idx,
		projectSlice("rel", "deploy golang binary to linux server with systemd"),
		projectSlice("un", "how to cook rice in a rice cooker"),
	)
	got, err := idx.Search("deploy golang binary linux systemd", 5, slice.Project)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("expected both slices, got %d", len(got))
	}
	if got[0].Slice.ID != "rel" {
		t.Fatalf("top hit = %s, want rel (similar), scores %v", got[0].Slice.ID, scoresOf(got))
	}
	if got[0].Score <= got[1].Score {
		t.Fatalf("similar score %v must exceed unrelated %v", got[0].Score, got[1].Score)
	}
}

func TestHybridScoresStayOnBoundedScale(t *testing.T) {
	idx := newRetriever("hybrid", 0, nil, "", 0)
	seedRetriever(t, idx,
		projectSlice("a", "deploy golang binary to linux server with systemd"),
		projectSlice("b", "how to cook rice in a rice cooker"),
	)
	got, err := idx.Search("deploy golang binary linux systemd", 5, slice.Project)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("hybrid search returned nothing")
	}
	for _, h := range got {
		if h.Score <= 0 || h.Score > 1 {
			t.Fatalf("fused score %v out of bounded (0,1] scale", h.Score)
		}
	}
	// The top fused score must be 1.0: the best candidate is top-1 on both
	// routes, so both normalized halves sum to 1.
	if got[0].Score != 1.0 {
		t.Fatalf("top fused score = %v, want 1.0 (both routes agree)", got[0].Score)
	}
}

func TestRetrieverEmptyQueryDoesNotPanic(t *testing.T) {
	for _, kind := range []string{"vector", "hybrid"} {
		idx := newRetriever(kind, 0, nil, "", 0)
		seedRetriever(t, idx, projectSlice("a", "some content here"))
		_, err := idx.Search("", 5, slice.Project)
		if err != nil {
			t.Fatalf("%s empty-query search: %v", kind, err)
		}
	}
}

func scoresOf(hits []slice.Hit) []float64 {
	out := make([]float64, len(hits))
	for i, h := range hits {
		out[i] = h.Score
	}
	return out
}

func TestFuseHitsCarriesLexicalRoute(t *testing.T) {
	bm := []slice.Hit{
		{Slice: projectSlice("a", "x"), Score: 5.0},
		{Slice: projectSlice("b", "y"), Score: 3.0},
	}
	vec := []slice.Hit{
		{Slice: projectSlice("a", "x"), Score: 0.9},
		{Slice: projectSlice("b", "y"), Score: 0.8},
		{Slice: projectSlice("c", "z"), Score: 0.7}, // pure-vector hit
	}
	got := fuseHits(bm, vec, 10)
	byID := map[string]slice.Hit{}
	for _, h := range got {
		byID[h.Slice.ID] = h
	}
	if len(byID) != 3 {
		t.Fatalf("fuseHits = %d candidates, want 3", len(byID))
	}
	if a := byID["a"]; !approx(a.Lexical, 1.0) || !a.LexicalValid {
		t.Fatalf("a: lexical=%v valid=%v, want 1.0/true (top-1 both routes)", a.Lexical, a.LexicalValid)
	}
	if b := byID["b"]; !approx(b.Lexical, 0.6) || !b.LexicalValid {
		t.Fatalf("b: lexical=%v valid=%v, want 0.6/true (normalized bm route)", b.Lexical, b.LexicalValid)
	}
	if c := byID["c"]; !approx(c.Lexical, 0.0) || !c.LexicalValid {
		t.Fatalf("c: lexical=%v valid=%v, want 0.0/true (pure-vector hit)", c.Lexical, c.LexicalValid)
	}
}

func TestVectorIndexFillsLexicalCoverage(t *testing.T) {
	idx := newRetriever("vector", 0, nil, "", 0)
	seedRetriever(t, idx, projectSlice("a", "deploy golang binary to linux server with systemd"))
	got, err := idx.Search("deploy golang binary linux", 5, slice.Project)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || !got[0].LexicalValid {
		t.Fatalf("vector hit must carry LexicalValid, got %+v", got)
	}
	if !approx(got[0].Lexical, 1.0) {
		t.Fatalf("lexical coverage = %v, want 1.0 (all query terms present)", got[0].Lexical)
	}
}

func TestBM25HitsCarryLexicalSupport(t *testing.T) {
	idx := newRetriever("bm25", 0, nil, "", 0)
	seedRetriever(t, idx, projectSlice("a", "deploy golang binary to linux server"))
	got, err := idx.Search("deploy golang", 5, slice.Project)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Lexical != 1.0 || !got[0].LexicalValid {
		t.Fatalf("bm25 hit must carry lexical=1/valid, got %+v", got)
	}
}

func approx(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}
