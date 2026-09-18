package semantix

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

func TestReadGitHeadPackedWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(root string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	git(dir, "init", "-q")
	git(dir, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "initial")
	want := git(dir, "rev-parse", "HEAD")
	git(dir, "pack-refs", "--all", "--prune")
	if got := readGitHead(dir); got != want {
		t.Errorf("packed HEAD=%q want %q", got, want)
	}
	wt := filepath.Join(t.TempDir(), "worktree")
	git(dir, "worktree", "add", "-q", "-b", "linked", wt)
	git(dir, "pack-refs", "--all", "--prune")
	if got := readGitHead(wt); got != want {
		t.Errorf("packed worktree HEAD=%q want %q", got, want)
	}
	git(wt, "checkout", "--detach", "-q")
	if got := readGitHead(wt); got != want {
		t.Errorf("detached HEAD=%q want %q", got, want)
	}
	if got := readGitHead(t.TempDir()); got != "" {
		t.Fatalf("nonrepo HEAD=%q", got)
	}
}

func TestSourceCountsIncludeConsolidatedSessions(t *testing.T) {
	var meta slice.SliceMeta
	if err := json.Unmarshal([]byte(`{"SourceSession":"a","source_sessions":["a","b","b",""]}`), &meta); err != nil {
		t.Fatal(err)
	}
	counts := sourceSessionCounts([]*slice.Slice{{Type: slice.Context, Meta: meta}, {Type: slice.Context, Meta: slice.SliceMeta{SourceSession: "a"}}})
	if counts[slice.Context] != 2 {
		t.Fatalf("source count=%d want 2 independent sessions", counts[slice.Context])
	}
}
