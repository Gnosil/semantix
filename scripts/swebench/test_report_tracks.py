#!/usr/bin/env python3
import json
import tempfile
import unittest
from pathlib import Path

import report_tracks
from report_tracks import (bootstrap_ci, official_resolution, repo_ordinals,
                           track_a_row, track_b_rows)


def dataset_rows():
    return [
        {"instance_id": "django-1", "repo": "django/django", "base_commit": "a"},
        {"instance_id": "flask-1", "repo": "pallets/flask", "base_commit": "b"},
        {"instance_id": "django-2", "repo": "django/django", "base_commit": "a"},
        {"instance_id": "flask-2", "repo": "pallets/flask", "base_commit": "b"},
    ]


def write_run(root: Path, run_id: str, cfg: dict, costs: list[dict],
              resolved_ids: list[str]):
    run_dir = root / run_id
    run_dir.mkdir(parents=True)
    (run_dir / "run_config.json").write_text(json.dumps(cfg), encoding="utf-8")
    with open(run_dir / "cost.jsonl", "w", encoding="utf-8") as handle:
        for row in costs:
            handle.write(json.dumps(row) + "\n")
    if resolved_ids is not None:
        # swebench's run_evaluation writes <model>.<run_id>.json; official
        # resolution globs *.<run_id>.json so the model prefix is required.
        (run_dir / f"m.{run_id}.json").write_text(
            json.dumps({"resolved_ids": resolved_ids}), encoding="utf-8")
    return run_dir


def cost(iid: str, cost_usd: float, **extra) -> dict:
    row = {"run_id": "", "harness": "semantix", "model": "deepseek-v4-flash",
           "instance_id": iid, "arm": "full", "protocol": "grouped",
           "agent_exit": 0, "error": "", "wall_ms": 10_000, "steps": 10,
           "input_tokens": 1000, "output_tokens": 200,
           "cache_hit_tokens": 400, "cache_miss_tokens": 600,
           "cache_hit_rate": 0.4, "cost_usd": cost_usd, "cost_native": None,
           "cost_native_currency": "", "patch_bytes": 100,
           "empty_patch": False, "semantix_inject_bytes": None}
    row.update(extra)
    return row


class TrackACellTests(unittest.TestCase):
    def test_track_a_row_joins_verdict_and_cost(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            run_dir = write_run(
                root, "full.std", {"harness": "semantix", "model": "deepseek-v4-flash",
                                   "arm": "full", "protocol": "standard", "seed": 7},
                [cost("django-1", 0.5, protocol="standard"),
                 cost("django-2", 1.5, protocol="standard")],
                resolved_ids=["django-1"])
            row = track_a_row(run_dir)
            self.assertEqual(row["arm"], "full")
            self.assertEqual(row["protocol"], "standard")
            self.assertEqual(row["n"], 2)
            self.assertEqual(row["resolved"], 1)
            self.assertAlmostEqual(row["resolve_rate"], 0.5)
            self.assertAlmostEqual(row["cost"]["mean"], 1.0)
            self.assertAlmostEqual(row["cost_total_usd"], 2.0)

    def test_bootstrap_ci_deterministic_and_bounded(self):
        values = [0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8]
        first = bootstrap_ci(values, 42)
        second = bootstrap_ci(values, 42)
        self.assertEqual(first, second)
        self.assertLess(first["ci95"][0], first["mean"])
        self.assertGreater(first["ci95"][1], first["mean"])


class OfficialResolutionTests(unittest.TestCase):
    def test_reads_evaluate_report(self):
        with tempfile.TemporaryDirectory() as td:
            run_dir = Path(td) / "semantix.deepseek-v4-flash.7"
            run_dir.mkdir()
            (run_dir / "m.semantix.deepseek-v4-flash.7.json").write_text(
                json.dumps({"resolved_ids": ["a"]}), encoding="utf-8")
            self.assertEqual(official_resolution(run_dir), {"a": 1.0})

    def test_absent_report_returns_none(self):
        with tempfile.TemporaryDirectory() as td:
            self.assertIsNone(official_resolution(Path(td)))


class TrackBTests(unittest.TestCase):
    def test_repo_ordinals_follow_run_course(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            cfg = {"harness": "semantix", "protocol": "grouped", "sample": 0,
                   "seed": 3, "ids": None}
            costs = [cost("django-1", 0.5), cost("flask-1", 0.5),
                     cost("django-2", 0.3), cost("flask-2", 0.3)]
            run_dir = write_run(root, "grp", cfg, costs, resolved_ids=[])
            ordinals = repo_ordinals(run_dir, dataset_rows())
            self.assertEqual(ordinals["django-1"], ("django/django", 1))
            self.assertEqual(ordinals["django-2"], ("django/django", 2))
            self.assertEqual(ordinals["flask-2"], ("pallets/flask", 2))

    def test_track_b_rows_only_for_grouped_semantix_memory_on(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            cfg = {"harness": "semantix", "protocol": "grouped",
                   "semantix_memory": "on", "sample": 0, "seed": 3, "ids": None}
            costs = [cost("django-1", 0.5), cost("django-2", 0.3)]
            run_dir = write_run(root, "grp", cfg, costs, resolved_ids=["django-1"])
            rows = track_b_rows(run_dir, dataset_rows())
            self.assertEqual([(r["instance_id"], r["ordinal"]) for r in rows],
                             [("django-1", 1), ("django-2", 2)])
            # memory off => no course
            cfg_off = dict(cfg, semantix_memory="off")
            run_off = write_run(root, "grp-off", cfg_off,
                                [cost("django-1", 0.5)], resolved_ids=[])
            self.assertEqual(track_b_rows(run_off, dataset_rows()), [])

    def test_track_a_without_verdict_keeps_rate_none(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            run_dir = write_run(root, "noverdict",
                                {"harness": "semantix", "arm": "full", "protocol": "standard"},
                                [cost("django-1", 0.5, protocol="standard")],
                                resolved_ids=None)
            row = track_a_row(run_dir)
            self.assertIsNone(row["resolve_rate"])
            self.assertFalse(row["resolution"])


if __name__ == "__main__":
    unittest.main()
