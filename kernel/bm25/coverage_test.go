package bm25

import (
	"fmt"
	"testing"

	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

func TestCoverageRejectsBestOfUnrelatedBatch(t *testing.T) {
	ix := New()
	mustInsert(t, ix, &slice.Slice{ID: "partial", Scope: slice.Project, Content: []byte("计算机设计")})
	// Increase IDF so even the unrelated top result exceeds the old floors.
	for i := 0; i < 100; i++ {
		mustInsert(t, ix, &slice.Slice{ID: fmt.Sprint(i), Scope: slice.Project, Content: []byte("web server")})
	}
	hits := mustSearch(t, ix, "量子计算机纠错码最小距离", 5, slice.Project)
	if len(hits) != 1 || hits[0].Score <= zone.Default().AbsHigh {
		t.Fatalf("expected large raw score for partial match, got %+v", hits)
	}
	hit := hits[0]
	if !hit.QueryCoverageValid || hit.QueryCoverage != 3.0/12 {
		t.Fatalf("coverage = %v (valid %v), want 3/12", hit.QueryCoverage, hit.QueryCoverageValid)
	}
	if got := zone.Default().ClassifyHit(hit, hit.Score); got != zone.Miss {
		t.Fatalf("unrelated top1 = %v, want Miss", got)
	}
	for _, query := range []string{"计算机设计", "计算机设计设计"} {
		hit := mustSearch(t, ix, query, 1, slice.Project)[0]
		if got := zone.Default().ClassifyHit(hit, hit.Score); got != zone.Hit {
			t.Fatalf("matching query %q = %v, want Hit", query, got)
		}
	}
}
