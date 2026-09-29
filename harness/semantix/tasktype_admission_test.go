package semantix

import (
	"context"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

// Raw tool_pattern and unverified result slices never reach
// the agent injection block, however strong their lexical match — and their
// scores must not act as the relative-confidence denominator that squeezes
// eligible candidates out. Verified Results are covered separately.
func TestBridgeNeverInjectsToolPatternOrUnverifiedResult(t *testing.T) {
	dir := writeKernelDir(t, []*slice.Slice{
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
	}, nil)

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

// The strict default admits Context cards only (issue #508 R1/R2: the only
// arm with a paired net win over memory-off in both runs). Memory and Result
// candidates stay diagnostic, carrying the stable reason shadow replay and
// the admission policy both promise.
func TestBridgeDefaultAdmitsContextOnly(t *testing.T) {
	dir := writeKernelDir(t, []*slice.Slice{
		&slice.Slice{ID: "ctx-strong", Type: slice.Context, Scope: slice.Project,
			Content: []byte("修复 go 测试失败"), Meta: slice.SliceMeta{SourceSession: "boot-1"}},
		&slice.Slice{ID: "m-blocked", Type: slice.Memory, Scope: slice.Project,
			Content: []byte("修复 go 测试失败"), Meta: slice.SliceMeta{SourceSession: "boot-1"}},
		&slice.Slice{ID: "r-blocked", Type: slice.Result, Scope: slice.Project,
			Content: []byte("修复 go 测试失败"), Meta: slice.SliceMeta{SourceSession: "boot-4"}},
	}, nil)

	b := NewBridge(Config{Enabled: true, Inject: true, ProjectDir: dir})
	defer b.Close()
	res := b.InjectDetailed(context.Background(), "修复 go 测试")

	if !strings.Contains(res.Text, "ctx-strong") {
		t.Errorf("context card should inject under the default allowlist:\n%s", res.Text)
	}
	reasons := candidateReasons(res)
	for _, id := range []string{"m-blocked", "r-blocked"} {
		if strings.Contains(res.Text, id) {
			t.Errorf("%s must not inject under the Context-only default:\n%s", id, res.Text)
		}
		if reasons[id] != "type_not_allowed" {
			t.Errorf("%s reason = %q, want type_not_allowed", id, reasons[id])
		}
	}
}

// A typo in admission_types must fall back to the Context-only default, so
// a bad config can never widen admission beyond it.
func TestBridgeAdmissionTypesTypoFallsBackToDefault(t *testing.T) {
	dir := writeKernelDir(t, []*slice.Slice{
		&slice.Slice{ID: "ctx-strong", Type: slice.Context, Scope: slice.Project,
			Content: []byte("修复 go 测试失败"), Meta: slice.SliceMeta{SourceSession: "boot-1"}},
		&slice.Slice{ID: "m-blocked", Type: slice.Memory, Scope: slice.Project,
			Content: []byte("修复 go 测试失败"), Meta: slice.SliceMeta{SourceSession: "boot-1"}},
	}, nil)

	b := NewBridge(Config{Enabled: true, Inject: true, ProjectDir: dir,
		AdmissionTypes: []string{"conetxt"}})
	defer b.Close()
	res := b.InjectDetailed(context.Background(), "修复 go 测试")

	if !strings.Contains(res.Text, "ctx-strong") {
		t.Errorf("typo'd list must keep the Context-only default:\n%s", res.Text)
	}
	if strings.Contains(res.Text, "m-blocked") {
		t.Errorf("typo'd list must not admit memory slices:\n%s", res.Text)
	}
}

// Task labels describe source actions, not whether equally matching history
// is useful for the current problem. Keep the original tags for the reader.
// Memory stays injectable behind explicit admission_types config — the S6
// task-label semantics must survive the Context-only default unchanged.
func TestBridgeKeepsRelatedMemoryAcrossTaskTypes(t *testing.T) {
	dir := writeKernelDir(t, []*slice.Slice{
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
	}, nil)

	b := NewBridge(Config{Enabled: true, Inject: true, ProjectDir: dir,
		AdmissionTypes: []string{"context", "memory"}})
	defer b.Close()
	// "修复 go 测试" classifies as bugfix (slice.ClassifyTask).
	res := b.InjectDetailed(context.Background(), "修复 go 测试")

	if !strings.Contains(res.Text, "m-bugfix") {
		t.Errorf("same-type outcome card missing from the block:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "m-feature") {
		t.Errorf("equally matching cross-type history missing from the block:\n%s", res.Text)
	}
	for _, marker := range []string{"task=bugfix", "task=feature", `source="boot-1"`, `source="boot-2"`} {
		if !strings.Contains(res.Text, marker) {
			t.Errorf("source task/provenance %q missing from the block", marker)
		}
	}
}

func candidateReasons(res InjectResult) map[string]string {
	reasons := map[string]string{}
	if res.Diagnostics == nil {
		return reasons
	}
	for _, c := range res.Diagnostics.Candidates {
		reasons[c.ID] = c.Reason
	}
	return reasons
}
