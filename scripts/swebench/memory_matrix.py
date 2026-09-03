#!/usr/bin/env python3
"""Run the Issue #447 paired memory experiment matrix.

Arms are executed sequentially to avoid cross-arm provider/CPU contention:

  A  memory off
  B  memory on + shadow retrieval
  C  memory on + current strict policy
  D  optional historical replay of the legacy all-type policy

Each repetition/arm gets an isolated state and work directory. The selected
dataset order is identical because every command reuses the same --ids/--seed.
"""

from __future__ import annotations

import argparse
import json
import shlex
import subprocess
import sys
from dataclasses import asdict, dataclass
from pathlib import Path

HERE = Path(__file__).resolve().parent


@dataclass(frozen=True)
class MatrixRun:
    arm: str
    label: str
    repetition: int
    run_id: str
    run_dir: str
    state_dir: str
    work_dir: str
    command: list[str]


ARM_CONFIG = (
    ("A", "off", "off", "off", False),
    ("B", "shadow", "on", "shadow", False),
    ("C", "strict", "on", "strict", False),
    ("D", "legacy", "on", "strict", True),
)


def add_optional(command: list[str], flag: str, value: str) -> None:
    if value:
        command.extend((flag, value))


def build_runs(args: argparse.Namespace) -> list[MatrixRun]:
    runs: list[MatrixRun] = []
    legacy_bin = getattr(args, "legacy_semantix_bin", "")
    arm_config = tuple(config for config in ARM_CONFIG if not config[4] or legacy_bin)
    for repetition in range(1, args.repetitions + 1):
        for arm, label, memory, retrieval, legacy in arm_config:
            run_id = f"{args.prefix}.r{repetition:02d}.{arm}-{label}"
            state_dir = str(Path(args.state_dir) / f"r{repetition:02d}" / f"{arm}-{label}")
            work_dir = str(Path(args.work_dir) / f"r{repetition:02d}" / f"{arm}-{label}")
            agent_bin = legacy_bin if legacy else args.semantix_bin
            command = [
                sys.executable, str(HERE / "run_bench.py"),
                "--harness", "semantix",
                "--dataset", args.dataset,
                "--ids", args.ids,
                "--model", args.model,
                "--run-id", run_id,
                "--results-dir", args.results_dir,
                "--work-dir", work_dir,
                "--state-dir", state_dir,
                "--workers", str(args.workers),
                "--timeout", str(args.timeout),
                "--max-turns", str(args.max_turns),
                "--preset", args.preset,
                "--semantix-memory", memory,
                "--semantix-retrieval-mode", retrieval,
                "--semantix-bin", agent_bin,
                "--semantix-kernel-bin", args.semantix_kernel_bin,
            ]
            add_optional(command, "--effort", args.effort)
            add_optional(command, "--prices", args.prices)
            add_optional(command, "--openai-base", args.openai_base)
            add_optional(command, "--anthropic-base", args.anthropic_base)
            if memory == "on":
                add_optional(command, "--semantix-seed-dir",
                             getattr(args, "semantix_seed_dir", ""))
            runs.append(MatrixRun(
                arm=arm, label=label, repetition=repetition, run_id=run_id,
                run_dir=str(Path(args.results_dir) / run_id),
                state_dir=state_dir, work_dir=work_dir, command=command,
            ))
    return runs


def manifest_for(args: argparse.Namespace, runs: list[MatrixRun]) -> dict:
    return {
        "schema": 1,
        "prefix": args.prefix,
        "dataset": args.dataset,
        "ids": args.ids,
        "model": args.model,
        "repetitions": args.repetitions,
        "semantix_seed_dir": getattr(args, "semantix_seed_dir", ""),
        "arm_order": list(dict.fromkeys(run.arm for run in runs)),
        "runs": [asdict(run) for run in runs],
    }


def parse_args() -> argparse.Namespace:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--dataset", required=True)
    ap.add_argument("--ids", required=True, help="frozen instance IDs in dataset order")
    ap.add_argument("--model", default="deepseek-v4-flash")
    ap.add_argument("--semantix-bin", required=True, help="current strict semantix-agent")
    ap.add_argument("--legacy-semantix-bin", default="",
                    help="optional historical semantix-agent retaining the old all-type policy")
    ap.add_argument("--semantix-kernel-bin", required=True)
    ap.add_argument("--semantix-seed-dir", default="",
                    help="frozen owner__repo store root copied into B/C/D")
    ap.add_argument("--repetitions", type=int, default=3)
    ap.add_argument("--prefix", default="issue447-memory")
    ap.add_argument("--results-dir", default=str(HERE / "results"))
    ap.add_argument("--work-dir", default=str(HERE / "work" / "issue447-memory"))
    ap.add_argument("--state-dir", default=str(HERE / "state" / "issue447-memory"))
    ap.add_argument("--workers", type=int, default=4)
    ap.add_argument("--timeout", type=int, default=2400)
    ap.add_argument("--max-turns", type=int, default=120)
    ap.add_argument("--preset", default="balanced")
    ap.add_argument("--effort", default="")
    ap.add_argument("--prices", default="")
    ap.add_argument("--openai-base", default="")
    ap.add_argument("--anthropic-base", default="")
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("--continue-on-error", action="store_true")
    return ap.parse_args()


def main() -> int:
    args = parse_args()
    if args.repetitions < 1:
        raise SystemExit("--repetitions must be >= 1")
    runs = build_runs(args)
    manifest_path = Path(args.results_dir) / f"{args.prefix}.matrix.json"
    manifest_path.parent.mkdir(parents=True, exist_ok=True)
    manifest_path.write_text(
        json.dumps(manifest_for(args, runs), indent=2, ensure_ascii=False) + "\n",
        encoding="utf-8",
    )
    print(f"manifest: {manifest_path}")

    failed = False
    for run in runs:
        print(f"=== repetition {run.repetition} arm {run.arm}-{run.label} ===", flush=True)
        print(shlex.join(run.command), flush=True)
        if args.dry_run:
            continue
        result = subprocess.run(run.command, cwd=HERE, check=False)
        if result.returncode:
            failed = True
            print(f"arm {run.arm} exit={result.returncode}", file=sys.stderr, flush=True)
            if not args.continue_on_error:
                return result.returncode
    return int(failed)


if __name__ == "__main__":
    raise SystemExit(main())
