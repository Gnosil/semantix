package semantix

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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
