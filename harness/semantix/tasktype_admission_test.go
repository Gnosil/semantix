package semantix

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

// seedProjectDB writes slices into <dir>/.semantix/project.db, the store the
// bridge's in-process injection path reads.
func seedProjectDB(t *testing.T, dir string, slices ...*slice.Slice) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".semantix"), 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := slice.NewFileStore(filepath.Join(dir, ".semantix", "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeSliceStore(store)
	for _, s := range slices {
		if err := store.Put(s); err != nil {
			t.Fatal(err)
		}
	}
}

// Four-layer distill spec §2.5: tool_pattern and result slices never reach
// the agent injection block, however strong their lexical match — and their
// scores must not act as the relative-confidence denominator that squeezes
// eligible candidates out. The fixture mirrors admissionFixtureSlices: five
// slices, two source sessions per admitted type, a strong match plus a
// runner-up so the strict gates (#454) are satisfied.
func TestBridgeNeverInjectsToolPatternOrResult(t *testing.T) {
	dir := t.TempDir()
	seedProjectDB(t, dir,
		&slice.Slice{ID: "t-blocked", Type: slice.ToolPattern, Scope: slice.Project,
			Content: []byte("修复 go 测试失败"), Meta: slice.SliceMeta{SourceSession: "boot-3"}},
		&slice.Slice{ID: "r-blocked", Type: slice.Result, Scope: slice.Project,
			Content: []byte("修复 go 测试失败"), Meta: slice.SliceMeta{SourceSession: "boot-4"}},
		&slice.Slice{ID: "ctx-strong", Type: slice.Context, Scope: slice.Project,
			Content: []byte("修复 go 测试失败"), Meta: slice.SliceMeta{SourceSession: "boot-1"}},
		&slice.Slice{ID: "ctx-runner", Type: slice.Context, Scope: slice.Project,
			Content: []byte("修复 release process"), Meta: slice.SliceMeta{SourceSession: "boot-2"}},
		&slice.Slice{ID: "mem-pad", Type: slice.Memory, Scope: slice.Project,
			Content: []byte("部署 kubernetes 服务"), Meta: slice.SliceMeta{SourceSession: "boot-1"}},
	)

	b := NewBridge(Config{Enabled: true, Inject: true, ProjectDir: dir})
	defer b.Close()
	res := b.InjectDetailed(context.Background(), "修复 go 测试")

	for _, id := range []string{"t-blocked", "r-blocked"} {
		if strings.Contains(res.Text, id) {
			t.Errorf("%s must never inject on the agent path:\n%s", id, res.Text)
		}
	}
	// With T/R excluded at the source (strictAllowedTypes), the context card
	// is the best eligible candidate and must survive despite the equally
	// strong raw scores of the excluded types.
	if !strings.Contains(res.Text, "ctx-strong") {
		t.Errorf("context card should inject once T/R are out of the way:\n%s", res.Text)
	}
}

// The turn's classified task type gates task-tagged Memory cards: a card
// distilled from a different task type never injects, an equally-matching
// same-type card does — and the rejected card must not act as the
// relative-confidence denominator or the runner-up.
func TestBridgeGatesMemoryCardsByTaskType(t *testing.T) {
	dir := t.TempDir()
	seedProjectDB(t, dir,
		&slice.Slice{ID: "m-bugfix", Type: slice.Memory, Scope: slice.Project,
			Content: []byte("Task outcome (task=bugfix): 修复 go 测试失败\nEdited:\n- core/numbers.py"),
			Meta:    slice.SliceMeta{SourceSession: "boot-1"}},
		&slice.Slice{ID: "m-feature", Type: slice.Memory, Scope: slice.Project,
			Content: []byte("Task outcome (task=feature): 修复 go 测试失败\nEdited:\n- core/other.py"),
			Meta:    slice.SliceMeta{SourceSession: "boot-2"}},
		&slice.Slice{ID: "ctx-runner", Type: slice.Context, Scope: slice.Project,
			Content: []byte("修复 release process"), Meta: slice.SliceMeta{SourceSession: "boot-1"}},
		&slice.Slice{ID: "ctx-pad", Type: slice.Context, Scope: slice.Project,
			Content: []byte("部署流程记录"), Meta: slice.SliceMeta{SourceSession: "boot-2"}},
		&slice.Slice{ID: "p-pad", Type: slice.Prompt, Scope: slice.Project,
			Content: []byte("修复 go 测试失败问题排查"), Meta: slice.SliceMeta{SourceSession: "boot-1"}},
	)

	b := NewBridge(Config{Enabled: true, Inject: true, ProjectDir: dir})
	defer b.Close()
	// "修复 go 测试" classifies as bugfix (slice.ClassifyTask).
	res := b.InjectDetailed(context.Background(), "修复 go 测试")

	if !strings.Contains(res.Text, "m-bugfix") {
		t.Errorf("same-type outcome card missing from the block:\n%s", res.Text)
	}
	if strings.Contains(res.Text, "m-feature") {
		t.Errorf("cross-type outcome card leaked into the block:\n%s", res.Text)
	}
}
