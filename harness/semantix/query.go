package semantix

import (
	"regexp"
	"sort"
	"strings"

	"semantix/kernel/bm25"
)

var (
	repoPattern   = regexp.MustCompile(`(?i)checkout\s+of\s+the\s+([a-z0-9_.-]+/[a-z0-9_.-]+)\s+repository`)
	pathPattern   = regexp.MustCompile(`(?:[A-Za-z0-9_.-]+/)+[A-Za-z0-9_.-]+`)
	symbolPattern = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)+\b|\b_+[A-Za-z0-9_]+_+\b|\b[A-Za-z][A-Za-z0-9]*_[A-Za-z0-9_]+\b`)
	// Exception names are case-insensitive ("ConnectionError"); code-style
	// identifiers (HTTP_404, FOO-BAR_1) must stay case-sensitive — under (?i)
	// the same pattern matched every hyphenated prose word ("pre-fix",
	// "read-only"), which put template words into the retrieval query.
	errorWordPattern = regexp.MustCompile(`(?i)\b[a-z][a-z0-9]*error\b`)
	errorCodePattern = regexp.MustCompile(`\b[A-Z]{2,}(?:[_-][A-Z0-9]+)+\b`)
	// Test names need a real identifier shape (test_foo / TestFoo); the bare
	// words test/tests/testing appear in almost every task shell.
	testFuncPattern  = regexp.MustCompile(`(?i)\btest_[a-z0-9_]+\b`)
	testClassPattern = regexp.MustCompile(`\bTest[A-Z][A-Za-z0-9_]*\b`)
	labelLinePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z ]{0,40}:$`)
	importPattern    = regexp.MustCompile(`(?i)\bimports?\s+([a-z_][a-z0-9_.]*)|\bfrom\s+([a-z_][a-z0-9_.]*)\s+import\b`)
	urlPattern       = regexp.MustCompile(`(?i)https?://\S+`)
)

// RetrievalQuery is the deterministic P1 query projection. Text is the only
// value sent to BM25; the named fields explain which high-information signals
// contributed to it and make fallback behavior observable.
type RetrievalQuery struct {
	Text           string
	Strategy       string
	Intent         string
	Repo           string
	Paths          []string
	Symbols        []string
	ErrorCodes     []string
	TestNames      []string
	Dependencies   []string
	FallbackReason string
}

var retrievalStopwords = map[string]struct{}{
	"a": {}, "at": {}, "below": {}, "checkout": {}, "commit": {},
	"git": {}, "github": {}, "in": {}, "issue": {}, "of": {},
	"please": {}, "repository": {}, "requirements": {}, "resolve": {},
	"task": {}, "the": {}, "working": {}, "you": {}, "are": {},
}

// cleanRetrievalQuery removes host/benchmark framing before lexical search.
// It intentionally returns a token projection rather than natural prose: BM25
// and QueryCoverage consume the same tokenizer, so punctuation loss cannot
// change the lexical evidence while boilerplate cannot dominate it.
func cleanRetrievalQuery(raw string) string {
	query := stripTaggedBlock(raw, "execution-policy")
	if issue, ok := issueBody(query); ok {
		query = issue
	}
	tokens := bm25.Tokenize(query)
	kept := tokens[:0]
	for _, token := range tokens {
		if _, drop := retrievalStopwords[token]; !drop {
			kept = append(kept, token)
		}
	}
	return strings.Join(kept, " ")
}

// buildRetrievalQuery extracts stable high-information fields from a task. If
// no such field exists it preserves the P0 lexical cleaning behavior rather
// than pretending a generic sentence is structured evidence.
func buildRetrievalQuery(raw string) RetrievalQuery {
	body := stripTaggedBlock(raw, "execution-policy")
	if issue, ok := issueBody(body); ok {
		body = issue
	}
	signalBody := urlPattern.ReplaceAllString(body, " ")
	q := RetrievalQuery{Intent: firstNonEmptyLine(body), Repo: firstCapture(repoPattern, raw)}
	q.Paths = collectPaths(signalBody, q.Repo)
	q.Symbols = collectMatches(symbolPattern, signalBody)
	q.ErrorCodes = uniqueSorted(append(collectMatches(errorWordPattern, signalBody), collectMatches(errorCodePattern, signalBody)...))
	q.Dependencies = collectImports(signalBody)
	for _, value := range append(append([]string(nil), q.Paths...), q.Symbols...) {
		if strings.Contains(strings.ToLower(value), "test") {
			q.TestNames = append(q.TestNames, strings.ToLower(value))
		}
	}
	q.TestNames = append(q.TestNames, collectMatches(testFuncPattern, signalBody)...)
	q.TestNames = append(q.TestNames, collectMatches(testClassPattern, signalBody)...)
	q.TestNames = uniqueSorted(q.TestNames)

	hasSignals := len(q.Paths)+len(q.Symbols)+len(q.ErrorCodes)+len(q.TestNames)+len(q.Dependencies) > 0
	if !hasSignals {
		q.Text = cleanRetrievalQuery(raw)
		q.Strategy = "lexical_fallback"
		q.FallbackReason = "no_structured_signals"
		if q.Text == "" {
			q.FallbackReason = "empty_after_cleaning"
		}
		return q
	}

	values := []string{q.Intent}
	values = append(values, q.Paths...)
	values = append(values, q.Symbols...)
	values = append(values, q.ErrorCodes...)
	values = append(values, q.TestNames...)
	values = append(values, q.Dependencies...)
	q.Text = joinRetrievalTokens(values...)
	q.Strategy = "structured"
	return q
}

// issueBody isolates the issue text from the task shell: a <issue> block
// (SWE-bench runner) or a labeled "Issue:" section (swe_pilot and hand-written
// prompts) that ends at the next bare label line such as "Instructions:".
// Without either the whole text is the issue.
func issueBody(text string) (string, bool) {
	if issue, ok := taggedBody(text, "issue"); ok {
		return issue, true
	}
	return labeledSection(text, "issue")
}

func labeledSection(text, label string) (string, bool) {
	lines := strings.Split(text, "\n")
	start := -1
	for i, line := range lines {
		if strings.EqualFold(strings.TrimSpace(line), label+":") {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return "", false
	}
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if labelLinePattern.MatchString(strings.TrimSpace(lines[i])) {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n"), true
}

func firstNonEmptyLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func firstCapture(re *regexp.Regexp, text string) string {
	match := re.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.ToLower(match[1])
}

func collectPaths(text, repo string) []string {
	var out []string
	for _, match := range pathPattern.FindAllString(text, -1) {
		value := strings.ToLower(strings.Trim(match, "./"))
		if value == "" || value == repo || strings.Contains(value, "://") {
			continue
		}
		// A single slash without a file-like suffix is normally an owner/repo
		// slug, not a workspace path.
		if strings.Count(value, "/") < 2 && !strings.Contains(pathBase(value), ".") {
			continue
		}
		out = append(out, value)
	}
	return uniqueSorted(out)
}

func pathBase(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func collectMatches(re *regexp.Regexp, text string) []string {
	matches := re.FindAllString(text, -1)
	for i := range matches {
		matches[i] = strings.ToLower(matches[i])
	}
	return uniqueSorted(matches)
}

func collectImports(text string) []string {
	var out []string
	for _, match := range importPattern.FindAllStringSubmatch(text, -1) {
		for _, value := range match[1:] {
			if value == "" {
				continue
			}
			root := strings.Split(strings.ToLower(value), ".")[0]
			out = append(out, root)
		}
	}
	return uniqueSorted(out)
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func joinRetrievalTokens(values ...string) string {
	var tokens []string
	seen := map[string]struct{}{}
	for _, value := range values {
		for _, token := range bm25.Tokenize(value) {
			if _, drop := retrievalStopwords[token]; drop {
				continue
			}
			if _, ok := seen[token]; ok {
				continue
			}
			seen[token] = struct{}{}
			tokens = append(tokens, token)
		}
	}
	return strings.Join(tokens, " ")
}

func stripTaggedBlock(text, tag string) string {
	for {
		lower := strings.ToLower(text)
		start := strings.Index(lower, "<"+tag)
		if start < 0 {
			return text
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
	lower := strings.ToLower(text)
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
