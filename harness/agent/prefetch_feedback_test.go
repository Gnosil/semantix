package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"semantix/harness/event"
	"semantix/harness/semantix"
	"semantix/harness/tool"
	kernelevent "semantix/kernel/event"
	"semantix/kernel/slice"
)

func TestPrefetchResultSettlesExactlyOnceAsHit(t *testing.T) {
	b := semantix.NewBridge(semantix.Config{Enabled: true})
	defer b.Close()
	var hits, wastes int
	b.Events().Subscribe(func(e kernelevent.Event) {
		switch e.Kind {
		case kernelevent.PrefetchHit:
			hits++
		case kernelevent.PrefetchWaste:
			wastes++
		}
	})
	a := &Agent{semantix: b}
	a.semantixTurn.Store(1)
	a.storePrefetch(&prefetchedInjectResult{Text: "block", Targets: []string{"s1"}, Turn: 1})
	if got := a.takePrefetch(1); got == nil || got.Text != "block" {
		t.Fatalf("take=%#v", got)
	}
	if got := a.takePrefetch(1); got != nil {
		t.Fatalf("second take=%#v", got)
	}
	if hits != 1 || wastes != 0 {
		t.Fatalf("hits=%d wastes=%d", hits, wastes)
	}
}

func TestPrefetchReplacementAndExpirySettleAsWaste(t *testing.T) {
	b := semantix.NewBridge(semantix.Config{Enabled: true})
	defer b.Close()
	var hits, wastes int
	b.Events().Subscribe(func(e kernelevent.Event) {
		switch e.Kind {
		case kernelevent.PrefetchHit:
			hits++
		case kernelevent.PrefetchWaste:
			wastes++
		}
	})
	a := &Agent{semantix: b}
	a.semantixTurn.Store(1)
	a.storePrefetch(&prefetchedInjectResult{Text: "old", Targets: []string{"s1"}, Turn: 1})
	a.storePrefetch(&prefetchedInjectResult{Text: "new", Targets: []string{"s2"}, Turn: 1})
	if got := a.takePrefetch(2); got != nil {
		t.Fatalf("expired take=%#v", got)
	}
	if hits != 0 || wastes != 2 {
		t.Fatalf("hits=%d wastes=%d", hits, wastes)
	}
}

func TestPrefetchGateControlsWarmWithoutCreatingFeedback(t *testing.T) {
	b := semantix.NewBridge(semantix.Config{Enabled: true})
	defer b.Close()
	var hits, wastes int
	b.Events().Subscribe(func(e kernelevent.Event) {
		switch e.Kind {
		case kernelevent.PrefetchHit:
			hits++
		case kernelevent.PrefetchWaste:
			wastes++
		}
	})
	a := &Agent{semantix: b}
	a.semantixTurn.Store(3)

	if !a.prefetchAllowed() {
		t.Fatal("cold start must preserve legacy allow behavior")
	}
	a.armPrefetch("load_saturated", nil)
	if a.prefetchAllowed() {
		t.Fatal("load skip must block the warm")
	}
	if hits != 0 || wastes != 0 {
		t.Fatalf("a skipped warm is not feedback: hits=%d wastes=%d", hits, wastes)
	}
	a.armPrefetch("candidate", nil)
	if !a.prefetchAllowed() {
		t.Fatal("candidate plan must allow the warm")
	}
	a.armPrefetch("no_candidate", nil)
	if !a.prefetchAllowed() {
		t.Fatal("no-candidate plan must preserve legacy fail-open warm")
	}
	a.semantixTurn.Add(1)
	if !a.prefetchAllowed() {
		t.Fatal("a stale prior-turn gate must not block a new turn")
	}
}

// TestRecordPrefetchLeadTime pins the Markov timeliness pipeline (Issue
// #272, c5): warm-up completion is stamped on store, the consumption
// decision carries lead_ms = consume − warm-up-completion on the hit event
// (positive = finished in time).
func TestRecordPrefetchLeadTime(t *testing.T) {
	b := semantix.NewBridge(semantix.Config{Enabled: true})
	defer b.Close()
	var leadMs int64 = -1
	b.Events().Subscribe(func(e kernelevent.Event) {
		if e.Kind != kernelevent.PrefetchHit {
			return
		}
		var p kernelevent.PrefetchHitPayload
		if json.Unmarshal(e.Data, &p) == nil {
			leadMs = p.LeadMs
		}
	})
	a := &Agent{semantix: b}
	a.semantixTurn.Store(1)
	warmAt := time.Now().Add(-time.Second) // warmed one second before consumption
	a.storePrefetch(&prefetchedInjectResult{Text: "block", Targets: []string{"s1"}, Turn: 1, WarmAt: warmAt})
	if got := a.takePrefetch(1); got == nil {
		t.Fatal("take must consume the warmed block")
	}
	if leadMs < 900 || leadMs > 3000 {
		t.Fatalf("lead_ms=%d, want ≈1000 (positive, consumed after warm-up)", leadMs)
	}
}

func TestRecordPrefetchCarriesCanonicalProbeTargets(t *testing.T) {
	b := semantix.NewBridge(semantix.Config{Enabled: true})
	defer b.Close()
	var got kernelevent.PrefetchHitPayload
	b.Events().Subscribe(func(e kernelevent.Event) {
		if e.Kind == kernelevent.PrefetchHit {
			if err := json.Unmarshal(e.Data, &got); err != nil {
				t.Errorf("decode hit: %v", err)
			}
		}
	})
	a := &Agent{semantix: b}
	a.semantixTurn.Store(1)
	a.armPrefetch("candidate", []string{"probe-b", "probe-a", "probe-a"})
	a.storePrefetch(&prefetchedInjectResult{
		Text: "block", Targets: []string{"s1"}, Turn: 1,
		ProbeTargets: a.prefetchProbeTargets(),
	})
	if result := a.takePrefetch(1); result == nil {
		t.Fatal("take must consume the warmed block")
	}
	want := []string{"probe-a", "probe-b"}
	if len(got.ProbeTargets) != len(want) || got.ProbeTargets[0] != want[0] || got.ProbeTargets[1] != want[1] {
		t.Fatalf("probe_targets=%v, want %v", got.ProbeTargets, want)
	}
}

func TestRecordPrefetchOrdinaryPayloadOmitsProbeTargets(t *testing.T) {
	b := semantix.NewBridge(semantix.Config{Enabled: true})
	defer b.Close()
	var data json.RawMessage
	b.Events().Subscribe(func(e kernelevent.Event) {
		if e.Kind == kernelevent.PrefetchWaste {
			data = append(data[:0], e.Data...)
		}
	})
	a := &Agent{semantix: b}
	a.semantixTurn.Store(1)
	a.storePrefetch(&prefetchedInjectResult{Text: "block", Targets: []string{"s1"}, Turn: 1})
	a.wastePrefetch()
	if bytes.Contains(data, []byte("probe_targets")) {
		t.Fatalf("ordinary payload must omit probe_targets: %s", data)
	}
}

func TestPrefetchStaleResultCannotReplaceCurrentTurn(t *testing.T) {
	for _, occupied := range []bool{false, true} {
		a := &Agent{}
		a.semantixTurn.Store(2)
		var current *prefetchedInjectResult
		if occupied {
			current = &prefetchedInjectResult{Text: "current history", Turn: 2}
			a.storePrefetch(current)
		}
		a.storePrefetch(&prefetchedInjectResult{Text: "old history", Turn: 1})
		if got := a.prefetchedInject.Load(); got != current {
			t.Fatalf("occupied=%v: stale completion replaced current cache: %+v", occupied, got)
		}
	}
}

func TestInjectWarmKeepsOriginatingTurn(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-qm", "fixture"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %s: %v", out, err)
		}
	}
	revision, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	const oldQuery = "repair orchard cache"
	if err := os.MkdirAll(filepath.Join(dir, ".semantix"), 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := slice.NewFileStore(filepath.Join(dir, ".semantix", "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	err = store.Put(&slice.Slice{ID: "old", Type: slice.Context, Scope: slice.Project, Content: []byte(oldQuery),
		Meta: slice.SliceMeta{BaseCommit: strings.TrimSpace(string(revision)), Origin: slice.OriginSessionAuto, SourceSession: "history-a"}})
	closeErr := store.(io.Closer).Close()
	if err != nil || closeErr != nil {
		t.Fatalf("store: put=%v close=%v", err, closeErr)
	}
	b := semantix.NewBridge(semantix.Config{Enabled: true, Mode: "strict", ProjectDir: dir})
	defer b.Close()
	started, release := make(chan struct{}), make(chan struct{})
	releaseWarm := sync.OnceFunc(func() { close(release) })
	defer releaseWarm()
	b.Sink(event.FuncSink(func(e event.Event) {
		if e.KernelCache != nil && e.KernelCache.Retrieval != nil && e.KernelCache.Retrieval.QueryStructure.Intent == oldQuery {
			close(started)
			<-release
		}
	}))
	wasted := make(chan kernelevent.Event, 2)
	b.Events().Subscribe(func(e kernelevent.Event) {
		if e.Kind == kernelevent.PrefetchWaste {
			wasted <- e
		}
	})
	recorder := &recordingProvider{reply: "done"}
	a := New(recorder, tool.NewRegistry(), NewSession("system"), Options{Semantix: b}, event.Discard)
	a.turn.turnInput = oldQuery
	a.semantixTurn.Store(1)
	a.startInjectWarm(context.Background())
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("warm did not reach retrieval")
	}
	// Advance through the real turn boundary while A is still assembling.
	a.beginRunTurn(context.Background(), "investigate database migration")
	const currentBlock = "[semantix-reuse]\ncurrent database history\n[/semantix-reuse]"
	a.storePrefetch(&prefetchedInjectResult{Text: currentBlock, Targets: []string{"new"}, Turn: 2})
	releaseWarm()
	var waste kernelevent.Event
	select {
	case waste = <-wasted: // emitted after the competing completion settles
	case <-time.After(5 * time.Second):
		t.Fatal("warm completion did not settle")
	}
	prepared, err := a.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := a.streamProviderRequest(context.Background(), prepared.req)
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	var contents string
	for _, message := range recorder.got[0].Messages {
		contents += message.Content
	}
	if waste.Turn != 1 || !strings.Contains(contents, currentBlock) || strings.Contains(contents, oldQuery) {
		t.Fatalf("stale task reached current provider request: waste turn=%d, messages=%s", waste.Turn, contents)
	}
}
