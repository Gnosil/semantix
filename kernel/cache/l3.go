package cache

import (
	"context"
	"os"
	"path/filepath"

	"semantix/kernel/fingerprint"
	"semantix/kernel/judge"
	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

// L3Decider implements Decider with a fail-closed L3 path (Issue #59 / U16):
// a Result slice is reusable only when (a) retrieval finds it clearly
// relevant (zone Hit), (b) its dependency files still match the captured
// mtimes (fast path) and (c) the sha256 fingerprint still matches (authority
// path). Any failure in the chain rejects reuse — never returns stale data.
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
		if z.Classify(h.Score, top1) == zone.Hit {
			out = append(out, h)
		}
	}
	return out, nil
}

// DecideL3 returns a verified reusable result, or nil when any gate fails
// (fail-closed). Verification chain, cheapest first:
//
//	1. retrieval: Result-typed slice, zone Hit — classified with the
//	   two-axis L3 verdict (Issue #241): prominence among Result peers
//	   (resultTop1) plus a scale anchor to the raw top-1 hit (globalTop1,
//	   usually the byte-identical Prompt twin)
//	2. mtime fast-fail: every captured dep's mtime unchanged
//	3. fingerprint authority: Verify reports zero changed paths
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
		verdict := z.ClassifyL3(h.Score, resultTop1, globalTop1)
		if fresh := q.Freshness.classify(s.CreatedAt); fresh < verdict {
			verdict = fresh // freshness may only make reuse more conservative
		}
		switch verdict {
		case zone.Hit:
			// clear hit: reuse after the remaining gates below
		case zone.Grey:
			// Ambiguous: reuse only when the judge confirms (spec §3.5
			// RuleGate.Chain; nil judge → conservative reject). Fingerprint
			// re-verification below still applies to grey-approved slices.
			if !d.judgeGrey(ctx, q, s, relConfidence(h.Score, resultTop1)) {
				continue
			}
		default: // zone.Miss
			continue // clearly not the same task
		}
		// Context/model isolation (Issue #133 gateway): a cached outcome
		// produced under a different conversation history or model must
		// never be served. When the query carries a context/model (gateway
		// always does), entries without a matching stamp — including
		// unstamped legacy slices — fail closed; empty query fields keep
		// the legacy CLI behavior.
		if q.ContextHash != "" && s.Meta.ContextHash != q.ContextHash {
			continue
		}
		if q.Model != "" && s.Meta.Model != q.Model {
			continue
		}
		if !d.verified(ctx, s) {
			continue // deps changed: stale, reject this candidate
		}
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
// all reject conservatively (fail-closed).
//
// rel is the score/top1 ratio that classified the candidate as grey; it is
// reported through OnJudge so a rejection can later be explained without
// re-running retrieval (Issue #242 gap 1). No observation is emitted when
// no judge is wired: that path is deterministic and costs nothing, so
// recording it would only add noise to the host's usage log.
func (d *L3Decider) judgeGrey(ctx context.Context, q Query, s *slice.Slice, rel float64) bool {
	if d.Judge == nil {
		return false
	}
	timed := &timedJudge{inner: d.Judge}
	gate := judge.RuleGate{Judge: timed}
	v, reason, err := gate.Chain(ctx, judge.Candidate{
		Query:   q.UserInput,
		SliceID: s.ID,
		Content: string(s.Content),
		Scope:   s.Scope,
		Type:    s.Type,
		Zone:    zone.Grey,
		Deps:    s.Meta.Deps,
		RootDir: d.Root,
	})
	ok := err == nil && v == judge.Confirm
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
