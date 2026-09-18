// Package semantix bridges the semantix harness to the semantix kernel:
// session events are mirrored to kernel-compatible session JSONL, and kernel
// retrieval (lookup/inject) is exposed to the agent via subprocess calls.
package semantix

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"semantix/harness/event"
	kernelevent "semantix/kernel/event"
)

// sessionLine is one line of the kernel session JSONL consumed by
// `semantix extract --input`. Fields mirror the extractor contract.
type sessionLine struct {
	Role              string         `json:"role,omitempty"`
	Content           string         `json:"content,omitempty"`
	ToolCalls         []toolCallLine `json:"tool_calls,omitempty"`
	Type              string         `json:"type,omitempty"`
	ToolCall          string         `json:"tool_call_id,omitempty"`
	Name              string         `json:"name,omitempty"`
	Verification      string         `json:"verification,omitempty"`
	WorkspaceMutation bool           `json:"workspace_mutation,omitempty"`
}

// EmitKernel appends the original kernel event wire object to the same real
// session JSONL as the harness transcript.
func (s *HarnessSink) EmitKernel(e kernelevent.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := kernelevent.ToJSON(e)
	if err != nil {
		return
	}
	if _, err := s.file.Write(append(b, '\n')); err != nil {
		s.err = err
		return
	}
	if s.err == nil {
		s.err = s.file.Sync()
	}
}

type toolCallLine struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// HarnessSink mirrors semantix events into kernel-compatible session JSONL.
// It depends only on the stable event subset (TurnStarted/Reasoning/Text/
// Message/ToolDispatch/ToolResult/TurnDone) so upstream interface changes
// stay contained here. Emit remains non-fatal; Close reports any mirror write
// error without changing the agent's execution outcome.
type HarnessSink struct {
	mu          sync.Mutex
	path        string // .semantix/sessions/<sessionID>.jsonl
	file        *os.File
	turn        bool
	first       string // user text of the current turn
	lines       []sessionLine
	assistant   int // current assistant line, or -1 after a tool result
	messageDone bool
	pending     map[string]toolCallLine
	err         error
}

// NewHarnessSink creates the sink, creating the sessions dir (0700).
// sessionID should be the agent's session id; firstUserText seeds the first
// turn's user line.
func NewHarnessSink(dir, sessionID, firstUserText string) (*HarnessSink, error) {
	// sessionID flows into a filename: reject separators (/ and \ — the
	// latter for Windows portability) and dot/empty ids so a caller-supplied
	// id can never escape dir on any platform.
	if sessionID == "" || sessionID == "." || sessionID == ".." || sessionID == "/" ||
		strings.ContainsAny(sessionID, `/\`) {
		return nil, fmt.Errorf("semantix sink: invalid sessionID %q", sessionID)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("semantix sink: mkdir: %w", err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	// Refuse a pre-placed symlink: OpenFile(APPEND) would follow it and append
	// session events into an attacker-chosen file. Lstat is cross-platform;
	// the 0700 dir bounds the race window to same-user tampering.
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("semantix sink: %s is a symlink", path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("semantix sink: open: %w", err)
	}
	return &HarnessSink{path: filepath.Join(dir, sessionID+".jsonl"), file: f, first: firstUserText}, nil
}

// Emit implements event.Sink.
func (s *HarnessSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.turn && e.Kind != event.TurnStarted {
		return
	}
	switch e.Kind {
	case event.TurnStarted:
		s.flushLocked()
		s.turn = true
		s.assistant = -1
		if e.Text != "" {
			s.first = e.Text
		}
	case event.Reasoning, event.Text, event.Message:
		// Message closes one provider response, not the whole user turn.
		// A later stream starts a new assistant record. Reasoning itself is
		// never mirrored, including the Reasoning field on Message.
		if s.messageDone {
			s.assistant, s.messageDone = -1, false
		}
		if e.Kind == event.Text {
			s.assistantLocked().Content += e.Text
		} else if e.Kind == event.Message {
			// Extensions can replace streamed text; the terminal message wins.
			s.assistantLocked().Content = e.Text
			s.messageDone = true
		}
	case event.ToolDispatch:
		// Streaming previews and UI refreshes are not additional calls.
		if e.Tool.Partial || e.Tool.Refreshed {
			return
		}
		var args json.RawMessage
		if e.Tool.Args != "" {
			if raw := json.RawMessage(e.Tool.Args); json.Valid(raw) {
				args = raw
			} else {
				args, _ = json.Marshal(e.Tool.Args)
			}
		}
		if args == nil {
			args = json.RawMessage("{}")
		}
		call := toolCallLine{ID: e.Tool.ID, Name: e.Tool.Name, Arguments: args}
		line := s.assistantLocked()
		line.ToolCalls = append(line.ToolCalls, call)
		if s.pending == nil {
			s.pending = make(map[string]toolCallLine)
		}
		s.pending[call.ID] = call
	case event.ToolResult:
		out := e.Tool.Output
		if e.Tool.Err != "" {
			out = e.Tool.Err
		}
		if out == "" {
			out = "(tool output)"
		}
		name := e.Tool.Name
		if call, ok := s.pending[e.Tool.ID]; ok {
			name = call.Name
		}
		delete(s.pending, e.Tool.ID)
		verification := ""
		if e.Tool.Execution != nil {
			verification = e.Tool.Execution.Verification
		}
		// Preserve host result order: a mutation after a passing test must
		// not become a verified Result by sorting outputs into dispatch order.
		s.lines = append(s.lines, sessionLine{
			Type: "tool", ToolCall: e.Tool.ID, Name: name, Content: out,
			Verification: verification, WorkspaceMutation: e.Tool.WorkspaceMutation,
		})
		s.assistant, s.messageDone = -1, false
	case event.TurnDone:
		s.flushLocked()
	}
}

func (s *HarnessSink) assistantLocked() *sessionLine {
	if s.assistant < 0 {
		s.lines = append(s.lines, sessionLine{Role: "assistant"})
		s.assistant = len(s.lines) - 1
	}
	return &s.lines[s.assistant]
}

// flushLocked seals the current turn exactly once, retaining message boundaries.
func (s *HarnessSink) flushLocked() {
	if !s.turn {
		return
	}
	var lines []sessionLine
	if s.first != "" {
		lines = append(lines, sessionLine{Role: "user", Content: s.first})
	}
	lines = append(lines, s.lines...)
	// Interrupted calls still retain a paired placeholder, in dispatch order.
	for _, line := range s.lines {
		for _, call := range line.ToolCalls {
			if _, ok := s.pending[call.ID]; ok {
				lines = append(lines, sessionLine{Type: "tool", ToolCall: call.ID, Name: call.Name, Content: "(tool output)"})
				delete(s.pending, call.ID)
			}
		}
	}
	s.turn, s.first, s.lines, s.pending = false, "", nil, nil
	s.assistant, s.messageDone = -1, false
	for _, ln := range lines {
		b, err := json.Marshal(ln)
		if err != nil {
			s.err = err
			return
		}
		if _, err := s.file.Write(append(b, '\n')); err != nil {
			s.err = err
			return
		}
	}
	if s.err == nil {
		s.err = s.file.Sync()
	}
}

// EndTurn flushes the open turn and closes it. Synchronous controller runs
// deliberately never emit TurnDone into the event stream (see stats.Recorder),
// and a headless single-turn process can exit without reaching Close — so the
// harness calls this at the end of each Run. Without it, `run -p` sessions
// leave a permanently empty mirror and the kernel extractor has no input.
func (s *HarnessSink) EndTurn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushLocked()
}

// Close flushes and closes the underlying file.
func (s *HarnessSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushLocked()
	closeErr := s.file.Close()
	if s.err != nil {
		return s.err
	}
	return closeErr
}
