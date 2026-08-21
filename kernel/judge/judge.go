// Package judge implements the two-stage verification chain (Issue #8):
// a zero-cost rule gate (fast reject) followed by an optional asynchronous
// LLM judge as final confirmation — the J(q, s, a) binary decision from
// Krites (arXiv:2602.13165). Fingerprints alone prove "files didn't change",
// not "this question is the same question"; the grey zone (Issue #7) is
// exactly where a judge is required before reuse.
package judge

import (
	"context"

	"semantix/kernel/fingerprint"
	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

// Verdict is the outcome of one verification stage.
type Verdict int

const (
	// Reject: do not reuse (rule gate failed, or grey zone with no judge).
	Reject Verdict = iota
	// Confirm: safe to reuse (rule gate passed, or judge approved).
	Confirm
	// NeedJudge: rules are inconclusive — route to the async LLM judge.
	NeedJudge
)

// Candidate is a slice under verification before reuse.
type Candidate struct {
	Query   string
	SliceID string
	Content string
	Scope   slice.Scope
	Type    slice.SliceType
	Zone    zone.Zone
	// Deps carries the slice's dependency fingerprints (Issue #8 stage ①);
	// RootDir is where they are verified against the live filesystem.
	Deps    fingerprint.Deps
	RootDir string
}

// Judge is the async LLM final-confirmation stage: J(q, s, a) — given the
// new request q, the cached slice s and its answer, decide whether reusing
// s's answer for q is acceptable. Implementations may be NoopJudge (rules
// only) until a model backend is wired.
type Judge interface {
	// Confirm reports whether reusing the candidate is acceptable.
	Confirm(ctx context.Context, c Candidate) (bool, error)
}

// NoopJudge is the default: no LLM wired, the grey zone falls back to Reject
// (failure-safe: never reuse without confirmation).
type NoopJudge struct{}

// Confirm implements Judge for NoopJudge.
func (NoopJudge) Confirm(context.Context, Candidate) (bool, error) { return false, nil }

// Stats accumulates verification observability (waste++ tracking, Issue #8):
// every rejection is a wasted reuse opportunity to be monitored.
type Stats struct {
	Confirmed     int // clear hits + judge-approved
	RulesReject   int // miss / grey-without-judge
	Fingerprint   int // dependency fingerprint changed (stage ① gate)
	JudgeReject   int // judge declined
	JudgeApproved int // judge approved
	JudgeError    int // judge call failed (network/timeout/parse) — conservative reject, not a verdict
	NeedJudge     int // routed to judge
}

func (s *Stats) add(o Stats) {
	s.Confirmed += o.Confirmed
	s.RulesReject += o.RulesReject
	s.Fingerprint += o.Fingerprint
	s.JudgeReject += o.JudgeReject
	s.JudgeApproved += o.JudgeApproved
	s.JudgeError += o.JudgeError
	s.NeedJudge += o.NeedJudge
}

// RuleGate is the deterministic zero-cost first stage. With the current
// metadata (no dependency fingerprints yet), it routes by zone:
//   - hit  -> Confirm (clearly reusable, no judge cost)
//   - grey -> NeedJudge when a judge is wired, else Reject (conservative)
//   - miss -> Reject
type RuleGate struct {
	// Judge, when non-nil, receives grey-zone candidates for final
	// confirmation. nil means grey is treated as Reject.
	Judge Judge
	// Stats, when non-nil, accumulates observability counters.
	Stats *Stats
}

// Check applies the rule gate to one candidate and returns the verdict plus
// a human-readable reason (for observability, Issue #7 dashboard).
func (g RuleGate) Check(c Candidate) (Verdict, string) {
	switch c.Zone {
	case zone.Hit:
		g.count(Stats{Confirmed: 1})
		return Confirm, "clear hit: reuse without judge"
	case zone.Miss:
		g.count(Stats{RulesReject: 1})
		return Reject, "clear miss"
	default: // grey
		if g.Judge != nil {
			g.count(Stats{NeedJudge: 1})
			return NeedJudge, "grey zone: async judge required"
		}
		g.count(Stats{RulesReject: 1})
		return Reject, "grey zone: no judge wired, conservative reject"
	}
}

// Chain runs the fingerprint gate, then rules, then (for grey candidates)
// the judge, returning the final verdict. A changed dependency fingerprint
// is a hard reject before any rules/judge cost (Issue #8 stage ①).
func (g RuleGate) Chain(ctx context.Context, c Candidate) (Verdict, string, error) {
	if len(c.Deps) > 0 {
		changed, err := fingerprint.Verify(c.RootDir, c.Deps)
		if err != nil {
			g.count(Stats{Fingerprint: 1})
			return Reject, "fingerprint gate error: conservative reject", nil
		}
		if len(changed) > 0 {
			g.count(Stats{Fingerprint: 1})
			return Reject, "fingerprint changed: " + changed[0], nil
		}
	}
	v, reason := g.Check(c)
	if v != NeedJudge {
		return v, reason, nil
	}
	if g.Judge == nil {
		// Keep the Check contract: grey without a judge is a conservative
		// reject, never an ambiguous NeedJudge.
		g.count(Stats{RulesReject: 1})
		return Reject, "grey zone: no judge wired, conservative reject", nil
	}
	ok, err := g.Judge.Confirm(ctx, c)
	if err != nil {
		// Judge call failure (network/timeout/parse) is not a judge verdict:
		// count it separately so observability can tell "judge unavailable"
		// apart from "judge declined" (Issue #245).
		g.count(Stats{JudgeError: 1})
		return Reject, "judge error: conservative reject", err
	}
	if ok {
		g.count(Stats{Confirmed: 1, JudgeApproved: 1})
		return Confirm, "judge approved", nil
	}
	g.count(Stats{JudgeReject: 1})
	return Reject, "judge declined", nil
}

func (g RuleGate) count(s Stats) {
	if g.Stats != nil {
		g.Stats.add(s)
	}
}
