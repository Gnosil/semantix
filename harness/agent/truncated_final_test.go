package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"semantix/harness/event"
	"semantix/harness/provider"
)

func TestLengthTruncationContinuesTaskBeforeReadiness(t *testing.T) {
	p := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkReasoning, Text: "partial analysis"},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "length", CompletionTokens: 8192}},
			{Type: provider.ChunkDone}},
		{toolCallChunk("read", "read_file", `{"path":"parser.go"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "I have not fixed it."}, {Type: provider.ChunkDone}},
	}}
	reg := evidenceRegistry()
	reg.Add(fakeReadFileTool{})
	a := New(p, reg, NewSession("system"), Options{DeliveryProfile: true, MaxSteps: 5}, event.Discard)
	err := a.Run(context.Background(), "fix the parser bug")
	var readiness *FinalReadinessError
	if !errors.As(err, &readiness) {
		t.Fatalf("unfinished work must still fail readiness: %v", err)
	}
	if p.call != 3 || toolResult(a.Session(), "read_file") != "contents" {
		t.Fatalf("truncation ended work before next tool: calls=%d", p.call)
	}
	if !sessionHasUserMessageContaining(a.Session(), "output limit") {
		t.Fatal("missing explicit truncation continuation")
	}
}

func TestLengthTruncationAllowsCompletedWorkAndResetsAfterTools(t *testing.T) {
	truncated := []provider.Chunk{
		{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "length", CompletionTokens: 8192}},
		{Type: provider.ChunkDone},
	}
	p := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		truncated, truncated,
		{toolCallChunk("todo", "todo_write", `{"todos":[{"content":"Fix parser","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("write", "write_file", `{"path":"parser.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("review", "read_file", `{"path":"parser.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("test", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff", "complete_step", `{"step":"Fix parser","result":"done","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`), {Type: provider.ChunkDone}},
		truncated, truncated,
		{{Type: provider.ChunkText, Text: "Fixed and tested the parser."}, {Type: provider.ChunkDone}},
	}}
	sink := &recordSink{}
	reg := evidenceRegistry()
	reg.Add(fakeReadFileTool{})
	a := New(p, reg, NewSession("system"), Options{DeliveryProfile: true, MaxSteps: 12}, sink)
	if err := a.Run(context.Background(), "fix the parser bug"); err != nil {
		t.Fatal(err)
	}
	if p.call != 10 || toolResult(a.Session(), "write_file") != "wrote" || toolResult(a.Session(), "bash") != "ok" {
		t.Fatalf("work did not finish after truncation: calls=%d", p.call)
	}
	if got := a.task.budget.outputTokens; got != 4*8192 {
		t.Fatalf("output accounting = %d, want %d", got, 4*8192)
	}
	notices := 0
	for _, e := range sink.kinds(event.Notice) {
		if e.Code == "output_truncated" {
			notices++
		}
	}
	if notices != 4 {
		t.Fatalf("truncation notices = %d, want 4", notices)
	}
}

func TestLengthTruncationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, finish, text string
		steps, calls       int
	}{
		{"bounded", "length", "", 40, 3},
		{"partial_visible", "length", "unfinished fragment", 40, 3},
		{"step_limit", "length", "", 1, 1},
		{"ordinary_stop", "stop", "not done", 40, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &scriptedProvider{name: "p", turns: [][]provider.Chunk{{
				{Type: provider.ChunkText, Text: tc.text},
				{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: tc.finish}},
				{Type: provider.ChunkDone},
			}}}
			a := New(p, evidenceRegistry(), NewSession("system"), Options{DeliveryProfile: true, MaxSteps: tc.steps}, event.Discard)
			err := a.Run(context.Background(), "fix the parser bug")
			if err == nil || p.call != tc.calls {
				t.Fatalf("calls=%d err=%v; want %d calls and non-success", p.call, err, tc.calls)
			}
			if tc.calls == 3 && !strings.Contains(err.Error(), "output limit") {
				t.Fatalf("lost truncation reason: %v", err)
			}
			if tc.name == "step_limit" && sessionHasUserMessageContaining(a.Session(), "output limit") {
				t.Fatal("added truncation continuation after the step budget was exhausted")
			}
		})
	}
}

func TestLengthTruncationDoesNotExtendFinalization(t *testing.T) {
	p := &scriptedProvider{name: "p", turns: [][]provider.Chunk{{{Type: provider.ChunkDone}}}}
	a := New(p, evidenceRegistry(), NewSession("system"), Options{}, event.Discard)
	for _, state := range []*turnRuntime{
		{graceRound: true, landCause: landCause{kind: "max_steps"}},
		{recoveryGraceRound: true},
	} {
		cont, err := a.handleFinalResponse(context.Background(), state, "", "partial", &provider.Usage{FinishReason: "length"})
		if cont || err == nil {
			t.Fatalf("finalization was extended: continue=%v err=%v", cont, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cont, err := a.handleFinalResponse(ctx, &turnRuntime{}, "", "partial", &provider.Usage{FinishReason: "length"})
	if cont || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled truncation continued: continue=%v err=%v", cont, err)
	}
}

func TestLengthTruncationDoesNotExceedSpendBudget(t *testing.T) {
	for _, window := range []bool{false, true} {
		p := &scriptedProvider{name: "p", turns: [][]provider.Chunk{{
			{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "length", CompletionTokens: 8192}},
			{Type: provider.ChunkDone},
		}}}
		a := New(p, evidenceRegistry(), NewSession("system"), Options{MaxSteps: 40}, event.Discard)
		ctx := WithTaskBudget(context.Background(), TaskBudget{Tokens: 8192})
		if window {
			ctx = context.Background()
			a.budgetCtrl = NewBudgetController(1, "session")
			a.budgetCtrl.Observe(1)
		}
		err := a.Run(ctx, "fix the parser bug")
		if p.call != 1 || err == nil || !strings.Contains(err.Error(), "budget") {
			t.Fatalf("window=%v calls=%d err=%v; want budget stop after one call", window, p.call, err)
		}
	}
}
