package inject

import (
	"strings"
	"testing"

	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

// This is a synthetic admission replay, not a model or task-quality benchmark.
// The script selects the first admitted configuration value; in a real task
// the user prompt, repository, tool evidence and model may resolve conflicts
// differently. "current" means the unversioned Codex-style admission call;
// it does not describe harnesses that already pass revision information.
func TestMemoryShadowReplay(t *testing.T) {
	const query = "orchard worker routing configuration"
	const required = "worker_route_v2"
	const retired = "worker_route_v1"
	const revision = "current-revision"

	candidate := func(id, value, commit string, score float64) slice.Hit {
		return slice.Hit{Score: score, Slice: &slice.Slice{
			ID: id, Type: slice.Context, Scope: slice.Project,
			Content: []byte(query + " = " + value),
			Meta:    slice.SliceMeta{BaseCommit: commit, Origin: slice.OriginSessionAuto},
		}}
	}
	same := candidate("same-revision", required, revision, 2)
	old := candidate("archived", retired, "old-revision", 3)
	legacy := candidate("unknown-revision", required, "", 2)

	for _, task := range []struct {
		name     string
		baseline string
		hits     []slice.Hit
		want     [3]bool
	}{
		{"protect_already_passing", required, []slice.Hit{old, same}, [3]bool{true, false, true}},
		{"recover_with_current_evidence", "", []slice.Hit{same}, [3]bool{false, true, true}},
		{"lose_useful_untagged_history", "", []slice.Hit{legacy}, [3]bool{false, true, false}},
	} {
		t.Run(task.name, func(t *testing.T) {
			for i, arm := range []string{"off", "current", "revision_aware"} {
				t.Run(arm, func(t *testing.T) {
					value := task.baseline
					if arm != "off" {
						z := zone.Default()
						in := Injector{K: 5, Zones: &z, MinOrigin: slice.OriginSessionAuto}
						if arm == "revision_aware" {
							in.CurrentCommit = revision
						}
						out, err := in.BuildHits(query, task.hits)
						if err != nil {
							t.Fatal(err)
						}
						if arm == "revision_aware" && strings.Contains(out.Text, retired) {
							t.Fatalf("retired current configuration entered the revision-aware block")
						}
						if len(out.Slices) > 0 {
							_, value, _ = strings.Cut(string(out.Slices[0].Content), " = ")
						}
					}
					if got := value == required; got != task.want[i] {
						t.Errorf("scripted result = %t, want %t", got, task.want[i])
					}
				})
			}
		})
	}
}
