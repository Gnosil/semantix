import base64
import json

from semantix_ml.dataset import (
    Pair,
    load_slices,
    pairs_from_events,
    pairs_from_synthetic,
    time_split,
)


def b64(s: str) -> str:
    return base64.b64encode(s.encode()).decode()


def write_jsonl(path, rows):
    with open(path, "w") as f:
        for r in rows:
            f.write(json.dumps(r) + "\n")


def slices_fixture(tmp_path):
    path = tmp_path / "slices.jsonl"
    write_jsonl(
        path,
        [
            {"ID": "s1", "Type": 0, "Scope": 1, "Content": b64("alpha doc"), "Weight": 0.5},
            {"ID": "s2", "Type": 0, "Scope": 1, "Content": b64("bravo doc"), "Weight": 0.5},
            {"ID": "s3", "Type": 0, "Scope": 1, "Content": b64("charlie doc"), "Weight": 0.5},
        ],
    )
    return path


def test_load_slices_decodes_content(tmp_path):
    slices = load_slices(slices_fixture(tmp_path))
    assert slices["s1"]["text"] == "alpha doc"
    assert set(slices) == {"s1", "s2", "s3"}


def test_pairs_from_events_labels_by_reason(tmp_path):
    slices = load_slices(slices_fixture(tmp_path))
    events = tmp_path / "events.jsonl"
    write_jsonl(
        events,
        [
            {
                "at": 100,
                "session": "x",
                "query": "alpha",
                "decisions": [
                    {"id": "s1", "score": 0.9, "zone": "hit", "admitted": True, "reason": "admitted"},
                    {"id": "s2", "score": 0.1, "zone": "miss", "reason": "zone_miss"},
                    # Governance rejections are NOT relevance negatives:
                    {"id": "s3", "score": 0.8, "zone": "hit", "reason": "budget"},
                ],
            }
        ],
    )
    pairs = pairs_from_events(events, slices)
    by_id = {(p.slice_id, p.label): p for p in pairs}
    assert ("s1", 1) in by_id  # admitted → positive
    assert ("s2", 0) in by_id  # zone_miss → hard negative
    assert not any(p.slice_id == "s3" for p in pairs)  # budget → excluded
    p = by_id[("s1", 1)]
    assert p.query == "alpha"
    assert p.text == "alpha doc"
    assert p.source == "events"
    assert p.at == 100


def test_pairs_from_events_skips_unknown_ids(tmp_path):
    slices = load_slices(slices_fixture(tmp_path))
    events = tmp_path / "events.jsonl"
    write_jsonl(
        events,
        [
            {
                "at": 1,
                "query": "q",
                "decisions": [
                    {"id": "gone", "score": 0.9, "zone": "hit", "admitted": True, "reason": "admitted"}
                ],
            }
        ],
    )
    assert pairs_from_events(events, slices) == []


def test_pairs_from_synthetic_adds_sampled_negatives(tmp_path):
    slices = load_slices(slices_fixture(tmp_path))
    synth = tmp_path / "synth.jsonl"
    write_jsonl(synth, [{"query": "find alpha", "slice_id": "s1"}])
    pairs = pairs_from_synthetic(synth, slices, n_neg=2, seed=7)
    pos = [p for p in pairs if p.label == 1]
    neg = [p for p in pairs if p.label == 0]
    assert len(pos) == 1 and pos[0].slice_id == "s1"
    assert len(neg) == 2
    assert all(p.slice_id != "s1" for p in neg)  # negatives never the source
    assert all(p.source == "synthetic" for p in pairs)
    # Deterministic under the same seed:
    again = pairs_from_synthetic(synth, slices, n_neg=2, seed=7)
    assert [(p.slice_id, p.label) for p in again] == [(p.slice_id, p.label) for p in pairs]


def test_time_split_holds_out_late_events():
    mk = lambda i, src: Pair(query=f"q{i}", slice_id=f"s{i}", text="t", label=1, source=src, at=i)
    events = [mk(i, "events") for i in range(10)]
    train, heldout = time_split(events, holdout_frac=0.2)
    assert len(heldout) == 2
    assert {p.at for p in heldout} == {8, 9}  # strictly the latest slice of time
    assert {p.at for p in train} == set(range(8))


def test_time_split_synthetic_stratified_stable():
    mk = lambda i: Pair(query=f"q{i}", slice_id=f"s{i}", text="t", label=1, source="synthetic", at=0)
    synth = [mk(i) for i in range(100)]
    train1, held1 = time_split(synth, holdout_frac=0.2, synth_holdout_frac=0.1)
    train2, held2 = time_split(synth, holdout_frac=0.2, synth_holdout_frac=0.1)
    assert {p.query for p in held1} == {p.query for p in held2}  # hash-stable
    assert 5 <= len(held1) <= 15  # ~10%
