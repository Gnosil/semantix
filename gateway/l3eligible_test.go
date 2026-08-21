package gateway

import (
	"testing"
	"time"

	"semantix/kernel/slice"
)

// lazyStore simulates a store where every Get misses (returns (nil, nil)),
// exactly the contract fileStore.Get uses for a slice that left the store.
type lazyStore struct{}

func (lazyStore) Get(_ string) (*slice.Slice, error)                 { return nil, nil }
func (lazyStore) Put(*slice.Slice) error                             { return nil }
func (lazyStore) List(slice.Scope) ([]*slice.Slice, error)           { return nil, nil }
func (lazyStore) ListAll() ([]*slice.Slice, error)                   { return nil, nil }
func (lazyStore) UpdateStats(string, slice.SliceStats) error         { return nil }
func (lazyStore) UpdateStatsBatch(map[string]slice.SliceStats) error { return nil }
func (lazyStore) Delete(string) error                                { return nil }

// TestL3EligibleFailClosedOnMissingSlice guards the nil-dereference in
// l3Eligible: a candidate slice that disappeared from the store (Get returns
// nil, nil) must make l3Eligible fail closed (return false). Use the deepseek
// vendor so TTLFor yields a non-zero window and cacheFresh has to touch
// s.CreatedAt — exactly the path that panicked on a nil slice before the
// s == nil guard.
func TestL3EligibleFailClosedOnMissingSlice(t *testing.T) {
	g := &Gateway{
		store: lazyStore{},
		cfg:   &Config{},
		now:   time.Now,
	}
	// deepseek resolves to the 24h default TTL; a missing slice must fail
	// closed without panicking in cacheFresh.
	if g.l3Eligible("gone", "deepseek") {
		t.Fatal("l3Eligible must fail closed when the slice is missing from the store")
	}
}

func TestCacheFreshFailsClosedOnInvalidTimestamps(t *testing.T) {
	const now = int64(1_000)
	g := &Gateway{cfg: &Config{}, now: func() time.Time { return time.Unix(now, 0) }}
	tests := []struct {
		name      string
		createdAt int64
		want      bool
	}{
		{"missing", 0, false},
		{"future", now + 1, false},
		{"fresh", now - 10, true},
		{"expired", now - 24*60*60 - 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := g.cacheFresh(&slice.Slice{CreatedAt: tt.createdAt}, "deepseek"); got != tt.want {
				t.Fatalf("cacheFresh(%d) = %v, want %v", tt.createdAt, got, tt.want)
			}
		})
	}
}
