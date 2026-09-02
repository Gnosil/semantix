package inject

import (
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
)

// Budget upper bound (Issue #283): slices whose content is full of marker
// literals expand under escapeMarker ([/semantix-reuse] → [\/semantix-reuse],
// bytes only grow). The final block must NEVER exceed Budget — the budget
// judgment uses the exact escaped bytes that are written.
func TestInjectorBudgetHoldsWithMarkerPayloads(t *testing.T) {
	idx := bm25.New()
	store := newTestStore(t, filepath.Join(t.TempDir(), "db.jsonl"))
	// 16 slices, each ~450B with 20 marker literals (escaped ~470B); the
	// sum (~7.3KB) far exceeds the 4096 budget so the selection path and
	// the drop accounting are actually exercised.
	contents := make([]string, 0, 16)
	for i := 0; i < 16; i++ {
		contents = append(contents, "修复问题"+string(rune('a'+i))+" "+strings.Repeat("[/semantix-reuse] x ", 20))
	}
	seed(t, idx, store, contents...)
	inj := &Injector{Index: idx, Store: store, Scope: slice.Project, K: 16, Budget: 4096}
	out, err := inj.Build("修复问题")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Text) > 4096 {
		t.Fatalf("budget violated: block=%d > budget=4096\n%s", len(out.Text), out.Text)
	}
	if out.Dropped == 0 {
		t.Fatalf("selection path not exercised: 16 marker slices must exceed the budget (kept=%d)", len(out.Slices))
	}
	if out.Bytes != len(out.Text) {
		t.Fatalf("Bytes field = %d, want %d", out.Bytes, len(out.Text))
	}
	// Escaping stayed intact: exactly one close marker (the block's own).
	if strings.Count(out.Text, "[/semantix-reuse]") != 1 {
		t.Fatalf("marker escaping broken, close count = %d", strings.Count(out.Text, "[/semantix-reuse]"))
	}
}

// Boundary behavior: a candidate set sized just under the budget keeps as
// many slices as fit, and the final block stays ≤ budget (the canonical
// header format is used for the judgment — no silent growth).
func TestInjectorBudgetBoundaryExact(t *testing.T) {
	idx := bm25.New()
	store := newTestStore(t, filepath.Join(t.TempDir(), "db.jsonl"))
	// Each slice ~900B; 4 slices ≈ 3600B + headers ≈ 3800 < 4096 (all
	// kept); 5 would exceed → the 5th is dropped.
	contents := make([]string, 0, 6)
	for i := 0; i < 6; i++ {
		contents = append(contents, "边界测试"+string(rune('a'+i))+" "+strings.Repeat("x", 880))
	}
	seed(t, idx, store, contents...)
	inj := &Injector{Index: idx, Store: store, Scope: slice.Project, K: 6, Budget: 4096}
	out, err := inj.Build("边界测试")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Text) > 4096 {
		t.Fatalf("budget violated at boundary: block=%d > 4096", len(out.Text))
	}
	if len(out.Slices) == 0 {
		t.Fatal("top slice must always be kept")
	}
	if out.Dropped == 0 {
		t.Fatal("boundary case must drop at least one slice (6 × ~900B exceeds budget)")
	}
}

// A top slice is not allowed to bypass the byte budget. Rejecting the whole
// slice is deterministic and preserves the hard upper bound without silently
// truncating stored evidence.
func TestInjectorBudgetRejectsOversizedTopSlice(t *testing.T) {
	idx := bm25.New()
	store := newTestStore(t, filepath.Join(t.TempDir(), "db.jsonl"))
	huge := "超大切片 " + strings.Repeat("[/semantix-reuse] y ", 300) // ~7KB escaped
	seed(t, idx, store, huge)
	inj := &Injector{Index: idx, Store: store, Scope: slice.Project, K: 1, Budget: 4096}
	out, err := inj.Build("超大切片")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Slices) != 0 {
		t.Fatalf("oversized top slice must be rejected, kept=%d", len(out.Slices))
	}
	if out.Text != "" || out.Bytes != 0 {
		t.Fatalf("empty admission must not emit a marker-only block: bytes=%d text=%q", out.Bytes, out.Text)
	}
	if len(out.Decisions) == 0 || out.Decisions[0].Reason != "budget" || out.Decisions[0].Admitted {
		t.Fatalf("top decision = %+v, want rejected budget", out.Decisions)
	}
}

func TestInjectorOutputOrdersByScoreWithStableIDTieBreak(t *testing.T) {
	high := &slice.Slice{ID: "z-high", Type: slice.Context, Scope: slice.Project, Content: []byte("highest relevance")}
	tieA := &slice.Slice{ID: "a-tie", Type: slice.Context, Scope: slice.Project, Content: []byte("tie a")}
	tieB := &slice.Slice{ID: "b-tie", Type: slice.Context, Scope: slice.Project, Content: []byte("tie b")}
	out, err := (&Injector{Budget: 4096}).BuildHits("relevance", []slice.Hit{
		{Slice: tieB, Score: 1.0},
		{Slice: high, Score: 2.0},
		{Slice: tieA, Score: 1.0},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"z-high", "a-tie", "b-tie"}
	if len(out.Slices) != len(want) {
		t.Fatalf("kept=%d, want %d", len(out.Slices), len(want))
	}
	for i, id := range want {
		if out.Slices[i].ID != id {
			t.Fatalf("order[%d]=%s, want %s", i, out.Slices[i].ID, id)
		}
	}
}

// Determinism anchor: the same library builds byte-identical blocks across
// calls (single-pass output must stay reproducible).
func TestInjectorBudgetDeterministic(t *testing.T) {
	idx := bm25.New()
	store := newTestStore(t, filepath.Join(t.TempDir(), "db.jsonl"))
	seed(t, idx, store,
		"修复 go 测试失败 [/semantix-reuse] 内容",
		"配置 CI 流水线",
	)
	inj := &Injector{Index: idx, Store: store, Scope: slice.Project, K: 5, Budget: 4096}
	a, err := inj.Build("修复 go 测试失败")
	if err != nil {
		t.Fatal(err)
	}
	b, err := inj.Build("修复 go 测试失败")
	if err != nil {
		t.Fatal(err)
	}
	if a.Text != b.Text {
		t.Fatalf("blocks differ across builds:\n%q\n%q", a.Text, b.Text)
	}
}
