package semantix

import (
	"context"
	"testing"

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
