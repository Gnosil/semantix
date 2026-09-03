#!/usr/bin/env python3
"""Fail when grouped-protocol L2 audit blocks contain SWE-bench answers.

The scanner deliberately uses only official dataset fields. It does not judge
whether a patch is correct: exact gold-patch text and FAIL_TO_PASS identifiers
are treated as prohibited benchmark leakage and reported for review.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path


def parse_tests(value: object) -> list[str]:
    if isinstance(value, str):
        try:
            value = json.loads(value)
        except json.JSONDecodeError:
            value = [value]
    if not isinstance(value, list):
        return []
    return [item.strip() for item in value if isinstance(item, str) and item.strip()]


def load_dataset(path: Path) -> dict[str, dict]:
    rows: dict[str, dict] = {}
    with path.open(encoding="utf-8") as handle:
        for line in handle:
            if not line.strip():
                continue
            row = json.loads(line)
            rows[row["instance_id"]] = row
    return rows


def scan(audit_dir: Path, dataset: dict[str, dict], expected_ids: list[str] | None = None) -> dict:
    findings: list[dict] = []
    files = sorted(audit_dir.glob("*.txt")) if audit_dir.exists() else []
    found_ids = {path.stem for path in files}
    for instance_id in expected_ids or []:
        if instance_id not in found_ids:
            findings.append({
                "instance_id": instance_id,
                "kind": "missing_audit",
                "value": instance_id,
            })
    for path in files:
        instance_id = path.stem
        row = dataset.get(instance_id)
        if row is None:
            findings.append({
                "instance_id": instance_id,
                "kind": "unknown_instance",
                "value": instance_id,
            })
            continue
        text = path.read_text(encoding="utf-8", errors="replace")
        gold_patch = row.get("patch") or row.get("gold_patch") or ""
        if isinstance(gold_patch, str) and gold_patch.strip() and gold_patch.strip() in text:
            findings.append({
                "instance_id": instance_id,
                "kind": "gold_patch",
                "value": "exact gold patch",
            })
        for test_name in parse_tests(row.get("FAIL_TO_PASS", row.get("fail_to_pass", []))):
            if test_name in text:
                findings.append({
                    "instance_id": instance_id,
                    "kind": "fail_to_pass",
                    "value": test_name,
                })
    return {
        "schema": 1,
        "audit_dir": str(audit_dir),
        "files_scanned": len(files),
        "findings": findings,
        "passed": not findings,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--audit-dir", required=True)
    parser.add_argument("--dataset", required=True)
    parser.add_argument("--ids", help="selected instance ids; missing audit files fail closed")
    parser.add_argument("--json-out", help="write the machine-readable report here")
    args = parser.parse_args()

    expected_ids = None
    if args.ids:
        expected_ids = [
            line.strip() for line in Path(args.ids).read_text(encoding="utf-8").splitlines()
            if line.strip() and not line.lstrip().startswith("#")
        ]
    report = scan(Path(args.audit_dir), load_dataset(Path(args.dataset)), expected_ids)
    payload = json.dumps(report, indent=2, ensure_ascii=False) + "\n"
    if args.json_out:
        output = Path(args.json_out)
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(payload, encoding="utf-8")
    print(payload, end="")
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
