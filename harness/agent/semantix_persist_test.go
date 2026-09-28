package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/harness/event"
	"semantix/harness/provider"
	"semantix/harness/semantix"
	"semantix/harness/tool"
	"semantix/kernel/slice"
)

// persistRecProvider replies "ok" and records every request's messages.
type persistRecProvider struct{ reqs [][]provider.Message }

func (r *persistRecProvider) Name() string { return "recording" }
func (r *persistRecProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	r.reqs = append(r.reqs, append([]provider.Message(nil), req.Messages...))
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func persistBridge(t *testing.T, placement string) *semantix.Bridge {
	t.Helper()
	root := t.TempDir()
	commit := strings.Repeat("1", 40)
	for _, dir := range []string{".git", ".semantix"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte(commit), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := slice.NewFileStore(filepath.Join(root, ".semantix", "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	card := &slice.Slice{ID: "parser-memory", Type: slice.Context, Scope: slice.Project,
		Content: []byte("parser regression parse parser"),
		Meta:    slice.SliceMeta{Origin: slice.OriginSessionAuto, BaseCommit: commit, SourceSession: "prior"}}
	if err := s.Put(card); err != nil {
		t.Fatal(err)
	}
	if err := s.(interface{ Close() error }).Close(); err != nil {
		t.Fatal(err)
	}
	b := semantix.NewBridge(semantix.Config{Enabled: true, Mode: "strict", Placement: placement,
		ProjectDir: root, SessionsDir: filepath.Join(root, "sessions"), Budget: 4096})
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func renderMessages(msgs []provider.Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, string(m.Role)+"\x00"+m.Content)
	}
	return out
}

func isMessagePrefix(prev, next []string) bool {
	if len(prev) > len(next) {
		return false
	}
	for i := range prev {
		if prev[i] != next[i] {
			return false
		}
	}
	return true
}

// TestPersistPlacementIsAppendOnly: with placement = "persist", the second
// user turn's request extends the first one message-for-message, the block
// lives in the transcript at the head of the turn's user message, and the
// policy sentence is present on every request. With the default ephemeral
// placement the same two turns are NOT append-only (the old block vanishes
// from the middle of history) — the regression this placement exists to fix.
func TestPersistPlacementIsAppendOnly(t *testing.T) {
	run := func(placement string) (*persistRecProvider, *Session) {
		p := &persistRecProvider{}
		sess := NewSession("system rules")
		a := New(p, tool.NewRegistry(), sess, Options{Semantix: persistBridge(t, placement)}, event.Discard)
		for _, in := range []string{"repair parser regression", "parser regression still failing"} {
			if err := a.Run(context.Background(), in); err != nil {
				t.Fatal(err)
			}
		}
		return p, sess
	}

	p, sess := run("persist")
	if len(p.reqs) < 2 {
		t.Fatalf("requests = %d, want >= 2", len(p.reqs))
	}
	first, second := renderMessages(p.reqs[0]), renderMessages(p.reqs[len(p.reqs)-1])
	if !isMessagePrefix(first, second) {
		t.Fatalf("persist: request 2 does not extend request 1:\n%q\n%q", first, second)
	}
	for i, req := range p.reqs {
		if req[0].Role != provider.RoleSystem || !strings.Contains(req[0].Content, semantixHistoryPolicy) {
			t.Fatalf("persist: request %d lacks the session-constant policy: %+v", i, req[0])
		}
	}
	var withBlock int
	for _, m := range sess.Messages {
		if m.Role == provider.RoleUser && strings.HasPrefix(m.Content, "[semantix-reuse]") {
			withBlock++
			if strings.Contains(m.RawContent, "[semantix-reuse]") {
				t.Fatalf("RawContent must keep the user's own text: %q", m.RawContent)
			}
		}
	}
	if withBlock == 0 {
		t.Fatal("persist: no user message in the transcript carries the block")
	}

	p, _ = run("")
	first, second = renderMessages(p.reqs[0]), renderMessages(p.reqs[len(p.reqs)-1])
	if isMessagePrefix(first, second) {
		t.Fatal("ephemeral placement unexpectedly append-only; the fixture no longer exercises the regression")
	}
}

// TestPersistFuseAppendsRetraction: a loop guard cannot remove a persisted
// block; it appends a retraction instead and keeps history untouched.
func TestPersistFuseAppendsRetraction(t *testing.T) {
	b := persistBridge(t, "persist")
	sess := NewSession("system rules")
	a := New(nil, tool.NewRegistry(), sess, Options{Semantix: b}, event.Discard)
	a.turn.injectBlock = "[semantix-reuse]\nx\n[/semantix-reuse]"
	a.turn.injectTargets = []string{"ctx-a"}
	a.turn.injectPersisted = true
	before := len(sess.Messages)
	a.armLoopGuardPass(0)
	if a.turn.injectBlock == "" || !a.turn.injectionFused {
		t.Fatalf("persisted block must stay and the fuse must latch: %+v", a.turn)
	}
	if len(sess.Messages) != before+1 {
		t.Fatalf("messages %d -> %d, want one appended retraction", before, len(sess.Messages))
	}
	last := sess.Messages[len(sess.Messages)-1]
	if last.Role != provider.RoleUser || !strings.HasPrefix(last.Content, semantixRetractionPrefix) || !strings.Contains(last.Content, "ctx-a") {
		t.Fatalf("retraction = %+v", last)
	}
	if !IsSyntheticUserText(last.Content) {
		t.Fatal("retraction must be classified as a synthetic user message")
	}
}

func TestStripSemantixReuseForSummary(t *testing.T) {
	in := "[semantix-reuse]\nslice body\n[/semantix-reuse]\n\nfix the parser"
	if got := stripSemantixReuse(in); got != "fix the parser" {
		t.Fatalf("got %q", got)
	}
	if got := stripSemantixReuse("plain text"); got != "plain text" {
		t.Fatalf("got %q", got)
	}
}
