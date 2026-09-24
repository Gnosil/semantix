import argparse
import json
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import memory_matrix  # noqa: E402
import memory_matrix_report  # noqa: E402


class MemoryMatrixCommandTest(unittest.TestCase):
    def test_defaults_to_core_abc_without_legacy_binary(self):
        args = argparse.Namespace(
            dataset="dataset.jsonl",
            ids="ids.txt",
            model="deepseek-v4-flash",
            semantix_bin="bin/current-agent",
            semantix_kernel_bin="bin/semantix",
            semantix_seed_dir="seed",
            repetitions=1,
            prefix="issue447",
            results_dir="results",
            work_dir="work",
            state_dir="state",
            workers=3,
            timeout=1200,
            max_turns=80,
            preset="balanced",
            effort="",
            prices="",
            openai_base="",
            anthropic_base="",
        )
        runs = memory_matrix.build_runs(args)
        self.assertEqual([r.arm for r in runs], ["A", "B", "C"])
        self.assertEqual(memory_matrix.manifest_for(args, runs)["arm_order"], ["A", "B", "C"])

    def test_builds_repeated_abcd_commands_with_isolated_state(self):
        args = argparse.Namespace(
            dataset="dataset.jsonl",
            ids="ids.txt",
            model="deepseek-v4-flash",
            semantix_bin="bin/current-agent",
            legacy_semantix_bin="bin/legacy-agent",
            semantix_kernel_bin="bin/semantix",
            semantix_seed_dir="seed",
            repetitions=2,
            prefix="issue447",
            results_dir="results",
            work_dir="work",
            state_dir="state",
            workers=3,
            timeout=1200,
            max_turns=80,
            preset="balanced",
            effort="",
            prices="",
            openai_base="",
            anthropic_base="",
        )
        runs = memory_matrix.build_runs(args)
        self.assertEqual([(r.repetition, r.arm) for r in runs], [
            (1, "A"), (1, "B"), (1, "C"), (1, "D"),
            (2, "A"), (2, "B"), (2, "C"), (2, "D"),
        ])
        commands = {r.arm: r.command for r in runs[:4]}
        self.assertIn("off", commands["A"])
        self.assertIn("shadow", commands["B"])
        self.assertIn("strict", commands["C"])
        self.assertIn("bin/legacy-agent", commands["D"])
        self.assertIn("bin/current-agent", commands["C"])
        self.assertNotIn("--semantix-seed-dir", commands["A"])
        for arm in ("B", "C", "D"):
            self.assertIn("--semantix-seed-dir", commands[arm])
            self.assertIn("seed", commands[arm])
        self.assertEqual(len({r.state_dir for r in runs}), 8)
        self.assertEqual(len({r.work_dir for r in runs}), 8)


class MemoryMatrixReportTest(unittest.TestCase):
    def write_run(self, root: Path, run_id: str, rows: list[dict], resolved: list[str]):
        run = root / run_id
        run.mkdir(parents=True)
        (run / "metrics.jsonl").write_text(
            "".join(json.dumps(row) + "\n" for row in rows), encoding="utf-8"
        )
        (run / f"test.{run_id}.json").write_text(json.dumps({
            "resolved_instances": len(resolved),
            "submitted_instances": len(rows),
            "resolved_ids": resolved,
        }), encoding="utf-8")

    def test_report_pairs_each_arm_against_off_by_instance(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            base_rows = [
                {"instance_id": "i1", "executor_calls": 5, "steps": 6,
                 "input_tokens": 100, "tool_calls": 8, "wall_ms": 1000,
                 "cost_usd": 1.0, "provider_retries": 0, "empty_patch": False,
                 "tool_calls_by_name": {"read_file": 3, "grep": 2, "bash": 1},
                 "repeated_tool_calls": 3,
                 "repeated_tool_calls_by_name": {"read_file": 2, "grep": 1}, "raw": {}},
                {"instance_id": "i2", "executor_calls": 7, "steps": 8,
                 "input_tokens": 200, "tool_calls": 10, "wall_ms": 2000,
                 "cost_usd": 2.0, "provider_retries": 1, "empty_patch": False,
                 "tool_calls_by_name": {"read_file": 4, "grep": 1, "bash": 2},
                 "repeated_tool_calls": 2,
                 "repeated_tool_calls_by_name": {"read_file": 1, "grep": 1}, "raw": {}},
            ]
            strict_rows = [
                dict(base_rows[0], executor_calls=4, input_tokens=80,
                     repeated_tool_calls=1,
                     repeated_tool_calls_by_name={"read_file": 1}),
                dict(base_rows[1], executor_calls=6, input_tokens=150,
                     repeated_tool_calls=0,
                     repeated_tool_calls_by_name={}),
            ]
            runs = []
            for arm, rows, resolved in (
                ("A", base_rows, ["i1"]),
                ("B", base_rows, ["i1"]),
                ("C", strict_rows, ["i1", "i2"]),
                ("D", base_rows, ["i1"]),
            ):
                run_id = f"m.r01.{arm}"
                self.write_run(root, run_id, rows, resolved)
                runs.append({"arm": arm, "label": arm, "repetition": 1,
                             "run_id": run_id, "run_dir": str(root / run_id)})
            manifest = {"schema": 1, "runs": runs}
            report = memory_matrix_report.build_report(manifest)
            c = report["arms"]["C"]
            self.assertEqual(c["instances"], 2)
            self.assertEqual(c["resolved_rate"], 1.0)
            self.assertEqual(c["metrics"]["executor_calls"]["delta_vs_A"]["median"], -1.0)
            self.assertEqual(c["metrics"]["input_tokens"]["delta_vs_A"]["p90"], -23.0)
            self.assertEqual(c["metrics"]["repeated_tool_calls"]["delta_vs_A"]["median"], -2.0)
            self.assertEqual(c["metrics"]["repeated_read_calls"]["delta_vs_A"]["median"], -1.0)
            self.assertEqual(c["metrics"]["repeated_search_calls"]["delta_vs_A"]["median"], -1.0)
            self.assertIn("Δ repeats median/P75/P90", memory_matrix_report.markdown(report))
            self.assertIn("Δ fuses median/P75/P90", memory_matrix_report.markdown(report))

    def test_report_rejects_unpaired_instance_sets(self):
        manifest = {"schema": 1, "runs": [
            {"arm": "A", "repetition": 1, "run_id": "a", "rows": {"i1": {}}},
            {"arm": "B", "repetition": 1, "run_id": "b", "rows": {"i2": {}}},
        ]}
        with self.assertRaisesRegex(ValueError, "instance set"):
            memory_matrix_report.build_report(manifest)

    def test_missing_repeat_fields_stay_unattributed(self):
        legacy = {"tool_calls_by_name": {"read_file": 3}}
        self.assertIsNone(memory_matrix_report.metric_value(legacy, "repeated_tool_calls"))
        self.assertIsNone(memory_matrix_report.metric_value(legacy, "repeated_read_calls"))
        normalized_legacy = {
            "repeated_tool_calls": None,
            "repeated_tool_calls_by_name": None,
        }
        self.assertIsNone(memory_matrix_report.metric_value(
            normalized_legacy, "repeated_read_calls"
        ))


if __name__ == "__main__":
    unittest.main()
