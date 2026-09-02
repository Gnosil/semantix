package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"semantix/harness/config"
	"semantix/harness/control"
	"semantix/harness/event"
	"semantix/harness/provider"
)

// TestWebProtoMappingCompleteness walks every wire kind this package knows
// and requires the classifier to output exactly one canonical type — or
// explicitly skip host-internal noise. Genuinely new kinds must degrade to
// ProtoTypeUnknown (the forward-compat contract), never to an empty type.
func TestWebProtoMappingCompleteness(t *testing.T) {
	for kind := range wireKindKnown {
		protoType, keep := ProtoTypeFor(kind)
		if !keep {
			if !protoSkips[kind] {
				t.Fatalf("kind %q skipped but not listed in protoSkips", kind)
			}
			continue
		}
		if !protoTypes[protoType] {
			t.Errorf("kind %q mapped to %q which is not a canonical type", kind, protoType)
		}
	}

	// Reserved types must never be reachable through the mapping while no
	// producer exists (same family as the no-fabrication acceptance rule).
	for _, reserved := range []string{ProtoTypePlan, ProtoTypeDiff} {
		for kind := range wireKindKnown {
			if protoType, keep := ProtoTypeFor(kind); keep && protoType == reserved {
				t.Errorf("reserved type %q mapped from kind %q", reserved, kind)
			}
		}
	}

	// Forward compatibility: unseen kinds become unknown, not crashes.
	if protoType, keep := ProtoTypeFor("brand_new_kind_from_the_future"); !keep || protoType != ProtoTypeUnknown {
		t.Errorf("unseen kind => (%q,%v), want (unknown,true)", protoType, keep)
	}
}

// TestWebProtoPayloadRefinement pins turn_done as the stream's only failure
// signal and confirms junk frames are dropped without killing the stream.
func TestWebProtoPayloadRefinement(t *testing.T) {
	cases := []struct {
		name  string
		frame []byte
		want  string
	}{
		{"notice info", []byte(`{"kind":"notice","level":"info"}`), ProtoTypeTaskStatus},
		{"notice warn", []byte(`{"kind":"notice","level":"warn"}`), ProtoTypeTaskStatus},
		{"turn_done ok", []byte(`{"kind":"turn_done","err":""}`), ProtoTypeTaskStatus},
		{"turn_done failed", []byte(`{"kind":"turn_done","err":"boom"}`), ProtoTypeError},
		{"usage carries cache", []byte(`{"kind":"usage","sessionCacheHitTokens":5}`), ProtoTypeCacheStatus},
		{"compaction is cache", []byte(`{"kind":"compaction_done"}`), ProtoTypeCacheStatus},
		{"kernel cache is cache", []byte(`{"kind":"kernel_cache"}`), ProtoTypeCacheStatus},
	}
	for _, tc := range cases {
		got, keep, _ := classifyFrame(tc.frame)
		if !keep {
			t.Errorf("%s: unexpectedly filtered", tc.name)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: type = %q, want %q", tc.name, got, tc.want)
		}
	}

	// Junk frames are tolerated: dropped, stream stays alive (#407 acceptance).
	if _, keep, _ := classifyFrame([]byte("{not json")); keep {
		t.Error("junk frame should be dropped, not forwarded")
	}
}

// sseRecord is one parsed SSE frame from /workspace/events.
type sseRecord struct {
	id      string
	ssetype string
	data    json.RawMessage
}

// wireKindOf maps the synthetic emission list to its stable wire name so the
// order assertion below can double-check the inner data.kind too.
func wireKindOf(idx int) string {
	names := []string{
		"steer", "message", "tool_dispatch", "tool_result",
		"approval_request", "ask_request", "usage", "compaction_done",
		"turn_started", "retrying", "notice", "turn_done", // err variant
		"turn_done", // ok variant
	}
	return names[idx]
}

// TestWorkspaceEventsProtocolFrames emits the synthetic event sequence through
// the live broadcaster and validates the resulting SSE stream end to end:
// strictly increasing ids, event:-line parity with envelope.type, stable
// task_id, per-frame canonical types in emission order, zero reserved types,
// and filtered host-internal kinds producing no frames at all (#407).
func TestWorkspaceEventsProtocolFrames(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := New(ctrl, bc, config.ServeConfig{})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, httpSrv.URL+"/workspace/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	frames := make(chan sseRecord, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		var cur *sseRecord
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, ":"):
				continue // ": connected" + keepalive comments
			case strings.HasPrefix(line, "id: "):
				cur = &sseRecord{id: strings.TrimPrefix(line, "id: ")}
			case strings.HasPrefix(line, "event: "):
				if cur == nil {
					cur = &sseRecord{}
				}
				cur.ssetype = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				if cur == nil {
					cur = &sseRecord{}
				}
				cur.data = json.RawMessage(strings.TrimPrefix(line, "data: "))
				frames <- *cur
				cur = nil
			}
		}
		close(frames)
	}()

	errBoom := errors.New("upstream gone")
	emitSeq := []event.Event{
		{Kind: event.Steer, Text: "收到"},
		{Kind: event.Message, Text: "完整回答"},
		{Kind: event.ToolDispatch, Tool: event.Tool{ID: "t1", Name: "read_file"}},
		{Kind: event.ToolResult, Tool: event.Tool{ID: "t1", Output: "42 bytes"}},
		{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "a1"}},
		{Kind: event.AskRequest},
		{Kind: event.Usage, Usage: &provider.Usage{CacheHitTokens: 7}},
		{Kind: event.CompactionDone},
		{Kind: event.TurnStarted},
		{Kind: event.Retrying},
		{Kind: event.Notice, Level: event.LevelWarn},
		{Kind: event.TurnDone, Err: errBoom},
		{Kind: event.TurnDone}, // user cancellation / clean finish
		// Filtered host-internal kinds must produce NO frames:
		{Kind: event.StreamAttempt},
		{Kind: event.WorkspaceChanged},
	}
	wantTypes := []string{
		ProtoTypeUser, ProtoTypeAssistant, ProtoTypeToolStart, ProtoTypeToolResult,
		ProtoTypePermissionRequest, ProtoTypePermissionRequest,
		ProtoTypeCacheStatus, ProtoTypeCacheStatus,
		ProtoTypeTaskStatus, ProtoTypeTaskStatus, ProtoTypeTaskStatus,
		ProtoTypeError, ProtoTypeTaskStatus,
	}

	bc.Emit(emitSeq[0]) // warm the subscription before the collector loops
	for _, ev := range emitSeq[1:] {
		bc.Emit(ev)
	}

	// Broadcaster fan-out preserves emission order (FIFO byte channel), so
	// collecting until the context deadline yields every non-filtered frame,
	// in order.
	var records []sseRecord
collect:
	for {
		select {
		case rec, ok := <-frames:
			if !ok {
				break collect
			}
			records = append(records, rec)
		case <-ctx.Done():
			break collect
		}
	}

	if len(records) != len(wantTypes) {
		t.Fatalf("frames = %d, want %d; types seen:", len(records), len(wantTypes))
	}

	expectedTaskID := filepath.Base(ctrl.SessionPath())
	if expectedTaskID == "" || expectedTaskID == "." {
		expectedTaskID = "current"
	}

	var lastSeq uint64
	for i, rec := range records {
		var env WebEnvelope
		if err := json.Unmarshal(rec.data, &env); err != nil {
			t.Fatalf("frame %d: decode envelope: %v", i, err)
		}
		if env.V != 1 {
			t.Errorf("frame %d: v = %d, want 1", i, env.V)
		}
		if fmt.Sprint(env.Seq) != rec.id {
			t.Errorf("frame %d: id %q != envelope seq %d", i, rec.id, env.Seq)
		}
		if env.Seq <= lastSeq {
			t.Errorf("frame %d: seq %d not increasing after %d", i, env.Seq, lastSeq)
		}
		lastSeq = env.Seq
		if env.TaskID != expectedTaskID {
			t.Errorf("frame %d: task_id = %q, want %q", i, env.TaskID, expectedTaskID)
		}
		if rec.ssetype != env.Type {
			t.Errorf("frame %d: event line %q != envelope type %q", i, rec.ssetype, env.Type)
		}
		if env.Type != wantTypes[i] {
			t.Errorf("frame %d (%s): type = %q, want %q", i, wireKindOf(i), env.Type, wantTypes[i])
		}
		var inner struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(env.Data, &inner); err != nil {
			t.Fatalf("frame %d: decode inner wire frame: %v", i, err)
		}
		if inner.Kind != wireKindOf(i) {
			t.Errorf("frame %d: inner kind = %q, want %q (order must mirror emissions)", i, inner.Kind, wireKindOf(i))
		}
	}
}

func TestBroadcasterWebReplayAndGap(t *testing.T) {
	bc := NewBroadcaster()
	for i := 0; i < 3; i++ {
		bc.Emit(event.Event{Kind: event.Message, Text: fmt.Sprintf("message-%d", i)})
	}

	ch, replay, gap, unsubscribe := bc.SubscribeWebSince(1, "task-1")
	defer unsubscribe()
	if gap {
		t.Fatal("recent Last-Event-ID unexpectedly reported a replay gap")
	}
	if len(replay) != 2 || replay[0].frame.seq != 2 || replay[1].frame.seq != 3 {
		t.Fatalf("replay = %#v, want seq 2 and 3", replay)
	}
	bc.Emit(event.Event{Kind: event.Message, Text: "live"})
	select {
	case delivery := <-ch:
		if delivery.frame.seq != 4 {
			t.Fatalf("live seq = %d, want 4", delivery.frame.seq)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live workspace frame")
	}

	// The retained window is bounded. Once the caller's cursor predates it,
	// replay still returns the suffix and reports that /history must be
	// refreshed for the missing prefix.
	for i := 0; i < webHistoryLimit+2; i++ {
		bc.Emit(event.Event{Kind: event.Message, Text: "overflow"})
	}
	_, suffix, gap, unsubscribe2 := bc.SubscribeWebSince(1, "task-2")
	defer unsubscribe2()
	if !gap {
		t.Fatal("expired Last-Event-ID did not report a replay gap")
	}
	if len(suffix) != webHistoryLimit {
		t.Fatalf("replay suffix = %d, want bounded history %d", len(suffix), webHistoryLimit)
	}
}

func TestBroadcasterLiveSubscriptionBridgesHistoryWithoutDuplicates(t *testing.T) {
	bc := NewBroadcaster()
	bc.Emit(event.Event{Kind: event.Message, Text: "already in /history"})
	ch, unsubscribe := bc.SubscribeWebLive("task-1")
	defer unsubscribe()
	select {
	case delivery := <-ch:
		t.Fatalf("live hydration replayed retained frame %d", delivery.frame.seq)
	default:
	}
	bc.Emit(event.Event{Kind: event.Message, Text: "arrived during hydration"})
	select {
	case delivery := <-ch:
		if delivery.frame.seq != 2 {
			t.Fatalf("buffered live seq = %d, want 2", delivery.frame.seq)
		}
	case <-time.After(time.Second):
		t.Fatal("live hydration lost an event")
	}
}

func TestBroadcasterResetSessionDropsWorkspaceHistory(t *testing.T) {
	bc := NewBroadcaster()
	bc.Emit(event.Event{Kind: event.Message, Text: "old task"})
	ch, _, _, unsubscribe := bc.SubscribeWebSince(0, "old-task")
	bc.ResetSession()
	defer unsubscribe()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("workspace subscriber remained open across session reset")
		}
	case <-time.After(time.Second):
		t.Fatal("workspace subscriber was not closed on session reset")
	}
	_, replay, gap, unsubscribe2 := bc.SubscribeWebSince(1, "new-task")
	defer unsubscribe2()
	if !gap || len(replay) != 0 {
		t.Fatalf("after reset replay=(%d frames, gap=%v), want empty history with gap", len(replay), gap)
	}
}

func readOneSSE(sc *bufio.Scanner) (sseRecord, error) {
	var rec sseRecord
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, ":") || line == "retry: 3000":
			continue
		case strings.HasPrefix(line, "id: "):
			rec.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			rec.ssetype = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			rec.data = json.RawMessage(strings.TrimPrefix(line, "data: "))
			return rec, nil
		}
	}
	if err := sc.Err(); err != nil {
		return rec, err
	}
	return rec, errors.New("SSE stream ended before a data frame")
}

func TestWorkspaceEventsReplaysAfterReconnect(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := New(ctrl, bc, config.ServeConfig{})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	ctx1, cancel1 := context.WithCancel(context.Background())
	req1, _ := http.NewRequestWithContext(ctx1, http.MethodGet, httpSrv.URL+"/workspace/events", nil)
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	sc1 := bufio.NewScanner(resp1.Body)
	bc.Emit(event.Event{Kind: event.Message, Text: "first"})
	first, err := readOneSSE(sc1)
	if err != nil {
		resp1.Body.Close()
		cancel1()
		t.Fatal(err)
	}
	var firstEnv WebEnvelope
	if err := json.Unmarshal(first.data, &firstEnv); err != nil {
		resp1.Body.Close()
		cancel1()
		t.Fatal(err)
	}
	if firstEnv.Seq == 0 {
		t.Fatal("first frame did not receive a sequence")
	}
	resp1.Body.Close()
	cancel1()

	bc.Emit(event.Event{Kind: event.Message, Text: "second"})
	bc.Emit(event.Event{Kind: event.Message, Text: "third"})

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	req2, _ := http.NewRequestWithContext(ctx2, http.MethodGet, httpSrv.URL+"/workspace/events", nil)
	req2.Header.Set("Last-Event-ID", fmt.Sprint(firstEnv.Seq))
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if got := resp2.Header.Get("X-Semantix-Replay"); got != "complete" {
		t.Fatalf("replay header = %q, want complete", got)
	}
	sc2 := bufio.NewScanner(resp2.Body)
	second, err := readOneSSE(sc2)
	if err != nil {
		t.Fatal(err)
	}
	third, err := readOneSSE(sc2)
	if err != nil {
		t.Fatal(err)
	}
	var secondEnv, thirdEnv WebEnvelope
	if err := json.Unmarshal(second.data, &secondEnv); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(third.data, &thirdEnv); err != nil {
		t.Fatal(err)
	}
	if secondEnv.Seq != firstEnv.Seq+1 || thirdEnv.Seq != secondEnv.Seq+1 {
		t.Fatalf("replay seqs = %d, %d after %d", secondEnv.Seq, thirdEnv.Seq, firstEnv.Seq)
	}
	if second.ssetype != ProtoTypeAssistant || third.ssetype != ProtoTypeAssistant {
		t.Fatalf("replay types = %q, %q", second.ssetype, third.ssetype)
	}
}
