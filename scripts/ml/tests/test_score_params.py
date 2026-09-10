import math

from semantix_ml.score_params import DEFAULTS, clamp_step, compute_weight, fit


def slice_row(created_at, last_used=0, hits=0, injected=0, rejected=0, feedback=0.0):
    return {
        "created_at": created_at,
        "Stats": {
            "Hits": hits,
            "Injected": injected,
            "Rejected": rejected,
            "UserFeedback": feedback,
            "LastUsed": last_used,
        },
    }


def test_compute_weight_matches_go_value_function():
    # Mirror of the Go-side fixture (cmd/semantix/score_params_test.go):
    # 5 days old, Hits=2 Injected=1, half_life_days=1 → decay 2^-5 ≈ 0.031,
    # freq (3.5)/(6)≈0.583, success 1, fb 1 → weight ≈ 0.0182.
    now = 1_000_000_000
    row = slice_row(created_at=now - 5 * 86400, last_used=now - 5 * 86400, hits=2, injected=1)
    p = dict(DEFAULTS, half_life_days=1)
    w = compute_weight(row, now, p)
    want = (2 ** -5) * (3.5 / 6.0)
    assert abs(w - want) < 1e-9


def test_compute_weight_clamps_to_floor():
    now = 1_000_000_000
    row = slice_row(created_at=now - 1000 * 86400, last_used=now - 1000 * 86400)
    w = compute_weight(row, now, dict(DEFAULTS, half_life_days=1))
    assert w == 0.001  # minWeight floor


def test_compute_weight_legacy_neutral_prior():
    # No timestamps at all → decay falls back to 0.5 (Go legacy prior).
    row = slice_row(created_at=0, last_used=0)
    w = compute_weight(row, 1_000_000_000, dict(DEFAULTS))
    want = 0.5 * (0.5 / 3.0) * 1.0 * 1.0
    assert abs(w - want) < 1e-12


def test_compute_weight_rejection_cost():
    now = 1_000_000_000
    row = slice_row(created_at=now, last_used=now, injected=1, rejected=1)
    w = compute_weight(row, now, dict(DEFAULTS))
    # success = (1+1)/(1+1+2*1) = 0.5
    freq = (1 + 0.5) / (1 + 3)
    assert abs(w - 1.0 * freq * 0.5 * 1.0) < 1e-12


def test_clamp_step_limits_relative_change():
    cur = dict(DEFAULTS)
    proposal = dict(DEFAULTS, half_life_days=300, freq_pseudo=0.1)
    out = clamp_step(proposal, cur, max_rel_step=0.2)
    assert abs(out["half_life_days"] - 36.0) < 1e-9  # 30 * 1.2
    assert abs(out["freq_pseudo"] - 2.4) < 1e-9  # 3 * 0.8
    assert out["reject_cost"] == cur["reject_cost"]  # untouched key passes through


def test_clamp_step_within_band_passes_verbatim():
    cur = dict(DEFAULTS)
    proposal = dict(DEFAULTS, half_life_days=33)
    assert clamp_step(proposal, cur, max_rel_step=0.2)["half_life_days"] == 33


def test_fit_moves_half_life_toward_signal_and_respects_clamp():
    # decay is monotone in age, so a pure-recency label cannot prefer any
    # half-life over another. The signal needs a conflict: fresh-but-unused
    # slices that DO get reused vs old-but-heavily-used ones that don't.
    # Under the default 30-day half-life the frequency term wins (wrong
    # order, AUC 0); a much shorter half-life flips it (AUC 1). fit() must
    # therefore move half_life_days down — but only to the 20% step floor.
    now = 1_000_000_000
    rows, labels = {}, {}
    for i in range(10):
        rows[f"fresh{i}"] = slice_row(
            created_at=now - 2 * 86400, last_used=now - 2 * 86400
        )
        labels[f"fresh{i}"] = 1
        rows[f"stale{i}"] = slice_row(
            created_at=now - 40 * 86400, last_used=now - 40 * 86400,
            hits=8, injected=2,
        )
        labels[f"stale{i}"] = 0
    fitted = fit(rows, labels, now, dict(DEFAULTS), max_rel_step=0.2)
    assert abs(fitted["half_life_days"] - 24.0) < 1e-9  # 30 * 0.8 step floor
    assert set(fitted) == set(DEFAULTS)


def test_fit_no_signal_keeps_params():
    now = 1_000_000_000
    rows = {f"s{i}": slice_row(created_at=now - 86400, last_used=now - 86400) for i in range(4)}
    labels = {rid: 1 for rid in rows}  # constant labels → nothing to rank
    fitted = fit(rows, labels, now, dict(DEFAULTS), max_rel_step=0.2)
    assert fitted == dict(DEFAULTS)
