package slice

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Distill turns one session transcript into the four-layer knowledge cards
// of docs/specs/semantic-four-layer-distill.md:
//
//   - repo-ops card (Context): verified test/build commands with their last
//     observed outcome, plus recurring environment pitfalls;
//   - plan-skeleton card (Memory): the session's tool trajectory abstracted
//     to canonical stages, tagged with the classified task type;
//   - outcome card (Memory): task summary, edited files and the verifying
//     command — the instance-level locator, admission-gated by task type.
//
// (The fourth layer — the subsystem overview card — is ConsolidateContext,
// which merges the Context cards this and the base extractor emit.)
//
// Unlike Extract, Distill parses tool RESULT lines and pairs them with their
// calls: the base transcript parser drops role-less lines, but outcomes
// (exit statuses, edit receipts, policy blocks) are exactly the signal the
// cards need. Mirror double-writes are deduplicated by tool-call ID.
// Deterministic: same transcript bytes → same cards, no model calls.
func Distill(sessionJSONL []byte, meta SliceMeta) ([]*Slice, error) {
	doc, err := parseDistillDoc(sessionJSONL)
	if err != nil {
		return nil, err
	}
	if meta.TaskType == "" {
		meta.TaskType = ClassifyTask(doc.userText)
	}
	var out []*Slice
	if s := repoOpsSlice(doc, meta); s != nil {
		out = append(out, s)
	}
	if s := planSkeletonSlice(doc, meta); s != nil {
		out = append(out, s)
	}
	if s := outcomeSlice(doc, meta); s != nil {
		out = append(out, s)
	}
	return dedup(out), nil
}

// Card content limits: a card must never dominate the injection budget
// (DefaultBudget 4096 bytes) — the four layers are meant to ride along with
// each other, not to crowd each other out.
const (
	maxVerifiedCommands = 8
	maxCommandLen       = 160
	maxPitfalls         = 6
	maxPitfallLen       = 120
	maxEditedFiles      = 10
	maxSummaryLen       = 240
	maxSkeletonStages   = 24
)

// distillCall is one tool call in first-seen order, deduplicated by ID.
type distillCall struct {
	id   string
	name string
	args json.RawMessage
}

// distillDoc is the paired view of one transcript: calls in order, results
// keyed by tool-call ID, and the first real user message.
type distillDoc struct {
	userText string
	calls    []distillCall
	results  map[string]string
}

// distillLine decodes the union of the three mirror line shapes: kernel
// event lines (kind, skipped), role lines (user/assistant with tool calls)
// and tool result lines (tool_call_id + content, no role).
type distillLine struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolCalls []struct {
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	} `json:"tool_calls,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

func parseDistillDoc(data []byte) (*distillDoc, error) {
	doc := &distillDoc{results: map[string]string{}}
	callIdx := map[string]int{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var dl distillLine
		if json.Unmarshal(line, &dl) != nil {
			continue // tolerant: skip malformed lines
		}
		switch {
		case dl.Role == "user":
			if doc.userText == "" && strings.TrimSpace(dl.Content) != "" {
				doc.userText = dl.Content
			}
		case dl.Role == "assistant":
			for _, tc := range dl.ToolCalls {
				if tc.ID == "" || tc.Name == "" {
					continue
				}
				if i, seen := callIdx[tc.ID]; seen {
					// Streamed mirrors re-emit calls; keep the most complete
					// argument payload for the pairing below.
					if len(tc.Arguments) > len(doc.calls[i].args) {
						doc.calls[i].args = tc.Arguments
					}
					continue
				}
				callIdx[tc.ID] = len(doc.calls)
				doc.calls = append(doc.calls, distillCall{id: tc.ID, name: tc.Name, args: tc.Arguments})
			}
		case dl.Role == "" && dl.ToolCallID != "":
			// Tool result line; mirror double-writes keep the first copy.
			if _, seen := doc.results[dl.ToolCallID]; !seen {
				doc.results[dl.ToolCallID] = dl.Content
			}
		}
	}
	return doc, sc.Err()
}

// --- repo-ops card (layer A) -----------------------------------------------

// pitfallPatterns are recurring environment/policy failure signatures worth
// remembering per repo. Matched case-insensitively against a result's head.
var pitfallPatterns = []string{
	"blocked:",
	"outside the writable roots",
	"no module named",
	"modulenotfounderror",
	"importerror",
	"command not found",
	"permission denied",
}

func repoOpsSlice(doc *distillDoc, meta SliceMeta) *Slice {
	type observed struct {
		command string
		outcome string // "ok" or "exit N"
	}
	var commands []observed
	seenCmd := map[string]int{}
	pitfalls := map[string]int{}
	pitfallOrder := []string{}

	for _, call := range doc.calls {
		result, hasResult := doc.results[call.id]
		head := strings.ToLower(strings.TrimSpace(result))
		if hasResult {
			for _, pat := range pitfallPatterns {
				if strings.Contains(head[:min(len(head), 200)], pat) {
					line := firstLine(result, maxPitfallLen)
					if _, seen := pitfalls[line]; !seen {
						pitfallOrder = append(pitfallOrder, line)
					}
					pitfalls[line]++
					break
				}
			}
		}
		if !shellToolNames[strings.ToLower(call.name)] {
			continue
		}
		cmd := normalizeCommand(commandValue(decodeToolArgs(call.args)))
		if cmd == "" || !(isTestCommand(cmd) || isBuildCommand(cmd)) {
			continue
		}
		outcome := commandOutcome(result, hasResult)
		if outcome == "" {
			continue // policy-blocked or never ran: not a verified observation
		}
		if i, seen := seenCmd[cmd]; seen {
			commands[i].outcome = outcome // later run wins: freshest signal
			continue
		}
		seenCmd[cmd] = len(commands)
		commands = append(commands, observed{command: cmd, outcome: outcome})
	}

	if len(commands) == 0 && len(pitfalls) == 0 {
		return nil
	}
	if len(commands) > maxVerifiedCommands {
		commands = commands[:maxVerifiedCommands]
	}

	var b strings.Builder
	b.WriteString("Repo operations (distilled from session):\n")
	if len(commands) > 0 {
		b.WriteString("Verified commands:\n")
		for _, c := range commands {
			fmt.Fprintf(&b, "- %s (%s)\n", c.command, c.outcome)
		}
	}
	if len(pitfalls) > 0 {
		// Count-desc then lexical for a deterministic card; cap the list.
		sort.SliceStable(pitfallOrder, func(i, j int) bool {
			if pitfalls[pitfallOrder[i]] != pitfalls[pitfallOrder[j]] {
				return pitfalls[pitfallOrder[i]] > pitfalls[pitfallOrder[j]]
			}
			return pitfallOrder[i] < pitfallOrder[j]
		})
		if len(pitfallOrder) > maxPitfalls {
			pitfallOrder = pitfallOrder[:maxPitfalls]
		}
		b.WriteString("Known pitfalls:\n")
		for _, p := range pitfallOrder {
			fmt.Fprintf(&b, "- %s (seen %d)\n", p, pitfalls[p])
		}
	}
	return newSlice(Context, Project, []byte(strings.TrimSpace(b.String())), meta)
}

// normalizeCommand strips the leading repo-position noise (`cd <dir> && `)
// and bounds the length so cards stay injectable.
func normalizeCommand(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, "cd ") {
		if _, rest, ok := strings.Cut(cmd, "&&"); ok {
			cmd = strings.TrimSpace(rest)
		}
	}
	if len(cmd) > maxCommandLen {
		cmd = cmd[:maxCommandLen] + "…"
	}
	return cmd
}

// isTestCommand reports whether cmd runs a test/verification suite.
func isTestCommand(cmd string) bool {
	l := strings.ToLower(cmd)
	for _, pat := range []string{"pytest", "runtests", "unittest", "tox "} {
		if strings.Contains(l, pat) {
			return true
		}
	}
	fields := strings.Fields(l)
	for i, f := range fields {
		if f == "test" && i > 0 { // go test / make test / npm test / cargo test
			return true
		}
	}
	return false
}

// isBuildCommand reports whether cmd builds or installs — the usual carrier
// of per-repo build quirks.
func isBuildCommand(cmd string) bool {
	l := strings.ToLower(cmd)
	if strings.Contains(l, "install") || strings.Contains(l, "setup.py") || strings.Contains(l, " build") {
		return true
	}
	return strings.HasPrefix(l, "make") || strings.HasPrefix(l, "cargo build") || strings.HasPrefix(l, "go build")
}

// commandOutcome maps a paired result to "ok" / "exit N"; "" means the
// command never actually ran (missing result or a policy block).
func commandOutcome(result string, hasResult bool) string {
	if !hasResult {
		return ""
	}
	head := strings.ToLower(strings.TrimSpace(result))
	for _, pat := range pitfallPatterns {
		if strings.HasPrefix(head, pat) {
			return ""
		}
	}
	if i := strings.LastIndex(result, "exit status "); i >= 0 {
		code := strings.TrimSpace(firstLine(result[i+len("exit status "):], 8))
		if code != "" {
			return "exit " + code
		}
	}
	return "ok"
}

// --- plan-skeleton card (layer C) ------------------------------------------

// stageOf maps one tool call to a canonical plan stage; "" means the call
// carries no planning signal (goal bookkeeping etc.).
func stageOf(call distillCall) string {
	name := strings.ToLower(call.name)
	args := decodeToolArgs(call.args)
	if shellToolNames[name] {
		cmd := strings.ToLower(normalizeCommand(commandValue(args)))
		switch {
		case cmd == "":
			return ""
		case isTestCommand(cmd):
			return "verify"
		case strings.Contains(cmd, "repro"):
			return "reproduce"
		default:
			head := strings.Fields(cmd)
			if len(head) > 0 {
				switch head[0] {
				case "rg", "grep", "find", "ls", "cat", "head", "tail", "git", "sed", "awk":
					return "locate"
				case "python", "python3":
					return "reproduce" // running a script that is not a test suite
				}
			}
			return "" // opaque shell work: no stage signal
		}
	}
	switch {
	case strings.Contains(name, "test"):
		return "verify"
	case strings.Contains(name, "read") || strings.Contains(name, "grep") ||
		strings.Contains(name, "search") || strings.Contains(name, "find") ||
		strings.Contains(name, "list") || strings.Contains(name, "glob"):
		return "locate"
	case strings.Contains(name, "edit") || strings.Contains(name, "write") ||
		strings.Contains(name, "patch") || strings.Contains(name, "replace"):
		for _, p := range contextPaths(args) {
			if strings.Contains(strings.ToLower(p), "repro") {
				return "reproduce"
			}
		}
		return "edit"
	}
	return ""
}

func planSkeletonSlice(doc *distillDoc, meta SliceMeta) *Slice {
	type stageRun struct {
		stage string
		count int
	}
	var runs []stageRun
	for _, call := range doc.calls {
		s := stageOf(call)
		if s == "" {
			continue
		}
		if n := len(runs); n > 0 && runs[n-1].stage == s {
			runs[n-1].count++
			continue
		}
		runs = append(runs, stageRun{stage: s, count: 1})
	}
	if len(runs) < 2 {
		return nil // one stage is a trajectory, not a plan
	}
	truncated := false
	if len(runs) > maxSkeletonStages {
		runs = runs[:maxSkeletonStages]
		truncated = true
	}
	parts := make([]string, 0, len(runs)+1)
	for _, r := range runs {
		if r.count > 1 {
			parts = append(parts, fmt.Sprintf("%s(×%d)", r.stage, r.count))
		} else {
			parts = append(parts, r.stage)
		}
	}
	if truncated {
		parts = append(parts, "…")
	}
	content := fmt.Sprintf("Plan skeleton (task=%s):\n%s", meta.TaskType, strings.Join(parts, " → "))
	return newSlice(Memory, Project, []byte(content), meta)
}

// --- outcome card (layer D) ------------------------------------------------

func outcomeSlice(doc *distillDoc, meta SliceMeta) *Slice {
	summary := summarizeTask(doc.userText)
	if summary == "" {
		return nil // nothing to anchor the locator on
	}
	roots := cdRoots(doc)
	var edited []string
	seenEdit := map[string]bool{}
	var verifiedBy string
	for _, call := range doc.calls {
		result, hasResult := doc.results[call.id]
		if hasResult {
			for _, p := range editReceiptPaths(result) {
				p = stripRoots(p, roots)
				if !seenEdit[p] {
					seenEdit[p] = true
					edited = append(edited, p)
				}
			}
		}
		if shellToolNames[strings.ToLower(call.name)] {
			cmd := normalizeCommand(commandValue(decodeToolArgs(call.args)))
			if cmd != "" && isTestCommand(cmd) && commandOutcome(result, hasResult) == "ok" {
				verifiedBy = cmd // last passing test run wins
			}
		}
	}
	if len(edited) == 0 {
		return nil // no observable outcome: a locator without a location misleads
	}
	if len(edited) > maxEditedFiles {
		edited = edited[:maxEditedFiles]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Task outcome (task=%s): %s\n", meta.TaskType, summary)
	b.WriteString("Edited:\n")
	for _, p := range edited {
		fmt.Fprintf(&b, "- %s\n", p)
	}
	if verifiedBy != "" {
		fmt.Fprintf(&b, "Verified-by: %s\n", verifiedBy)
	}
	return newSlice(Memory, Project, []byte(strings.TrimSpace(b.String())), meta)
}

// summarizeTask condenses the first user message to one bounded line. Task
// templates bury the distinguishing text behind a preamble, so an "Issue:"
// section wins over the head of the message; leading tag lines are skipped.
func summarizeTask(text string) string {
	if i := strings.Index(text, "Issue:"); i >= 0 {
		text = text[i+len("Issue:"):]
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "<") {
			continue
		}
		if len(line) > maxSummaryLen {
			line = line[:maxSummaryLen] + "…"
		}
		return line
	}
	return ""
}

// editReceiptPaths extracts file paths from write/edit receipts ("edited
// <path>", "wrote N bytes to <path>").
func editReceiptPaths(result string) []string {
	var out []string
	for _, line := range strings.SplitN(result, "\n", 4) {
		line = strings.TrimSpace(line)
		if p, ok := strings.CutPrefix(line, "edited "); ok {
			out = append(out, strings.TrimSpace(p))
			continue
		}
		if _, rest, ok := strings.Cut(line, " bytes to "); ok && strings.HasPrefix(line, "wrote ") {
			out = append(out, strings.TrimSpace(rest))
		}
	}
	return out
}

// cdRoots collects the directories the session cd'ed into — the strongest
// repo-root signal a transcript carries — for edit-path normalization.
func cdRoots(doc *distillDoc) []string {
	seen := map[string]bool{}
	var roots []string
	for _, call := range doc.calls {
		if !shellToolNames[strings.ToLower(call.name)] {
			continue
		}
		cmd := strings.TrimSpace(commandValue(decodeToolArgs(call.args)))
		if !strings.HasPrefix(cmd, "cd ") {
			continue
		}
		target := cmd[len("cd "):]
		if i := strings.IndexAny(target, " \t&;"); i >= 0 {
			target = target[:i]
		}
		target = strings.TrimRight(strings.TrimSpace(target), "/")
		if target != "" && !seen[target] {
			seen[target] = true
			roots = append(roots, target)
		}
	}
	// Longest first so nested roots strip the most specific prefix.
	sort.SliceStable(roots, func(i, j int) bool { return len(roots[i]) > len(roots[j]) })
	return roots
}

func stripRoots(p string, roots []string) string {
	for _, r := range roots {
		if rest, ok := strings.CutPrefix(p, r+"/"); ok {
			return rest
		}
	}
	return p
}

func firstLine(s string, limit int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > limit {
		s = s[:limit] + "…"
	}
	return s
}
