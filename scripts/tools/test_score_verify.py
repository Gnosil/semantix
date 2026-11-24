import hashlib
import json
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from score_verify import LABELS_HEADER, read_labels, replay_rows


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

    def write_labels(self, rows):
        replay_digest = hashlib.sha256(self.report.read_bytes()).hexdigest()
        self.labels.write_text(LABELS_HEADER + replay_digest + "\n" + rows, encoding="utf-8")

    def test_template_excludes_transcript_text_and_score_separates_rates(self):
        self.run_cli("--prepare")
        if os.name == "posix":
            self.assertEqual(stat.S_IMODE(self.labels.stat().st_mode) & 0o077, 0)
        with self.assertRaises(subprocess.CalledProcessError):
            self.run_cli("--prepare")
        template = self.labels.read_text().splitlines()
        self.assertEqual(template[0], LABELS_HEADER + hashlib.sha256(self.report.read_bytes()).hexdigest())
        self.assertEqual(template[2:], [
            "s.jsonl\t1\t", "s.jsonl\t2\t", "s.jsonl\t3\t",
        ])
        # CRLF from a Windows editor does not alter the replay binding.
        self.labels.write_bytes(("\n".join(template[:2]) +
                                 "\ns.jsonl\t1\t1\ns.jsonl\t2\t1\ns.jsonl\t3\t0\n").replace("\n", "\r\n").encode())
        report = json.loads(self.run_cli("--labels").stdout)
        self.assertEqual(report["replayed"], 3)
        self.assertEqual(report["human_yield_pct"], 66.667)
        self.assertEqual(report["auto_hit_pct"], 100.0)
        self.assertFalse(report["human_yield_at_least_70_pct"])
        self.assertTrue(report["grey_at_most_30_pct"])

    def test_requires_exactly_one_binary_label_per_row(self):
        keyed, _, replay_digest = replay_rows(self.report)
        cases = [
            ("s.jsonl\t1\t1\n", "incomplete labels"),
            ("s.jsonl\t1\t1\ns.jsonl\t1\t0\n", "duplicate or unknown"),
            ("s.jsonl\t4\t1\n", "duplicate or unknown"),
            ("s.jsonl\t1\t?\n", "expected session"),
        ]
        for labels, error in cases:
            with self.subTest(labels=labels):
                self.write_labels(labels)
                with self.assertRaisesRegex(ValueError, error):
                    read_labels(self.labels, keyed, replay_digest)

    def test_no_candidate_cannot_be_labeled_relevant(self):
        self.rows[0]["top1_content"] = ""
        self.write_replay()
        keyed, _, replay_digest = replay_rows(self.report)
        self.write_labels("s.jsonl\t1\t1\n")
        with self.assertRaisesRegex(ValueError, "no answer candidate"):
            read_labels(self.labels, keyed, replay_digest)

    def test_same_row_keys_do_not_allow_reusing_labels_for_different_bytes(self):
        self.write_labels("s.jsonl\t1\t1\ns.jsonl\t2\t1\ns.jsonl\t3\t0\n")
        original = self.report.read_bytes()
        self.rows[0]["top1_content"] = "different synthetic candidate"
        self.write_replay()
        for raw in (self.report.read_bytes(), original + b"\n"):
            self.report.write_bytes(raw)
            with self.assertRaises(subprocess.CalledProcessError) as error:
                self.run_cli("--labels")
            self.assertEqual(error.exception.stdout, "")
            self.assertIn("bound to a different replay", error.exception.stderr)
            self.assertNotIn("synthetic candidate", error.exception.stderr)
            self.assertNotIn(hashlib.sha256(raw).hexdigest(), error.exception.stderr)
        # Mismatch is rejected before inspecting even malformed marks.
        with self.labels.open("ab") as labels:
            labels.write(b"\xff\n")
        with self.assertRaises(subprocess.CalledProcessError) as error:
            self.run_cli("--labels")
        self.assertIn("bound to a different replay", error.exception.stderr)

    def test_legacy_labels_require_regeneration(self):
        self.labels.write_text("# session\tturn\trelevant_0_or_1\ns.jsonl\t1\t1\ns.jsonl\t2\t1\ns.jsonl\t3\t0\n")
        with self.assertRaises(subprocess.CalledProcessError) as error:
            self.run_cli("--labels")
        self.assertEqual(error.exception.stdout, "")
        self.assertIn("no supported replay binding", error.exception.stderr)

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
