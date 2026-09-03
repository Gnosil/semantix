import json

from semantix_ml.compare import arm_summary, render_comparison


def write_jsonl(path, rows):
    with open(path, "w") as f:
        for r in rows:
            f.write(json.dumps(r) + "\n")


def arm_fixture(tmp_path, name, *, injected, rejected, tokens_in):
    d = tmp_path / name
    d.mkdir()
    write_jsonl(
        d / "usage.jsonl",
        [
            {"tokens_in": tokens_in, "tokens_out": 50, "injected_tokens": 20, "slice_hits": 1, "at": 1},
            {"tokens_in": 100, "tokens_out": 10, "injected_tokens": 0, "at": 2},
        ],
    )
    write_jsonl(
        d / "events.jsonl",
        [
            {
                "at": 1,
                "query": "q",
                "decisions": [
                    {"id": "a", "score": 1, "zone": "hit", "admitted": True, "reason": "admitted"},
                    {"id": "b", "score": 0.5, "zone": "miss", "reason": "zone_miss"},
                ],
            }
        ],
    )
    b64 = "YWJj"
    write_jsonl(
        d / "gateway.jsonl",
        [
            {
                "ID": "a",
                "Type": 0,
                "Scope": 1,
                "Content": b64,
                "Stats": {"Injected": injected, "Rejected": rejected},
                "created_at": 1,
            }
        ],
    )
    return d


def test_arm_summary_aggregates(tmp_path):
    d = arm_fixture(tmp_path, "on", injected=8, rejected=2, tokens_in=200)
    s = arm_summary(d)
    assert s["requests"] == 2
    assert s["tokens_in"] == 300
    assert s["injected_tokens"] == 20
    assert s["slice_hits"] == 1
    assert s["events"] == 1
    assert s["admitted"] == 1
    assert s["candidates"] == 2
    assert abs(s["acceptance"] - 0.8) < 1e-12  # 8/(8+2) from store stats


def test_arm_summary_tolerates_missing_files(tmp_path):
    d = tmp_path / "empty"
    d.mkdir()
    s = arm_summary(d)
    assert s["requests"] == 0
    assert s["acceptance"] is None


def test_render_comparison_has_both_arms(tmp_path):
    on = arm_summary(arm_fixture(tmp_path, "on", injected=8, rejected=2, tokens_in=200))
    off = arm_summary(arm_fixture(tmp_path, "off", injected=5, rejected=5, tokens_in=400))
    text = render_comparison(on, off)
    assert "| requests | 2 | 2 |" in text
    assert "0.800" in text and "0.500" in text
