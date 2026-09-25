package semantix

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	kernelevent "semantix/kernel/event"
	"semantix/kernel/slice"
)

// A detached prefetch can reach the bridge after its owner has closed it.
// It must not reopen the store or emit provider-visible history at that point.
func TestBridgeAdmissionAfterClose(t *testing.T) {
	b := NewBridge(Config{Enabled: true, Mode: "strict", ProjectDir: writeKernelDir(t, []*slice.Slice{
		{ID: "ctx", Type: slice.Context, Scope: slice.Project, Content: []byte("repair parser regression")},
	}, nil)})
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	got := b.InjectDetailed(context.Background(), "repair parser regression")
	if got.Text != "" || got.Diagnostics != nil || len(got.Targets) != 0 {
		t.Fatalf("closed bridge performed retrieval: %+v", got)
	}
}

// Close tears the mirror sink down; late telemetry (a detached prefetch
// outcome, a final EndTurn) must not resurrect the session JSONL handle.
func TestBridgeSessionSinkStaysClosedAfterClose(t *testing.T) {
	sessions := t.TempDir()
	b := NewBridge(Config{Enabled: true, Inject: true, ProjectDir: writeKernelDir(t, nil, nil), SessionsDir: sessions, Budget: 4096})
	b.SetLabel("closed-mirror")
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	b.RecordPrefetch(false, []string{"slice-a"}, nil, 1, time.Second)
	b.EndTurn()
	if _, err := os.Stat(filepath.Join(sessions, "closed-mirror.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("session mirror resurrected after Close: %v", err)
	}
}

// A provider-accepted delivery that races Close can no longer persist stats,
// but its SliceInject bus event — the durable delivery record — must still be
// emitted instead of vanishing without a trace.
func TestBridgeDeliveryAtCloseStillEmitsBusEvent(t *testing.T) {
	b := NewBridge(Config{Enabled: true, Inject: true, ProjectDir: writeKernelDir(t, nil, nil), Budget: 4096})
	b.SetLabel("delivery-at-close")
	var seen []kernelevent.Kind
	b.Events().Subscribe(func(e kernelevent.Event) {
		if e.Kind == kernelevent.SliceInject {
			seen = append(seen, e.Kind)
		}
	})
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	b.RecordInjectionDelivery([]string{"slice-a"}, 128)
	if len(seen) != 1 {
		t.Fatalf("SliceInject bus events after delivery-at-close = %v, want exactly one", seen)
	}
}

// The reuse-panel read is part of the close protocol: after Close it must
// degrade to a zero summary without reopening the project store.
func TestBridgeReuseAfterCloseReturnsZero(t *testing.T) {
	b := NewBridge(Config{Enabled: true, ProjectDir: writeKernelDir(t, []*slice.Slice{
		{ID: "ctx", Type: slice.Context, Scope: slice.Project, Content: []byte("repair parser regression")},
	}, nil), Budget: 4096})
	if sum := b.Reuse(context.Background(), "repair parser regression"); sum.Hits != 1 {
		t.Fatalf("pre-close Reuse hits = %d, want 1 (fixture sanity)", sum.Hits)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if sum := b.Reuse(context.Background(), "repair parser regression"); sum != (ReuseSummary{}) {
		t.Fatalf("post-close Reuse = %+v, want zero summary", sum)
	}
}
