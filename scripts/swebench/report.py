#!/usr/bin/env python3
"""Merge run metrics with official evaluation reports into one comparison table.

Usage:
  python report.py --runs results/run-a results/run-b ... [--format md|json]

Each run directory needs cost.jsonl (or legacy metrics.jsonl) from run_bench.py; the resolve rate
column fills in when an official report json (evaluate.py output) is present,
and shows "n/a" otherwise.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path


def sum_int(metrics: list[dict], key: str) -> int:
    """Sum one optional integer field while tolerating legacy records."""
    return sum(
        value
        for row in metrics
        if isinstance((value := row.get(key, 0)), int)
        and not isinstance(value, bool)
    )


def sum_count_maps(metrics: list[dict], key: str) -> dict[str, int]:
    """Merge optional non-negative string-to-int counter maps."""
    out = {}
    for row in metrics:
        counts = row.get(key, {})
        if not isinstance(counts, dict):
            continue
        for name, count in counts.items():
            if (not isinstance(name, str) or not isinstance(count, int)
                    or isinstance(count, bool) or count < 0):
                continue
            out[name] = out.get(name, 0) + count
    return dict(sorted(out.items()))


def load_run(run_dir: Path) -> dict:
    metrics = []
    mpath = run_dir / "cost.jsonl"
    if not mpath.exists():
        mpath = run_dir / "metrics.jsonl"
    if mpath.exists():
        with open(mpath) as f:
            metrics = [json.loads(l) for l in f if l.strip()]
    report = None
    for p in sorted(run_dir.glob(f"*.{run_dir.name}.json")):
        report = json.loads(p.read_text())
    n = len(metrics)
    agg = {
        "run_id": run_dir.name,
        "harness": metrics[0]["harness"] if metrics else "?",
        "model": metrics[0]["model"] if metrics else "?",
        "instances": n,
        "resolved": report.get("resolved_instances") if report else None,
        "submitted": report.get("submitted_instances") if report else None,
        "resolve_rate": None,
        "wall_s_total": sum(m["wall_ms"] for m in metrics) / 1000,
        "wall_s_mean": (sum(m["wall_ms"] for m in metrics) / n / 1000) if n else 0,
        "input_tokens": sum(m["input_tokens"] for m in metrics),
        "output_tokens": sum(m["output_tokens"] for m in metrics),
        "cache_hit_tokens": sum(m["cache_hit_tokens"] for m in metrics),
        "cache_miss_tokens": sum(m["cache_miss_tokens"] for m in metrics),
        "cache_hit_rate": None,
        "steps": sum_int(metrics, "steps"),
        "executor_calls": sum_int(metrics, "executor_calls"),
        "planner_calls": sum_int(metrics, "planner_calls"),
        "subagent_calls": sum_int(metrics, "subagent_calls"),
        "compaction_calls": sum_int(metrics, "compaction_calls"),
        "other_model_calls": sum_int(metrics, "other_model_calls"),
        "source_call_total": sum_int(metrics, "source_call_total"),
        "source_call_delta": sum_int(metrics, "source_call_delta"),
        "model_calls_by_source": sum_count_maps(metrics, "model_calls_by_source"),
        "provider_retries": sum_int(metrics, "provider_retries"),
        "compactions": sum_int(metrics, "compactions"),
        "subagent_runs": sum_int(metrics, "subagent_runs"),
        "tool_calls": sum_int(metrics, "tool_calls"),
        "tool_failures": sum_int(metrics, "tool_failures"),
        "tool_calls_by_name": sum_count_maps(metrics, "tool_calls_by_name"),
        "repeated_tool_calls": sum_int(metrics, "repeated_tool_calls"),
        "repeated_tool_calls_by_name": sum_count_maps(metrics, "repeated_tool_calls_by_name"),
        "cost_usd": sum(m["cost_usd"] or 0 for m in metrics),
        "empty_patches": sum(1 for m in metrics if m["empty_patch"]),
        "errors": sum(1 for m in metrics if m["error"]),
    }
    denom = agg["cache_hit_tokens"] + agg["cache_miss_tokens"]
    if denom:
        agg["cache_hit_rate"] = agg["cache_hit_tokens"] / denom
    if report and report.get("submitted_instances"):
        agg["resolve_rate"] = report["resolved_instances"] / report["submitted_instances"]
    return agg


def fmt(v, kind=""):
    if v is None:
        return "n/a"
    if kind == "pct":
        return f"{v:.1%}"
    if kind == "usd":
        return f"${v:.3f}"
    if kind == "s":
        return f"{v:,.0f}s"
    if kind == "k":
        return f"{v / 1000:,.1f}k"
    return str(v)


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--runs", nargs="+", required=True)
    ap.add_argument("--format", default="md", choices=["md", "json"])
    args = ap.parse_args()

    rows = [load_run(Path(r)) for r in args.runs]
    if args.format == "json":
        print(json.dumps(rows, indent=2))
        return

    headers = ["harness", "model", "n", "resolved", "resolve %", "mean wall",
               "input tok", "output tok", "cache hit %", "cost (USD)",
               "calls E/P/S/C/O", "tools/repeat", "retry", "empty", "err"]
    print("| " + " | ".join(headers) + " |")
    print("|" + "---|" * len(headers))
    for r in rows:
        resolved = "n/a" if r["resolved"] is None else f"{r['resolved']}/{r['submitted']}"
        print("| " + " | ".join([
            r["harness"], r["model"], str(r["instances"]), resolved,
            fmt(r["resolve_rate"], "pct"), fmt(r["wall_s_mean"], "s"),
            fmt(r["input_tokens"], "k"), fmt(r["output_tokens"], "k"),
            fmt(r["cache_hit_rate"], "pct"), fmt(r["cost_usd"], "usd"),
            "/".join(str(r[key]) for key in (
                "executor_calls", "planner_calls", "subagent_calls",
                "compaction_calls", "other_model_calls")),
            f"{r['tool_calls']}/{r['repeated_tool_calls']}", str(r["provider_retries"]),
            str(r["empty_patches"]), str(r["errors"]),
        ]) + " |")


if __name__ == "__main__":
    main()
