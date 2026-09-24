package event

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// T1+T2+T3: roundtrip for every Kind with data payloads.
func TestRoundTripAllKinds(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		kind Kind
		data any
	}{
		{TurnStarted, TurnStartedPayload{}},
		{Usage, UsagePayload{PromptTokens: 100, CacheHit: 80, CacheMiss: 20, CompletionTok: 50, CostUSD: 0.0012}},
		{ToolDispatch, ToolDispatchPayload{CallID: "c1", Name: "readFile", Args: json.RawMessage(`{"path":"a.go"}`), ReadOnly: true}},
		{ToolResult, ToolResultPayload{CallID: "c1", OK: true, Latency: 12 * time.Millisecond}},
		{ToolRoundEnd, ToolRoundEndPayload{Dispatched: 3, Succeeded: 2}},
		{SliceHit, SliceHitPayload{Layer: "L2", SliceIDs: []string{"s1", "s2"}, Scores: []float64{0.91, 0.84}}},
		{SliceInject, SliceInjectPayload{SliceIDs: []string{"s1", "s2"}, Bytes: 2048}},
		{SliceReject, SliceRejectPayload{SliceID: "s1", Reason: "user_edit"}},
		{PrefetchHit, PrefetchHitPayload{Targets: []string{"f1.go"}}},
		{PrefetchWaste, PrefetchWastePayload{Targets: []string{"f2.go"}}},
		{Compact, CompactPayload{Trigger: "prune", Before: 100000, After: 70000}},
		{Compact, CompactPayload{Trigger: "evict", Before: 9, After: 6, EvictedByType: map[string]int{"result": 2, "tool_pattern": 1}}},
		{EvolutionTick, EvolutionTickPayload{ParamsJSON: json.RawMessage(`{"tau":0.8}`)}},
		{ResourceCatalog, ResourceCatalogPayload{
			Tools:  []ResourceTool{{Name: "read_file", ReadOnly: true}, {Name: "bash", Suspended: true}},
			Models: []ResourceModel{{ID: "flash-model", Tier: "flash", InputPrice: 0.1, OutputPrice: 0.2}},
			Budget: ResourceBudget{LimitUSD: 1, SpentUSD: 0.25, Window: "session"},
		}},
		{SliceOutcome, SliceOutcomePayload{SliceID: "s1", Outcome: "useful", Reason: "resolved_fewer_steps"}},
	}
	for _, c := range cases {
		data, err := json.Marshal(c.data)
		if err != nil {
			t.Fatalf("kind %d: marshal payload: %v", c.kind, err)
		}
		e := Event{Kind: c.kind, SessionID: "sess-1", Turn: 3, At: now, Data: data}
		b, err := ToJSON(e)
		if err != nil {
			t.Fatalf("kind %d: ToJSON: %v", c.kind, err)
		}
		got, err := FromJSON(b)
		if err != nil {
			t.Fatalf("kind %d: FromJSON: %v", c.kind, err)
		}
		if got.Kind != c.kind || got.SessionID != e.SessionID || got.Turn != e.Turn || !got.At.Equal(e.At) {
			t.Fatalf("kind %d: roundtrip mismatch: %+v vs %+v", c.kind, got, e)
		}
		// T2: payload field-level equality via re-unmarshal and JSON comparison.
		gotData, err := json.Marshal(c.data)
		if err != nil {
			t.Fatalf("kind %d: re-marshal: %v", c.kind, err)
		}
		if string(got.Data) != string(gotData) {
			t.Fatalf("kind %d: data mismatch: %s vs %s", c.kind, got.Data, gotData)
		}
	}
}

func TestResourceCatalogAppendedAfterEvolutionTick(t *testing.T) {
	if ResourceCatalog != EvolutionTick+1 || SliceOutcome != ResourceCatalog+1 || KindCount != SliceOutcome+1 {
		t.Fatalf("event kinds must be append-only: evolution=%d catalog=%d outcome=%d count=%d", EvolutionTick, ResourceCatalog, SliceOutcome, KindCount)
	}
	// A wire event written before ResourceCatalog existed must retain its kind.
	old, err := FromJSON([]byte(`{"kind":11,"session_id":"old","at":"2026-08-07T12:00:00Z"}`))
	if err != nil {
		t.Fatalf("old event replay: %v", err)
	}
	if old.Kind != EvolutionTick {
		t.Fatalf("old kind 11 changed meaning: got %v", old.Kind)
	}
}

// T3: empty-data event roundtrip.
func TestRoundTripEmptyData(t *testing.T) {
	e := Event{Kind: TurnStarted, SessionID: "s", At: time.Now()}
	b, err := ToJSON(e)
	if err != nil {
		t.Fatal(err)
	}
	got, err := FromJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != TurnStarted || len(got.Data) != 0 {
		t.Fatalf("unexpected: %+v", got)
	}
	var p TurnStartedPayload
	if err := UnmarshalData(got, &p); err != nil {
		t.Fatalf("UnmarshalData empty: %v", err)
	}
}

// T4: out-of-range Kind rejected.
func TestFromJSONInvalidKind(t *testing.T) {
	b := []byte(`{"kind":99,"session_id":"s"}`)
	if _, err := FromJSON(b); err == nil {
		t.Fatal("expected error for invalid kind")
	}
}

// T5: malformed JSON rejected.
func TestFromJSONMalformed(t *testing.T) {
	if _, err := FromJSON([]byte(`{not json`)); err == nil {
		t.Fatal("expected error for malformed json")
	}
}

// T6: fan-out order — handlers receive in subscription order.
func TestSyncBusFanOutOrder(t *testing.T) {
	b := NewSyncBus()
	var order []int
	var mu sync.Mutex
	record := func(id int) func(Event) {
		return func(Event) {
			mu.Lock()
			order = append(order, id)
			mu.Unlock()
		}
	}
	b.Subscribe(record(1))
	b.Subscribe(record(2))
	b.Emit(Event{Kind: Usage})
	b.Emit(Event{Kind: Usage})
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 4 {
		t.Fatalf("expected 4 deliveries, got %d", len(order))
	}
	for i, id := range order {
		want := []int{1, 2, 1, 2}[i]
		if id != want {
			t.Fatalf("order mismatch at %d: got %d want %d", i, id, want)
		}
	}
}

// T7: unsubscribe stops delivery.
func TestSyncBusUnsubscribe(t *testing.T) {
	b := NewSyncBus()
	var n atomic.Int32
	unsub := b.Subscribe(func(Event) { n.Add(1) })
	b.Emit(Event{Kind: Usage})
	unsub()
	unsub() // double unsubscribe must be safe
	b.Emit(Event{Kind: Usage})
	if n.Load() != 1 {
		t.Fatalf("expected 1 delivery after unsubscribe, got %d", n.Load())
	}
}

// T8: concurrent Emit — no race, correct total count (run with -race).
func TestSyncBusConcurrentEmit(t *testing.T) {
	b := NewSyncBus()
	var n1, n2 atomic.Int32
	b.Subscribe(func(Event) { n1.Add(1) })
	b.Subscribe(func(Event) { n2.Add(1) })
	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				b.Emit(Event{Kind: Usage})
			}
		}()
	}
	wg.Wait()
	if n1.Load() != 1000 || n2.Load() != 1000 {
		t.Fatalf("expected 1000 deliveries per handler, got %d/%d", n1.Load(), n2.Load())
	}
}

// T9: subscribing from inside a handler must not deadlock.
func TestSyncBusSubscribeInHandler(t *testing.T) {
	b := NewSyncBus()
	var n atomic.Int32
	b.Subscribe(func(Event) {
		b.Subscribe(func(Event) { n.Add(1) })
	})
	done := make(chan struct{})
	go func() {
		b.Emit(Event{Kind: Usage})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("deadlock: Emit did not return")
	}
}

// T10: UnmarshalData with mismatched type returns error.
func TestUnmarshalDataMismatch(t *testing.T) {
	data, _ := json.Marshal(UsagePayload{PromptTokens: 5})
	e := Event{Kind: Usage, Data: data}
	var p ToolResultPayload
	if err := UnmarshalData(e, &p); err == nil {
		t.Fatal("expected error for type mismatch")
	}
}

// T11 (Issue #272, c4): Prefetch payloads are wire-additive — an old event
// without lead_ms decodes to 0, and the new field round-trips.
func TestPrefetchPayloadLeadMsAdditive(t *testing.T) {
	old := []byte(`{"targets":["f1.go"]}`)
	var p PrefetchHitPayload
	if err := json.Unmarshal(old, &p); err != nil {
		t.Fatalf("old payload must decode: %v", err)
	}
	if p.LeadMs != 0 || len(p.Targets) != 1 {
		t.Fatalf("old payload: lead_ms must default to 0, got %+v", p)
	}
	hit := PrefetchHitPayload{Targets: []string{"f1.go"}, LeadMs: 750}
	data, err := json.Marshal(hit)
	if err != nil {
		t.Fatal(err)
	}
	var back PrefetchHitPayload
	if err := json.Unmarshal(data, &back); err != nil || back.LeadMs != 750 {
		t.Fatalf("lead_ms roundtrip failed: %v %+v", err, back)
	}
}
