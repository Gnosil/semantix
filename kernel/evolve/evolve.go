package evolve

import (
	"errors"
	"math"
	"sync"
)

// Default tuning constants (MVP, U15). Freeze window protects the vendor
// byte-level prefix cache: a changed injection set invalidates the cached
// prefix for ~24h (DeepSeek policy) — we never change params inside the
// window (Reasonix lesson, cache_policy.go:21-45).
const (
	DefaultAlpha       = 0.1  // EWMA smoothing (10% weight on newest sample)
	DefaultTauL2       = 0.55 // relative-confidence threshold for L2 hit
	DefaultInjectCap   = 0.3  // max injected budget as fraction of context
	DefaultFreezeEpoch = 60   // epochs a param change stays frozen
	DefaultMinTau      = 0.30 // floor for tau tuning (never below)
	DefaultMaxTau      = 0.80 // ceiling for tau tuning (never above)
	TauStep            = 0.05 // per adjustment step
	PollutionRiseAt    = 0.30 // pollution EWMA above this → tighten tau
	HitTarget          = 0.70 // hit EWMA above this (and pollution low) → relax tau
	PollutionLow       = 0.10 // pollution below this to allow relaxation
	MinSamples         = 20   // signals required before the first adjustment
)

// Config carries operator-provided tuning constants (zero values → defaults).
type Config struct {
	Alpha        float64
	TauL2        float64
	InjectCap    float64
	FreezeEpochs uint64
	MinTau       float64
	MaxTau       float64
	MinSamples   uint64
}

// Signal is one feedback sample consumed by the evolution engine.
type Signal struct {
	Name  string // "cache_hit" | "success" (hit EWMA) | "inject_pollution" | "prefetch_waste" (pollution EWMA); unknown names are ignored
	Value float64
	Epoch uint64
}

// Params is the tunable parameter snapshot (frozen semantics: injected set
// must not change within the freeze window — see architecture spec §6.2).
type Params struct {
	TauL2        float64
	TauL3        float64
	InjectCap    float64
	PrefetchConf float64
	FreezeEpochs uint64 // injection-set freeze duration
}

// Engine runs online EWMA tuning and offline retraining (MVP in M1).
type Engine interface {
	RecordSignal(s Signal) error
	Params() Params
	Apply(p Params) error
}

// ewmaEngine is the MVP Engine: it folds feedback signals into EWMA
// estimates of hit rate and pollution rate, and adjusts TauL2 in small
// steps inside a freeze window (epoch-based).
type ewmaEngine struct {
	mu            sync.Mutex
	alpha         float64
	minTau        float64
	maxTau        float64
	minSamples    uint64
	epoch         uint64
	freezeUntil   uint64 // freeze opened by Apply or an auto-adjustment
	firstAdjustAt uint64 // cold-start observation window (no auto-adjust before this epoch)
	params        Params

	hitEWMA     float64
	polEWMA     float64
	sampleCount uint64
	adjustments uint64
}

// New returns a ready MVP engine with default tuning constants.
func New(cfg Config) *ewmaEngine {
	e := &ewmaEngine{
		alpha:      cfg.Alpha,
		minTau:     cfg.MinTau,
		maxTau:     cfg.MaxTau,
		minSamples: cfg.MinSamples,
		params: Params{
			TauL2:        DefaultTauL2,
			InjectCap:    DefaultInjectCap,
			PrefetchConf: 0.5,
			FreezeEpochs: DefaultFreezeEpoch,
		},
	}
	if e.alpha <= 0 || math.IsNaN(e.alpha) || math.IsInf(e.alpha, 0) {
		e.alpha = DefaultAlpha
	}
	if e.minTau <= 0 || math.IsNaN(e.minTau) || math.IsInf(e.minTau, 0) {
		e.minTau = DefaultMinTau
	}
	if e.maxTau <= 0 || math.IsNaN(e.maxTau) || math.IsInf(e.maxTau, 0) {
		e.maxTau = DefaultMaxTau
	}
	if e.minSamples == 0 {
		e.minSamples = MinSamples
	}
	if cfg.TauL2 > 0 && finite(cfg.TauL2) {
		e.params.TauL2 = cfg.TauL2
	}
	if cfg.InjectCap > 0 && finite(cfg.InjectCap) {
		e.params.InjectCap = cfg.InjectCap
	}
	if cfg.FreezeEpochs > 0 {
		e.params.FreezeEpochs = cfg.FreezeEpochs
	}
	// Cold-start observation window: no auto-adjustment before
	// FreezeEpochs signals (protects the vendor byte cache from churn
	// while the EWMA is warming up). Explicit operator Apply is NOT
	// blocked by this window — only by a freeze opened by a prior
	// Apply/adjustment.
	e.firstAdjustAt = e.params.FreezeEpochs
	return e
}

// RecordSignal folds one feedback sample into the EWMA state and, once the
// sample count and freeze window allow it, nudges TauL2 in the direction the
// signals indicate. Epoch is the caller's round counter; a signal with a
// lower epoch than the previous one is rejected (out-of-order guard).
func (e *ewmaEngine) RecordSignal(s Signal) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Reject NaN/Inf before any state change: comparisons are false for NaN,
	// so clamping would be bypassed and the EWMA would silently poison itself.
	if math.IsNaN(s.Value) || math.IsInf(s.Value, 0) {
		return errors.New("evolve: non-finite signal value rejected")
	}
	if s.Epoch < e.epoch {
		return errors.New("evolve: out-of-order signal epoch")
	}
	if s.Epoch > e.epoch {
		e.epoch = s.Epoch
	}
	e.sampleCount++
	// Clamp signal value to [0,1] so a corrupt source cannot skew the EWMA.
	v := s.Value
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	switch s.Name {
	case "cache_hit", "success":
		e.hitEWMA = e.alpha*v + (1-e.alpha)*e.hitEWMA
	case "inject_pollution", "prefetch_waste":
		e.polEWMA = e.alpha*v + (1-e.alpha)*e.polEWMA
	default:
		// Unknown signal names are tolerated but ignored (forward compat).
		return nil
	}
	e.maybeAdjustLocked()
	return nil
}

// Params returns a defensive copy of the current parameter snapshot.
func (e *ewmaEngine) Params() Params {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.params
}

// Apply installs operator-provided parameters. Changes inside the freeze
// window are rejected (fail-closed) so the vendor prefix cache is not
// invalidated by churn; the operator can force by bumping FreezeEpochs to 0
// in the applied snapshot (explicit override, not silent).
func (e *ewmaEngine) Apply(p Params) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	// NaN/Inf rejected before the freeze check: a frozen engine must not
	// mask the real reason with a confusing "frozen" error.
	if !finite(p.TauL2) || !finite(p.InjectCap) || !finite(p.PrefetchConf) {
		return errors.New("evolve: non-finite param rejected")
	}
	if e.epoch < e.freezeUntil && p.FreezeEpochs != 0 {
		return errors.New("evolve: params frozen until epoch " + itoa(e.freezeUntil))
	}
	if p.TauL2 < e.minTau {
		p.TauL2 = e.minTau
	}
	if p.TauL2 > e.maxTau {
		p.TauL2 = e.maxTau
	}
	e.params = p
	e.freezeUntil = e.epoch + p.FreezeEpochs
	return nil
}

// maybeAdjustLocked nudges TauL2 when enough samples exist and the freeze
// window is open. Direction rule (MVP):
//   - pollution high → tighten (injected content is hurting)
//   - hits high and pollution low → relax (reuse is safe, be more generous)
//   - otherwise stay put
func (e *ewmaEngine) maybeAdjustLocked() {
	if e.sampleCount < e.minSamples {
		return
	}
	if e.epoch < e.firstAdjustAt {
		return
	}
	if e.epoch < e.freezeUntil {
		return
	}
	old := e.params.TauL2
	// TauL2 is a relative-confidence floor (zone.TauLow semantics): raising
	// it admits fewer slices (tighten), lowering it admits more (relax).
	switch {
	case e.polEWMA >= PollutionRiseAt:
		e.params.TauL2 = e.params.TauL2 + TauStep // tighten: raise the floor
	case e.hitEWMA >= HitTarget && e.polEWMA <= PollutionLow:
		e.params.TauL2 = e.params.TauL2 - TauStep // relax: admit more reuse
	default:
		return
	}
	if e.params.TauL2 < e.minTau {
		e.params.TauL2 = e.minTau
	}
	if e.params.TauL2 > e.maxTau {
		e.params.TauL2 = e.maxTau
	}
	if e.params.TauL2 != old {
		e.adjustments++
		// Freeze after every real change: protects the byte cache AND gives
		// the new threshold a full window to accumulate evidence.
		e.freezeUntil = e.epoch + e.params.FreezeEpochs
	}
}

// Adjustments reports how many times the engine actually changed TauL2
// (observability for tests and the future cost dashboard).
func (e *ewmaEngine) Adjustments() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.adjustments
}

// Epoch returns the last observed epoch (observability).
func (e *ewmaEngine) Epoch() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.epoch
}

// finite reports whether x is neither NaN nor ±Inf (helper for config and
// Apply validation).
func finite(x float64) bool {
	return !math.IsNaN(x) && !math.IsInf(x, 0)
}

// itoa is a tiny integer formatter avoiding strconv for a hot error path.
func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
