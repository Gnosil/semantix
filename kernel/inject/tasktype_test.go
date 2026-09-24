package inject

import (
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
)

func seedTyped(t *testing.T, idx slice.Index, id string, typ slice.SliceType, content string) {
	t.Helper()
	sl := &slice.Slice{ID: id, Type: typ, Scope: slice.Project, Content: []byte(content)}
	if err := idx.Insert(sl); err != nil {
		t.Fatal(err)
	}
}

func TestTaskTagOf(t *testing.T) {
	cases := []struct {
		content, want string
	}{
		{"Plan skeleton (task=bugfix):\nlocate → edit", "bugfix"},
		{"Task outcome (task=test-update): summary here\nEdited:", "test-update"},
		{"Plan skeleton (task=bugfix", "bugfix"}, // head-line EOF, no delimiter
		{"no tag at all", ""},
		{"second line only\nPlan skeleton (task=bugfix):", ""}, // tag must be in the head line
	}
	for _, c := range cases {
		if got := taskTagOf(c.content); got != c.want {
			t.Errorf("taskTagOf(%q) = %q, want %q", c.content, got, c.want)
		}
	}
}

func TestTaskTypeGatesTaggedMemoryCards(t *testing.T) {
	idx := bm25.New()
	// Both cards match the query lexically; only the same-type card may
	// enter the block when TaskType is set.
	seedTyped(t, idx, "m-bugfix", slice.Memory,
		"Task outcome (task=bugfix): rational constructor bug\nEdited:\n- sympy/core/numbers.py")
	seedTyped(t, idx, "m-feature", slice.Memory,
		"Task outcome (task=feature): rational constructor bug support\nEdited:\n- sympy/core/other.py")

	inj, err := (&Injector{Index: idx, Scope: slice.Project, K: 5, TaskType: "bugfix"}).
		Build("rational constructor bug")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inj.Text, "m-bugfix") {
		t.Errorf("same-type card missing from block:\n%s", inj.Text)
	}
	if strings.Contains(inj.Text, "m-feature") {
		t.Errorf("cross-type card leaked into block:\n%s", inj.Text)
	}
	if inj.Dropped != 1 {
		t.Errorf("Dropped = %d, want 1 (the cross-type card)", inj.Dropped)
	}
}

func TestTaskTypeLeavesUntaggedAndOtherTypesAlone(t *testing.T) {
	idx := bm25.New()
	seedTyped(t, idx, "m-legacy", slice.Memory, "legacy memory note about rational bug")
	seedTyped(t, idx, "c-ops", slice.Context, "Repo operations: rational bug commands")

	inj, err := (&Injector{Index: idx, Scope: slice.Project, K: 5, TaskType: "feature"}).
		Build("rational bug")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"m-legacy", "c-ops"} {
		if !strings.Contains(inj.Text, id) {
			t.Errorf("%s should be unaffected by TaskType gating:\n%s", id, inj.Text)
		}
	}
}

func TestEmptyTaskTypeKeepsHistoricalBehavior(t *testing.T) {
	idx := bm25.New()
	seedTyped(t, idx, "m-bugfix", slice.Memory,
		"Task outcome (task=bugfix): rational constructor bug\nEdited:\n- a.py")

	inj, err := (&Injector{Index: idx, Scope: slice.Project, K: 5}).Build("rational constructor bug")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inj.Text, "m-bugfix") {
		t.Errorf("empty TaskType must not gate anything:\n%s", inj.Text)
	}
}
