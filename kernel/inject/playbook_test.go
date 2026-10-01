package inject

import (
	"strings"
	"testing"

	"semantix/kernel/slice"
)

// fixtures mirror the runner simulation samples (slice-e0 round 0-3).
func resultSlice(id, content string) *slice.Slice {
	return &slice.Slice{ID: id, Type: slice.Result, Content: []byte(content)}
}

func TestRenderPlaybookResultSummary(t *testing.T) {
	kept := []*slice.Slice{resultSlice("b", "**Summary:** I set the default `FILE_UPLOAD_PERMISSIONS` to `0o644` in `django/conf/global_settings.py` so uploaded files get consistent permissions. And updated docs too.")}
	pb, rules := RenderPlaybook(kept, PlaybookOptions{RepoShort: "django"})
	if pb == "" {
		t.Fatal("expected non-empty playbook")
	}
	if !strings.HasPrefix(pb, playbookHeader) || !strings.HasSuffix(pb, playbookFooter) {
		t.Fatalf("missing header/footer:\n%s", pb)
	}
	if !strings.Contains(pb, "[p1] fix: Set the default `FILE_UPLOAD_PERMISSIONS`") {
		t.Errorf("expected first-sentence summary with I-prefix stripped and capitalized:\n%s", pb)
	}
	if strings.Contains(pb, "And updated docs") {
		t.Errorf("expected sentence-level truncation:\n%s", pb)
	}
	if !strings.Contains(pb, "files: conf/global_settings.py") {
		t.Errorf("expected repo-relative hint (repo segment stripped, matching the runner):\n%s", pb)
	}
	if !strings.Contains(rules, rulePrior) || !strings.Contains(rules, ruleVerify) {
		t.Errorf("rules block missing economy directives:\n%s", rules)
	}
}

func TestRenderPlaybookPromptDropped(t *testing.T) {
	kept := []*slice.Slice{
		{ID: "a", Type: slice.Prompt, Content: []byte("entire old prompt noise with D:/x/slice-e0/worktrees/django-005/django/core/management/commands/sqlmigrate.py")},
	}
	if pb, _ := RenderPlaybook(kept, PlaybookOptions{RepoShort: "django"}); pb != "" {
		t.Errorf("prompt-only kept set must distill to empty (miss-fallback), got:\n%s", pb)
	}
}

func TestRenderPlaybookDedupByFileSet(t *testing.T) {
	body := "Task outcome (task=bugfix): Edited: - D:/x/wt/django-005/django/core/management/commands/sqlmigrate.py - D:/x/wt/django-005/tests/migrations/test_commands.py Verified-by: PYTHONPATH=\"D:/x/wt/django-005\" python tests/runtests.py migrations.test_commands"
	kept := []*slice.Slice{
		{ID: "a", Type: slice.Memory, Content: []byte(body)},
		{ID: "b", Type: slice.Memory, Content: []byte(body + " (duplicate)")},
	}
	pb, _ := RenderPlaybook(kept, PlaybookOptions{RepoShort: "django"})
	if got := strings.Count(pb, "[p"); got != 1 {
		t.Errorf("expected dedup to 1 entry, got %d:\n%s", got, pb)
	}
	if !strings.Contains(pb, "django/core/management/commands/sqlmigrate.py") {
		t.Errorf("expected repo-relative file hints:\n%s", pb)
	}
	if strings.Contains(pb, "D:/x") {
		t.Errorf("absolute paths must be cut to repo-relative:\n%s", pb)
	}
	if !strings.Contains(pb, "verified-by: python tests/runtests.py") {
		t.Errorf("expected normalized verified-by command:\n%s", pb)
	}
}

func TestRenderPlaybookBudgetDrop(t *testing.T) {
	var kept []*slice.Slice
	for i := 0; i < 6; i++ {
		id := string(rune('a'+i)) + strings.Repeat("z", 6)
		kept = append(kept, resultSlice(id, "**Summary:** Fix number "+strings.Repeat("detail ", 40)+"."))
	}
	pb, _ := RenderPlaybook(kept, PlaybookOptions{RepoShort: "", MaxEntries: 6, MaxBytes: 400})
	if len(pb) > 400 {
		t.Errorf("budget not enforced: %d bytes", len(pb))
	}
	if !strings.Contains(pb, playbookFooter) {
		t.Errorf("budget trimming must keep the footer:\n%s", pb)
	}
}

func TestSwapIntoBase(t *testing.T) {
	base := "Rules:\n- Do NOT commit.\n- You may read files, search, edit code, and run tests to check your work.\n- Stop when done.\n\n--- ISSUE ---\nThe issue.\n"
	pb := playbookHeader + "\n[p1] fix: X\n" + playbookFooter
	out := SwapIntoBase(base, pb, rulePrior+"\n"+ruleVerify)
	if !strings.Contains(out, rulePrior) || strings.Contains(out, genericExploreRule) {
		t.Errorf("generic rule must be swapped:\n%s", out)
	}
	if idx := strings.Index(out, "--- ISSUE ---"); idx < 0 || !strings.Contains(out[:idx], playbookFooter) {
		t.Errorf("playbook must sit before the issue marker:\n%s", out)
	}
	// miss-fallback: empty playbook leaves the base prompt byte-identical
	if got := SwapIntoBase(base, "", ""); got != base {
		t.Errorf("miss-fallback must return base unchanged:\n%s", got)
	}
}
