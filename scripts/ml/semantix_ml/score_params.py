"""ScoreParams replay fitting (spec §3 G3, §7 I-3).

compute_weight mirrors kernel/slice/score.go ComputeWeight exactly (same
formula, same clamps) so a fitted parameter set ranks slices the same way
the Go gc will. fit() does a small coordinate grid search maximizing the
rank correlation between weight and the observed "reused later" label,
then clamps every hyperparameter to at most ±max_rel_step relative change
per round — the I-3 delta-only invariant lives HERE, not in the caller.
"""

import math

DEFAULTS = {
    "half_life_days": 30.0,
    "grace_days": 7.0,
    "freq_pseudo": 3.0,
    "reject_cost": 2.0,
    "feedback_gain": 0.5,
}

MIN_WEIGHT = 0.001
MAX_WEIGHT = 1.0


def compute_weight(slice_row, now, params):
    """Python mirror of Go ComputeWeight (kernel/slice/score.go)."""
    stats = slice_row.get("Stats") or {}
    created = int(slice_row.get("created_at") or 0)
    last_used = int(stats.get("LastUsed") or 0)

    active = max(last_used, created)
    decay = 0.5  # legacy neutral prior
    if active > 0:
        age = max(0.0, float(now - active))
        decay = math.exp(-math.log(2) * age / (params["half_life_days"] * 86400))

    use = float((stats.get("Hits") or 0) + (stats.get("Injected") or 0))
    freq = (use + 0.5) / (use + params["freq_pseudo"])

    inj = float(stats.get("Injected") or 0)
    rejected = float(stats.get("Rejected") or 0)
    success = (inj + 1) / (inj + 1 + params["reject_cost"] * rejected)

    fb_raw = float(stats.get("UserFeedback") or 0)
    if math.isnan(fb_raw) or math.isinf(fb_raw):
        fb_raw = 0.0
    fb = 1 + params["feedback_gain"] * fb_raw
    fb = min(2.0, max(0.25, fb))

    w = decay * freq * success * fb
    if w < MIN_WEIGHT or math.isnan(w):
        w = MIN_WEIGHT
    return min(w, MAX_WEIGHT)


def clamp_step(proposal, current, max_rel_step=0.2):
    """I-3: each hyperparameter moves at most ±max_rel_step relative to the
    currently deployed value in one round."""
    out = {}
    for key, cur in current.items():
        prop = proposal.get(key, cur)
        lo, hi = cur * (1 - max_rel_step), cur * (1 + max_rel_step)
        out[key] = min(hi, max(lo, prop))
    return out


def _rank_auc(rows, labels, now, params):
    """Concordance (AUC) between weight order and binary reuse labels —
    scale-free, so it measures ranking quality, not calibration."""
    scored = [(compute_weight(rows[rid], now, params), labels[rid]) for rid in rows]
    pos = [s for s, y in scored if y == 1]
    neg = [s for s, y in scored if y == 0]
    if not pos or not neg:
        return None
    wins = 0.0
    for p in pos:
        for n in neg:
            if p > n:
                wins += 1
            elif p == n:
                wins += 0.5
    return wins / (len(pos) * len(neg))


# Per-key multiplicative probe grid: one coordinate pass, candidates chosen
# wide (×/÷ up to 4) so the search sees past the clamp band; the clamp then
# pulls the winner back inside ±max_rel_step (the fitter proposes, I-3
# disposes).
_PROBE_FACTORS = [0.25, 0.5, 0.8, 1.0, 1.25, 2.0, 4.0]


def fit(rows, labels, now, current_params, max_rel_step=0.2):
    """One coordinate-descent round over the probe grid, then clamp. Keys
    with no ranking signal (single-class labels) return current_params
    verbatim."""
    base = dict(current_params)
    if _rank_auc(rows, labels, now, base) is None:
        return base

    fitted = dict(base)
    for key in ("half_life_days", "freq_pseudo", "reject_cost", "feedback_gain"):
        best_val, best_auc = fitted[key], -1.0
        for factor in _PROBE_FACTORS:
            candidate = dict(fitted)
            candidate[key] = base[key] * factor
            auc = _rank_auc(rows, labels, now, candidate)
            if auc is None:
                continue
            # Strict improvement keeps ties on the incumbent value, so a
            # flat objective never wanders the parameters.
            if auc > best_auc + 1e-12 or (
                abs(auc - best_auc) <= 1e-12 and factor == 1.0
            ):
                best_auc, best_val = auc, candidate[key]
        fitted[key] = best_val
    # grace_days shapes eviction ordering only (not the weight value), so
    # replay fitting has no signal for it — carry it through.
    fitted["grace_days"] = base["grace_days"]
    return clamp_step(fitted, base, max_rel_step)
