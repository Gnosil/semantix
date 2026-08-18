package slice

import "math"

// ScoreParams tunes the value function (架构 §3.2 ④评分器). Zero values fall
// back to the defaults below, so ScoreParams{} is always usable. The struct
// is a plain parameter bag on purpose: a future evolve offline pass can fill
// it (parameters flow one way, evolve → params → gc), no callbacks, no
// signal interface — kernel/slice and kernel/evolve never import each other.
type ScoreParams struct {
	// HalfLifeDays: recency half-life for the exp decay term. A slice
	// untouched for one half-life keeps 50% of its recency factor.
	HalfLifeDays float64
	// GraceDays: slices created within this window sort last in eviction
	// (cold-start protection — a fresh slice has had no chance to earn
	// hits). It protects ordering only, never exempts from a hard cap.
	GraceDays float64
	// FreqPseudo: Laplace pseudo-count for the use-frequency term. With the
	// default 3, a never-used slice scores 0.5/3.5 ≈ 0.14 instead of 0.
	FreqPseudo float64
	// RejectCost: how many successful injections one rejection cancels.
	RejectCost float64
	// FeedbackGain: multiplier slope for UserFeedback (±1 → ×(1±gain)).
	FeedbackGain float64
}

// DefaultScoreParams returns the spec defaults
// (docs/specs/slice-value-eviction.md §2).
func DefaultScoreParams() ScoreParams {
	return ScoreParams{HalfLifeDays: 30, GraceDays: 7, FreqPseudo: 3, RejectCost: 2, FeedbackGain: 0.5}
}

func (p ScoreParams) withDefaults() ScoreParams {
	d := DefaultScoreParams()
	if p.HalfLifeDays <= 0 {
		p.HalfLifeDays = d.HalfLifeDays
	}
	if p.GraceDays < 0 {
		p.GraceDays = 0 // grace is opt-in: zero disables, defaults come from config
	}
	if p.FreqPseudo <= 0 {
		p.FreqPseudo = d.FreqPseudo
	}
	if p.RejectCost <= 0 {
		p.RejectCost = d.RejectCost
	}
	if p.FeedbackGain <= 0 {
		p.FeedbackGain = d.FeedbackGain
	}
	return p
}

// Weight bounds. The floor deliberately clears the Weight==0 sentinel
// ("never scored", protected by gc --min-weight) so a rescored slice can
// always be distinguished from a legacy one.
const (
	minWeight = 0.001
	maxWeight = 1.0
)

// ComputeWeight is the value function:
//
//	价值 = 时效衰减 · 使用频次 · 注入成功率 · 用户反馈
//
// (架构 §3.2 的 f(命中率, 时效衰减, 用户反馈, 注入成功率)；「意图相关度」无
// 数据源，v1 移除。) Pure and deterministic: same slice + same now → same
// weight, bit for bit.
//
// The result feeds ONLY eviction ordering and gc --min-weight. Retrieval
// (BM25, zones) never reads Weight — that invariant is what structurally
// breaks the self-reinforcement loop of Security §4.1 (a wrongly-cached
// answer can live longer, but can never become easier to hit).
func ComputeWeight(s *Slice, now int64, p ScoreParams) float64 {
	p = p.withDefaults()

	active := s.Stats.LastUsed
	if s.CreatedAt > active {
		active = s.CreatedAt
	}
	decay := 0.5 // legacy neutral prior: unknown age is neither fresh nor stale
	if active > 0 {
		age := float64(now - active)
		if age < 0 {
			age = 0
		}
		decay = math.Exp(-math.Ln2 * age / (p.HalfLifeDays * 86400))
	}

	use := float64(s.Stats.Hits + s.Stats.Injected)
	freq := (use + 0.5) / (use + p.FreqPseudo)

	inj := float64(s.Stats.Injected)
	success := (inj + 1) / (inj + 1 + p.RejectCost*float64(s.Stats.Rejected))

	fbRaw := s.Stats.UserFeedback
	if math.IsNaN(fbRaw) || math.IsInf(fbRaw, 0) {
		fbRaw = 0 // defensive: a poisoned feedback value must not nuke the score
	}
	fb := 1 + p.FeedbackGain*fbRaw
	if fb < 0.25 {
		fb = 0.25
	}
	if fb > 2.0 {
		fb = 2.0
	}

	w := decay * freq * success * fb
	if w < minWeight || math.IsNaN(w) {
		w = minWeight
	}
	if w > maxWeight {
		w = maxWeight
	}
	return w
}

// Rescore recomputes Weight in place for every slice and reports how many
// changed. Pure over its inputs (no I/O): persistence is the caller's job —
// GC folds the new weights through CompactWith so raw vector bytes survive.
func Rescore(all []*Slice, now int64, p ScoreParams) int {
	changed := 0
	for _, s := range all {
		if s == nil {
			continue
		}
		w := ComputeWeight(s, now, p)
		if s.Weight != w {
			s.Weight = w
			changed++
		}
	}
	return changed
}
