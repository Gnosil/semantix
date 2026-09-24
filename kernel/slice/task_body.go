package slice

import "strings"

// TaskBody recognizes only explicit task boundaries and known runner
// framing. Ordinary headings inside a task (Expected, Actual, Requirements)
// are task evidence, not generic delimiters.
func TaskBody(raw string) string {
	body := strings.TrimSpace(stripTaggedBlock(strings.ReplaceAll(raw, "\r\n", "\n"), "execution-policy"))
	if issue, ok := taggedBody(body, "issue"); ok {
		return strings.TrimSpace(issue)
	}
	const testbedPrefix = "Fix the following issue in /testbed. The matching Python environment and dependencies are preinstalled at /opt/miniconda3/envs/testbed; use that Python and the existing tests.\n\n"
	if task, ok := strings.CutPrefix(body, testbedPrefix); ok {
		return strings.TrimSpace(task)
	}
	if strings.HasPrefix(body, "You are working in a git checkout of the ") || strings.HasPrefix(body, "You are solving a real GitHub issue in this repository.") {
		if _, task, ok := strings.Cut(body, "\n\nIssue:\n"); ok {
			// This exact suffix is the runner's instruction, not an arbitrary
			// Requirements heading that may belong to the issue itself.
			task, _, _ = strings.Cut(task, "\n\nRequirements:\n- Find the root cause and implement a complete fix")
			return strings.TrimSpace(task)
		}
	}
	if task, ok := strings.CutPrefix(body, "Issue:\n"); ok {
		return strings.TrimSpace(task)
	}
	return body
}

func stripTaggedBlock(text, tag string) string {
	for {
		lower := asciiLower(text)
		start := 0
		for {
			rel := strings.Index(lower[start:], "<"+tag)
			if rel < 0 {
				return text
			}
			start += rel
			next := start + len(tag) + 1
			if next == len(lower) || strings.ContainsRune("> \t\r\n\f", rune(lower[next])) {
				break
			}
			start = next
		}
		openEndRel := strings.Index(lower[start:], ">")
		if openEndRel < 0 {
			return text[:start]
		}
		closeStartRel := strings.Index(lower[start+openEndRel+1:], "</"+tag+">")
		if closeStartRel < 0 {
			return text[:start]
		}
		end := start + openEndRel + 1 + closeStartRel + len(tag) + 3
		text = text[:start] + " " + text[end:]
	}
}

func taggedBody(text, tag string) (string, bool) {
	lower := asciiLower(text)
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(lower, open)
	if start < 0 {
		return "", false
	}
	start += len(open)
	endRel := strings.Index(lower[start:], close)
	if endRel < 0 {
		return "", false
	}
	return text[start : start+endRel], true
}

// Tag matching must preserve byte offsets into the original Unicode task.
func asciiLower(text string) string {
	b := []byte(text)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
