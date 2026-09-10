from semantix_ml.gate import decide


def test_publish_when_both_metrics_improve():
    d = decide({"ndcg": 0.8, "mrr": 0.7, "n": 100}, {"ndcg": 0.7, "mrr": 0.6, "n": 100})
    assert d.publish


def test_publish_when_equal():
    # "均不低于" — equality passes (a retrained equal model refreshes recency).
    d = decide({"ndcg": 0.7, "mrr": 0.6, "n": 100}, {"ndcg": 0.7, "mrr": 0.6, "n": 100})
    assert d.publish


def test_reject_when_ndcg_regresses():
    d = decide({"ndcg": 0.69, "mrr": 0.9, "n": 100}, {"ndcg": 0.7, "mrr": 0.6, "n": 100})
    assert not d.publish
    assert "ndcg" in d.reason


def test_reject_when_mrr_regresses():
    d = decide({"ndcg": 0.9, "mrr": 0.5, "n": 100}, {"ndcg": 0.7, "mrr": 0.6, "n": 100})
    assert not d.publish
    assert "mrr" in d.reason


def test_reject_on_empty_heldout():
    # A gate with no evidence must fail closed, not wave the model through.
    d = decide({"ndcg": 0.0, "mrr": 0.0, "n": 0}, {"ndcg": 0.0, "mrr": 0.0, "n": 50})
    assert not d.publish
    assert "empty" in d.reason
