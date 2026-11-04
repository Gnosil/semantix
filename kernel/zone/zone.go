// Package zone implements the grey-zone three-region classifier proposed by
// Krites (arXiv:2602.13165) and vCache (arXiv:2502.03771): a single
// similarity threshold cannot separate "rephrased, same intent" from
// "different intent" — the distributions overlap in the middle. We split the
// similarity axis into clear-hit / grey / clear-miss with two thresholds and
// treat the grey zone as "verify before reuse" instead of deciding blindly.
package zone

import (
	"math"

	"semantix/kernel/slice"
)

// Zone is the three-region verdict for a retrieval candidate.
type Zone int

const (
	// Miss: clearly not reusable (do not reuse, call backend).
	Miss Zone = iota
	// Grey: ambiguous — verify (LLM judge, Issue #8) before reuse; the
	// injector must not place grey slices into the prompt without verdict.
	Grey
	// Hit: clearly reusable.
	Hit
)

// String renders the zone as a stable lowercase tag for JSON/CLI output.
func (z Zone) String() string {
	switch z {
	case Hit:
		return "hit"
	case Grey:
		return "grey"
	default:
		return "miss"
	}
}

// Zones combines relative confidence (score/top1) with absolute score floors.
// Raw BM25 scores are unbounded: L2 callers use ClassifyHit to additionally
// require query-token coverage independent of the batch's best score.
type Zones struct {
	TauHigh float64 // relative confidence >= TauHigh -> Hit
	TauLow  float64 // relative confidence >= TauLow  -> Grey
	AbsHigh float64 // absolute score floor for Hit (top1 >= AbsHigh)
	AbsLow  float64 // absolute score floor for Grey (top1 >= AbsLow)
	// ByType holds per-slice-type overrides (Issue #259 阶段 2), keyed by
	// the stable wire name of slice.SliceType (prompt|context|
	// tool_pattern|result|memory — see slice.Type.String). Each override
	// is a complete four-threshold snapshot; the global fields above act
	// as the baseline for types without an override. nil = no per-type
	// differentiation (the historical behavior).
	ByType map[string]Zones
}

// ForType returns the effective thresholds for a slice type: the per-type
// override when present, otherwise the global baseline itself (the
// receiver, so a zero ByType costs nothing and behavior is unchanged).
// The returned override's own ByType is cleared: overrides are leaf
// snapshots, never nested.
func (z Zones) ForType(typeName string) Zones {
	if z.ByType != nil {
		if o, ok := z.ByType[typeName]; ok {
			o.ByType = nil
			return o
		}
	}
	return z
}

// Default returns the tuned default thresholds.
func Default() Zones {
	return Zones{TauHigh: 0.8, TauLow: 0.55, AbsHigh: 0.7, AbsLow: 0.45}
}

// Classify maps (score, top1) to a Zone. NaN/Inf/zero/negative inputs are
// always Miss (failure-safe: never a false reuse).
func (z Zones) Classify(score, top1 float64) Zone {
	if math.IsNaN(score) || math.IsNaN(top1) || math.IsInf(score, 0) || math.IsInf(top1, 0) ||
		top1 <= 0 || score <= 0 {
		return Miss
	}
	switch {
	case top1 < z.AbsLow:
		return Miss
	case score/top1 >= z.TauHigh && top1 >= z.AbsHigh:
		return Hit
	case score/top1 >= z.TauLow && top1 >= z.AbsLow:
		return Grey
	default:
		return Miss
	}
}

// ClassifyHit applies the L2 verdict to a retrieval candidate. Native BM25
// hits must also meet TauHigh/TauLow on query-token coverage: a large raw
// score or rank-one position alone cannot certify relevance (Issue #537).
// Raw-score absolute floors still apply, including corpus-calibrated values.
// Setting both absolute floors to zero explicitly selects relative-only
// classification, as used by the harness's reference-history admission policy.
// Vector/fused and legacy hits without coverage retain Classify's behavior.
func (z Zones) ClassifyHit(hit slice.Hit, top1 float64) Zone {
	verdict := z.Classify(hit.Score, top1)
	if !hit.QueryCoverageValid || (z.AbsHigh == 0 && z.AbsLow == 0) {
		return verdict
	}
	coverage := hit.QueryCoverage
	switch {
	case math.IsNaN(coverage) || math.IsInf(coverage, 0) || coverage <= 0 || coverage > 1:
		return Miss
	case coverage < z.TauLow:
		return Miss
	case coverage < z.TauHigh && verdict == Hit:
		return Grey
	default:
		return verdict
	}
}

// ClassifyL3 is the two-axis verdict for the L3 reuse path (Issue #241).
//
// The single-axis Classify assumes one denominator plays both roles at
// once: "how prominent is this candidate among its peers" (normalization)
// and "how relevant is this whole batch" (absolute anchor). In the gateway
// the store always holds a Prompt slice byte-identical to the query, which
// BM25 ranks first by a wide margin; measuring every Result against it
// (Classify's top1) depresses the relative confidence of every reusable
// candidate for a reason unrelated to reusability — GW4 measured 8 of 10
// repeated tasks landing in grey and the hit rate hinged on whether a
// judge was configured. But simply scoping the denominator to Result
// candidates (the issue's option 1) throws the batch-relevance anchor
// away: the best Result then has ratio 1.0 unconditionally and the
// relative axis stops discriminating (Allenllii's review on the issue).
//
// ClassifyL3 splits the two roles into an explicit three-argument verdict:
//
//   - resultTop1  (relative axis): max score among the Result candidates
//     L3 actually considers; relSame = score/resultTop1 measures prominence
//     within that set.
//   - globalTop1  (scale anchor): max score over the raw hit list (which
//     the prompt twin usually leads). relGlobal = score/globalTop1 is the
//     scale-normalized relevance floor — a Result far below the batch's
//     best match is not "the best of a bad batch".
//   - absolute floors still bind on globalTop1 (AbsLow/AbsHigh), exactly
//     as Classify binds them on top1: on a bounded (cosine) scale they
//     guard reuse outright, while BM25 scores (typically >> 1) never trip
//     them.
//
// The relevance floor reuses AbsLow (0.45), calibrated on real BM25
// behavior: byte-identical re-asks and rewritten same-task answers measure
// 0.52–0.83 against their twin (GW4 acceptance §5.2: 0.525–0.826), while
// same-field-but-different-task documents sit near 0.16 and unrelated ones
// at 0 — AbsLow sits in the wide gap between the two clusters.
//
// Hit requires prominence (relSame >= TauHigh) AND batch relevance
// (relGlobal >= AbsLow); a weaker but still relevant peer falls to Grey
// (judge-gated downstream); anything below either floor is Miss
// (fail-closed). NaN/Inf/zero/negative inputs are always Miss.
func (z Zones) ClassifyL3(score, resultTop1, globalTop1 float64) Zone {
	if math.IsNaN(score) || math.IsNaN(resultTop1) || math.IsNaN(globalTop1) ||
		math.IsInf(score, 0) || math.IsInf(resultTop1, 0) || math.IsInf(globalTop1, 0) ||
		score <= 0 || resultTop1 <= 0 || globalTop1 <= 0 {
		return Miss
	}
	switch {
	case globalTop1 < z.AbsLow:
		return Miss
	case score/resultTop1 >= z.TauHigh && score/globalTop1 >= z.AbsLow && globalTop1 >= z.AbsHigh:
		return Hit
	case score/resultTop1 >= z.TauLow && score/globalTop1 >= z.AbsLow && globalTop1 >= z.AbsLow:
		return Grey
	default:
		return Miss
	}
}
