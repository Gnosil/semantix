#!/usr/bin/env python3
import json
import tempfile
import unittest
from pathlib import Path

import scan_leaks


def entry(seq: int, text: str, targets=("ctx-strong",)) -> dict:
    return {"seq": seq, "at": 1, "query": "q", "query_sha256": "x",
            "degraded": False, "budget": 4096, "bytes": len(text),
            "targets": list(targets), "text": text}


GOLD = (
    "diff --git a/tests/test_forms.py b/tests/test_forms.py\n"
    "--- a/tests/test_forms.py\n+++ b/tests/test_forms.py\n"
    "@@ -42,7 +42,7 @@\n"
    " def test_render_required(self):\n"
    "     form = self.build_form()\n"
    "-    self.assertFalse(form.fields['name'].required)\n"
    "+    self.assertTrue(form.fields['name'].required)\n"
)


class GoldAddedLineTests(unittest.TestCase):
    def test_extracts_only_real_additions_above_min_len(self):
        lines = scan_leaks.gold_added_lines(GOLD, min_len=20)
        self.assertEqual(lines, ["self.assertTrue(form.fields['name'].required)"])

    def test_ignores_trivial_additions(self):
        patch = "+++ b/x\n+ \n+\n+def short():\n"
        self.assertEqual(scan_leaks.gold_added_lines(patch, min_len=20), [])


class ScanInstanceTests(unittest.TestCase):
    def test_clean_block_injects_nothing(self):
        entries = [entry(1, "[semantix-reuse] 修复 go 测试失败 go run ./...")]
        flags = scan_leaks.scan_instance_entries(entries, GOLD, ["tests.test_forms::test_ok"])
        self.assertEqual(flags, [])

    def test_flags_leaked_test_name_and_gold_line(self):
        block = ("[semantix-reuse] 部署生产表单："
                 "tests.test_forms::test_render_required 需要 required=True，"
                 "self.assertTrue(form.fields['name'].required)")
        entries = [entry(1, block)]
        flags = scan_leaks.scan_instance_entries(entries, GOLD, ["tests.test_forms::test_render_required"])
        kinds = {f["kind"] for f in flags}
        self.assertIn("test_name", kinds)
        self.assertIn("gold_line", kinds)

    def test_full_gold_patch_match(self):
        entries = [entry(1, "prefix " + GOLD + " suffix")]
        flags = scan_leaks.scan_instance_entries(entries, GOLD, [])
        kinds = {f["kind"] for f in flags}
        self.assertIn("gold_patch", kinds)
        # The full patch match implies its added lines matched too.
        self.assertIn("gold_line", kinds)

    def test_dedupes_repeated_needles(self):
        entries = [entry(1, "tests.test_forms::test_render_required again"),
                   entry(2, "tests.test_forms::test_render_required again")]
        flags = scan_leaks.scan_instance_entries(entries, "", ["tests.test_forms::test_render_required"])
        self.assertEqual(len(flags), 2)  # one per distinct (seq, kind, needle)


class ScanRunTests(unittest.TestCase):
    def test_scan_run_end_to_end(self):
        with tempfile.TemporaryDirectory() as td:
            run_dir = Path(td) / "run"
            audit = run_dir / "audit"
            audit.mkdir(parents=True)
            (audit / "django-1.jsonl").write_text(
                json.dumps(entry(1, "clean unrelated text"), ensure_ascii=False) + "\n",
                encoding="utf-8")
            (audit / "django-2.jsonl").write_text(
                json.dumps(entry(1, "tests.test_forms::test_render_required leaked"),
                           ensure_ascii=False) + "\n",
                encoding="utf-8")
            rows = {
                "django-1": {"instance_id": "django-1", "repo": "django/django",
                             "patch": GOLD, "FAIL_TO_PASS": ["tests.test_forms::test_render_required"]},
                "django-2": {"instance_id": "django-2", "repo": "django/django",
                             "patch": GOLD, "FAIL_TO_PASS": ["tests.test_forms::test_render_required"]},
            }
            result = scan_leaks.scan_run(run_dir, rows)
            self.assertEqual(result["instances_audited"], 2)
            self.assertEqual(result["entries"], 2)
            self.assertEqual(result["status"], "flagged")
            self.assertEqual(len(result["flagged"]), 1)
            self.assertEqual(result["flagged"][0]["instance_id"], "django-2")

    def test_scan_run_no_audit_dir(self):
        with tempfile.TemporaryDirectory() as td:
            result = scan_leaks.scan_run(Path(td), {})
            self.assertEqual(result["status"], "no_audit")
            self.assertEqual(result["flagged"], [])


if __name__ == "__main__":
    unittest.main()
