#!/usr/bin/env python3
"""Self-evolution daemon (spec §6): watch the slice library and the
retrieval events log; when enough new signal accumulates, run one offline
round — dataset → train → export → gate → publish → SIGHUP the rerank
server — then go back to watching. Pure file observation (no kernel
hooks); training runs in subprocesses so heavyweight deps never live in
the daemon. All round output tees to train/reports/.

  uv run python watch_and_train.py --lab ~/.semantix-lab/retrieval-lab \
      --semantix-bin ~/.semantix-lab/bin/semantix [--once] [--min-new-events 200]
"""

import argparse
import datetime
import json
import os
import signal
import subprocess
import sys
import time
from pathlib import Path

ML_DIR = Path(__file__).resolve().parent


def count_lines(path):
    try:
        with open(path, "rb") as f:
            return sum(1 for _ in f)
    except OSError:
        return 0


def mtime(path):
    try:
        return os.stat(path).st_mtime
    except OSError:
        return 0.0


def sh(cmd, log, env=None):
    """Run one pipeline step, teeing output to the round log (spec §5:
    verification output must land on disk, never pipe-only). cwd is pinned
    to the ml project dir so `uv run` resolves this pyproject regardless of
    where the daemon was launched from."""
    log.write(f"\n$ {' '.join(str(c) for c in cmd)}\n")
    log.flush()
    proc = subprocess.run(
        [str(c) for c in cmd],
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        env=env,
        cwd=ML_DIR,
    )
    log.write(proc.stdout.decode(errors="replace"))
    log.flush()
    return proc.returncode


def run_round(lab, semantix_bin, args):
    from semantix_ml import registry

    train_dir = lab / "train"
    reports = train_dir / "reports"
    reports.mkdir(parents=True, exist_ok=True)
    stamp = datetime.datetime.now().strftime("%Y%m%d-%H%M%S")
    round_log = reports / f"round-{stamp}.log"
    staging = train_dir / "staging" / stamp
    datasets = train_dir / "datasets" / stamp
    db = lab / "on" / "gateway.jsonl"
    events = lab / "on" / "events.jsonl"
    synth = train_dir / "datasets" / "synth.jsonl"

    with open(round_log, "w", encoding="utf-8") as log:
        log.write(f"round {stamp}\n")
        export = staging / "slices-export.jsonl"
        staging.mkdir(parents=True, exist_ok=True)

        # 1. Export the library (Get/List strip vectors; Export is the
        #    sanctioned full read — spec "关键现状事实").
        if sh([semantix_bin, "export", "--db", db, "--out", export], log) != 0:
            return False, "export failed", round_log

        # 2. Dataset.
        cmd = ["uv", "run", "python", ML_DIR / "build_dataset.py", "--slices", export,
               "--events", events, "--out", datasets]
        if synth.exists():
            cmd += ["--synth", synth]
        if sh(cmd, log) != 0:
            return False, "build_dataset failed", round_log

        # 3. Train (resumes from current when it exists — I-3).
        train_cmd = ["uv", "run", "--extra", "train", "python", ML_DIR / "train_reranker.py",
                     "--data", datasets / "train.jsonl", "--out", staging / "hf",
                     "--epochs", str(args.epochs)]
        if registry.current_version(train_dir):
            train_cmd += ["--current", train_dir / "current"]
        if sh(train_cmd, log) != 0:
            return False, "train failed", round_log

        # 4. Export ONNX into the checkpoint root (current/ serves directly).
        if sh(["uv", "run", "--extra", "train", "python", ML_DIR / "export_onnx.py",
               "--hf", staging / "hf", "--out", staging], log) != 0:
            return False, "onnx export failed", round_log

        # 5. Gate against the published metrics — or the no-reranker
        #    baseline on the very first round.
        heldout = datasets / "heldout.jsonl"
        reference = train_dir / "current" / "metrics.json"
        if not reference.exists():
            baseline_report = staging / "baseline.json"
            if sh(["uv", "run", "python", ML_DIR / "eval_retrieval.py", "--data", heldout,
                   "--baseline", "--report", baseline_report], log) != 0:
                return False, "baseline eval failed", round_log
            reference = baseline_report
        gate_report = staging / "eval.json"
        code = sh(["uv", "run", "--extra", "serve", "python", ML_DIR / "eval_retrieval.py",
                   "--data", heldout, "--model", staging, "--reference", reference,
                   "--report", gate_report], log)
        if code == 3:
            return False, "gate REJECTED (checkpoint kept in staging)", round_log
        if code != 0:
            return False, "candidate eval failed", round_log

        # 6. Publish + hot-reload the sidecar.
        metrics = json.loads(gate_report.read_text())["metrics"]
        version = registry.publish(train_dir, staging, metrics)
        log.write(f"\npublished {version}\n")
        pid_file = lab / "rerank-server.pid"
        if pid_file.exists():
            try:
                os.kill(int(pid_file.read_text().strip()), signal.SIGHUP)
                log.write("SIGHUP sent to rerank server\n")
            except (ValueError, ProcessLookupError) as e:
                log.write(f"rerank server reload skipped: {e}\n")
        return True, f"published {version}", round_log


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--lab", required=True)
    ap.add_argument("--semantix-bin", required=True)
    ap.add_argument("--min-new-events", type=int, default=200)
    ap.add_argument("--max-quiet-hours", type=float, default=24.0)
    ap.add_argument("--debounce-secs", type=int, default=600)
    ap.add_argument("--poll-secs", type=int, default=60)
    ap.add_argument("--epochs", type=int, default=1)
    ap.add_argument("--once", action="store_true", help="run one round now and exit")
    args = ap.parse_args()

    lab = Path(args.lab).expanduser()
    events = lab / "on" / "events.jsonl"
    db = lab / "on" / "gateway.jsonl"
    state_file = lab / "train" / "watch-state.json"
    state = {"events_seen": 0, "last_round": 0.0}
    if state_file.exists():
        state.update(json.loads(state_file.read_text()))

    def one_round():
        ok, msg, log = run_round(lab, Path(args.semantix_bin).expanduser(), args)
        state["events_seen"] = count_lines(events)
        state["last_round"] = time.time()
        state_file.parent.mkdir(parents=True, exist_ok=True)
        state_file.write_text(json.dumps(state))
        print(f"watch_and_train: {'OK' if ok else 'SKIP'} — {msg} (log: {log})", flush=True)
        return ok

    if args.once:
        sys.exit(0 if one_round() else 1)

    print(f"watch_and_train: watching {lab} (min_new_events={args.min_new_events})", flush=True)
    while True:
        new_events = count_lines(events) - state["events_seen"]
        quiet_h = (time.time() - state["last_round"]) / 3600
        data_moved = mtime(db) > state["last_round"] or new_events > 0
        if new_events >= args.min_new_events or (quiet_h >= args.max_quiet_hours and data_moved):
            # Debounce: wait for a write-quiet window so we never train
            # against a mid-burst library state.
            while True:
                snapshot = (mtime(db), count_lines(events))
                time.sleep(args.debounce_secs)
                if (mtime(db), count_lines(events)) == snapshot:
                    break
            one_round()
        time.sleep(args.poll_secs)


if __name__ == "__main__":
    main()
