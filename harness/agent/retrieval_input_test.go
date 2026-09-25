package agent

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"semantix/harness/event"
	semantixbridge "semantix/harness/semantix"
	"semantix/harness/tool"
	"semantix/kernel/sched"
	"semantix/kernel/slice"
)

func TestRetrievalUsesTaskInput(t *testing.T) {
	const raw = "repair parser"
	const composed = "<capability-route>\nuse capability routing\n</capability-route>\n\n" + raw
	for _, tc := range []struct {
		name       string
		input      string
		classifier string
		scope      string
		want       string
		degraded   bool
		direct     bool
		prefetch   bool
	}{
		{name: "synchronous", input: composed, want: raw},
		{name: "degraded", input: composed, want: raw, degraded: true},
		{name: "scoped", input: composed, classifier: "classifier task", scope: "repair tokenizer", want: "repair tokenizer"},
		{name: "classifier", input: composed, classifier: "repair lexer", want: "repair lexer"},
		{name: "direct_fallback", input: raw, want: raw, direct: true},
		{name: "prefetch", input: composed, want: raw, prefetch: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, ".semantix"), 0o755); err != nil {
				t.Fatal(err)
			}
			store, err := slice.NewFileStore(filepath.Join(dir, ".semantix", "project.db"))
			if err != nil {
				t.Fatal(err)
			}
			for _, sl := range []*slice.Slice{
				{ID: "task", Type: slice.Context, Scope: slice.Project, Content: []byte(tc.want), Meta: slice.SliceMeta{SourceSession: "task-source"}},
				{ID: "routing", Type: slice.Context, Scope: slice.Project, Content: []byte("capability routing execution policy balanced"), Meta: slice.SliceMeta{SourceSession: "routing-source"}},
			} {
				if err := store.Put(sl); err != nil {
					t.Fatal(err)
				}
			}
			if closer, ok := store.(io.Closer); ok {
				if err := closer.Close(); err != nil {
					t.Fatal(err)
				}
			}
			bridge := semantixbridge.NewBridge(semantixbridge.Config{Enabled: true, Mode: "strict", ProjectDir: dir})
			t.Cleanup(func() { _ = bridge.Close() })
			retrievals := make(chan *event.KernelCachePayload, 2)
			bridge.Sink(event.FuncSink(func(e event.Event) {
				if e.KernelCache != nil && e.KernelCache.Retrieval != nil {
					retrievals <- e.KernelCache
				}
			}))
			sess := NewSession("system")
			a := New(nil, tool.NewRegistry(), sess, Options{Semantix: bridge, ClassifierTaskText: tc.classifier}, event.Discard)
			if tc.degraded {
				a.budgetCtrl = &BudgetController{limitUSD: 1, window: "session", lastAction: sched.BudgetActionDegradeInject}
			}
			ctx := context.Background()
			if !tc.direct {
				ctx = WithRawUserInput(ctx, raw)
			}
			if tc.scope != "" {
				ctx = WithDeliveryExecutionScope(ctx, DeliveryExecutionScope{ID: "goal", TaskText: tc.scope})
			}
			_, state := a.beginRunTurn(ctx, tc.input)
			checkQuery := func(cache *event.KernelCachePayload) {
				t.Helper()
				d := cache.Retrieval
				if d.QueryStructure.Intent != tc.want || d.QueryBefore.Bytes != len(tc.want) {
					t.Errorf("retrieval intent = %q, input bytes = %d; want task %q (%d bytes)", d.QueryStructure.Intent, d.QueryBefore.Bytes, tc.want, len(tc.want))
				}
			}
			select {
			case cache := <-retrievals:
				checkQuery(cache)
				if tc.degraded && cache.Op != "degraded" {
					t.Errorf("retrieval operation = %q, want degraded", cache.Op)
				}
			default:
				t.Fatal("synchronous retrieval emitted no diagnostics")
			}
			if state.reuse.Hits != 1 || len(state.reuse.Sources) != 1 || state.reuse.Sources[0] != "task-source" {
				t.Errorf("reuse = %+v, want only the task source", state.reuse)
			}
			if tc.prefetch {
				warmCtx, cancel := context.WithCancel(ctx)
				cancel()
				a.startInjectWarm(warmCtx)
				select {
				case cache := <-retrievals:
					checkQuery(cache)
				case <-time.After(3 * time.Second):
					t.Fatal("prefetch emitted no retrieval diagnostics")
				}
			}
			stored := sess.Snapshot()
			if len(stored) != 2 || !strings.HasPrefix(stored[1].Content, tc.input) || !strings.Contains(stored[1].Content, "<execution-policy") {
				t.Fatalf("provider session lost composed framing or execution policy: %+v", stored)
			}
			prepared, err := a.prepareSamplingRequest(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(prepared.req.Messages) != 2 || prepared.req.Messages[1].Content != stored[1].Content {
				t.Fatalf("provider request changed composed rules: %+v", prepared.req.Messages)
			}
		})
	}
}
