package agent

import (
	"strings"
	"testing"

	"semantix/harness/provider"
)

// applyPlaybookMessages must mirror the runner simulation exactly:
// swap the generic exploration rule in the FIRST user message for the
// economy directives, insert the playbook before the issue marker.
func TestApplyPlaybookMessages(t *testing.T) {
	generic := "- You may read files, search, edit code, and run tests to check your work."
	base := "You are working in a repo.\n\nFix the following issue. Rules:\n- Do NOT commit.\n" +
		generic + "\n- Stop when done.\n\n--- ISSUE ---\nThe issue body.\n--- END ISSUE ---\n"
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system prompt"},
		{Role: provider.RoleUser, Content: base},
	}
	pb := "--- PRIOR FIXES IN THIS REPO ---\n[p1] fix: X\n--- END PRIOR FIXES ---"
	rules := "- Use the PRIOR FIXES below.\n- Verify only your own edit."

	out := applyPlaybookMessages(msgs, pb, rules)
	got := out[1].Content
	if strings.Contains(got, generic) {
		t.Errorf("generic rule must be swapped:\n%s", got)
	}
	if !strings.Contains(got, "- Use the PRIOR FIXES below.") {
		t.Errorf("economy rules missing:\n%s", got)
	}
	idx := strings.Index(got, "--- ISSUE ---")
	if idx < 0 || !strings.Contains(got[:idx], "--- END PRIOR FIXES ---") {
		t.Errorf("playbook must sit before the issue marker:\n%s", got)
	}
	// input must not be mutated
	if !strings.Contains(msgs[1].Content, generic) {
		t.Error("input slice was mutated")
	}
	// non-user messages untouched
	if out[0].Content != "system prompt" {
		t.Error("system message changed")
	}
}

// miss-fallback: fallback turns must serve the plain base prompt — exercised
// via the turn flags the sampling layer reads.
func TestPlaybookFallbackClearsBlock(t *testing.T) {
	// The sampling layer clears injectBlock when injectFallback is set; here we
	// pin the contract that applyPlaybookMessages is never called with an empty
	// playbook (the caller guards it), and that a fallback turn's message list
	// is byte-identical to no-injection.
	base := "Rules:\n" + "- You may read files, search, edit code, and run tests to check your work.\n"
	msgs := []provider.Message{{Role: provider.RoleUser, Content: base}}
	out := applyPlaybookMessages(msgs, "", "- rules")
	if out[0].Content != base {
		t.Errorf("empty playbook must leave the message unchanged:\n%s", out[0].Content)
	}
}

// harness-shape prompt (no generic rule line): prepend strategy.
func TestApplyPlaybookMessagesPrependsWhenNoGenericLine(t *testing.T) {
	base := "Fix the following issue.\n\n--- ISSUE ---\nThe issue body.\n"
	msgs := []provider.Message{{Role: provider.RoleUser, Content: base}}
	pb := "--- PRIOR FIXES IN THIS REPO ---\n[p1] fix: X\n--- END PRIOR FIXES ---"
	rules := "- Use the PRIOR FIXES below.\n- Verify only your own edit."
	out := applyPlaybookMessages(msgs, pb, rules)
	got := out[0].Content
	if !strings.HasPrefix(got, "--- PRIOR FIXES IN THIS REPO ---") {
		t.Errorf("combined block must be prepended:\n%s", got)
	}
	if !strings.Contains(got, "- Verify only your own edit.") {
		t.Errorf("rules missing:\n%s", got)
	}
	if !strings.Contains(got, "--- ISSUE ---") {
		t.Errorf("original message lost:\n%s", got)
	}
}
