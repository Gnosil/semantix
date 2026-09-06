package semantix

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/harness/event"
)

// readSessionLines reads a session JSONL file back into sessionLine values.
func readSessionLines(t *testing.T, path string) []sessionLine {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []sessionLine
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var ln sessionLine
		if err := json.Unmarshal(sc.Bytes(), &ln); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		lines = append(lines, ln)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

// toolLine picks the tool line with the given call ID from its turn's lines.
func toolLine(t *testing.T, lines []sessionLine, callID string) sessionLine {
	t.Helper()
	for _, ln := range lines {
		if ln.Type == "tool" && ln.ToolCall == callID {
			return ln
		}
	}
	t.Fatalf("no tool line for call ID %q in %d lines", callID, len(lines))
	return sessionLine{}
}

// TestSinkMultipleToolResultsEachCarryOwnOutput is the regression for #252: a
// turn with several tool calls must emit one tool line per call, each carrying
// that call's own result — not the last result for every call.
func TestSinkMultipleToolResultsEachCarryOwnOutput(t *testing.T) {
	dir := t.TempDir()
	s, err := NewHarnessSink(dir, "multi-tool", "")
	if err != nil {
		t.Fatal(err)
	}
	emit := func(k event.Kind, tool event.Tool) {
		s.Emit(event.Event{Kind: k, Tool: tool})
	}
	emit(event.TurnStarted, event.Tool{})
	// readA gets its own result, then readB gets a different one.
	emit(event.ToolDispatch, event.Tool{ID: "ta", Name: "readA", Args: "{}"})
	emit(event.ToolResult, event.Tool{ID: "ta", Output: "OUTPUT-A"})
	emit(event.ToolDispatch, event.Tool{ID: "tb", Name: "readB", Args: "{}"})
	emit(event.ToolResult, event.Tool{ID: "tb", Output: "OUTPUT-B"})
	emit(event.TurnDone, event.Tool{})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	lines := readSessionLines(t, filepath.Join(dir, "multi-tool.jsonl"))
	if got := toolLine(t, lines, "ta").Content; got != "OUTPUT-A" {
		t.Errorf("readA content = %q, want %q (must not be overwritten by readB)", got, "OUTPUT-A")
	}
	if got := toolLine(t, lines, "tb").Content; got != "OUTPUT-B" {
		t.Errorf("readB content = %q, want %q", got, "OUTPUT-B")
	}
}

// TestSinkToolResultErrorUsesErrText verifies an errored tool stores the Err
// text on its own line and a call without a result falls back to the placeholder.
func TestSinkToolResultErrorUsesErrText(t *testing.T) {
	dir := t.TempDir()
	s, err := NewHarnessSink(dir, "tool-err", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Emit(event.Event{Kind: event.TurnStarted})
	s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "t1", Name: "failing", Args: "{}"}})
	s.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "t1", Output: "partial", Err: "boom"}})
	s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "t2", Name: "nominal", Args: "{}"}})
	// t2 is dispatched but gets no ToolResult before the turn ends.
	s.Emit(event.Event{Kind: event.TurnDone})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	lines := readSessionLines(t, filepath.Join(dir, "tool-err.jsonl"))
	if got := toolLine(t, lines, "t1").Content; got != "boom" {
		t.Errorf("t1 content = %q, want %q (Err text)", got, "boom")
	}
	if got := toolLine(t, lines, "t2").Content; got != "(tool output)" {
		t.Errorf("t2 content = %q, want %q (placeholder for missing result)", got, "(tool output)")
	}
}

func TestSinkPersistsHostVerificationAndMutationMetadata(t *testing.T) {
	dir := t.TempDir()
	s, err := NewHarnessSink(dir, "verification-meta", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Emit(event.Event{Kind: event.TurnStarted})
	s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "test", Name: "exec_command", Args: "{}"}})
	s.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID: "test", Output: "ok", WorkspaceMutation: true,
		Execution: &event.ShellExecution{Verification: "passed"},
	}})
	s.Emit(event.Event{Kind: event.TurnDone})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	got := toolLine(t, readSessionLines(t, filepath.Join(dir, "verification-meta.jsonl")), "test")
	if got.Verification != "passed" || !got.WorkspaceMutation {
		t.Fatalf("tool result metadata = %+v", got)
	}
}
