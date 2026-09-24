package agent

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/harness/event"
	"semantix/harness/semantix"
	"semantix/harness/tool"
	"semantix/kernel/slice"
)

func TestLoopGuardFusesInjectedSlicesOnce(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".semantix"), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := slice.NewFileStore(filepath.Join(dir, ".semantix", "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(&slice.Slice{ID: "ctx-a", Type: slice.Context, Scope: slice.Project, Content: []byte("cache repair")}); err != nil {
		t.Fatal(err)
	}
	if closer, ok := store.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	bridge := semantix.NewBridge(semantix.Config{Enabled: true, Mode: "strict", ProjectDir: dir})
	defer bridge.Close()
	var events []event.Event
	a := New(nil, tool.NewRegistry(), NewSession(""), Options{Semantix: bridge}, event.FuncSink(func(e event.Event) {
		events = append(events, e)
	}))
	a.turn.injectBlock = "[semantix-reuse]\ncache repair\n[/semantix-reuse]"
	a.turn.injectTargets = []string{"ctx-a"}

	a.armLoopGuardPass(0)
	a.armLoopGuardPass(0)
	if a.turn.injectBlock != "" || !a.turn.injectionFused {
		t.Fatalf("turn injection not fused: %+v", a.turn)
	}
	fuseNotices := 0
	for _, e := range events {
		if e.Kind == event.Notice && e.Code == event.NoticeCodeSemantixFuse {
			fuseNotices++
		}
	}
	if fuseNotices != 1 {
		t.Fatalf("fuse notices = %d, want 1", fuseNotices)
	}
	store, err = slice.NewFileStore(filepath.Join(dir, ".semantix", "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closer, ok := store.(io.Closer); ok {
			_ = closer.Close()
		}
	}()
	got, err := store.Get("ctx-a")
	if err != nil || got.Stats.Rejected != 1 {
		t.Fatalf("slice after fuse = %+v, %v", got, err)
	}
	prepared, err := a.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range prepared.req.Messages {
		if strings.Contains(msg.Content, "cache repair") {
			t.Fatalf("fused history remained provider-visible: %+v", prepared.req.Messages)
		}
	}
}

func TestPrefetchedInjectionBecomesFuseableTurnHistory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".semantix"), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := slice.NewFileStore(filepath.Join(dir, ".semantix", "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(&slice.Slice{ID: "ctx-prefetch", Type: slice.Context, Scope: slice.Project, Content: []byte("prefetched cache repair")}); err != nil {
		t.Fatal(err)
	}
	if closer, ok := store.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	bridge := semantix.NewBridge(semantix.Config{Enabled: true, Mode: "strict", ProjectDir: dir})
	defer bridge.Close()
	a := New(nil, tool.NewRegistry(), NewSession(""), Options{Semantix: bridge}, event.FuncSink(func(event.Event) {}))
	a.storePrefetch(&prefetchedInjectResult{
		Text:    "[semantix-reuse]\nprefetched cache repair\n[/semantix-reuse]",
		Targets: []string{"ctx-prefetch"},
		Turn:    a.semantixTurn.Load(),
	})

	prepared, err := a.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if a.turn.injectBlock == "" || len(a.turn.injectTargets) != 1 {
		t.Fatalf("prefetch was not retained as active turn history: %+v", a.turn)
	}
	seen := false
	for _, msg := range prepared.req.Messages {
		seen = seen || strings.Contains(msg.Content, "prefetched cache repair")
	}
	if !seen {
		t.Fatalf("prefetched history was not provider-visible: %+v", prepared.req.Messages)
	}

	a.armLoopGuardPass(0)
	prepared, err = a.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range prepared.req.Messages {
		if strings.Contains(msg.Content, "prefetched cache repair") {
			t.Fatalf("fused prefetch remained provider-visible: %+v", prepared.req.Messages)
		}
	}
}
