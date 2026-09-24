package semantix

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

func TestBridgeCandidateStarvation(t *testing.T) {
	for _, mode := range []string{"fresh", "stale", "probation", "probation_blockers"} {
		t.Run(mode, func(t *testing.T) {
			items := []*slice.Slice{
				{ID: "ctx-strong", Type: slice.Context, Scope: slice.Project, Content: []byte("repair parser regression with traceback"), Meta: slice.SliceMeta{SourceSession: "source-1"}},
				{ID: "ctx-runner", Type: slice.Context, Scope: slice.Project, Content: []byte("repair unrelated logging conventions"), Meta: slice.SliceMeta{SourceSession: "source-2"}},
			}
			for _, item := range items {
				if mode == "stale" {
					item.Meta.BaseCommit = "2222222222222222222222222222222222222222"
				}
				if mode == "probation" {
					item.Type = slice.Result
				}
			}
			blockedType, blockedReason := slice.Prompt, "type_not_allowed"
			if mode == "probation_blockers" {
				blockedType, blockedReason = slice.Result, "result_probation"
			}
			for i := 0; i < 5; i++ {
				items = append(items, &slice.Slice{ID: fmt.Sprintf("blocked-%d", i), Type: blockedType, Scope: slice.Project, Content: []byte("repair parser regression repair parser regression")})
			}
			for i := 0; i < 10; i++ {
				items = append(items, &slice.Slice{ID: fmt.Sprintf("other-%d", i), Type: slice.Prompt, Scope: slice.Project, Content: []byte("unrelated deployment configuration")})
			}
			dir := writeKernelDir(t, items, nil)
			b := NewBridge(Config{Enabled: true, Mode: "strict", ProjectDir: dir})
			defer b.Close()
			const query = "repair parser regression"
			store, idx, err := b.kernelIndex()
			if err != nil {
				t.Fatal(err)
			}
			all, err := idx.Search(buildRetrievalQuery(query).Text, len(items), slice.Project)
			closeSliceStore(store)
			if err != nil {
				t.Fatal(err)
			}
			if len(all) != 7 {
				t.Fatalf("matching candidates = %d, want 7", len(all))
			}
			scores := make(map[string]float64)
			for i, hit := range all {
				scores[hit.Slice.ID] = hit.Score
				if i < 5 && hit.Slice.Type != blockedType {
					t.Fatalf("fixture top five contains %s", hit.Slice.ID)
				}
			}
			result := b.InjectDetailed(context.Background(), query)
			if result.Diagnostics == nil {
				t.Fatal("missing diagnostics")
			}
			if mode == "fresh" || mode == "probation_blockers" {
				if len(result.Targets) != 1 || result.Targets[0] != "ctx-strong" {
					t.Fatalf("targets = %v, want [ctx-strong]; candidates = %+v", result.Targets, result.Diagnostics.Candidates)
				}
			} else if result.Text != "" || len(result.Targets) != 0 {
				t.Fatalf("%s evidence injected: %v", mode, result.Targets)
			}
			blocked := 0
			for _, candidate := range result.Diagnostics.Candidates {
				if candidate.Score != scores[candidate.ID] {
					t.Fatalf("score changed for %s: %v != %v", candidate.ID, candidate.Score, scores[candidate.ID])
				}
				if strings.HasPrefix(candidate.ID, "blocked-") {
					blocked++
					if candidate.Reason != blockedReason || candidate.Admitted {
						t.Fatalf("type/status guard changed: %+v", candidate)
					}
				}
			}
			if blocked != 5 {
				t.Fatalf("original blocked diagnostics = %d, want 5", blocked)
			}
		})
	}
}

func TestBridgeCandidateTypeLimit(t *testing.T) {
	var items []*slice.Slice
	for i := 0; i < 7; i++ {
		items = append(items, &slice.Slice{ID: fmt.Sprintf("ctx-%d", i), Type: slice.Context, Scope: slice.Project, Content: []byte("repair parser regression"), Meta: slice.SliceMeta{SourceSession: fmt.Sprintf("source-%d", i)}})
	}
	for i := 0; i < 5; i++ {
		items = append(items, &slice.Slice{ID: fmt.Sprintf("blocked-%d", i), Type: slice.Prompt, Scope: slice.Project, Content: []byte("repair parser regression repair parser regression")})
	}
	b := NewBridge(Config{Enabled: true, Mode: "strict", ProjectDir: writeKernelDir(t, items, nil)})
	defer b.Close()
	result := b.InjectDetailed(context.Background(), "repair parser regression")
	if result.Diagnostics == nil || len(result.Diagnostics.Candidates) != 10 {
		t.Fatalf("diagnostics = %+v, want original five rejects and five allowed types", result.Diagnostics)
	}
	for _, candidate := range result.Diagnostics.Candidates {
		if candidate.ID == "ctx-5" || candidate.ID == "ctx-6" {
			t.Fatalf("candidate cap exceeded: %+v", candidate)
		}
	}
	if result.Text == "" || len(result.Targets) != 5 {
		t.Fatalf("equal-score matches should fill, not veto, the bounded window: %v", result.Targets)
	}
	for i, id := range result.Targets {
		if want := fmt.Sprintf("ctx-%d", i); id != want {
			t.Fatalf("target[%d]=%q, want deterministic tie order %q", i, id, want)
		}
	}
}

func TestBridgeCandidateWindowAfterTaskAndFreshness(t *testing.T) {
	for _, blocker := range []string{"stale", "task_mismatch", "all_stale"} {
		for _, mode := range []string{"strict", "shadow"} {
			t.Run(blocker+"/"+mode, func(t *testing.T) {
				items := []*slice.Slice{
					{ID: "ctx-strong", Type: slice.Context, Scope: slice.Project, Content: []byte("repair parser regression with traceback"), Meta: slice.SliceMeta{SourceSession: "source-1"}},
					{ID: "ctx-runner", Type: slice.Context, Scope: slice.Project, Content: []byte("repair unrelated logging conventions"), Meta: slice.SliceMeta{SourceSession: "source-2"}},
				}
				if blocker == "all_stale" {
					for _, item := range items {
						item.Meta.BaseCommit = "2222222222222222222222222222222222222222"
					}
				}
				for i := 0; i < 6; i++ {
					item := &slice.Slice{ID: fmt.Sprintf("blocked-%d", i), Type: slice.Context, Scope: slice.Project, Content: []byte("repair parser regression repair parser regression"), Meta: slice.SliceMeta{BaseCommit: "2222222222222222222222222222222222222222"}}
					if blocker == "task_mismatch" {
						item.Type = slice.Memory
						item.Meta.BaseCommit = ""
						item.Meta.SourceSession = fmt.Sprintf("blocked-source-%d", i)
						item.Content = []byte("Plan skeleton (task=feature): repair parser regression repair parser regression repair parser regression")
					}
					items = append(items, item)
				}
				for i := 0; i < 10; i++ {
					items = append(items, &slice.Slice{ID: fmt.Sprintf("other-%d", i), Type: slice.Prompt, Scope: slice.Project, Content: []byte("unrelated deployment configuration")})
				}
				b := NewBridge(Config{Enabled: true, Mode: mode, ProjectDir: writeKernelDir(t, items, nil)})
				defer b.Close()
				const query = "repair parser regression"
				store, idx, err := b.kernelIndex()
				if err != nil {
					t.Fatal(err)
				}
				all, err := idx.Search(buildRetrievalQuery(query).Text, len(items), slice.Project)
				closeSliceStore(store)
				if err != nil || len(all) != 8 {
					t.Fatalf("fixture search = %d, %v", len(all), err)
				}
				scores := make(map[string]float64)
				byID := make(map[string]*slice.Slice)
				for rank, hit := range all {
					scores[hit.Slice.ID] = hit.Score
					byID[hit.Slice.ID] = hit.Slice
					if rank < 6 && !strings.HasPrefix(hit.Slice.ID, "blocked-") {
						t.Fatalf("fixture top six contains %s", hit.Slice.ID)
					}
				}
				result := b.InjectDetailed(context.Background(), query)
				if result.Diagnostics == nil {
					t.Fatal("missing diagnostics")
				}
				if blocker == "all_stale" {
					if len(result.Targets) != 0 || result.Text != "" {
						t.Fatalf("all-stale candidates admitted: %+v", result)
					}
				} else if len(result.Targets) != 1 || result.Targets[0] != "ctx-strong" {
					t.Fatalf("targets = %v, want [ctx-strong]; candidates = %+v", result.Targets, result.Diagnostics.Candidates)
				}
				if mode == "shadow" && (result.Text != "" || result.Diagnostics.Injected) {
					t.Fatal("shadow mode injected instead of observing")
				}
				blocked := 0
				for _, candidate := range result.Diagnostics.Candidates {
					if candidate.Score != scores[candidate.ID] {
						t.Fatalf("changed corpus score: %+v", candidate)
					}
					original := byID[candidate.ID]
					if original == nil || candidate.Type != original.Type.String() || candidate.SourceSession != original.Meta.SourceSession || candidate.BaseCommit != original.Meta.BaseCommit || candidate.Origin != string(original.Meta.Origin) {
						t.Fatalf("decision metadata not joined by ID: candidate=%+v original=%+v", candidate, original)
					}
					if strings.HasPrefix(candidate.ID, "blocked-") {
						blocked++
						want := "stale_commit"
						if blocker == "task_mismatch" {
							want = "task_type_mismatch"
						}
						if candidate.Admitted || candidate.Reason != want {
							t.Fatalf("blocker admission = %+v, want %s", candidate, want)
						}
					}
				}
				if blocked != 5 {
					t.Fatalf("original top-five blocker diagnostics = %d", blocked)
				}
			})
		}
	}
}

func TestBridgeOriginFloor(t *testing.T) {
	for _, origin := range []slice.Origin{"", slice.OriginImport, slice.OriginSessionAuto, slice.OriginPrefetch, slice.OriginUserCurated} {
		for _, mode := range []string{"strict", "shadow"} {
			t.Run(string(origin)+"/"+mode, func(t *testing.T) {
				items := []*slice.Slice{
					{ID: "ctx-strong", Type: slice.Context, Scope: slice.Project, Content: []byte("repair parser regression with traceback"), Meta: slice.SliceMeta{SourceSession: "source-1"}},
					{ID: "ctx-runner", Type: slice.Context, Scope: slice.Project, Content: []byte("repair unrelated logging conventions"), Meta: slice.SliceMeta{SourceSession: "source-2"}},
				}
				for i := 0; i < 10; i++ {
					items = append(items, &slice.Slice{ID: fmt.Sprintf("other-%d", i), Type: slice.Prompt, Scope: slice.Project, Content: []byte("unrelated deployment configuration")})
				}
				dir := writeKernelDir(t, items, nil)
				// Explicitly persist even the empty legacy origin after trusted
				// fixture creation; missing provenance is the negative under test.
				store, err := slice.NewFileStore(filepath.Join(dir, ".semantix", "project.db"))
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range items {
					item.Meta.Origin = origin
					if err := store.Put(item); err != nil {
						t.Fatal(err)
					}
				}
				closeSliceStore(store)
				b := NewBridge(Config{Enabled: true, Mode: mode, ProjectDir: dir})
				defer b.Close()
				result := b.InjectDetailed(context.Background(), "repair parser regression")
				if result.Diagnostics == nil {
					t.Fatal("missing diagnostics")
				}
				if origin.Level() < slice.OriginSessionAuto.Level() {
					if len(result.Targets) != 0 || result.Text != "" {
						t.Fatalf("low-integrity origin %q admitted: %v", origin, result.Targets)
					}
					for _, d := range result.Diagnostics.Candidates {
						if d.Admitted || d.Reason != "origin_below_floor" {
							t.Fatalf("origin rejection = %+v", d)
						}
					}
				} else if len(result.Targets) != 1 || result.Targets[0] != "ctx-strong" {
					t.Fatalf("trusted origin %q targets = %v", origin, result.Targets)
				}
				if mode == "shadow" && (result.Text != "" || result.Diagnostics.Injected) {
					t.Fatal("shadow origin decision injected")
				}
			})
		}
	}
}

func TestBridgeUntrustedOriginDoesNotSpendCandidateWindow(t *testing.T) {
	for _, mode := range []string{"strict", "shadow"} {
		t.Run(mode, func(t *testing.T) {
			items := []*slice.Slice{
				{ID: "ctx-strong", Type: slice.Context, Scope: slice.Project, Content: []byte("repair parser regression with traceback"), Meta: slice.SliceMeta{SourceSession: "source-1", Origin: slice.OriginSessionAuto}},
				{ID: "ctx-runner", Type: slice.Context, Scope: slice.Project, Content: []byte("repair unrelated logging conventions"), Meta: slice.SliceMeta{SourceSession: "source-2", Origin: slice.OriginSessionAuto}},
			}
			for i := 0; i < 5; i++ {
				items = append(items, &slice.Slice{ID: fmt.Sprintf("import-%d", i), Type: slice.Context, Scope: slice.Project, Content: []byte("repair parser regression repair parser regression"), Meta: slice.SliceMeta{Origin: slice.OriginImport}})
			}
			for i := 0; i < 10; i++ {
				items = append(items, &slice.Slice{ID: fmt.Sprintf("other-%d", i), Type: slice.Prompt, Scope: slice.Project, Content: []byte("unrelated deployment configuration")})
			}
			b := NewBridge(Config{Enabled: true, Mode: mode, ProjectDir: writeKernelDir(t, items, nil)})
			defer b.Close()
			result := b.InjectDetailed(context.Background(), "repair parser regression")
			if len(result.Targets) != 1 || result.Targets[0] != "ctx-strong" {
				t.Fatalf("trusted targets = %v, diagnostics = %+v", result.Targets, result.Diagnostics)
			}
			blocked := 0
			for _, d := range result.Diagnostics.Candidates {
				if strings.HasPrefix(d.ID, "import-") {
					blocked++
					if d.Admitted || d.Reason != "origin_below_floor" {
						t.Fatalf("untrusted blocker = %+v", d)
					}
				}
			}
			if blocked != 5 {
				t.Fatalf("origin blocker diagnostics = %d", blocked)
			}
		})
	}
}
