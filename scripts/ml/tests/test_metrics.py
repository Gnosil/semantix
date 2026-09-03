import math

from semantix_ml.metrics import evaluate_ranking, mrr_at_k, ndcg_at_k


def test_ndcg_perfect_ranking_is_one():
    assert ndcg_at_k(["a", "b", "c"], {"a"}, 5) == 1.0


def test_ndcg_relevant_at_second_position():
    # DCG = 1/log2(3), IDCG = 1/log2(2) = 1
    want = 1.0 / math.log2(3)
    got = ndcg_at_k(["x", "a", "y"], {"a"}, 5)
    assert abs(got - want) < 1e-12


def test_ndcg_no_relevant_is_zero():
    assert ndcg_at_k(["x", "y"], {"a"}, 5) == 0.0


def test_ndcg_empty_relevant_set_is_zero():
    assert ndcg_at_k(["x", "y"], set(), 5) == 0.0


def test_ndcg_truncates_at_k():
    # Relevant appears beyond k → contributes nothing.
    assert ndcg_at_k(["x", "y", "z", "a"], {"a"}, 3) == 0.0


def test_ndcg_two_relevant_reversed_order():
    # Ranking [b?, a✓, c✓] with two relevant: DCG = 1/log2(3) + 1/log2(4);
    # IDCG = 1 + 1/log2(3).
    want = (1 / math.log2(3) + 1 / math.log2(4)) / (1 + 1 / math.log2(3))
    got = ndcg_at_k(["b", "a", "c"], {"a", "c"}, 5)
    assert abs(got - want) < 1e-12


def test_mrr_first_relevant_at_third():
    assert mrr_at_k(["x", "y", "a"], {"a"}, 5) == 1.0 / 3


def test_mrr_beyond_k_is_zero():
    assert mrr_at_k(["x", "y", "a"], {"a"}, 2) == 0.0


def test_evaluate_ranking_averages():
    rankings = [
        (["a", "x"], {"a"}),  # ndcg 1, mrr 1
        (["x", "a"], {"a"}),  # ndcg 1/log2(3), mrr 1/2
    ]
    out = evaluate_ranking(rankings, k=5)
    assert out["n"] == 2
    assert abs(out["mrr"] - 0.75) < 1e-12
    assert abs(out["ndcg"] - (1 + 1 / math.log2(3)) / 2) < 1e-12


def test_evaluate_ranking_empty():
    out = evaluate_ranking([], k=5)
    assert out == {"ndcg": 0.0, "mrr": 0.0, "n": 0}
