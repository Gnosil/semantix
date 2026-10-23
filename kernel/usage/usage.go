// Package usage records per-turn LLM usage events and summarizes the cost
// savings produced by the semantic cache layers (Issue #60 / U17):
//
//   - L2 savings: injected tokens that would otherwise be re-sent at miss
//     price, charged at the cache-hit price difference
//   - L3 savings: a fully reused Result slice skips the backend call — the
//     entire turn cost is saved
//
// Pricing defaults follow the DeepSeek-chat example (cache hit ≈ 1/4 of
// miss; see docs/reports/cache-taxonomy.md) and are overridable per call.
package usage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Default pricing (USD per 1M tokens), DeepSeek-chat example prices.
const (
	DefaultCostMissPerMTok = 0.27
	DefaultCostHitPerMTok  = 0.07
)

// Event is one LLM turn's usage observation.
type Event struct {
	SessionID string `json:"session_id,omitempty"`
	Turn      uint64 `json:"turn"`
	// Provider is the upstream endpoint's configured name and Vendor its wire
	// shape ("openai" | "anthropic" | ...). Together with Model they key the
	// per-provider L1 hit-rate calibration (GLM-P0-3 / #291; spec §4.2).
	// Empty on logs written before these fields existed (wire: append-only).
	Provider string `json:"provider,omitempty"`
	Vendor   string `json:"vendor,omitempty"`
	Model    string `json:"model,omitempty"`
	// Exact marks token counts relayed from real provider usage accounting;
	// false means the gateway's bytes/4 estimate (pre-#291 logs are all
	// estimates and unmarshal to false).
	Exact         bool  `json:"exact,omitempty"`
	TokensIn      int64 `json:"tokens_in"`
	TokensOut     int64 `json:"tokens_out"`
	CacheHitToken int64 `json:"cache_hit_tokens"`
	// CacheWriteToken is the subset of TokensIn written into the provider
	// prefix cache (billed at the write premium, e.g. 1.25x on Anthropic's
	// 5-minute TTL). Only providers that report writes fill it; omitted
	// otherwise, and on logs written before the field existed.
	CacheWriteToken int64 `json:"cache_write_tokens,omitempty"`
	// L3Reuse marks a turn fully served by a verified L3 result (no backend
	// call at all) — its entire cost is saved.
	L3Reuse bool `json:"l3_reuse,omitempty"`
	// L3SliceID is the source slice that served an L3 reuse (Issue #267 step
	// 4): the veto entry point — a consumer can reject the exact slice that
	// produced a wrong reused answer. Empty when L3Reuse is false.
	L3SliceID string `json:"l3_slice_id,omitempty"`
	// InjectedTokens is the L2 injection block size for this turn.
	InjectedTokens int64 `json:"injected_tokens,omitempty"`
	// SliceHits is the number of semantic slices that hit and were injected
	// this turn (L2 injected slice count). Omitted for older logs, which
	// Summarize reads as 0.
	SliceHits int `json:"slice_hits,omitempty"`
	// JudgeDecisions records the grey-zone LLM judge verifications this turn
	// triggered (Issue #242 gap 1). Empty on every turn that never entered
	// the grey zone, and on every deployment without a judge configured.
	JudgeDecisions []JudgeDecision `json:"judge,omitempty"`
	// L3 negative observability (Issue #262): per-turn L3 decision detail.
	// All fields are additive — older logs without them read as zero.
	L3GreyCandidates int `json:"l3_grey_candidates,omitempty"`
	L3JudgeReject    int `json:"l3_judge_reject,omitempty"`
	// L3JudgeError counts judge calls that FAILED (transport/timeout/parse) —
	// the judge was unavailable, which is not a verdict (Issue #245). The same
	// physical incident is also recorded on the Issue #242 channel as a
	// JudgeDecision with Verdict=="fail_closed"; the two must never be summed.
	L3JudgeError        int `json:"l3_judge_error,omitempty"`
	L3JudgeApproved     int `json:"l3_judge_approved,omitempty"`
	L3RulesReject       int `json:"l3_rules_reject,omitempty"`
	L3FingerprintReject int `json:"l3_fingerprint_reject,omitempty"`
	L3IsolatedReject    int `json:"l3_isolated_reject,omitempty"`
	// L3FalseHit marks a suspected false hit: the user retried a query that
	// was just served from L3 within the same session (gateway heuristic,
	// Issue #262 §2.2). The request bypassed L3 and went upstream.
	L3FalseHit bool  `json:"l3_false_hit,omitempty"`
	At         int64 `json:"at"` // unix seconds
}

// JudgeDecision is one grey-zone judge verification, as recorded on the
// turn that triggered it (Issue #242 gap 1).
//
// It lives in the usage log rather than only in a process log line because
// the judge is a *separate upstream model call*: it does not go through the
// serving channel, so nothing else meters it. Summarize is the only place
// that turns this library's savings into a number, so a cost the savings
// figure must be reconciled against has to be reachable from here.
type JudgeDecision struct {
	// SliceID is the candidate that entered the grey zone.
	SliceID string `json:"slice_id,omitempty"`
	// RelConfidence is the score/top1 ratio that classified it as grey.
	RelConfidence float64 `json:"rel_confidence,omitempty"`
	// Verdict: "approved" | "declined" | "fail_closed" | "skipped".
	// fail_closed means the call errored or timed out — the rejection
	// carries no semantic information, which is exactly the distinction
	// the GW4 acceptance could not make from disk.
	Verdict string `json:"verdict"`
	// Reason is the verification chain's own explanation.
	Reason string `json:"reason,omitempty"`
	// Called reports that the judge model was actually invoked. This is
	// the unit of judge cost: a "skipped" decision never left the process.
	Called bool `json:"called,omitempty"`
	// FailClosed mirrors Verdict=="fail_closed" as a filterable flag.
	FailClosed bool `json:"fail_closed,omitempty"`
	// Error is the judge error text when FailClosed.
	Error string `json:"error,omitempty"`
	// LatencyMs is the judge call duration in milliseconds.
	LatencyMs int64 `json:"latency_ms,omitempty"`
	// PromptTokens estimates the judge's input tokens (byte/4, the same
	// estimator the gateway uses elsewhere — never a tokenizer count).
	PromptTokens int64 `json:"prompt_tokens,omitempty"`
}

// Recorder appends usage events to a JSONL file (0600, atomic rewrite via
// temp+rename, tolerant of trailing bad lines).
type Recorder struct {
	path string
}

// NewRecorder opens (creating if needed) the usage log at path.
func NewRecorder(path string) (*Recorder, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("usage: mkdir: %w", err)
	}
	// Touch the file so Summarize on an empty-but-created log yields zeros.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("usage: create: %w", err)
	}
	f.Close()
	return &Recorder{path: path}, nil
}

// Append writes one event (append-only; a corrupt tail never blocks writes).
func (r *Recorder) Append(e Event) error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("usage: append: %w", err)
	}
	defer f.Close()
	enc, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("usage: marshal: %w", err)
	}
	if _, err := f.Write(append(enc, '\n')); err != nil {
		return fmt.Errorf("usage: write: %w", err)
	}
	return nil
}

// Summary aggregates a usage log.
type Summary struct {
	Events           int     // total turns recorded
	TokensIn         int64   // total input tokens (including cache hits)
	TokensOut        int64   // total output tokens
	CacheHitTokens   int64   // tokens served from the provider prefix cache
	CacheWriteTokens int64   // tokens written into the provider prefix cache
	L3Reuses         int     // turns fully served by L3 reuse
	InjectedTokens   int64   // total L2 injected tokens
	SliceHits        int     // total slices hit and injected across turns
	CostPaidUSD      float64 // what the user actually paid
	CostNoCacheUSD   float64 // what it would cost without any cache
	SavingsUSD       float64 // CostNoCache - CostPaid, gross of judge cost
	SavingsRate      float64 // Savings / CostNoCache (0 when no cost)
	InjectROI        float64 // cache savings per 1M injected tokens (Issue #270 step 1)

	// Grey-zone judge accounting (Issue #242 gap 1). These describe a
	// different model on a different channel, so they are reported
	// separately and deliberately do NOT move SavingsUSD/SavingsRate —
	// see JudgeCostUSD for the net figure.
	JudgeDecisions    int   // grey-zone verifications recorded
	JudgeCalls        int   // of those, ones that actually invoked the judge (billable)
	JudgeApproved     int   // judge said the cached answer is reusable
	JudgeDeclined     int   // judge said no (a real verdict)
	JudgeFailClosed   int   // judge errored/timed out; rejection carries no verdict
	JudgeSkipped      int   // rejected before the judge was reached (free)
	JudgePromptTokens int64 // byte/4 estimate of tokens sent to the judge
	JudgeLatencyMs    int64 // total time spent in judge calls

	// L3 negative observability (Issue #262): sums of the per-turn fields.
	L3GreyCandidates int
	L3JudgeReject    int
	// L3JudgeError counts judge unavailability (Issue #245). NOTE: this and
	// JudgeFailClosed (Issue #242) count the same events through different
	// channels — report them separately, never add them together.
	L3JudgeError        int
	L3JudgeApproved     int
	L3RulesReject       int
	L3FingerprintReject int
	L3IsolatedReject    int
	L3FalseHits         int // turns marked as suspected false hits

	// ByProvider groups per-endpoint L1 telemetry (GLM-P0-3 / #291). The key
	// is Event.Provider; events from pre-#291 logs land under "". Hit-rate
	// calibration must use ExactEvents-backed figures — estimated events
	// never observed provider cache accounting and would read as 0% hits.
	ByProvider map[string]*ProviderStats
}

// ProviderStats aggregates one upstream endpoint's usage telemetry.
type ProviderStats struct {
	Events           int   // turns recorded against this endpoint
	ExactEvents      int   // of those, ones carrying real provider usage
	TokensIn         int64 // exact-only input tokens (cache hits included)
	TokensOut        int64 // exact-only output tokens
	CacheHitTokens   int64 // exact-only prefix-cache-served input tokens
	CacheWriteTokens int64 // exact-only tokens written into the prefix cache
	L3Reuses         int   // turns served without an upstream call
}

// L1HitRate is the prefix-cache hit share of exact-metered input tokens.
// Zero when the endpoint has no exact events yet.
func (p *ProviderStats) L1HitRate() float64 {
	if p == nil || p.TokensIn == 0 {
		return 0
	}
	return float64(p.CacheHitTokens) / float64(p.TokensIn)
}

// JudgeCostUSD estimates what the grey-zone judge calls cost, at the judge
// model's own input price per 1M tokens (Issue #242 gap 1).
//
// It is a separate call rather than a Summary field because the judge model
// is configured independently of the served model ([cache] judge_model in
// the gateway config) and its price is not derivable from this log. The
// judge's completion is a single word — one to a handful of tokens, orders
// of magnitude below the byte/4 estimator's own error — so only the input
// side is modeled.
//
// Net savings = SavingsUSD - JudgeCostUSD(judgePrice). SavingsUSD itself
// stays gross so the served-model arithmetic keeps one meaning.
func (s *Summary) JudgeCostUSD(costPerMTok float64) float64 {
	return float64(s.JudgePromptTokens) * costPerMTok / 1e6
}

// Summarize reads the log at path and computes the aggregate. Malformed
// lines are skipped and never fatal.
//
// Cost model: provider input tokens are billed at miss price except the
// prefix-cached portion at hit price; output tokens at miss price. A turn
// with L3Reuse=true consumed nothing (no backend call) — its would-be cost
// is excluded from CostPaidUSD and counted entirely as savings.
func Summarize(path string, costMiss, costHit float64) (*Summary, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("usage: open: %w", err)
	}
	defer f.Close()

	s := &Summary{ByProvider: map[string]*ProviderStats{}}
	var l3In, l3Out int64 // tokens of L3-reused turns (not billed)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // tolerate entries up to 8 MiB
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			continue // tolerate bad lines
		}
		s.Events++
		s.TokensIn += e.TokensIn
		s.TokensOut += e.TokensOut
		s.CacheHitTokens += e.CacheHitToken
		s.CacheWriteTokens += e.CacheWriteToken
		s.InjectedTokens += e.InjectedTokens
		s.SliceHits += e.SliceHits
		p := s.ByProvider[e.Provider]
		if p == nil {
			p = &ProviderStats{}
			s.ByProvider[e.Provider] = p
		}
		p.Events++
		if e.L3Reuse {
			p.L3Reuses++
		}
		if e.Exact {
			// Only exact events feed the calibration figures: an estimated
			// event never saw provider cache accounting, so folding it in
			// would drag every hit-rate toward zero.
			p.ExactEvents++
			p.TokensIn += e.TokensIn
			p.TokensOut += e.TokensOut
			p.CacheHitTokens += e.CacheHitToken
			p.CacheWriteTokens += e.CacheWriteToken
		}
		if e.L3Reuse {
			s.L3Reuses++
			l3In += e.TokensIn
			l3Out += e.TokensOut
		}
		for _, jd := range e.JudgeDecisions {
			s.JudgeDecisions++
			s.JudgeLatencyMs += jd.LatencyMs
			if jd.Called {
				// Only a reached judge sent a prompt upstream; a skipped
				// decision must never inflate the cost estimate.
				s.JudgeCalls++
				s.JudgePromptTokens += jd.PromptTokens
			}
			switch jd.Verdict {
			case "approved":
				s.JudgeApproved++
			case "declined":
				s.JudgeDeclined++
			case "fail_closed":
				s.JudgeFailClosed++
			case "skipped":
				s.JudgeSkipped++
			}
		}
		// L3 negative observability (Issue #262): sum the per-turn detail;
		// older logs read as zero via the additive fields.
		s.L3GreyCandidates += e.L3GreyCandidates
		s.L3JudgeReject += e.L3JudgeReject
		s.L3JudgeError += e.L3JudgeError
		s.L3JudgeApproved += e.L3JudgeApproved
		s.L3RulesReject += e.L3RulesReject
		s.L3FingerprintReject += e.L3FingerprintReject
		s.L3IsolatedReject += e.L3IsolatedReject
		if e.L3FalseHit {
			s.L3FalseHits++
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("usage: scan: %w", err)
	}

	billedIn := s.TokensIn - s.CacheHitTokens - l3In
	billedOut := s.TokensOut - l3Out
	paid := float64(billedIn)*costMiss/1e6 +
		float64(s.CacheHitTokens)*costHit/1e6 +
		float64(billedOut)*costMiss/1e6
	noCache := float64(s.TokensIn)*costMiss/1e6 + float64(s.TokensOut)*costMiss/1e6

	s.CostPaidUSD = paid
	s.CostNoCacheUSD = noCache
	s.SavingsUSD = noCache - paid
	if noCache > 0 {
		s.SavingsRate = s.SavingsUSD / noCache
	}
	// Issue #270 step 1: injection economics. The L2 injection is the
	// only cache layer that spends tokens upfront; the ROI pairs the
	// injection spend against the savings it participates in (L2 price
	// delta + L3 full-turn savings). Reported per 1M injected tokens for
	// readability.
	if s.InjectedTokens > 0 {
		l2Savings := float64(s.InjectedTokens) * (costMiss - costHit) / 1e6
		l3Savings := (float64(l3In)*costMiss + float64(l3Out)*costMiss) / 1e6
		s.InjectROI = (l2Savings + l3Savings) / float64(s.InjectedTokens) * 1e6
	}
	return s, nil
}
