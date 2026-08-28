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
	Role      string         `json:"role,omitempty"`
	Content   string         `json:"content,omitempty"`
	ToolCalls []toolCallLine `json:"tool_calls,omitempty"`
	Type      string         `json:"type,omitempty"`
	ToolCall  string         `json:"tool_call_id,omitempty"`
	Name      string         `json:"name,omitempty"`
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
// stay contained here. Failures are non-fatal: a write error drops the turn
// and is surfaced on the next success instead of breaking the agent.
type HarnessSink struct {
	mu      sync.Mutex
	path    string // .semantix/sessions/<sessionID>.jsonl
	file    *os.File
	turn    bool   // a turn is currently open
	first   string // user text of the current turn
	text    string // assistant text buffer
	reason  string // reasoning buffer
	tools   []toolCallLine
	outputs map[string]string // tool output keyed by tool call ID (filled by ToolResult)
	err     error
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
	switch e.Kind {
	case event.TurnStarted:
		s.flushLocked()
		s.turn = true
		s.text, s.reason, s.tools = "", "", nil
		// The harness sends the turn's user text on TurnStarted; without it
		// the mirrored transcript has no user lines and the kernel extractor
		// can never produce Prompt slices from these sessions.
		if e.Text != "" {
			s.first = e.Text
		}
	case event.Reasoning:
		s.reason += e.Text
	case event.Text:
		s.text += e.Text
	case event.Message:
		// keep the accumulated text; nothing extra to do
	case event.ToolDispatch:
		var args json.RawMessage
		if e.Tool.Args != "" {
			// Args is a JSON string (may be a raw object or an encoded blob).
			if raw := json.RawMessage(e.Tool.Args); json.Valid(raw) {
				args = raw
			} else {
				args, _ = json.Marshal(e.Tool.Args)
			}
		}
		if args == nil {
			args = json.RawMessage("{}")
		}
		s.tools = append(s.tools, toolCallLine{ID: e.Tool.ID, Name: e.Tool.Name, Arguments: args})
	case event.ToolResult:
		out := e.Tool.Output
		if e.Tool.Err != "" {
			out = e.Tool.Err
		}
		if s.outputs == nil {
			s.outputs = make(map[string]string)
		}
		s.outputs[e.Tool.ID] = out
	case event.TurnDone:
		s.flushLocked()
		s.turn = false
	}
}

// flushLocked writes the current turn (user + assistant + tool lines).
func (s *HarnessSink) flushLocked() {
	if !s.turn {
		return
	}
	var lines []sessionLine
	if s.first != "" {
		lines = append(lines, sessionLine{Role: "user", Content: s.first})
	}
	if s.text != "" || len(s.tools) > 0 {
		lines = append(lines, sessionLine{Role: "assistant", Content: s.text, ToolCalls: s.tools})
	}
	for _, t := range s.tools {
		content := s.outputs[t.ID]
		if content == "" {
			content = "(tool output)"
		}
		lines = append(lines, sessionLine{Type: "tool", ToolCall: t.ID, Name: t.Name, Content: content})
	}
	s.first = ""
	s.outputs = nil
	for _, ln := range lines {
		b, err := json.Marshal(ln)
		if err != nil {
			continue
		}
		if _, err := s.file.Write(append(b, '\n')); err != nil {
			s.err = err
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
	s.turn = false
	s.text, s.reason, s.tools = "", "", nil
}

// Close flushes and closes the underlying file.
func (s *HarnessSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushLocked()
	return s.file.Close()
}
