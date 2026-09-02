#!/usr/bin/env python3
"""Regression tests for Issue #447 call-source attribution."""

from __future__ import annotations

import json
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock


HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

from common import InstanceMetrics  # noqa: E402
from report import load_run  # noqa: E402
from run_bench import SemantixAdapter  # noqa: E402


class SemantixMetricAttributionTest(unittest.TestCase):
    def setUp(self) -> None:
        self.adapter = SemantixAdapter(SimpleNamespace(), HERE)

    def new_metrics(self) -> InstanceMetrics:
        return InstanceMetrics(
            run_id="test-run",
            harness="semantix",
            model="test-model",
            instance_id="test-instance",
        )

    def test_fill_metrics_attributes_every_model_call(self) -> None:
        raw = {
            "prompt_tokens": 1000,
            "completion_tokens": 100,
            "steps": 11,
            "usage_by_source": {
                "executor": {"calls": 5},
                "planner": {"calls": 1},
                "subagent": {"calls": 2},
                "compaction": {"calls": 1},
                "classifier": {"calls": 2},
            },
            "retries": 3,
            "compactions": 1,
            "subagent_runs": 2,
            "tool_calls": 8,
            "tool_failures": 1,
            "tool_calls_by_name": {"read_file": 4, "grep": 3, "bash": 1},
            "repeated_tool_calls": 3,
            "repeated_tool_calls_by_name": {"read_file": 2, "grep": 1},
        }

        metrics = self.new_metrics()
        self.adapter.fill_metrics(metrics, raw)

        self.assertEqual(metrics.model_calls_by_source, {
            "executor": 5,
            "planner": 1,
            "subagent": 2,
            "compaction": 1,
            "classifier": 2,
        })
        self.assertEqual(metrics.executor_calls, 5)
        self.assertEqual(metrics.planner_calls, 1)
        self.assertEqual(metrics.subagent_calls, 2)
        self.assertEqual(metrics.compaction_calls, 1)
        self.assertEqual(metrics.other_model_calls, 2)
        self.assertEqual(metrics.source_call_total, 11)
        self.assertEqual(metrics.source_call_delta, 0)
        self.assertEqual(metrics.provider_retries, 3)
        self.assertEqual(metrics.compactions, 1)
        self.assertEqual(metrics.subagent_runs, 2)
        self.assertEqual(metrics.tool_failures, 1)
        self.assertEqual(metrics.tool_calls_by_name, {
            "read_file": 4,
            "grep": 3,
            "bash": 1,
        })
        self.assertEqual(metrics.repeated_tool_calls, 3)
        self.assertEqual(metrics.repeated_tool_calls_by_name, {"read_file": 2, "grep": 1})

    def test_fill_metrics_keeps_old_records_explicitly_unattributed(self) -> None:
        metrics = self.new_metrics()

        self.adapter.fill_metrics(metrics, {"steps": 7, "tool_calls": 2})

        self.assertEqual(metrics.model_calls_by_source, {})
        self.assertEqual(metrics.source_call_total, 0)
        self.assertEqual(metrics.source_call_delta, 7)
        self.assertEqual(metrics.executor_calls, 0)
        self.assertEqual(metrics.other_model_calls, 0)
        self.assertIsNone(metrics.repeated_tool_calls)
        self.assertIsNone(metrics.repeated_tool_calls_by_name)

    def test_fill_metrics_ignores_malformed_or_negative_counts(self) -> None:
        raw = {
            "steps": 4,
            "usage_by_source": {
                "executor": {"calls": 3},
                "negative": {"calls": -1},
                "boolean": {"calls": True},
                "string": {"calls": "9"},
                "not-a-bucket": 2,
                42: {"calls": 1},
            },
            "tool_calls_by_name": {
                "grep": 2,
                "negative": -2,
                "boolean": False,
                "string": "4",
            },
        }

        metrics = self.new_metrics()
        self.adapter.fill_metrics(metrics, raw)

        self.assertEqual(metrics.model_calls_by_source, {"executor": 3})
        self.assertEqual(metrics.tool_calls_by_name, {"grep": 2})
        self.assertEqual(metrics.source_call_total, 3)
        self.assertEqual(metrics.source_call_delta, 1)

    def test_run_instance_persists_cli_output_for_failure_diagnosis(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            args = SimpleNamespace(
                openai_base="http://127.0.0.1:8139/v1",
                effort="",
                model="deepseek-v4-flash",
                semantix_retrieval_mode="off",
                preset="balanced",
                ablate="",
                timeout=30,
            )
            adapter = SemantixAdapter(args, root / "run")
            adapter.binary = "semantix-agent"
            adapter.home = root / "home"
            adapter.memory_on = False
            completed = SimpleNamespace(returncode=1, stdout="provider stdout\n",
                                        stderr="provider stderr\n")
            with mock.patch("run_bench.subprocess.run", return_value=completed) as run:
                adapter.run_instance(root, "prompt", {"instance_id": "i1"})

            native = root / "run" / "native"
            self.assertEqual((native / "i1.semantix.stdout.txt").read_text(),
                             "provider stdout\n")
            self.assertEqual((native / "i1.semantix.stderr.txt").read_text(),
                             "provider stderr\n")
            command = run.call_args.args[0]
            self.assertEqual(command[command.index("--model") + 1], "deepseek-flash")
            config = (root / "home" / "inst" / "i1" / "config.toml").read_text()
            self.assertIn('default_model = "deepseek-flash"', config)
            self.assertIn('name        = "deepseek-flash"', config)


class AttributionReportTest(unittest.TestCase):
    @staticmethod
    def metric_row(**overrides: object) -> dict:
        row = {
            "harness": "semantix",
            "model": "test-model",
            "wall_ms": 1000,
            "input_tokens": 100,
            "output_tokens": 10,
            "cache_hit_tokens": 80,
            "cache_miss_tokens": 20,
            "cost_usd": 0.01,
            "empty_patch": False,
            "error": "",
        }
        row.update(overrides)
        return row

    def write_run(self, rows: list[dict]) -> tuple[tempfile.TemporaryDirectory, Path]:
        tmp = tempfile.TemporaryDirectory()
        run_dir = Path(tmp.name) / "test-run"
        run_dir.mkdir()
        payload = "".join(json.dumps(row) + "\n" for row in rows)
        (run_dir / "metrics.jsonl").write_text(payload, encoding="utf-8")
        return tmp, run_dir

    def test_load_run_aggregates_call_sources_retries_and_tools(self) -> None:
        rows = [
            self.metric_row(
                executor_calls=5,
                planner_calls=1,
                subagent_calls=2,
                compaction_calls=1,
                other_model_calls=2,
                source_call_total=11,
                source_call_delta=0,
                provider_retries=1,
                compactions=1,
                subagent_runs=2,
                tool_calls=4,
                tool_failures=1,
                model_calls_by_source={"executor": 5, "classifier": 2},
                tool_calls_by_name={"grep": 3, "bash": 1},
                repeated_tool_calls=2,
                repeated_tool_calls_by_name={"grep": 2},
            ),
            self.metric_row(
                executor_calls=3,
                planner_calls=1,
                subagent_calls=1,
                compaction_calls=1,
                other_model_calls=2,
                source_call_total=8,
                source_call_delta=0,
                provider_retries=2,
                compactions=1,
                subagent_runs=1,
                tool_calls=2,
                tool_failures=0,
                model_calls_by_source={"executor": 3, "goal-evaluator": 2},
                tool_calls_by_name={"grep": 1, "bash": 1},
                repeated_tool_calls=1,
                repeated_tool_calls_by_name={"grep": 1},
            ),
        ]
        tmp, run_dir = self.write_run(rows)
        self.addCleanup(tmp.cleanup)

        aggregate = load_run(run_dir)

        self.assertEqual(aggregate["executor_calls"], 8)
        self.assertEqual(aggregate["planner_calls"], 2)
        self.assertEqual(aggregate["subagent_calls"], 3)
        self.assertEqual(aggregate["compaction_calls"], 2)
        self.assertEqual(aggregate["other_model_calls"], 4)
        self.assertEqual(aggregate["source_call_total"], 19)
        self.assertEqual(aggregate["source_call_delta"], 0)
        self.assertEqual(aggregate["provider_retries"], 3)
        self.assertEqual(aggregate["compactions"], 2)
        self.assertEqual(aggregate["subagent_runs"], 3)
        self.assertEqual(aggregate["tool_calls"], 6)
        self.assertEqual(aggregate["tool_failures"], 1)
        self.assertEqual(aggregate["model_calls_by_source"], {
            "classifier": 2,
            "executor": 8,
            "goal-evaluator": 2,
        })
        self.assertEqual(aggregate["tool_calls_by_name"], {"bash": 2, "grep": 4})
        self.assertEqual(aggregate["repeated_tool_calls"], 3)
        self.assertEqual(aggregate["repeated_tool_calls_by_name"], {"grep": 3})

    def test_load_run_treats_legacy_attribution_fields_as_zero(self) -> None:
        tmp, run_dir = self.write_run([self.metric_row()])
        self.addCleanup(tmp.cleanup)

        aggregate = load_run(run_dir)

        self.assertEqual(aggregate["executor_calls"], 0)
        self.assertEqual(aggregate["source_call_total"], 0)
        self.assertEqual(aggregate["provider_retries"], 0)
        self.assertEqual(aggregate["model_calls_by_source"], {})
        self.assertEqual(aggregate["tool_calls_by_name"], {})
        self.assertEqual(aggregate["repeated_tool_calls"], 0)
        self.assertEqual(aggregate["repeated_tool_calls_by_name"], {})


if __name__ == "__main__":
    unittest.main()
