package inject

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"semantix/kernel/sanitize"
	"semantix/kernel/slice"
)

// HandoffPreview is a display-only rendering of an already admitted block.
// It returns the original block if any selected slice is unsupported or if
// the complete preview would not be shorter. Never use it as model input.
func HandoffPreview(inj *Injection) string {
	if inj == nil {
		return ""
	}
	if len(inj.Slices) == 0 || inj.GreyIncluded != 0 {
		return inj.Text
	}
	var out strings.Builder
	out.WriteString("Historical handoff preview (revalidate; completion not established)\n")
	for _, sl := range inj.Slices {
		if !sl.IsHistoricalObservation() || sl.Type != slice.Memory || sl.Scope != slice.Project {
			return inj.Text
		}
		body := string(sl.Content)
		if !utf8.ValidString(body) || sanitize.Sanitize(body) != body || escapeMarker(body) != body {
			return inj.Text
		}
		verified, supported := historicalOutcomeShape(body)
		if !supported {
			return inj.Text
		}
		fmt.Fprintf(&out, "slice %q project=%q source=%q commit=%q origin=%q\n",
			sl.ID, sl.Meta.ProjectSlug, sl.Meta.SourceSession, sl.Meta.BaseCommit, sl.Meta.Origin)
		out.WriteString(body) // do not rewrite, normalize, or cut a body field
		out.WriteByte('\n')
		if !verified {
			out.WriteString("Verification: not recorded\n")
		}
	}
	preview := strings.TrimSuffix(out.String(), "\n")
	if len(preview) >= len(inj.Text) {
		return inj.Text
	}
	return preview
}

// historicalOutcomeShape accepts the current Distill outcome-card shape. A
// new or ambiguous field falls back to the original block instead of losing
// an unresolved state or accidentally presenting it as a file hint.
func historicalOutcomeShape(body string) (verified, supported bool) {
	lines := strings.Split(body, "\n")
	if len(lines) < 3 || lines[1] != "Edited:" {
		return false, false
	}
	head, ok := strings.CutPrefix(lines[0], "Task outcome (task=")
	task, summary, split := strings.Cut(head, "): ")
	if !ok || !split || task == "" || strings.TrimSpace(summary) == "" {
		return false, false
	}
	files := 0
	for i, line := range lines[2:] {
		if p, ok := strings.CutPrefix(line, "- "); ok && strings.TrimSpace(p) != "" {
			files++
			continue
		}
		if cmd, ok := strings.CutPrefix(line, "Verified-by: "); ok && i == len(lines)-3 && strings.TrimSpace(cmd) != "" {
			verified = true
			continue
		}
		return false, false
	}
	return verified, files > 0
}
