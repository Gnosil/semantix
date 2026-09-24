package semantix

import (
	"context"
	"fmt"
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
