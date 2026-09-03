import json
import tempfile
import unittest
from pathlib import Path

import scan_injection_leaks as scanner


class InjectionLeakScannerTests(unittest.TestCase):
    def test_clean_and_leaking_blocks(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            audit = root / "audit"
            audit.mkdir()
            dataset = {
                "org__repo-1": {
                    "instance_id": "org__repo-1",
                    "patch": "diff --git a/x b/x\n+gold answer",
                    "FAIL_TO_PASS": json.dumps(["tests/test_x.py::test_bug"]),
                }
            }

            (audit / "org__repo-1.txt").write_text("use the public API\n", encoding="utf-8")
            report = scanner.scan(audit, dataset)
            self.assertTrue(report["passed"])
            self.assertEqual(report["files_scanned"], 1)

            (audit / "org__repo-1.txt").write_text(
                "tests/test_x.py::test_bug\n", encoding="utf-8"
            )
            report = scanner.scan(audit, dataset)
            self.assertFalse(report["passed"])
            self.assertEqual(report["findings"][0]["kind"], "fail_to_pass")

    def test_unknown_audit_file_fails_closed(self):
        with tempfile.TemporaryDirectory() as td:
            audit = Path(td)
            (audit / "unknown.txt").write_text("anything", encoding="utf-8")
            report = scanner.scan(audit, {})
            self.assertFalse(report["passed"])
            self.assertEqual(report["findings"][0]["kind"], "unknown_instance")

    def test_expected_instance_without_audit_fails_closed(self):
        with tempfile.TemporaryDirectory() as td:
            report = scanner.scan(Path(td), {"expected": {}}, ["expected"])
            self.assertFalse(report["passed"])
            self.assertEqual(report["findings"][0]["kind"], "missing_audit")


if __name__ == "__main__":
    unittest.main()
