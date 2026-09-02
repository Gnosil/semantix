#!/usr/bin/env python3
"""Scan a run's L2 injection audit journal for answer leakage (Issue #326).

Issue #326 §五 requires the Track B (grouped, cross-instance reuse) run to dump
every provider-visible injection block and prove it contains no leak of the
instance being solved: not the gold patch, not its FAIL_TO_PASS test names.
This script is that scan. It reads the journal the runner copied from each
agent's [semantix] audit_dir into run_dir/audit/<instance>.jsonl and checks
every block against the dataset row of the instance that was being solved.

Semantics (documented, deliberate):

  * A block injected while solving instance N may only contain slices distilled
    from same-repo instances solved before N — the runner extracts slices only
    after an instance finishes, and each repo store is written by one worker in
    selection order. So checking N's own gold patch / FAIL_TO_PASS test names
    is the leakage contract; there is no store that could contain N's answer
    before N runs.
  * Matching is substring-based on whitespace-normalized text: (a) the full
    normalized gold patch, (b) any normalized gold-added line of length >=
    --min-added-len (default 20), (c) any normalized FAIL_TO_PASS test id.
    A hit is a *candidate* for manual review, not a verdict: same-repo issue
    statements and shared test files legitimately overlap, which is exactly
    why the issue mandates dumping + scanning instead of asserting by
    construction.

Examples:
  python scan_leaks.py --run-dir results/semantix.deepseek-v4-flash.20260824 \
      --dataset data/swebench_verified.jsonl
  python scan_leaks.py --run-dir ... --dataset ... --report-json leaks.json --no-fail
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

# ---------------------------------------------------------------------------
# library API (imported by report_tracks.py too)
# ---------------------------------------------------------------------------


def _norm(text: str) -> str:
    return re.sub(r"\s+", "", text or "")


def gold_added_lines(gold_patch: str, min_len: int = 20) -> list[str]:
    """Content lines a gold patch *adds* (unified diff '+', minus the '+++'
    file header), whitespace-normalized and length-filtered so trivial
    additions (blank lines, one-word imports) do not flood the report."""
    out = []
    for line in (gold_patch or "").splitlines():
        if not line.startswith("+") or line.startswith("+++"):
            continue
        content = line[1:].lstrip()
        normalized = _norm(content)
        if len(normalized) >= min_len:
            out.append(normalized)
    return out


def scan_instance_entries(entries: list[dict], gold_patch: str,
                          fail_to_pass: list[str], min_added_len: int = 20) -> list[dict]:
    """Check every injected block in `entries` (audit lines recorded while one
    instance was being solved) against that instance's gold patch and
    FAIL_TO_PASS test ids. Returns one dict per distinct hit."""
    needles: list[tuple[str, str]] = []  # (kind, normalized needle)
    if gold_patch:
        needles.append(("gold_patch", _norm(gold_patch)))
        for line in gold_added_lines(gold_patch, min_added_len):
            needles.append(("gold_line", line))
    if fail_to_pass:
        for test_id in fail_to_pass:
            needles.append(("test_name", _norm(test_id)))
    if not needles:
        return []

    flags: list[dict] = []
    seen: set[tuple[int, str, str]] = set()
    for entry in entries:
        seq = entry.get("seq")
        block = _norm(entry.get("text", ""))
        if not block:
            continue
        for kind, needle in needles:
            if not needle:
                continue
            if needle not in block:
                continue
            key = (seq, kind, needle)
            if key in seen:
                continue
            seen.add(key)
            # Recover a short human-readable excerpt from the original text.
            raw_text = entry.get("text", "") or ""
            excerpt = raw_text[:160].replace("\n", " ")
            flags.append({
                "instance_id": entry.get("_instance_id"),
                "seq": seq,
                "kind": kind,
                "needle_chars": len(needle),
                "targets": entry.get("targets") or [],
                "excerpt": excerpt,
            })
    return flags


def scan_run(run_dir: Path, rows_by_id: dict[str, dict], min_added_len: int = 20) -> dict:
    """Scan every audit/<instance>.jsonl under run_dir. rows_by_id maps
    instance_id -> dataset row (gold 'patch', FAIL_TO_PASS list)."""
    audit_root = Path(run_dir) / "audit"
    flagged: list[dict] = []
    entries_total = 0
    instances_audited = 0
    if audit_root.is_dir():
        for path in sorted(audit_root.glob("*.jsonl")):
            iid = path.name[:-len(".jsonl")]
            row = rows_by_id.get(iid)
            try:
                raw_entries = [json.loads(line) for line in
                               path.read_text(encoding="utf-8").splitlines() if line.strip()]
            except (OSError, ValueError):
                continue
            entries = []
            for e in raw_entries:
                e = dict(e)
                e["_instance_id"] = iid
                entries.append(e)
            if not entries:
                continue
            instances_audited += 1
            entries_total += len(entries)
            gold = (row or {}).get("patch", "") if row else ""
            tests = [str(x) for x in ((row or {}).get("FAIL_TO_PASS") or [])] if row else []
            flagged.extend(scan_instance_entries(entries, gold, tests, min_added_len))
    status = "no_audit" if instances_audited == 0 else (
        "flagged" if flagged else "clean")
    return {
        "run_dir": str(run_dir),
        "instances_audited": instances_audited,
        "entries": entries_total,
        "flagged": flagged,
        "status": status,
    }


def format_report(result: dict) -> str:
    lines = [f"# Leak scan: {result['run_dir']}",
             "",
             f"- status: **{result['status']}**",
             f"- instances with injections audited: {result['instances_audited']}",
             f"- injected blocks checked: {result['entries']}",
             f"- leak candidates: {len(result['flagged'])}",
             ""]
    if result["flagged"]:
        lines.append("| instance | seq | kind | needle chars | targets |")
        lines.append("| --- | --- | --- | --- | --- |")
        for flag in result["flagged"]:
            targets = ",".join(flag["targets"])[:60]
            lines.append(f"| {flag['instance_id']} | {flag['seq']} | {flag['kind']} "
                         f"| {flag['needle_chars']} | {targets} |")
        lines.append("")
        lines.append("> Candidates need manual review: same-repo issues and shared "
                     "test files legitimately overlap.")
    elif result["status"] == "no_audit":
        lines.append("No injection audit artifacts found (shadow/off retrieval, "
                     "empty slice library, or pre-#326 run).")
    return "\n".join(lines)


def load_rows_by_id(dataset: Path) -> dict[str, dict]:
    rows = {}
    with open(dataset, encoding="utf-8") as handle:
        for line in handle:
            line = line.strip()
            if not line:
                continue
            row = json.loads(line)
            rows[row["instance_id"]] = row
    return rows


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--run-dir", required=True)
    ap.add_argument("--dataset", required=True, help="local SWE-bench Verified jsonl")
    ap.add_argument("--min-added-len", type=int, default=20,
                    help="shortest gold-added line (normalized chars) to match (default 20)")
    ap.add_argument("--report-json", default="", help="optional path to write the JSON result")
    ap.add_argument("--no-fail", action="store_true",
                    help="exit 0 even when candidates are flagged (default exit 1)")
    args = ap.parse_args()

    result = scan_run(Path(args.run_dir), load_rows_by_id(Path(args.dataset)),
                      min_added_len=args.min_added_len)
    print(format_report(result))
    if args.report_json:
        Path(args.report_json).parent.mkdir(parents=True, exist_ok=True)
        Path(args.report_json).write_text(
            json.dumps(result, indent=2, ensure_ascii=False), encoding="utf-8")
    if result["flagged"] and not args.no_fail:
        sys.exit(1)


if __name__ == "__main__":
    main()
