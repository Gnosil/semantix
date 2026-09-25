package semantix

import (
	"context"
	"os"
	"path/filepath"
	"semantix/harness/event"
	"semantix/kernel/slice"
	"testing"
)

func TestAssemblyDoesNotRecordDelivery(t *testing.T) {
	for _, degraded := range []bool{false, true} {
		dir := writeKernelDir(t, admissionFixtureSlices(), nil)
		b := NewBridge(Config{Enabled: true, Mode: "strict", ProjectDir: dir, Budget: 4096})
		r := b.InjectDetailed(context.Background(), "修复 go 测试")
		if degraded {
			r = b.InjectDegradedDetailed(context.Background(), "修复 go 测试")
		}
		if r.Text == "" {
			t.Fatal("fixture did not assemble")
		}
		if r.Diagnostics.Injected {
			t.Error("assembly advertised actual delivery")
		}
		if err := b.Close(); err != nil {
			t.Fatal(err)
		}
		s, err := slice.NewFileStore(filepath.Join(dir, ".semantix", "project.db"))
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range r.Targets {
			got, err := s.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if got.Stats.Injected != 0 || got.Stats.LastUsed != 0 {
				t.Errorf("unconsumed assembly changed usage: %+v", got.Stats)
			}
		}
		closeSliceStore(s)
	}
}

func TestDeliveryPersistenceFailureIsVisible(t *testing.T) {
	dir := writeKernelDir(t, admissionFixtureSlices(), nil)
	b := NewBridge(Config{Enabled: true, Mode: "strict", ProjectDir: dir, SessionsDir: t.TempDir(), Budget: 4096})
	defer b.Close()
	var events []event.Event
	b.Sink(event.FuncSink(func(e event.Event) { events = append(events, e) }))
	result := b.InjectDetailed(context.Background(), "修复 go 测试")
	if result.Text == "" {
		t.Fatal("fixture did not assemble")
	}
	db := filepath.Join(dir, ".semantix", "project.db")
	if err := os.Rename(db, db+".before"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(db, 0700); err != nil {
		t.Fatal(err)
	}
	recorder, ok := any(b).(interface{ RecordInjectionDelivery([]string, int) })
	if !ok {
		t.Fatal("no explicit provider-delivery recorder")
	}
	recorder.RecordInjectionDelivery(result.Targets, len(result.Text))
	delivered, failed := false, false
	for _, e := range events {
		if e.KernelCache != nil {
			delivered = delivered || (e.KernelCache.Op == "inject" && e.KernelCache.Reason == "provider_accepted")
			failed = failed || (e.KernelCache.Op == "stats_error" && e.KernelCache.Reason != "")
		}
	}
	if !delivered || !failed {
		t.Fatalf("dispatch and persistence status were conflated: delivered=%v failure=%v", delivered, failed)
	}
}
