package cache

import (
	"context"

	"semantix/kernel/slice"
)

// Query is the semantic-cache lookup input.
type Query struct {
	SessionID   string
	UserInput   string
	Intent      string // from the intent classifier (later milestone)
	ContextHash string // stable hash of the current context fingerprint
	Scope       slice.Scope
	// Model is the client-visible model alias. The L3 gate requires the
	// cached slice to carry the same Model (cross-model reuse is never
	// allowed, Issue #133). Empty disables the check for legacy entries.
	Model string
	// Freshness optionally applies an age gate to L3 candidates. Its zero
	// value disables the gate so non-gateway callers keep legacy behavior.
	Freshness Freshness
}

// Freshness is one request's L3 age policy. NowUnix is captured once by the
// caller so every candidate sees the same clock; TTLSeconds <= 0 disables the
// policy.
type Freshness struct {
	NowUnix    int64
	TTLSeconds int64
}

// L3Result is a reusable cached outcome (verified, fail-closed).
type L3Result struct {
	SliceID  string
	Response string
	CostUSD  float64
}

// Decider makes L2/L3 decisions (MVP implementation in M1; constants below
// are the shared params file — U6+ must read, not modify).
type Decider interface {
	// DecideL2 returns slices eligible for stable injection (tau-filtered,
	// budget-truncated, canonical order by ID).
	DecideL2(ctx context.Context, q Query) ([]slice.Hit, error)
	// DecideL3 returns a verified reusable result or nil (fingerprint + whitelist).
	DecideL3(ctx context.Context, q Query) (*L3Result, error)
}

// Default parameters shared across the kernel (frozen by U1; see §4 rules).
const (
	TauL2      = 0.75 // similarity threshold for L2 injection (evolution-tuned)
	TauL3      = 0.90 // similarity threshold for L3 reuse (fail-closed high)
	InjectCap  = 0.15 // max injected bytes as fraction of prompt budget
	L3TTLHours = 24   // L3 results expire after this
)
