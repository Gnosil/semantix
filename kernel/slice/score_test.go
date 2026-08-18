package slice

import (
	"math"
	"reflect"
	"testing"
)

const day = int64(86400)

func TestComputeWeightBounds(t *testing.T) {
	now := int64(1_700_000_000)
	cases := []struct {
		name string
		s    Slice
		lo   float64
		hi   float64
	}{
		{"cold-start fresh slice", Slice{CreatedAt: now}, 0.10, 0.20},        // decay 1 · freq ~0.14
		{"legacy (no timestamps)", Slice{}, 0.05, 0.10},                      // neutral decay 0.5 · 0.14
		{"heavy use fresh", Slice{CreatedAt: now, Stats: SliceStats{Hits: 50, LastUsed: now}}, 0.85, 1.0},
		{"stale two half-lives", Slice{CreatedAt: now - 60*day, Stats: SliceStats{Hits: 10, LastUsed: now - 60*day}}, 0.15, 0.25},
	}
	for _, c := range cases {
		w := ComputeWeight(&c.s, now, ScoreParams{})
		if w < c.lo || w > c.hi {
			t.Errorf("%s: weight = %.4f, want in [%.2f, %.2f]", c.name, w, c.lo, c.hi)
		}
		if w < minWeight || w > maxWeight {
			t.Errorf("%s: weight %.4f outside global bounds", c.name, w)
		}
	}
}

func TestComputeWeightDecayHalfLife(t *testing.T) {
	now := int64(1_700_000_000)
	fresh := Slice{CreatedAt: now, Stats: SliceStats{Hits: 5, LastUsed: now}}
	aged := fresh
	aged.Stats.LastUsed = now - 30*day
	aged.CreatedAt = now - 30*day
	wf := ComputeWeight(&fresh, now, ScoreParams{})
	wa := ComputeWeight(&aged, now, ScoreParams{})
	if ratio := wa / wf; math.Abs(ratio-0.5) > 0.01 {
		t.Fatalf("one half-life must halve the score: fresh %.4f aged %.4f ratio %.3f", wf, wa, ratio)
	}
}

func TestComputeWeightLastUsedBeatsCreatedAt(t *testing.T) {
	now := int64(1_700_000_000)
	old := Slice{CreatedAt: now - 300*day, Stats: SliceStats{Hits: 3}}
	recentlyUsed := old
	recentlyUsed.Stats.LastUsed = now
	if ComputeWeight(&recentlyUsed, now, ScoreParams{}) <= ComputeWeight(&old, now, ScoreParams{}) {
		t.Fatal("recent use must outweigh old creation age")
	}
}

func TestComputeWeightRejectionMonotone(t *testing.T) {
	now := int64(1_700_000_000)
	prev := math.Inf(1)
	for _, rej := range []uint64{0, 1, 3, 10} {
		s := Slice{CreatedAt: now, Stats: SliceStats{Injected: 5, Rejected: rej, LastUsed: now}}
		w := ComputeWeight(&s, now, ScoreParams{})
		if w >= prev {
			t.Fatalf("rejections must monotonically lower the score: rej=%d w=%.4f prev=%.4f", rej, w, prev)
		}
		prev = w
	}
}

func TestComputeWeightFeedbackClampAndNaN(t *testing.T) {
	now := int64(1_700_000_000)
	base := Slice{CreatedAt: now, Stats: SliceStats{Hits: 5, LastUsed: now}}
	pos, neg, nan := base, base, base
	pos.Stats.UserFeedback = 100  // must clamp at ×2
	neg.Stats.UserFeedback = -100 // must clamp at ×0.25
	nan.Stats.UserFeedback = math.NaN()
	wb := ComputeWeight(&base, now, ScoreParams{})
	if w := ComputeWeight(&pos, now, ScoreParams{}); w > 2*wb+1e-9 || w > maxWeight {
		t.Fatalf("positive feedback uncapped: %.4f (base %.4f)", w, wb)
	}
	if w := ComputeWeight(&neg, now, ScoreParams{}); w < 0.25*wb-1e-9 || w < minWeight {
		t.Fatalf("negative feedback undercapped: %.4f (base %.4f)", w, wb)
	}
	if w := ComputeWeight(&nan, now, ScoreParams{}); math.Abs(w-wb) > 1e-9 {
		t.Fatalf("NaN feedback must be neutral: %.4f vs %.4f", w, wb)
	}
}

func TestRescoreDeterministic(t *testing.T) {
	now := int64(1_700_000_000)
	mk := func() []*Slice {
		return []*Slice{
			{ID: "a", CreatedAt: now - 10*day, Stats: SliceStats{Hits: 2, LastUsed: now - day}},
			{ID: "b"}, // legacy
			{ID: "c", CreatedAt: now, Stats: SliceStats{Injected: 4, Rejected: 1, UserFeedback: 1}},
			nil, // defensive: Rescore must tolerate nil entries
		}
	}
	one, two := mk(), mk()
	n1 := Rescore(one, now, ScoreParams{})
	n2 := Rescore(two, now, ScoreParams{})
	if n1 != n2 || n1 != 3 {
		t.Fatalf("changed counts diverge: %d vs %d (want 3)", n1, n2)
	}
	if !reflect.DeepEqual(one, two) {
		t.Fatal("same input + same now must produce identical weights")
	}
	for _, s := range one {
		if s == nil {
			continue
		}
		if s.Weight == 0 {
			t.Fatalf("%s: rescored weight must clear the ==0 legacy sentinel", s.ID)
		}
	}
	// Idempotent: a second pass changes nothing.
	if n := Rescore(one, now, ScoreParams{}); n != 0 {
		t.Fatalf("second rescore changed %d weights, want 0", n)
	}
}
