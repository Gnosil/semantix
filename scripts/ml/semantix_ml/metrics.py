"""Ranking metrics for the retrieval held-out gate (spec §5).

Binary relevance only: a candidate is either a known-good document for the
query or it is not. NDCG uses the standard log2 discount; both metrics
truncate at k.
"""

import math


def ndcg_at_k(ranked_ids, relevant, k):
    if not relevant:
        return 0.0
    dcg = 0.0
    for i, rid in enumerate(ranked_ids[:k]):
        if rid in relevant:
            dcg += 1.0 / math.log2(i + 2)
    ideal_hits = min(len(relevant), k)
    idcg = sum(1.0 / math.log2(i + 2) for i in range(ideal_hits))
    if idcg == 0.0:
        return 0.0
    return dcg / idcg


def mrr_at_k(ranked_ids, relevant, k):
    for i, rid in enumerate(ranked_ids[:k]):
        if rid in relevant:
            return 1.0 / (i + 1)
    return 0.0


def evaluate_ranking(rankings, k):
    """rankings: iterable of (ranked_ids, relevant_set). Returns mean
    metrics plus the sample count — n rides along so the gate can fail
    closed on an empty held-out set."""
    n = 0
    ndcg_sum = 0.0
    mrr_sum = 0.0
    for ranked_ids, relevant in rankings:
        n += 1
        ndcg_sum += ndcg_at_k(ranked_ids, relevant, k)
        mrr_sum += mrr_at_k(ranked_ids, relevant, k)
    if n == 0:
        return {"ndcg": 0.0, "mrr": 0.0, "n": 0}
    return {"ndcg": ndcg_sum / n, "mrr": mrr_sum / n, "n": n}
