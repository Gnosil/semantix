"""Release gate (spec §5): a checkpoint publishes only when NDCG@5 and
MRR@5 on the frozen held-out set are both no worse than the reference
(the current published version, or the identity-order baseline for the
first release). An empty held-out set fails closed.
"""

from dataclasses import dataclass


@dataclass
class GateDecision:
    publish: bool
    reason: str


def decide(new, reference):
    if new.get("n", 0) <= 0:
        return GateDecision(False, "empty held-out set: no evidence to publish on")
    if new["ndcg"] < reference["ndcg"]:
        return GateDecision(
            False, f"ndcg regressed: {new['ndcg']:.4f} < {reference['ndcg']:.4f}"
        )
    if new["mrr"] < reference["mrr"]:
        return GateDecision(
            False, f"mrr regressed: {new['mrr']:.4f} < {reference['mrr']:.4f}"
        )
    return GateDecision(
        True,
        f"ndcg {reference['ndcg']:.4f}→{new['ndcg']:.4f}, "
        f"mrr {reference['mrr']:.4f}→{new['mrr']:.4f} (n={new['n']})",
    )
