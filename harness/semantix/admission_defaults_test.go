package semantix

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

// These are retrieval regressions, not evidence of model adoption or benefit.
// Library size, source counts and competing cards do not determine whether a
// matching piece of current-repository history may reach the original zone filter.
func TestBridgeAdmissionWithoutExtraGates(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		contents    []string
		sameSource  bool
		fillers     int
		want        int
	}{
		{"singleton", "repair parser regression", []string{"repair parser regression"}, false, 0, 1},
		{"single_source", "repair parser regression", []string{"repair parser regression", "repair logging conventions"}, true, 3, 1},
		{"no_runner_up", "repair parser regression", []string{"repair parser regression", "account permissions"}, false, 3, 1},
		{"corroborating_ties", "repair parser regression", []string{"repair parser regression", "repair parser regression"}, false, 3, 2},
		{"low_absolute_score", "parser", []string{"parser", "parser"}, false, 0, 2},
		{"long_query", "repair parser regression involving nested expressions arithmetic precedence parentheses unicode tokens whitespace comments literals escape sequences operators identifiers", []string{"repair parser regression", "repair logging conventions"}, false, 3, 1},
		{"no_overlap", "account permissions", []string{"repair parser regression"}, false, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var items []*slice.Slice
			for i, content := range tc.contents {
				source := fmt.Sprintf("history-%d", i)
				if tc.sameSource {
					source = "history-0"
				}
				items = append(items, &slice.Slice{ID: fmt.Sprintf("ctx-%d", i), Type: slice.Context, Scope: slice.Project,
					Content: []byte(content), Meta: slice.SliceMeta{SourceSession: source}})
			}
			for i := 0; i < tc.fillers; i++ {
				items = append(items, &slice.Slice{ID: fmt.Sprintf("other-%d", i), Type: slice.Prompt, Scope: slice.Project, Content: []byte("deployment configuration")})
			}
			b := NewBridge(Config{Enabled: true, Mode: "strict", ProjectDir: writeKernelDir(t, items, nil)})
			defer b.Close()
			got := b.InjectDetailed(context.Background(), tc.query)
			if got.Diagnostics == nil {
				t.Fatal("missing admission diagnostics")
			}
			for _, d := range got.Diagnostics.Candidates {
				t.Logf("id=%s score=%.6f coverage=%.6f reason=%s", d.ID, d.Score, d.Coverage, d.Reason)
				if tc.name == "low_absolute_score" && d.Score >= 0.70 {
					t.Fatalf("fixture does not exercise a sub-0.70 BM25 score: %+v", d)
				}
				if tc.name == "long_query" && d.ID == "ctx-0" && d.Coverage >= 0.25 {
					t.Fatalf("fixture does not exercise sub-0.25 query coverage: %+v", d)
				}
			}
			if len(got.Targets) != tc.want || (got.Text != "") != (tc.want > 0) {
				t.Fatalf("targets=%v, want %d matching cards; decisions=%+v", got.Targets, tc.want, got.Diagnostics.Candidates)
			}
			if len(got.Text) > 4096 || got.Diagnostics.Injected != (tc.want > 0) {
				t.Fatalf("budget/injection status changed: %+v", got.Diagnostics)
			}
			if tc.want > 0 && got.Diagnostics.MessageRole != "user" {
				t.Fatalf("history role=%q, want user", got.Diagnostics.MessageRole)
			}
		})
	}
}

func TestBridgeTaskLabelsAreDescriptive(t *testing.T) {
	const runner = "Fix the following issue in /testbed. The matching Python environment and dependencies are preinstalled at /opt/miniconda3/envs/testbed; use that Python and the existing tests.\n\n"
	for _, tc := range []struct {
		name, query, body, oldCommit string
		typ                          slice.SliceType
		want                         bool
	}{
		{"investigate_after_bugfix", "Investigate orchard expiry", "Task outcome (task=bugfix): orchard expiry", "", slice.Memory, true},
		{"test_after_bugfix", "Fix failing test orchard expiry", "Task outcome (task=bugfix): orchard expiry", "", slice.Memory, true},
		{"runner_after_investigation", runner + "Investigate orchard expiry", "Task outcome (task=investigate): orchard expiry", "", slice.Memory, true},
		{"untagged_history", "Investigate orchard expiry", "orchard expiry", "", slice.Memory, true},
		{"same_tag_unrelated", "wrong orchard expiry", "Task outcome (task=bugfix): quasar photometry spectrum", "", slice.Memory, false},
		{"stale_cross_task", "Investigate orchard expiry", "Task outcome (task=bugfix): orchard expiry", "older-revision", slice.Memory, false},
		{"probation_result", "Investigate orchard expiry", "orchard expiry", "", slice.Result, false},
	} {
		for _, mode := range []string{"strict", "shadow", "off"} {
			for _, degraded := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/degraded=%t", tc.name, mode, degraded), func(t *testing.T) {
					card := &slice.Slice{ID: "history", Type: tc.typ, Scope: slice.Project, Content: []byte(tc.body),
						Meta: slice.SliceMeta{SourceSession: "prior-task", Origin: slice.OriginSessionAuto, BaseCommit: tc.oldCommit}}
					b := NewBridge(Config{Enabled: true, Mode: mode, Budget: 4096, ProjectDir: writeKernelDir(t, []*slice.Slice{card}, nil)})
					defer b.Close()
					call, budget := b.InjectDetailed, 4096
					if degraded {
						call, budget = b.InjectDegradedDetailed, 2048
					}
					got := call(context.Background(), tc.query)
					wantTarget, wantText := tc.want && mode != "off", tc.want && mode == "strict"
					if (len(got.Targets) == 1 && got.Targets[0] == "history") != wantTarget || len(got.Targets) > 1 {
						t.Fatalf("task=%s targets=%v want=%t diagnostics=%+v", slice.ClassifyTask(tc.query), got.Targets, wantTarget, got.Diagnostics)
					}
					if (got.Text != "") != wantText || len(got.Text) > budget {
						t.Fatalf("text bytes=%d wantText=%t budget=%d", len(got.Text), wantText, budget)
					}
					if mode == "off" {
						if got.Diagnostics != nil {
							t.Fatal("off mode performed retrieval")
						}
						return
					}
					if got.Diagnostics == nil || got.Diagnostics.Injected != wantText {
						t.Fatalf("assembly status=%+v", got.Diagnostics)
					}
					if wantText && (got.Diagnostics.MessageRole != "user" || !strings.Contains(got.Text, tc.body) || !strings.Contains(got.Text, `source="prior-task"`)) {
						t.Fatalf("history content/provenance/user-role changed: %+v", got)
					}
				})
			}
		}
	}
}
