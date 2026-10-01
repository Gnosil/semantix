// Package inject — playbook.go implements the A7 distilled-prior rendering
// (Round3 winner, GLM econ12 2026-10-01): instead of injecting the raw
// [semantix-reuse] block, distill kept slices into a compact playbook of
// prior fixes and swap the harness's generic exploration rule for two
// economy directives.
//
// Semantics ported from slice-e0/scripts/run_episode.py (runner simulation,
// rounds 0-3) with three fixes required by Go-side typing:
//   - Prompt slices are dropped entirely (old prompt full text is noise);
//   - result slices contribute a first-sentence summary; procedure/outcome
//     slices contribute their edited-file set (repo-relative, deduped);
//   - no kept slice survives → RenderPlaybook returns "" and the caller
//     falls back to the plain base prompt (the fb arm's miss-fallback).
//
// Determinism contract (same as Build): output depends only on the kept
// slice set — ID-sorted, fixed formatting — so identical retrievals render
// byte-identical playbooks (prefix-cache friendly).
package inject

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"semantix/kernel/slice"
)

const (
	// playbookHeader opens the distilled block placed before the issue.
	playbookHeader = "--- PRIOR FIXES IN THIS REPO (distilled from earlier fixes; file hints are repo-relative) ---"
	// playbookFooter closes it.
	playbookFooter = "--- END PRIOR FIXES ---"
	// economy rules swapped in place of the harness's generic exploration rule.
	rulePrior  = "- Use the PRIOR FIXES below to jump straight to likely files when they match the issue; do not re-open, re-check, or re-verify the priors themselves."
	ruleVerify = "- Verify only your own edit with the minimal test that covers it; after it passes, run one quick regression check of the directly related tests, then stop."
	// genericExploreRule is the base-prompt line A7 replaces (must match the
	// harness prompt template; the runner uses the same constant).
	genericExploreRule = "- You may read files, search, edit code, and run tests to check your work."
)

// Playbook limits (ported from the runner; Round3 winner settings).
const (
	playbookMaxEntries = 4
	playbookMaxBytes   = 1800
	summaryMaxRunes    = 240
	summarySentence    = 240
	maxFilesPerSlice   = 6
	maxFilesListed     = 5
)

var (
	// repoRelPath matches "<repo>[\\/]path/file.ext" inside slice text; the
	// match is cut at the repo root so hints are repo-relative.
	repoRelPath = regexp.MustCompile(`(?i)([\w.\-]+)/((?:[\w.\-]+[\\/])*[\w.\-]+\.\w+)`)
	// sentenceEnd finds the first sentence boundary for summary trimming.
	sentenceEnd = regexp.MustCompile(`^(.*?[.!?])(\s|$)`)
	// verifyCmd extracts a Verified-by: trailing command.
	verifyCmd = regexp.MustCompile(`Verified-by:\s*(.+)$`)
	// envPrefix strips VAR="..." prefixes from a Verified-by command.
	envPrefix = regexp.MustCompile(`^[A-Z_]+="[^"]*"\s+`)
	// summaryLine picks the Summary: bold line of a result slice.
	summaryBold = regexp.MustCompile(`\*\*Summary:\*\*\s*(.+)$`)
)

// PlaybookOptions tunes RenderPlaybook; zero fields take Round3 defaults.
type PlaybookOptions struct {
	// RepoShort is the repo name used to cut absolute paths to repo-relative
	// ones (e.g. "django" for django/django). Empty disables path cutting.
	RepoShort string
	MaxEntries int
	MaxBytes   int
}

// RenderPlaybook distills kept slices into the A7 playbook text. It returns
// "" when nothing survives distillation (caller falls back to the base prompt).
// The rulesBlock return carries the two economy directives that replace the
// generic exploration rule.
func RenderPlaybook(kept []*slice.Slice, opts PlaybookOptions) (playbook string, rulesBlock string) {
	maxEntries := opts.MaxEntries
	if maxEntries <= 0 {
		maxEntries = playbookMaxEntries
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = playbookMaxBytes
	}

	type entry struct {
		id     string
		text   string
	}
	entries := make([]entry, 0, len(kept))
	seenFiles := map[string]bool{}

	for _, sl := range kept {
		if len(entries) >= maxEntries {
			break
		}
		if sl.Type == slice.Prompt { // old prompt full text: noise source, dropped
			continue
		}
		text := string(sl.Content)
		files := relPaths(text, opts.RepoShort)
		key := strings.Join(files, "|")
		if key == "" {
			key = sl.ID
		}
		if seenFiles[key] { // same file set = same prior; keep the first
			continue
		}
		seenFiles[key] = true

		var action string
		switch sl.Type {
		case slice.Result:
			action = summaryLine(text)
		case slice.Context, slice.Memory, slice.ToolPattern:
			action = summaryLine(text)
		default:
			if len(files) > 0 {
				action = "earlier fix edited these files (see files list)"
			} else {
				action = scaffoldLines(text)
			}
		}
		if action == "" && len(files) == 0 {
			continue
		}
		var b strings.Builder
		b.WriteString("fix: ")
		if action != "" {
			b.WriteString(action)
		} else {
			b.WriteString("(see files)")
		}
		if len(files) > 0 {
			b.WriteString(" | files: ")
			b.WriteString(strings.Join(files, ", "))
		}
		if cmd := verifiedBy(text, opts.RepoShort); cmd != "" {
			b.WriteString(" | verified-by: ")
			b.WriteString(cmd)
		}
		entries = append(entries, entry{id: sl.ID, text: b.String()})
	}
	if len(entries) == 0 {
		return "", ""
	}

	lines := make([]string, 0, len(entries)+2)
	lines = append(lines, playbookHeader)
	for i, e := range entries {
		lines = append(lines, fmt.Sprintf("[p%d] %s", i+1, e.text))
	}
	lines = append(lines, playbookFooter)
	out := strings.Join(lines, "\n")
	// Over budget: drop whole entries from the tail (byte-stable: never reflow).
	for len(out) > maxBytes && len(lines) > 3 {
		lines = append(lines[:len(lines)-2], lines[len(lines)-1])
		out = strings.Join(lines, "\n")
	}
	rules := rulePrior + "\n" + ruleVerify
	return out, rules
}

// SwapIntoBase replaces the generic exploration rule with the playbook-aware
// economy rules and inserts the playbook before the issue marker. playbook ""
// means miss-fallback: the base prompt is returned byte-identical (A7's fb —
// R0 showed economy directives without priors are actively harmful).
// Base prompts without the issue marker get the rules swap only.
func SwapIntoBase(base, playbook, rulesBlock string) string {
	if playbook == "" {
		return base
	}
	out := strings.Replace(base, genericExploreRule, rulesBlock, 1)
	if idx := strings.Index(out, "--- ISSUE ---"); idx >= 0 {
		out = out[:idx] + playbook + "\n\n" + out[idx:]
	} else {
		out = out + "\n\n" + playbook
	}
	return out
}

// relPaths extracts repo-relative file paths from slice text. Absolute
// occurrences (drive letter, leading separator, or "<repo>/…" duplicate of
// the repo short name) are cut at the repo root; paths already repo-relative
// pass through untouched.
func relPaths(text, repoShort string) []string {
	if repoShort == "" {
		return nil
	}
	out := make([]string, 0, maxFilesPerSlice)
	seen := map[string]bool{}
	lower := strings.ToLower(text)
	root := strings.ToLower(repoShort) + "/"
	abs := regexp.MustCompile(`(?i)(?:[a-z]:[\\/]|[\\/])` + regexp.QuoteMeta(repoShort) + `[\\/]?((?:[\w.\-]+[\\/])+[\w.\-]+\.\w+|\w+\.\w+)`)
	dup := regexp.MustCompile(`(?i)(?:^|[^/\w.])` + regexp.QuoteMeta(repoShort) + `/((?:[\w.\-]+/)*[\w.\-]+\.\w+)`)
	for _, m := range abs.FindAllStringSubmatch(lower, -1) {
		p := strings.ReplaceAll(m[1], "\\", "/")
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
		if len(out) >= maxFilesPerSlice {
			return out
		}
	}
	for _, m := range dup.FindAllStringSubmatch(lower, -1) {
		p := m[1]
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
		if len(out) >= maxFilesPerSlice {
			break
		}
	}
	_ = root
	// bare repo-relative paths (e.g. "django/conf/global_settings.py") that
	// were not part of an absolute occurrence: catch them without double-count
	// by requiring the repo short name as first segment.
	bare := regexp.MustCompile(`(?i)(?:^|[\s:;` + "`" + `"'"])((?:` + regexp.QuoteMeta(repoShort) + `/)(?:[\w.\-]+/)*[\w.\-]+\.\w+)`)
	for _, m := range bare.FindAllStringSubmatch(lower, -1) {
		p := strings.TrimPrefix(m[1], root)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
		if len(out) >= maxFilesPerSlice {
			break
		}
	}
	return out
}

// summaryLine returns the first-sentence summary of a result slice.
func summaryLine(text string) string {
	raw := ""
	for _, l := range strings.Split(text, "\n") {
		if m := summaryBold.FindStringSubmatch(l); m != nil {
			raw = strings.Trim(m[1], "*")
			break
		}
	}
	if raw == "" {
		for _, l := range strings.Split(text, "\n") {
			s := strings.TrimSpace(l)
			if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "-") ||
				strings.HasPrefix(s, "<") || strings.HasPrefix(s, "type=") ||
				strings.HasPrefix(s, "Task outcome") {
				continue
			}
			raw = s
			break
		}
	}
	if raw == "" {
		return ""
	}
	s := raw
	if m := sentenceEnd.FindStringSubmatch(raw); m != nil {
		s = strings.TrimSpace(m[1])
	}
	if len(s) > summarySentence {
		if cut := strings.LastIndex(s[:summarySentence], " "); cut > 0 {
			s = strings.TrimRight(s[:cut], ",;")
		}
	}
	s = strings.TrimPrefix(s, "I ")
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// scaffoldLines joins up to two non-scaffold lines for slices without files.
func scaffoldLines(text string) string {
	var picked []string
	for _, l := range strings.Split(text, "\n") {
		s := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "-*"))
		if s == "" || strings.HasPrefix(s, "Task outcome") ||
			strings.HasPrefix(s, "Relevant capabilities") || strings.HasPrefix(s, "Edited:") ||
			strings.HasPrefix(s, "type=") {
			continue
		}
		picked = append(picked, s)
		if len(picked) == 2 {
			break
		}
	}
	if len(picked) == 0 {
		return ""
	}
	s := strings.Join(picked, "; ")
	if len(s) > summaryMaxRunes {
		s = s[:summaryMaxRunes]
	}
	return s
}

// verifiedBy extracts and normalizes a Verified-by command to repo-relative form.
func verifiedBy(text, repoShort string) string {
	for _, l := range strings.Split(text, "\n") {
		m := verifyCmd.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		cmd := strings.TrimSpace(m[1])
		cmd = envPrefix.ReplaceAllString(cmd, "")
		if repoShort != "" {
			pat := regexp.MustCompile(`(?i)(?:[A-Za-z]:[\\/]|[\\/])[^\s"]*?[\\/]` + regexp.QuoteMeta(repoShort) + `[\\/]`)
			cmd = pat.ReplaceAllString(cmd, "")
		}
		cmd = strings.ReplaceAll(cmd, "\\", "/")
		if len(cmd) > 200 {
			cmd = cmd[:200]
		}
		return cmd
	}
	return ""
}

// SortKept is a convenience for callers that assembled kept slices without
// going through Build: ID order keeps playbook output byte-stable.
func SortKept(kept []*slice.Slice) {
	sort.Slice(kept, func(i, j int) bool { return kept[i].ID < kept[j].ID })
}
