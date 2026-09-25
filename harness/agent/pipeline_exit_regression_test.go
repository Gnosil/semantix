package agent

import (
	"context"
	"semantix/harness/event"
	"semantix/harness/provider"
	"semantix/harness/tool"
	"strings"
	"testing"
)

func TestVerifierFailurePipelinePreflightRegression(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("masked", "bash", `{"command":"cd /testbed && python -m pytest tmp_test_b.py 2>&1 | tail -10"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "ok"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	got := toolResultByID(a.sess.conversation, "masked")
	if strings.Contains(got, "bash done") || !strings.Contains(got, "masks the verifier's exit status") {
		t.Fatalf("unsafe verifier dispatched or no recovery: %q", got)
	}
	for _, m := range a.sess.conversation.Snapshot() {
		if m.ToolCallID == "masked" {
			if m.ToolExecution == nil || m.ToolExecution.State != tool.ShellStateNotRun || m.ToolExecution.Verification == tool.ShellVerificationPassed {
				t.Fatalf("false success receipt: %+v", m.ToolExecution)
			}
			return
		}
	}
	t.Fatal("missing blocked result")
}
