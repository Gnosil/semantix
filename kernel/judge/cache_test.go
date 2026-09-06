package judge

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingJudge struct {
	mu     sync.Mutex
	calls  int
	cond   func() (bool, error) // returns (confirm, err) per call
	callsW sync.WaitGroup
}

func (r *recordingJudge) Confirm(ctx context.Context, c Candidate) (bool, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	confirm, err := r.cond()
	return confirm, err
}

func (r *recordingJudge) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func TestVerdictCacheHitSkipsInner(t *testing.T) {
	inner := &recordingJudge{cond: func() (bool, error) { return true, nil }}
	cj := &CachedJudge{Inner: inner, Cache: NewVerdictCache()}
	cand := Candidate{Query: "where is auth middleware", SliceID: "s1"}
	for i := 0; i < 5; i++ {
		ok, err := cj.Confirm(context.Background(), cand)
		if err != nil || !ok {
			t.Fatalf("call %d: ok=%v err=%v", i, ok, err)
		}
	}
	if got := inner.callCount(); got != 1 {
		t.Fatalf("inner called %d times, want 1 (cache must absorb repeats)", got)
	}
}

// TestCacheDoesNotMemoizeErrors: a judge error must stay uncached — a
// transient outage must not permanently reject a reusable candidate.
func TestCacheDoesNotMemoizeErrors(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	inner := &recordingJudge{cond: func() (bool, error) {
		if fail.Load() {
			return false, errors.New("timeout")
		}
		return true, nil
	}}
	cj := &CachedJudge{Inner: inner, Cache: NewVerdictCache()}
	cand := Candidate{Query: "q", SliceID: "s"}
	for i := 0; i < 3; i++ {
		if _, err := cj.Confirm(context.Background(), cand); err == nil {
			t.Fatal("expected error while inner failing")
		}
	}
	fail.Store(false)
	ok, err := cj.Confirm(context.Background(), cand)
	if err != nil || !ok {
		t.Fatalf("after recovery: ok=%v err=%v (error must not be cached)", ok, err)
	}
}

// TestWarmPopulatesCacheInBackground: on error with Warm, the background
// retry must land a verdict so the next Confirm is instant.
func TestWarmPopulatesCacheInBackground(t *testing.T) {
	// Fail on the first call only. The warm goroutine retries exactly once
	// and immediately, so a shared "recovered" flag races against it: if
	// the goroutine runs before the flip, the single retry fails and the
	// cache never fills. Counting calls makes the retry deterministically
	// succeed regardless of scheduling.
	var calls atomic.Int32
	inner := &recordingJudge{cond: func() (bool, error) {
		if calls.Add(1) == 1 {
			return false, errors.New("timeout")
		}
		return true, nil
	}}
	cj := &CachedJudge{Inner: inner, Cache: NewVerdictCache(), Warm: true}
	cand := Candidate{Query: "q", SliceID: "s"}
	if _, err := cj.Confirm(context.Background(), cand); err == nil {
		t.Fatal("expected error while inner failing")
	}
	// Count-based inner makes the outcome scheduling-independent; the wait
	// only covers the goroutine getting a slot. CI runs the suite under
	// -race where 2s proved tight (flaked on #444), so wait generously —
	// the callCount check below still tells apart "goroutine never ran"
	// (scheduling) from "ran but cached nothing" (a real warm-path bug).
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		// Primary-rubric warm lands under the "p" namespace.
		if confirm, ok := cj.Cache.Get("p" + VerdictKey("q", "s")); ok {
			if !confirm {
				t.Fatal("warmed verdict should be confirm")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if inner.callCount() < 2 {
		t.Fatal("background warm goroutine never ran")
	}
	t.Fatal("background warm did not populate the cache in time")
}

func TestVerdictKeyStable(t *testing.T) {
	if VerdictKey("a", "s") != VerdictKey("a", "s") {
		t.Fatal("key must be deterministic")
	}
	if VerdictKey("a", "s") == VerdictKey("a", "s2") {
		t.Fatal("different slices must map to different keys")
	}
	if VerdictKey("a", "s") == VerdictKey("b", "s") {
		t.Fatal("different queries must map to different keys")
	}
}
