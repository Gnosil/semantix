package agent

import "time"

type prefetchGateDecision struct {
	Turn         int64
	Allow        bool
	Reason       string
	ProbeTargets []string
}

type prefetchedInjectResult struct {
	Text    string
	Targets []string
	Turn    int64
	// WarmAt is when the speculative warm-up finished; the timeliness
	// (Markov lead time) is measured from here to the outcome decision
	// (Issue #272). Zero value means the producer did not stamp it and the
	// lead is reported as 0.
	WarmAt time.Time
	// ProbeTargets carries the scheduler candidates admitted for exploration.
	// Targets above remain the canonical injected slice identities.
	ProbeTargets []string
}

func (a *Agent) armPrefetch(reason string, probeTargets []string) {
	if a == nil || reason == "" {
		if a != nil {
			a.prefetchGate.Store(nil)
		}
		return
	}
	allow := true
	switch reason {
	case "load_saturated", "window_too_short", "budget:halt_prefetch", "budget:hard_stop":
		allow = false
	}
	a.prefetchGate.Store(&prefetchGateDecision{
		Turn:         a.semantixTurn.Load(),
		Allow:        allow,
		Reason:       reason,
		ProbeTargets: append([]string(nil), probeTargets...),
	})
}

func (a *Agent) prefetchAllowed() bool {
	if a == nil {
		return false
	}
	decision := a.prefetchGate.Load()
	return decision == nil || decision.Turn != a.semantixTurn.Load() || decision.Allow
}

func (a *Agent) prefetchProbeTargets() []string {
	if a == nil {
		return nil
	}
	decision := a.prefetchGate.Load()
	if decision == nil || decision.Turn != a.semantixTurn.Load() || !decision.Allow {
		return nil
	}
	return append([]string(nil), decision.ProbeTargets...)
}

func (a *Agent) storePrefetch(next *prefetchedInjectResult) {
	if next == nil || next.Text == "" {
		return
	}
	for {
		old := a.prefetchedInject.Load()
		if next.Turn != a.semantixTurn.Load() {
			a.recordPrefetch(false, next)
			return
		}
		// Read the cached pointer before checking the turn. If the new turn
		// has stored history meanwhile, CAS retries instead of evicting it.
		if a.prefetchedInject.CompareAndSwap(old, next) {
			if old != nil {
				a.recordPrefetch(false, old)
			}
			return
		}
	}
}

func (a *Agent) takePrefetch(turn int64) *prefetchedInjectResult {
	got := a.prefetchedInject.Swap(nil)
	if got == nil {
		return nil
	}
	if got.Turn != turn {
		a.recordPrefetch(false, got)
		return nil
	}
	a.recordPrefetch(true, got)
	return got
}

func (a *Agent) wastePrefetch() {
	if got := a.prefetchedInject.Swap(nil); got != nil {
		a.recordPrefetch(false, got)
	}
}

func (a *Agent) recordPrefetch(hit bool, got *prefetchedInjectResult) {
	if a != nil && a.semantix != nil && got != nil {
		lead := time.Duration(0)
		if !got.WarmAt.IsZero() {
			lead = time.Since(got.WarmAt)
		}
		a.semantix.RecordPrefetch(hit, got.Targets, got.ProbeTargets, int(got.Turn), lead)
	}
}
