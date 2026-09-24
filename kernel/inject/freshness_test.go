package inject

import (
	"os"
	"path/filepath"
	"testing"

	"semantix/kernel/fingerprint"
	"semantix/kernel/slice"
)

func freshnessDecision(t *testing.T, in *Injector, sl *slice.Slice) CandidateDecision {
	t.Helper()
	out, err := in.BuildHits("cache repair", []slice.Hit{{Slice: sl, Score: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Decisions) != 1 {
		t.Fatalf("decisions = %+v", out.Decisions)
	}
	return out.Decisions[0]
}

func TestStrictFreshnessAdmission(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "pkg", "cache.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package cache\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps, err := fingerprint.Capture(root, []string{"pkg/cache.go"})
	if err != nil {
		t.Fatal(err)
	}
	in := &Injector{
		Budget: 4096, AllowedTypes: map[slice.SliceType]bool{slice.Context: true},
		RootDir: root, CurrentCommit: "current",
	}
	cases := []struct {
		name string
		meta slice.SliceMeta
		want string
	}{
		{"missing provenance", slice.SliceMeta{}, "commit_unknown"},
		{"same commit", slice.SliceMeta{BaseCommit: "current"}, "admitted"},
		{"mismatch without dependencies", slice.SliceMeta{BaseCommit: "old"}, "stale_commit"},
		{"mismatch with matching dependency", slice.SliceMeta{BaseCommit: "old", Deps: deps}, "admitted"},
		{"missing dependency", slice.SliceMeta{BaseCommit: "old", Deps: fingerprint.Deps{"pkg/missing.go": "abc"}}, "path_missing"},
		{"unsafe dependency", slice.SliceMeta{BaseCommit: "old", Deps: fingerprint.Deps{"../outside": "abc"}}, "dependency_path_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sl := &slice.Slice{ID: tc.name, Type: slice.Context, Scope: slice.Project, Content: []byte("cache repair"), Meta: tc.meta}
			if got := freshnessDecision(t, in, sl).Reason; got != tc.want {
				t.Fatalf("reason = %q, want %q", got, tc.want)
			}
		})
	}

	if err := os.WriteFile(path, []byte("package cache\n// changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := &slice.Slice{ID: "changed", Type: slice.Context, Scope: slice.Project, Content: []byte("cache repair"), Meta: slice.SliceMeta{BaseCommit: "old", Deps: deps}}
	if got := freshnessDecision(t, in, changed).Reason; got != "dependency_changed" {
		t.Fatalf("changed dependency reason = %q", got)
	}
}

func TestStrictFreshnessRejectsUnknownCurrentCommit(t *testing.T) {
	in := &Injector{Budget: 4096, AllowedTypes: map[slice.SliceType]bool{slice.Context: true}, RootDir: t.TempDir()}
	sl := &slice.Slice{ID: "ctx", Type: slice.Context, Scope: slice.Project, Content: []byte("cache repair"), Meta: slice.SliceMeta{BaseCommit: "old"}}
	if got := freshnessDecision(t, in, sl).Reason; got != "current_commit_unknown" {
		t.Fatalf("reason = %q", got)
	}
}
