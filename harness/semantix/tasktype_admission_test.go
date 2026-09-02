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
// eligible candidates out.
func TestBridgeNeverInjectsToolPatternOrResult(t *testing.T) {
	dir := t.TempDir()
	seedProjectDB(t, dir,
		&slice.Slice{ID: "t-slice", Type: slice.ToolPattern, Scope: slice.Project,
			Content: []byte("fix the rational constructor crash read_file bash edit_file")},
		&slice.Slice{ID: "r-slice", Type: slice.Result, Scope: slice.Project,
			Content: []byte("fix the rational constructor crash resolved in an earlier session")},
		&slice.Slice{ID: "c-ops", Type: slice.Context, Scope: slice.Project,
			Content: []byte("Repo operations: rational constructor crash test commands")},
	)

	b := NewBridge(Config{Enabled: true, Inject: true, ProjectDir: dir})
	defer b.Close()
	res := b.InjectDetailed(context.Background(), "fix the rational constructor crash")

	for _, id := range []string{"t-slice", "r-slice"} {
		if strings.Contains(res.Text, id) {
			t.Errorf("%s must never inject on the agent path:\n%s", id, res.Text)
		}
	}
	// With T/R excluded from the denominator, the context card is the best
	// eligible candidate and must survive despite the stronger raw scores
	// of the excluded types.
	if !strings.Contains(res.Text, "c-ops") {
		t.Errorf("context card should inject once T/R are out of the denominator:\n%s", res.Text)
	}
}

// The turn's classified task type gates task-tagged Memory cards: a card
// distilled from a different task type never injects, an equally-matching
// same-type card does.
func TestBridgeGatesMemoryCardsByTaskType(t *testing.T) {
	dir := t.TempDir()
	seedProjectDB(t, dir,
		&slice.Slice{ID: "m-bugfix", Type: slice.Memory, Scope: slice.Project,
			Content: []byte("Task outcome (task=bugfix): fix the rational constructor crash\nEdited:\n- core/numbers.py")},
		&slice.Slice{ID: "m-feature", Type: slice.Memory, Scope: slice.Project,
			Content: []byte("Task outcome (task=feature): fix the rational constructor crash\nEdited:\n- core/other.py")},
	)

	b := NewBridge(Config{Enabled: true, Inject: true, ProjectDir: dir})
	defer b.Close()
	// "fix ... crash" classifies as bugfix (slice.ClassifyTask).
	res := b.InjectDetailed(context.Background(), "fix the rational constructor crash")

	if !strings.Contains(res.Text, "m-bugfix") {
		t.Errorf("same-type outcome card missing from the block:\n%s", res.Text)
	}
	if strings.Contains(res.Text, "m-feature") {
		t.Errorf("cross-type outcome card leaked into the block:\n%s", res.Text)
	}
}
