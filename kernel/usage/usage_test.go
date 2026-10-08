package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecorderAppendAndSummarize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	r, err := NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	evs := []Event{
		{SessionID: "s1", Turn: 1, TokensIn: 1000, TokensOut: 200, CacheHitToken: 600, SliceHits: 2},
		{SessionID: "s1", Turn: 2, TokensIn: 1200, TokensOut: 250, CacheHitToken: 1000, InjectedTokens: 300, SliceHits: 3},
		{SessionID: "s2", Turn: 1, TokensIn: 800, TokensOut: 150, L3Reuse: true},
	}
	for _, e := range evs {
		if err := r.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Summarize(path, DefaultCostMissPerMTok, DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	if s.Events != 3 {
		t.Fatalf("events = %d, want 3", s.Events)
	}
	if s.TokensIn != 3000 || s.TokensOut != 600 {
		t.Fatalf("tokens = %d/%d, want 3000/600", s.TokensIn, s.TokensOut)
	}
	if s.CacheHitTokens != 1600 {
		t.Fatalf("cache hits = %d, want 1600", s.CacheHitTokens)
	}
	if s.L3Reuses != 1 || s.InjectedTokens != 300 {
		t.Fatalf("L3=%d injected=%d", s.L3Reuses, s.InjectedTokens)
	}
	if s.SliceHits != 5 {
		t.Fatalf("slice hits = %d, want 5 (2+3)", s.SliceHits)
	}
	// Billed: in 3000-1600-800=600 miss + 1600 hit + out 600-150=450 miss.
	// L3 turn (800 in + 150 out) excluded entirely.
	wantPaid := 600*0.27/1e6 + 1600*0.07/1e6 + 450*0.27/1e6
	wantNoCache := 3000*0.27/1e6 + 600*0.27/1e6
	if !approx(s.CostPaidUSD, wantPaid) {
		t.Fatalf("paid = %v, want %v", s.CostPaidUSD, wantPaid)
	}
	if !approx(s.CostNoCacheUSD, wantNoCache) {
		t.Fatalf("noCache = %v, want %v", s.CostNoCacheUSD, wantNoCache)
	}
	if !approx(s.SavingsUSD, wantNoCache-wantPaid) {
		t.Fatalf("savings = %v", s.SavingsUSD)
	}
	if s.SavingsRate <= 0 || s.SavingsRate >= 1 {
		t.Fatalf("savings rate = %v, want in (0,1)", s.SavingsRate)
	}
}

// TestSummarizeByProvider covers the #291 per-endpoint grouping: exact
// events feed the calibration figures, estimated events count presence only
// (they never observed provider cache accounting), and pre-#291 lines land
// under the "" key.
func TestSummarizeByProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	r, err := NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	evs := []Event{
		{Provider: "tokenhub", Vendor: "openai", Model: "glm-5.3", Exact: true,
			TokensIn: 2620, TokensOut: 15, CacheHitToken: 2560},
		{Provider: "tokenhub", Vendor: "anthropic", Model: "glm-5.3", Exact: true,
			TokensIn: 2620, TokensOut: 15, CacheHitToken: 2556},
		{Provider: "tokenhub", TokensIn: 999, TokensOut: 9}, // estimated turn
		{Provider: "tokenhub", TokensIn: 40, L3Reuse: true, CacheHitToken: 30},
		{TokensIn: 500, TokensOut: 50}, // pre-#291 line: no provider
	}
	for _, e := range evs {
		if err := r.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Summarize(path, DefaultCostMissPerMTok, DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	th := s.ByProvider["tokenhub"]
	if th == nil || th.Events != 4 || th.ExactEvents != 2 {
		t.Fatalf("tokenhub stats = %+v, want 4 events / 2 exact", th)
	}
	// Estimated + L3 turns must not pollute the exact-metered figures.
	if th.TokensIn != 5240 || th.CacheHitTokens != 5116 || th.TokensOut != 30 {
		t.Fatalf("tokenhub exact meters = %+v", th)
	}
	if th.L3Reuses != 1 {
		t.Fatalf("tokenhub L3 = %d, want 1", th.L3Reuses)
	}
	if got := th.L1HitRate(); got <= 0.97 || got >= 0.98 {
		t.Fatalf("L1HitRate = %v, want ≈0.976 (5116/5240)", got)
	}
	legacy := s.ByProvider[""]
	if legacy == nil || legacy.Events != 1 || legacy.ExactEvents != 0 || legacy.TokensIn != 0 {
		t.Fatalf("legacy bucket = %+v", legacy)
	}
	if (&ProviderStats{}).L1HitRate() != 0 {
		t.Fatal("empty stats hit rate must be 0")
	}
}

func TestSummarizeToleratesBadLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if _, err := NewRecorder(path); err != nil {
		t.Fatal(err)
	}
	bad := []byte("not json\n{\"turn\": 1, \"tokens_in\": 500, \"tokens_out\": 100}\n\ngarbage\n")
	// Direct write to create a mixed log.
	writeAll(t, path, bad)
	s, err := Summarize(path, DefaultCostMissPerMTok, DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	if s.Events != 1 {
		t.Fatalf("events = %d, want 1 (bad lines skipped)", s.Events)
	}
	if s.TokensIn != 500 {
		t.Fatalf("tokens_in = %d, want 500", s.TokensIn)
	}
	if s.SliceHits != 0 {
		t.Fatalf("slice_hits = %d, want 0 (older log without the field)", s.SliceHits)
	}
}

func TestSummarizeEmptyLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if _, err := NewRecorder(path); err != nil {
		t.Fatal(err)
	}
	s, err := Summarize(path, DefaultCostMissPerMTok, DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	if s.Events != 0 || s.SavingsUSD != 0 || s.SavingsRate != 0 {
		t.Fatalf("empty log must yield zero summary, got %+v", s)
	}
}

func TestSummarizeMissingFile(t *testing.T) {
	if _, err := Summarize(filepath.Join(t.TempDir(), "nope.jsonl"), 1, 1); err == nil {
		t.Fatal("missing file must error")
	}
}

func approx(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

func writeAll(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestEventL3SliceIDRoundTrip: the L3 source slice id survives the usage
// wire (Issue #267 step 4) so a wrong reused answer can be traced back
// to the exact slice for rejection.
func TestEventL3SliceIDRoundTrip(t *testing.T) {
	e := Event{L3Reuse: true, L3SliceID: "s-42"}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"l3_slice_id":"s-42"`) {
		t.Fatalf("wire = %s, want l3_slice_id field", b)
	}
	var back Event
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !back.L3Reuse || back.L3SliceID != "s-42" {
		t.Fatalf("round-trip = %+v", back)
	}
}

// TestSummarizeInjectROI (Issue #270 step 1): the injection economics
// figure pairs the L2 injection spend against the savings it participates
// in (L2 price delta + L3 full-turn savings), per 1M injected tokens.
func TestSummarizeInjectROI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	r, err := NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	evs := []Event{
		{SessionID: "s1", Turn: 1, TokensIn: 5000, TokensOut: 300, CacheHitToken: 4000, InjectedTokens: 1000, SliceHits: 2},
		{SessionID: "s1", Turn: 2, TokensIn: 20000, TokensOut: 500, L3Reuse: true},
	}
	for _, e := range evs {
		if err := r.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Summarize(path, DefaultCostMissPerMTok, DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	if s.InjectedTokens != 1000 || s.L3Reuses != 1 {
		t.Fatalf("baseline = injected %d l3 %d", s.InjectedTokens, s.L3Reuses)
	}
	// l2Savings = 1000*(0.27-0.07)/1e6 = 0.0002
	// l3Savings = (20000*0.27 + 500*0.27)/1e6 = 0.005535
	// InjectROI = (0.0002+0.005535)/1000*1e6 = 5.735 USD per 1M injected
	want := 5.735
	if diff := s.InjectROI - want; diff < -1e-9 || diff > 1e-9 {
		t.Fatalf("InjectROI = %v, want %v", s.InjectROI, want)
	}
}

// TestSummarizeInjectROIZeroWhenNoInjection: no injection, no figure.
func TestSummarizeInjectROIZeroWhenNoInjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	r, err := NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Append(Event{SessionID: "s", Turn: 1, TokensIn: 1000, TokensOut: 100}); err != nil {
		t.Fatal(err)
	}
	s, err := Summarize(path, DefaultCostMissPerMTok, DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	if s.InjectROI != 0 {
		t.Fatalf("InjectROI = %v, want 0", s.InjectROI)
	}
}

// TestSummarizeL3NegativeObservability covers the Issue #262 additive
// fields: per-turn L3 decision detail sums into the Summary, and older
// log lines without the fields read as zero (backward compatible).
func TestSummarizeL3NegativeObservability(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	r, err := NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	evs := []Event{
		// Legacy-style line: no L3 negative fields at all.
		{SessionID: "s1", Turn: 1, TokensIn: 100, L3Reuse: true},
		// Full negative detail on one turn.
		{
			SessionID: "s1", Turn: 2, TokensIn: 200,
			L3GreyCandidates: 3, L3JudgeReject: 1, L3JudgeApproved: 2,
			L3RulesReject: 4, L3FingerprintReject: 1, L3IsolatedReject: 2,
		},
		// Suspected false hit: retry bypassed L3 and went upstream.
		{SessionID: "s2", Turn: 1, TokensIn: 300, L3FalseHit: true},
	}
	for _, e := range evs {
		if err := r.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Summarize(path, DefaultCostMissPerMTok, DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	if s.L3Reuses != 1 {
		t.Fatalf("l3 reuses = %d, want 1", s.L3Reuses)
	}
	if s.L3GreyCandidates != 3 || s.L3JudgeReject != 1 || s.L3JudgeApproved != 2 {
		t.Fatalf("grey/judge = %d/%d/%d, want 3/1/2",
			s.L3GreyCandidates, s.L3JudgeReject, s.L3JudgeApproved)
	}
	if s.L3RulesReject != 4 || s.L3FingerprintReject != 1 || s.L3IsolatedReject != 2 {
		t.Fatalf("rejects = %d/%d/%d, want 4/1/2",
			s.L3RulesReject, s.L3FingerprintReject, s.L3IsolatedReject)
	}
	if s.L3FalseHits != 1 {
		t.Fatalf("false hits = %d, want 1", s.L3FalseHits)
	}
}

// TestSummarizeCacheWrite: cache writes aggregate globally and per provider
// (exact events only), and old lines without the field read as zero.
func TestSummarizeCacheWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	r, err := NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []Event{
		{Provider: "claude", Exact: true, TokensIn: 1000, CacheHitToken: 600, CacheWriteToken: 350},
		{Provider: "claude", TokensIn: 500, CacheWriteToken: 999}, // estimated: excluded per provider
		{Provider: "claude", Exact: true, TokensIn: 100},          // no writes reported
	} {
		if err := r.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Summarize(path, DefaultCostMissPerMTok, DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	if s.CacheWriteTokens != 1349 {
		t.Errorf("global cache writes = %d, want 1349", s.CacheWriteTokens)
	}
	if p := s.ByProvider["claude"]; p == nil || p.CacheWriteTokens != 350 {
		t.Errorf("per-provider exact cache writes = %+v, want 350", p)
	}
}
