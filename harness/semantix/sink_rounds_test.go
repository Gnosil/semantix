package semantix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/harness/event"
	"semantix/kernel/slice"
)

func TestSinkPreservesAssistantRoundsAndExtractsFinalResult(t *testing.T) {
	for _, ending := range []string{"EndTurn", "TurnDone", "Close"} {
		t.Run(ending, func(t *testing.T) {
			dir := t.TempDir()
			s, err := NewHarnessSink(dir, "rounds", "")
			if err != nil {
				t.Fatal(err)
			}
			s.Emit(event.Event{Kind: event.TurnStarted, Text: "Fix the cache key"})
			s.Emit(event.Event{Kind: event.Reasoning, Text: "PRIVATE REASONING"})
			s.Emit(event.Event{Kind: event.Text, Text: "draft text replaced by extension"})
			s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "edit", Name: "write_file", Partial: true}})
			s.Emit(event.Event{Kind: event.Message, Text: "Apply the fix", Reasoning: "PRIVATE REASONING"})
			s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "edit", Name: "write_file", Args: `{"path":"cache.go"}`}})
			s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "edit", Name: "write_file", Refreshed: true}})
			s.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "edit", Output: "updated", WorkspaceMutation: true}})
			s.Emit(event.Event{Kind: event.Message, Text: "Run the focused test"})
			s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "test", Name: "exec_command", Args: `{"command":"go test ./cache"}`}})
			s.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "test", Output: "ok", Execution: &event.ShellExecution{Verification: "passed"}}})
			s.Emit(event.Event{Kind: event.Text, Text: "Fixed the key."})
			s.Emit(event.Event{Kind: event.Message, Text: "Fixed the key.", Reasoning: "PRIVATE REASONING"})
			switch ending {
			case "EndTurn":
				s.EndTurn()
				s.EndTurn()
			case "TurnDone":
				s.Emit(event.Event{Kind: event.TurnDone})
				s.EndTurn()
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "rounds.jsonl")
			lines := readSessionLines(t, path)
			if len(lines) != 6 {
				t.Fatalf("got %d lines, want user + assistant/tool + assistant/tool + final assistant: %+v", len(lines), lines)
			}
			if lines[1].Content != "Apply the fix" || len(lines[1].ToolCalls) != 1 || lines[3].Content != "Run the focused test" || len(lines[3].ToolCalls) != 1 || lines[5].Content != "Fixed the key." || len(lines[5].ToolCalls) != 0 {
				t.Fatalf("assistant message boundaries or authoritative text lost: %+v", lines)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "PRIVATE REASONING") || strings.Contains(string(raw), "draft text") {
				t.Fatalf("non-final stream/reasoning leaked into mirror: %s", raw)
			}
			slices, err := slice.NewExtractor().Extract(raw, slice.SliceMeta{})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, sl := range slices {
				if sl.Type == slice.Result {
					found = true
					if string(sl.Content) != "Fixed the key." || sl.Meta.ResultStatus != slice.ResultStatusVerified || sl.Meta.ResultVerificationEvidence != "go test ./cache" {
						t.Fatalf("unexpected final Result: %+v", sl)
					}
				}
			}
			if !found {
				t.Fatal("final Result missing after successful tool-using turn")
			}
		})
	}
}

func TestSinkResultVerificationFollowsActualToolResultOrder(t *testing.T) {
	for _, laterMutation := range []bool{false, true} {
		t.Run(map[bool]string{false: "verified", true: "mutation_after_verification"}[laterMutation], func(t *testing.T) {
			dir := t.TempDir()
			s, err := NewHarnessSink(dir, "order", "")
			if err != nil {
				t.Fatal(err)
			}
			s.Emit(event.Event{Kind: event.TurnStarted, Text: "Fix cache"})
			// Dispatch order deliberately differs from result order.
			s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "test", Name: "exec_command", Args: `{"command":"go test ./cache"}`}})
			s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "edit", Name: "write_file", Args: `{"path":"cache.go"}`}})
			edit := event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "edit", Output: "updated", WorkspaceMutation: true}}
			check := event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "test", Output: "ok", Execution: &event.ShellExecution{Verification: "passed"}}}
			if laterMutation {
				s.Emit(check)
				s.Emit(edit)
			} else {
				s.Emit(edit)
				s.Emit(check)
			}
			s.Emit(event.Event{Kind: event.Message, Text: "Fixed cache"})
			s.EndTurn()
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "order.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			slices, err := slice.NewExtractor().Extract(raw, slice.SliceMeta{})
			if err != nil {
				t.Fatal(err)
			}
			for _, sl := range slices {
				if sl.Type == slice.Result {
					want := slice.ResultStatusVerified
					if laterMutation {
						want = slice.ResultStatusProbation
					}
					if sl.Meta.ResultStatus != want {
						t.Fatalf("Result status = %s, want %s", sl.Meta.ResultStatus, want)
					}
					return
				}
			}
			t.Fatal("Result missing")
		})
	}
}

func TestSinkTurnIsolationAndNoReasoningContent(t *testing.T) {
	dir := t.TempDir()
	s, err := NewHarnessSink(dir, "turns", "seed user")
	if err != nil {
		t.Fatal(err)
	}
	s.Emit(event.Event{Kind: event.TurnStarted})
	s.Emit(event.Event{Kind: event.Text, Text: "first answer"})
	// A new TurnStarted seals the previous turn even without TurnDone.
	s.Emit(event.Event{Kind: event.TurnStarted, Text: "second user"})
	s.Emit(event.Event{Kind: event.Reasoning, Text: "private thought"})
	s.Emit(event.Event{Kind: event.Message, Reasoning: "private thought"})
	s.EndTurn()
	s.Emit(event.Event{Kind: event.Text, Text: "late orphan delta"})
	s.Emit(event.Event{Kind: event.TurnDone})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	lines := readSessionLines(t, filepath.Join(dir, "turns.jsonl"))
	if len(lines) != 4 || lines[0].Content != "seed user" || lines[1].Content != "first answer" || lines[2].Content != "second user" || lines[3].Content != "" || len(lines[3].ToolCalls) != 0 {
		t.Fatalf("turn state leaked or duplicated: %+v", lines)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "turns.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private thought") || strings.Contains(string(raw), "orphan") {
		t.Fatalf("unexpected mirror content: %s", raw)
	}
	slices, err := slice.NewExtractor().Extract(raw, slice.SliceMeta{})
	if err != nil {
		t.Fatal(err)
	}
	for _, sl := range slices {
		if sl.Type == slice.Result {
			t.Fatal("reasoning-only last response became a Result")
		}
	}
}

func TestSinkCloseReportsWriteFailureWithoutReplayingTurn(t *testing.T) {
	dir := t.TempDir()
	s, err := NewHarnessSink(dir, "write-error", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Emit(event.Event{Kind: event.TurnStarted, Text: "user"})
	s.Emit(event.Event{Kind: event.Message, Text: "answer"})
	if err := s.file.Close(); err != nil {
		t.Fatal(err)
	}
	s.EndTurn()
	writeErr := s.err
	if writeErr == nil {
		t.Fatal("mirror write failure not retained")
	}
	s.file, err = os.OpenFile(s.path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != writeErr {
		t.Fatalf("Close error = %v, want original write error %v", err, writeErr)
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 0 {
		t.Fatalf("failed turn replayed by Close: %s", raw)
	}
}

func TestSinkDoesNotPromoteAssistantVerificationClaim(t *testing.T) {
	dir := t.TempDir()
	s, err := NewHarnessSink(dir, "claim", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Emit(event.Event{Kind: event.TurnStarted, Text: "Fix cache"})
	s.Emit(event.Event{Kind: event.Message, Text: "All tests passed; fix verified."})
	s.EndTurn()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	slices, err := slice.NewExtractor().Extract(raw, slice.SliceMeta{})
	if err != nil {
		t.Fatal(err)
	}
	for _, sl := range slices {
		if sl.Type == slice.Result {
			if sl.Meta.ResultStatus != slice.ResultStatusProbation || sl.Meta.ResultVerifiedBy != "" || sl.Meta.ResultVerificationEvidence != "" {
				t.Fatalf("assistant claim promoted without host evidence: %+v", sl.Meta)
			}
			return
		}
	}
	t.Fatal("unverified answer should remain a probation Result")
}
