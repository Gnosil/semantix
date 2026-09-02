import json

import pytest

from semantix_ml.registry import current_version, list_versions, publish, rollback


def mk_checkpoint(tmp_path, name):
    d = tmp_path / "staging" / name
    d.mkdir(parents=True)
    (d / "model.onnx").write_bytes(b"fake-onnx")
    return d


def test_publish_assigns_versions_and_moves_current(tmp_path):
    train = tmp_path / "train"
    v1 = publish(train, mk_checkpoint(tmp_path, "a"), {"ndcg": 0.5, "mrr": 0.4, "n": 10})
    v2 = publish(train, mk_checkpoint(tmp_path, "b"), {"ndcg": 0.6, "mrr": 0.5, "n": 10})
    assert v1 == "v0001" and v2 == "v0002"
    assert current_version(train) == "v0002"
    assert (train / "current" / "model.onnx").read_bytes() == b"fake-onnx"
    metrics = json.loads((train / "checkpoints" / "v0002" / "metrics.json").read_text())
    assert metrics["ndcg"] == 0.6
    assert list_versions(train) == ["v0001", "v0002"]


def test_rollback_to_previous_and_explicit(tmp_path):
    train = tmp_path / "train"
    publish(train, mk_checkpoint(tmp_path, "a"), {"ndcg": 0.5, "mrr": 0.4, "n": 10})
    publish(train, mk_checkpoint(tmp_path, "b"), {"ndcg": 0.6, "mrr": 0.5, "n": 10})
    assert rollback(train) == "v0001"  # no arg → previous version
    assert current_version(train) == "v0001"
    assert rollback(train, "v0002") == "v0002"
    assert current_version(train) == "v0002"


def test_rollback_without_history_raises(tmp_path):
    train = tmp_path / "train"
    with pytest.raises(ValueError):
        rollback(train)
    publish(train, mk_checkpoint(tmp_path, "a"), {"ndcg": 0.5, "mrr": 0.4, "n": 10})
    with pytest.raises(ValueError):
        rollback(train)  # only one version — nothing earlier to roll to


def test_current_version_none_before_first_publish(tmp_path):
    assert current_version(tmp_path / "train") is None
