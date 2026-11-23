import json
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from score_verify import read_labels, replay_rows


class ScoreVerifyTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        self.report = self.root / "replay.json"
        self.labels = self.root / "labels.tsv"
        self.rows = [
            {"session": "s.jsonl", "turn": i, "zone": "hit",
             "query": "synthetic query", "top1_content": "synthetic candidate"}
            for i in range(1, 4)
        ]
        self.write_replay()

    def write_replay(self):
        self.report.write_text(json.dumps({
            "ok": True, "command": "verify", "data": {
                "rows": self.rows, "replayed_turns": 3,
                "zones_hit": 3, "zones_grey": 0, "zones_miss": 0,
            },
        }), encoding="utf-8")

    def run_cli(self, flag):
        return subprocess.run(
            [sys.executable, str(Path(__file__).with_name("score_verify.py")),
             str(self.report), flag, str(self.labels)],
            check=True, capture_output=True, text=True,
        )

    def test_template_excludes_transcript_text_and_score_separates_rates(self):
        self.run_cli("--prepare")
        if os.name == "posix":
            self.assertEqual(stat.S_IMODE(self.labels.stat().st_mode) & 0o077, 0)
        with self.assertRaises(subprocess.CalledProcessError):
            self.run_cli("--prepare")
        self.assertEqual(self.labels.read_text().splitlines()[1:], [
            "s.jsonl\t1\t", "s.jsonl\t2\t", "s.jsonl\t3\t",
        ])
        self.labels.write_text("s.jsonl\t1\t1\ns.jsonl\t2\t1\ns.jsonl\t3\t0\n")
        report = json.loads(self.run_cli("--labels").stdout)
        self.assertEqual(report["replayed"], 3)
        self.assertEqual(report["human_yield_pct"], 66.667)
        self.assertEqual(report["auto_hit_pct"], 100.0)
        self.assertFalse(report["human_yield_at_least_70_pct"])
        self.assertTrue(report["grey_at_most_30_pct"])

    def test_requires_exactly_one_binary_label_per_row(self):
        keyed, _ = replay_rows(self.report)
        cases = [
            ("s.jsonl\t1\t1\n", "incomplete labels"),
            ("s.jsonl\t1\t1\ns.jsonl\t1\t0\n", "duplicate or unknown"),
            ("s.jsonl\t4\t1\n", "duplicate or unknown"),
            ("s.jsonl\t1\t?\n", "expected session"),
        ]
        for labels, error in cases:
            with self.subTest(labels=labels):
                self.labels.write_text(labels)
                with self.assertRaisesRegex(ValueError, error):
                    read_labels(self.labels, keyed)

    def test_no_candidate_cannot_be_labeled_relevant(self):
        self.rows[0]["top1_content"] = ""
        self.write_replay()
        keyed, _ = replay_rows(self.report)
        self.labels.write_text("s.jsonl\t1\t1\n")
        with self.assertRaisesRegex(ValueError, "no answer candidate"):
            read_labels(self.labels, keyed)

    def test_inconsistent_replay_rejected(self):
        self.rows[0]["zone"] = "grey"
        self.write_replay()
        with self.assertRaisesRegex(ValueError, "zones do not match"):
            replay_rows(self.report)
        self.rows[0] = dict(self.rows[1])
        self.write_replay()
        with self.assertRaisesRegex(ValueError, "duplicate replay row") as error:
            replay_rows(self.report)
        self.assertNotIn("s.jsonl", str(error.exception))


if __name__ == "__main__":
    unittest.main()
