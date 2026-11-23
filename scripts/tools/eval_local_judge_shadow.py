"""Offline-only test of L3 local-judge acceptance and latency accounting.

Scores, labels, authoritative decisions, and modeled durations are synthetic.
This tool has no gateway, scorer endpoint, or cache hook. It cannot promote
or serve a cached answer.
"""

import argparse
from dataclasses import dataclass
import json
from math import ceil, isfinite
from typing import Callable


@dataclass(frozen=True)
class Pair:
    id: str
    category: str
    oracle_reusable: bool
    reference_accepted: bool
    reference_ms: int


@dataclass(frozen=True)
class Attempt:
    score: float | None
    modeled_ms: int


def latency_summary(samples: list[int]) -> dict[str, int | None]:
    ordered = sorted(samples)
    return {
        f"p{q}_ms": ordered[ceil(len(ordered) * q / 100) - 1] if ordered else None
        for q in (50, 95, 99)
    }


def assess(
    pairs: tuple[Pair, ...],
    score: Callable[[str], Attempt],
    threshold: float = 0.9,
    timeout_ms: int = 100,
) -> dict:
    if not isfinite(threshold) or threshold < 0 or threshold > 1:
        raise ValueError("threshold must be a finite number between 0 and 1")
    if timeout_ms <= 0:
        raise ValueError("timeout must be positive")

    unsafe = sum(not pair.oracle_reusable for pair in pairs)
    reference_false_accepts = sum(
        pair.reference_accepted and not pair.oracle_reusable for pair in pairs
    )
    true_accepts = false_accepts = evaluated = errors = timeouts = disagreements = 0
    by_category: dict[str, dict[str, int]] = {}
    durations = []
    for pair in pairs:
        stats = by_category.setdefault(
            pair.category, {"pairs": 0, "unsafe": 0, "evaluated": 0, "false_accepts": 0}
        )
        stats["pairs"] += 1
        stats["unsafe"] += not pair.oracle_reusable
        # The scoring stub receives the case ID, not the oracle or reference decision.
        attempt = score(pair.id)
        durations.append(min(attempt.modeled_ms, timeout_ms))
        if attempt.modeled_ms >= timeout_ms:
            timeouts += 1
            continue
        value = attempt.score
        if type(value) not in (float, int) or not isfinite(value) or not 0 <= value <= 1:
            errors += 1
            continue

        evaluated += 1
        stats["evaluated"] += 1
        accepted = value >= threshold
        disagreements += accepted != pair.reference_accepted
        if accepted:
            if pair.oracle_reusable:
                true_accepts += 1
            else:
                false_accepts += 1
                stats["false_accepts"] += 1

    approvals = true_accepts + false_accepts
    return {
        "mode": "synthetic_offline_shadow",
        "promotion_enabled": False,
        "pairs": len(pairs),
        "threshold": threshold,
        "timeout_ms": timeout_ms,
        "reference": {
            "accepted": sum(pair.reference_accepted for pair in pairs),
            "false_accepts": reference_false_accepts,
            "modeled_latency": latency_summary([pair.reference_ms for pair in pairs]),
        },
        "shadow": {
            "evaluated": evaluated,
            "errors": errors,
            "timeouts": timeouts,
            "candidate_approvals": approvals,
            "true_accepts": true_accepts,
            "false_accepts": false_accepts,
            "false_accept_rate_among_unsafe": false_accepts / unsafe if unsafe else None,
            "false_share_of_approvals": false_accepts / approvals if approvals else None,
            "disagreements_among_evaluated": disagreements,
            "modeled_latency": latency_summary(durations),
        },
        "by_category": by_category,
    }


def fixture() -> tuple[tuple[Pair, ...], Callable[[str], Attempt]]:
    pairs = (
        Pair("paraphrase", "equivalent", True, True, 128),
        Pair("negated-constraint", "negation", False, False, 170),
        Pair("old-api", "version", False, False, 140),
        Pair("minutes-vs-hours", "units", False, False, 156),
        Pair("different-repository", "scope", False, False, 162),
        Pair("changed-file-revision", "dependency", False, False, 185),
        Pair("late-score", "equivalent", True, True, 149),
        Pair("malformed-score", "schema", False, False, 147),
    )
    attempts = {
        "paraphrase": Attempt(0.96, 13),
        "negated-constraint": Attempt(0.97, 20),
        "old-api": Attempt(0.90, 24),
        "minutes-vs-hours": Attempt(0.6, 17),
        "different-repository": Attempt(0.41, 18),
        "changed-file-revision": Attempt(0.72, 36),
        "late-score": Attempt(1.0, 113),
        "malformed-score": Attempt(float("nan"), 19),
    }
    return pairs, attempts.__getitem__


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--threshold", type=float, default=0.9)
    parser.add_argument("--timeout-ms", type=int, default=100)
    args = parser.parse_args()
    rows, scorer = fixture()
    try:
        result = assess(rows, scorer, args.threshold, args.timeout_ms)
    except ValueError as err:
        parser.error(str(err))
    print(json.dumps(result, indent=2, allow_nan=False))


if __name__ == "__main__":
    main()
