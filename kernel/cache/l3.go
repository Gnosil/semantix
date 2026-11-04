package cache

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"semantix/kernel/fingerprint"
	"semantix/kernel/judge"
	"semantix/kernel/promote"
	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

// L3Decider implements Decider with a fail-closed L3 path (Issue #59 / U16):
// a Result slice is reusable only when (a) retrieval finds it relevant,
// (b) an optional age policy keeps it in Hit or a judge approves its Grey
// verdict, (c) its dependency files still match the captured mtimes (fast
// path) and (d) the sha256 fingerprint still matches (authority path). Any
// failure in the chain rejects reuse — never returns stale data.
type L3Decider struct {
	Index slice.Index
	Store slice.Store // optional; used to re-read full slices by ID
	Root  string      // dependency root (project dir) for mtime/Verify
	Zones *zone.Zones // grey-zone classifier (nil → zone.Default)
	Judge judge.Judge // grey-zone LLM judge (nil → conservative reject, kernel/judge RuleGate)
	K     int         // top-k candidates (default 3)

	// OnJudge, when non-nil, receives one JudgeObservation per grey-zone
	// verification (Issue #242 gap 1) so the host can make the verdict —
	// and the judge's cost — durable. It is a hook rather than a direct
	// write so kernel/cache stays free of the event bus / gateway
	// (docs/specs/h2h3-resource-orchestration.md §6). Called synchronously
	// on the decision path with the request context, so implementations
	// must not block; a nil hook disables observation entirely.
	OnJudge func(ctx context.Context, obs JudgeObservation)

	// LexicalFloor is the minimum lexical-support score for a zone Hit to
	// reuse directly (Issue #260). A Hit whose index reports zero lexical
	// support (pure-vector hit, no term overlap) is downgraded to Grey —
	// judge-gated reuse, fail-closed without a judge. nil → default 0.05;
	// an explicit 0 disables the gate (escape hatch). Hits with
	// LexicalValid=false are never blocked (not measured).
	LexicalFloor *float64

	// OnLexicalGate, when non-nil, receives one LexicalGateObservation per
	// zone-Hit candidate the lexical gate evaluated (Issue #260), so the
	// host can count blocks and measure hit-rate loss. Hook-only, like
	// OnJudge; nil disables observation.
	OnLexicalGate func(ctx context.Context, obs LexicalGateObservation)

	// MinOrigin is the lowest provenance level allowed to pass the L3 Hit
	// gate straight through (Issue #279): a candidate below the floor is
	// downgraded to Grey — judge-gated reuse, fail-closed without a judge
	// (same path as the #260 lexical gate). The zero value (empty origin,
	// level 1) downgrades nothing — embedding callers opt in explicitly.
	MinOrigin slice.Origin

	// Negative observability (Issue #262): both are nil-safe and never
	// change DecideL3's behavior. Obs accumulates decision-path counters
	// across calls (thread-safe, Snapshot for reads); OnDecide receives
	// each call's delta right before return — the gateway concurrent path
	// consumes the delta to write per-turn usage records. OnJudge (#242)
	// observes the judge stage; Obs covers every rejection path.
	Obs      *ObsAccum
	OnDecide func(Obs)

	// Adapt provides per-entry TauLow overrides (Issue #259 阶段 3,
	// vCache route): for each Result candidate the learned value replaces
	// the effective TauLow before classification, clamped so the grey
	// zone never collapses. nil-safe — without it every entry uses the
	// global/per-type threshold (cold start).
	Adapt interface {
		TauLow(sliceID string, global float64) float64
	}

	// Promote is the judge-approved promotion decision surface (Issue
	// #280): a consensus-approved (query, slice, version) inside the TTL
	// window skips the judge on later hits; a blacklisted candidate's
	// promotion write is blocked; judge declines and consensus failures
	// are recorded as failure lessons in the independent rejection
	// namespace. nil-safe — without it the L3 path behaves exactly as
	// before (single judge per grey candidate, no promotion).
	Promote Promote

	// Consensus is the second-perspective provider of the promotion gate
	// (Issue #280): after the primary judge (d.Judge) approves, a
	// non-nil Consensus that implements judge.VariantJudge contributes
	// its rephrased-rubric ConfirmSecondary — both perspectives must
	// approve before a promotion entry is written. nil → single-
	// perspective baseline (consensus=1). The L3 reuse path itself
	// always uses d.Judge (main-path cost unchanged: primary 1 call +
	// secondary 1 call when consensus is on).
	Consensus judge.Judge
}

// Promote is the promotion decision surface the L3 grey path consults
// (Issue #280). Implementations aggregate a promotion store and the
// independent failure-lesson (rejection) namespace; see kernel/promote.
type Promote interface {
	// Lookup reports whether (slice, query) has a promotion entry whose
	// content version matches and whose TTL window is open.
	Lookup(sourceSliceID, query, currentVersion string, now int64) bool
	// Promote writes a promotion entry unless the candidate is
	// blacklisted (ErrBlacklisted from kernel/promote).
	Promote(e promote.Entry) error
	// Rejected records one failure lesson (judge decline / consensus
	// failure) in the independent rejection namespace.
	Rejected(sourceSliceID, query, reason string, now int64)
}

// minGreyWidth keeps a non-empty grey zone when an adaptive TauLow would
// otherwise reach or pass TauHigh: the learned value is clamped to
// TauHigh − minGreyWidth so ambiguous candidates still route to the judge
// instead of flipping straight to Miss. Matches the evolve tuning step so
// clamping cannot fight the engine's own granularity.
const minGreyWidth = 0.05

// Obs is a negative-observation counter snapshot (Issue #262): the L3
// runtime paths that rejected reuse and why, plus the approved/reused
// totals. It is a plain value type — safe to copy and pass by value.
type Obs struct {
	Candidates        int // Result-typed candidates found by retrieval
	Grey              int // grey-zone candidates routed to the judge
	RulesReject       int // clear miss, or grey with no judge wired
	FingerprintReject int // fingerprint gate error / mtime|sha256 changed / L3Safe=false
	IsolatedReject    int // context/model isolation mismatch (Issue #133)
	JudgeReject       int // judge declined
	JudgeError        int // judge call failed/timed out — unavailable, not a verdict (Issue #245)
	JudgeApproved     int // judge approved (later gates may still reject)
	Reused            int // finally reused (all gates passed)
	// Promotion decision counters (Issue #280; zero when promote is not
	// wired or nothing happened).
	PromoteHit         int // promotion hit: judge skipped for this candidate
	PromoteWritten     int // consensus passed, promotion entry written
	PromoteRejected    int // consensus failed (secondary rubric declined)
	PromoteBlacklisted int // promotion write blocked by repeated rejections
}

// ObsAccum is a thread-safe accumulator for Obs. Consumers read snapshots
// via Snapshot; the gateway instead consumes per-call deltas through
// L3Decider.OnDecide.
type ObsAccum struct {
	mu sync.Mutex
	n  Obs
}

// Snapshot returns a copy of the accumulated counters.
func (a *ObsAccum) Snapshot() Obs {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.n
}

// add folds a per-call delta into the accumulator.
func (a *ObsAccum) add(o Obs) {
	a.mu.Lock()
	a.n.Candidates += o.Candidates
	a.n.Grey += o.Grey
	a.n.RulesReject += o.RulesReject
	a.n.FingerprintReject += o.FingerprintReject
	a.n.IsolatedReject += o.IsolatedReject
	a.n.JudgeReject += o.JudgeReject
	a.n.JudgeError += o.JudgeError
	a.n.JudgeApproved += o.JudgeApproved
	a.n.Reused += o.Reused
	a.n.PromoteHit += o.PromoteHit
	a.n.PromoteWritten += o.PromoteWritten
	a.n.PromoteRejected += o.PromoteRejected
	a.n.PromoteBlacklisted += o.PromoteBlacklisted
	a.mu.Unlock()
}

// DecideL2 returns top-k hits filtered by the grey zone (hit-only enters the
// injection block; grey/miss are skipped — Krites §3.1).
func (d *L3Decider) DecideL2(ctx context.Context, q Query) ([]slice.Hit, error) {
	z := d.zones()
	k := d.k()
	hits, err := d.Index.Search(q.UserInput, k, q.Scope)
	if err != nil {
		return nil, err
	}
	top1 := 0.0
	if len(hits) > 0 {
		top1 = hits[0].Score
	}
	out := hits[:0]
	for _, h := range hits {
		z2 := z
		if h.Slice != nil {
			z2 = z.ForType(h.Slice.Type.String()) // Issue #259 阶段 2
		}
		if z2.ClassifyHit(h, top1) == zone.Hit {
			out = append(out, h)
		}
	}
	return out, nil
}

// DecideL3 returns a verified reusable result, or nil when any gate fails
// (fail-closed). Verification chain, cheapest first:
//
//  1. retrieval: Result-typed slice, classified with the two-axis L3 verdict
//     (Issue #241) and the optional freshness policy; combined Grey verdicts
//     require judge approval
//  2. context/model isolation
//  3. mtime fast-fail: every captured dep's mtime unchanged
//  4. fingerprint authority: Verify reports zero changed paths
//
// A slice with no dependency capture (Deps nil) is eligible without
// verification — it depended on nothing, so nothing can go stale.
func (d *L3Decider) DecideL3(ctx context.Context, q Query) (*L3Result, error) {
	z := d.zones()
	k := d.k()
	hits, err := d.Index.Search(q.UserInput, k, q.Scope)
	if err != nil {
		return nil, err
	}
	// Negative observability (Issue #262): count every decision path of
	// this call, then hand the delta to OnDecide / ObsAccum on the way
	// out — every return path is covered by the defer.
	var o Obs
	defer func() { d.observe(o) }()

	if len(hits) == 0 {
		return nil, nil // no candidate → no reuse
	}
	// Two-axis denominator split (Issue #241). globalTop1 is the scale
	// anchor over the whole hit list (the Prompt twin usually leads it);
	// resultTop1 is the max among the Result candidates L3 actually
	// considers. ClassifyL3 needs both — scoping to resultTop1 alone would
	// make the best Result's ratio 1.0 unconditionally and gut the relative
	// axis (the refuted option 1, see Allenllii's review on the issue).
	globalTop1 := hits[0].Score
	cands := make([]slice.Hit, 0, len(hits))
	resultTop1 := 0.0
	for _, h := range hits {
		if h.Score > globalTop1 {
			globalTop1 = h.Score // max, not hits[0]: never assume Search sorted
		}
		if h.Slice == nil || h.Slice.Type != slice.Result {
			continue // only Result slices carry reusable outcomes
		}
		o.Candidates++ // Issue #262: every Result candidate under consideration
		cands = append(cands, h)
		if h.Score > resultTop1 {
			resultTop1 = h.Score
		}
	}
	if len(cands) == 0 {
		return nil, nil // no reusable candidate → no reuse
	}

	for _, h := range cands {
		s := h.Slice
		// Per-type thresholds (Issue #259 阶段 2): each candidate is
		// classified with its slice type's override (or the global
		// baseline when the type is not configured).
		tz := z.ForType(s.Type.String())
		// Per-entry adaptive TauLow (Issue #259 阶段 3): the learned value
		// replaces the effective TauLow. It is clamped below TauHigh so
		// the grey zone never collapses (ambiguous candidates must still
		// reach the judge rather than flip to Miss); when the configured
		// threshold band is narrower than minGreyWidth, the clamp floor
		// is the value in effect — a tighten must never turn into a
		// loosening because of the ceiling.
		if d.Adapt != nil {
			prior := tz.TauLow
			tz.TauLow = d.Adapt.TauLow(s.ID, prior)
			cap := tz.TauHigh - minGreyWidth
			if cap < prior {
				cap = prior
			}
			if tz.TauLow > cap {
				tz.TauLow = cap
			}
		}
		verdict := tz.ClassifyL3(h.Score, resultTop1, globalTop1)
		if fresh := q.Freshness.classify(s.CreatedAt); fresh < verdict {
			verdict = fresh // freshness may only make reuse more conservative
		}
		switch verdict {
		case zone.Hit:
			// Issue #279 integrity gate: a Hit whose provenance is below the
			// configured floor (import/legacy under a session-auto floor)
			// must not pass straight through — downgrade to Grey, judge-
			// gated reuse, fail-closed without a judge (same path as the
			// lexical gate below).
			if d.MinOrigin.Level() > s.Meta.Origin.Level() {
				if !d.judgeGrey(ctx, q, s, relConfidence(h.Score, resultTop1), &o) {
					continue
				}
				break
			}
			// Issue #260 lexical support gate: a Hit with no term overlap
			// (pure-vector hit) is downgraded to Grey — judge-gated reuse,
			// fail-closed without a judge. First mitigation against embedding
			// key-collision attacks (CacheAttack, arXiv:2601.23088).
			if !d.lexicalSupported(h) {
				d.observeLexicalGate(ctx, h, true)
				if !d.judgeGrey(ctx, q, s, relConfidence(h.Score, resultTop1), &o) {
					continue
				}
				break
			}
			d.observeLexicalGate(ctx, h, false)
		case zone.Grey:
			// Ambiguous: reuse only when the judge confirms (spec §3.5
			// RuleGate.Chain; nil judge → conservative reject). Fingerprint
			// re-verification below still applies to grey-approved slices.
			if !d.judgeGrey(ctx, q, s, relConfidence(h.Score, resultTop1), &o) {
				continue
			}
		default: // zone.Miss
			o.RulesReject++
			continue // clearly not the same task
		}
		// Context/model isolation (Issue #133 gateway): a cached outcome
		// produced under a different conversation history or model must
		// never be served. When the query carries a context/model (gateway
		// always does), entries without a matching stamp — including
		// unstamped legacy slices — fail closed; empty query fields keep
		// the legacy CLI behavior.
		if q.ContextHash != "" && s.Meta.ContextHash != q.ContextHash {
			o.IsolatedReject++
			continue
		}
		if q.Model != "" && s.Meta.Model != q.Model {
			o.IsolatedReject++
			continue
		}
		if !d.verified(ctx, s) {
			o.FingerprintReject++ // deps changed: stale, reject this candidate
			continue
		}
		o.Reused++
		return &L3Result{
			SliceID:  s.ID,
			Response: string(s.Content),
			CostUSD:  0, // usage accounting lands in U17
		}, nil
	}
	return nil, nil
}

// classify maps candidate age onto the existing three-zone decision model.
// A disabled policy is neutral (Hit); active policies reject timestamps they
// cannot establish as valid rather than silently treating legacy data as new.
func (f Freshness) classify(createdAt int64) zone.Zone {
	if f.TTLSeconds <= 0 {
		return zone.Hit
	}
	if f.NowUnix <= 0 || createdAt <= 0 || createdAt > f.NowUnix {
		return zone.Miss
	}
	age := f.NowUnix - createdAt
	if age > f.TTLSeconds {
		return zone.Miss
	}
	if age > f.TTLSeconds/2 {
		return zone.Grey
	}
	return zone.Hit
}

// observe folds a per-call delta into the observers (Issue #262): the
// OnDecide callback receives the raw delta (gateway per-turn usage records),
// and ObsAccum accumulates it for snapshots. Both are nil-safe.
func (d *L3Decider) observe(o Obs) {
	if d.OnDecide != nil {
		d.OnDecide(o)
	}
	if d.Obs != nil {
		d.Obs.add(o)
	}
}

// verified runs the two-stage dependency check; false is fail-closed.
func (d *L3Decider) verified(ctx context.Context, s *slice.Slice) bool {
	if len(s.Meta.Deps) == 0 {
		// No captured dependencies: reusable only under explicit opt-in
		// (extract --l3-safe). A shared/injected library must not be able
		// to mark dependency-free results reusable by omission.
		return s.Meta.L3Safe
	}
	// Every Mtimes key must exist in Deps: a half-covered entry is rejected
	// rather than partially verified (a key present in one map but not the
	// other would otherwise skip the Lstat guard or the mtime check).
	for p := range s.Meta.Mtimes {
		if _, ok := s.Meta.Deps[p]; !ok {
			return false
		}
	}
	// Uniform guard over ALL Deps keys (not just Mtimes-covered ones):
	//  1. IsLocal — never touch anything outside the dependency root
	//  2. Lstat — symlinked deps are rejected outright, verification must
	//     not follow links outside the root (MEDIUM fix, sa_20260811_123652)
	//  3. mtime fast-fail when a snapshot exists for this key
	for p := range s.Meta.Deps {
		if !filepath.IsLocal(p) {
			return false
		}
		fi, err := os.Lstat(filepath.Join(d.Root, p))
		if err != nil || fi.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if want, ok := s.Meta.Mtimes[p]; ok && fi.ModTime().Unix() != want {
			return false
		}
	}
	// Stage 2: sha256 authority (full content re-read).
	changed, err := fingerprint.Verify(d.Root, s.Meta.Deps)
	if err != nil || len(changed) > 0 {
		return false
	}
	return true
}

func (d *L3Decider) zones() zone.Zones {
	if d.Zones != nil {
		return *d.Zones
	}
	return zone.Default()
}

// judgeGrey runs the spec §3.5 grey-zone confirmation: RuleGate.Chain
// (fingerprint gate → rules → judge). The L3Decider already re-verifies
// dependency fingerprints after this (verified), so a judge "yes" still
// cannot surface stale data. A nil judge, a judge error, or a judge "no"
// all reject conservatively (fail-closed). The per-call observer o is
// folded with the rule-gate counters (Issue #262).
//
// Two of judge.Stats' six counters are deliberately NOT folded, because the
// caller already accounts for them: Confirmed is redundant with o.Reused
// (set on the DecideL3 success path) and NeedJudge is redundant with o.Grey
// (incremented at the top of this function). The omission is intentional.
//
// Callers: both the zone.Grey arm and the Issue #260 lexical-support
// downgrade inside the zone.Hit arm reach this, so JudgeError is not a
// grey-verdict-only counter (Issue #245).
//
// rel is the score/top1 ratio that classified the candidate as grey; it is
// reported through OnJudge so a rejection can later be explained without
// re-running retrieval (Issue #242 gap 1). No observation is emitted when
// no judge is wired: that path is deterministic and costs nothing, so
// recording it would only add noise to the host's usage log.
func (d *L3Decider) judgeGrey(ctx context.Context, q Query, s *slice.Slice, rel float64, o *Obs) bool {
	o.Grey++ // the zone verdict is grey; whether a judge exists decides the path
	if d.Judge == nil {
		o.RulesReject++ // grey without a judge: conservative reject
		return false
	}
	now := time.Now().Unix()
	// Issue #280: a (query, slice, version) that was consensus-approved
	// inside the TTL window skips the judge entirely — the promotion
	// entry IS the approval (still subject to the fingerprint/isolation
	// gates later in DecideL3; promotion only replaces the judge step).
	if d.Promote != nil && d.Promote.Lookup(s.ID, q.UserInput, promote.ContentVersion(s.Content), now) {
		o.PromoteHit++
		return true
	}
	timed := &timedJudge{inner: d.Judge}
	var gs judge.Stats
	cand := judge.Candidate{
		Query:   q.UserInput,
		SliceID: s.ID,
		Content: string(s.Content),
		Scope:   s.Scope,
		Type:    s.Type,
		Zone:    zone.Grey,
		Deps:    s.Meta.Deps,
		RootDir: d.Root,
	}
	gate := judge.RuleGate{Judge: timed, Stats: &gs}
	v, reason, err := gate.Chain(ctx, cand)
	o.RulesReject += gs.RulesReject
	o.FingerprintReject += gs.Fingerprint
	o.JudgeReject += gs.JudgeReject
	o.JudgeError += gs.JudgeError
	o.JudgeApproved += gs.JudgeApproved
	ok := err == nil && v == judge.Confirm
	if d.Promote != nil {
		if ok {
			// Consensus gate (Issue #280, A-MemGuard): the primary judge
			// already approved (that IS the first perspective); a second
			// perspective — the rephrased rubric — must approve too before
			// promotion eligibility is granted. L3 reuse already succeeded;
			// consensus only decides whether future hits skip the judge. A
			// consensus error is judge unavailability (Issue #245) — no
			// promotion written, counted as a judge error.
			approve := true
			var cerr error
			if d.Consensus != nil {
				if vj, ok := d.Consensus.(judge.VariantJudge); ok {
					approve, cerr = vj.ConfirmSecondary(ctx, cand)
				} else {
					approve = false // a non-variant consensus provider cannot contribute a second perspective
				}
			}
			switch {
			case cerr != nil:
				o.JudgeError++
			case !approve:
				o.PromoteRejected++
				d.Promote.Rejected(s.ID, q.UserInput, "consensus_failed", now)
			default:
				if err := d.Promote.Promote(promote.Entry{
					SourceSliceID:  s.ID,
					ContentVersion: promote.ContentVersion(s.Content),
					Query:          q.UserInput,
					PromotedAt:     now,
				}); err != nil {
					if err == promote.ErrBlacklisted {
						o.PromoteBlacklisted++
					}
					// Other store errors are best-effort: promotion is an
					// optimization, never a decision-path dependency.
				} else {
					o.PromoteWritten++
				}
			}
		} else if gs.JudgeReject > 0 {
			// Failure lesson (Issue #280 double-memory): a judge decline
			// is blacklist evidence for this (query, slice). Rule and
			// fingerprint rejections are NOT recorded — they mean the
			// candidate is stale or structurally ineligible, not that the
			// pair is a poisoned false success.
			d.Promote.Rejected(s.ID, q.UserInput, "judge_declined", now)
		}
	}
	if d.OnJudge != nil {
		obs := JudgeObservation{
			SliceID:       s.ID,
			RelConfidence: rel,
			Reason:        reason,
			Called:        timed.called,
			Latency:       timed.latency,
			PromptBytes:   len(q.UserInput) + len(s.Content),
		}
		switch {
		case err != nil:
			// The only error Chain surfaces is the judge call's own —
			// a timeout or transport failure, not a verdict.
			obs.Verdict = JudgeFailClosed
			obs.FailClosed = true
			obs.Err = err.Error()
		case !timed.called:
			obs.Verdict = JudgeSkipped
		case ok:
			obs.Verdict = JudgeApproved
		default:
			obs.Verdict = JudgeDeclined
		}
		d.OnJudge(ctx, obs)
	}
	return ok
}
func (d *L3Decider) k() int {
	if d.K > 0 {
		return d.K
	}
	return 3
}

// lexicalFloor returns the lexical-support floor: explicit override,
// default 0.05 (a tiny epsilon above the strict zero of a pure-vector
// hit), or 0 when the gate is explicitly disabled.
func (d *L3Decider) lexicalFloor() float64 {
	if d.LexicalFloor != nil {
		return *d.LexicalFloor
	}
	return 0.05
}

// lexicalSupported reports whether a candidate carries enough lexical
// support to reuse directly. LexicalValid=false means the index never
// evaluated lexical support — not measured, never blocked.
func (d *L3Decider) lexicalSupported(h slice.Hit) bool {
	if !h.LexicalValid {
		return true
	}
	return h.Lexical >= d.lexicalFloor()
}

func (d *L3Decider) observeLexicalGate(ctx context.Context, h slice.Hit, blocked bool) {
	if d.OnLexicalGate == nil {
		return
	}
	z := "hit"
	if blocked {
		z = "grey"
	}
	d.OnLexicalGate(ctx, LexicalGateObservation{
		SliceID: h.Slice.ID,
		Score:   h.Score,
		Lexical: h.Lexical,
		Blocked: blocked,
		Zone:    z,
	})
}
