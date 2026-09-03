#!/usr/bin/env python3
"""Score a predictions file with the official SWE-bench evaluation harness.

Wraps `python -m swebench.harness.run_evaluation` (docker required; swebench
>= 5.x CLI) and moves the report into the run directory. Prebuilt image names
come from the dataset's `image` column (Docker Hub `swebench/` namespace).

Usage:
  python evaluate.py --run-dir results/<run_id> --dataset data/swebench_verified.jsonl \
      [--max-workers 4] [--timeout 1800]
"""

from __future__ import annotations

import argparse
import json
import shutil
import subprocess
import sys
from pathlib import Path


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--run-dir", required=True)
    ap.add_argument("--dataset", required=True)
    ap.add_argument("--max-workers", type=int, default=4)
    ap.add_argument("--timeout", type=int, default=1800)
    args = ap.parse_args()

    run_dir = Path(args.run_dir).resolve()
    preds = run_dir / "predictions.jsonl"
    if not preds.exists():
        preds = run_dir / "preds.jsonl"  # pre-issue-326 run compatibility
    if not preds.exists():
        sys.exit(f"no predictions at {preds}")
    with open(preds) as f:
        model_name = json.loads(f.readline())["model_name_or_path"]
    run_id = run_dir.name

    # swebench 5.x resolves per-instance eval images from the dataset's own
    # `image` field (pre-pull + retag from the Epoch ghcr mirror if Docker Hub
    # rate-limits anonymous pulls).
    cmd = [
        sys.executable, "-m", "swebench.harness.run_evaluation",
        "--dataset_name", str(Path(args.dataset).resolve()),
        "--predictions_path", str(preds),
        "--max_workers", str(args.max_workers),
        "--run_id", run_id,
        "--timeout", str(args.timeout),
    ]
    print("+", " ".join(cmd), flush=True)
    proc = subprocess.run(cmd, cwd=run_dir)
    if proc.returncode != 0:
        sys.exit(proc.returncode)

    # run_evaluation writes <model_name_with_dots_sanitized>.<run_id>.json in cwd
    report = None
    for p in run_dir.glob(f"*.{run_id}.json"):
        report = p
    if report is None:
        for p in Path.cwd().glob(f"*.{run_id}.json"):
            report = p
            shutil.move(str(p), run_dir / p.name)
            report = run_dir / p.name
    if report is None:
        sys.exit("evaluation finished but no report json found")
    data = json.loads(Path(report).read_text())
    print(json.dumps({k: data.get(k) for k in (
        "total_instances", "submitted_instances", "completed_instances",
        "resolved_instances", "unresolved_instances", "empty_patch_instances",
        "error_instances")}, indent=2))
    print(f"report: {report}")


if __name__ == "__main__":
    main()
