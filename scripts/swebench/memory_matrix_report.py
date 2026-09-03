#!/usr/bin/env python3
"""Build per-instance paired A-D summaries from a memory matrix manifest."""

from __future__ import annotations

import argparse
import json
import math
import statistics
from pathlib import Path

METRICS = (
    "resolved", "executor_calls", "steps", "input_tokens", "tool_calls",
    "read_calls", "search_calls", "test_calls", "wall_ms", "cost_usd",
    "provider_retries", "semantix_inject_turns", "semantix_inject_bytes",
    "semantix_fuse_turns", "semantix_rejected_slices",
    "repeated_tool_calls", "repeated_read_calls", "repeated_search_calls",
    "repeated_test_calls",
)

READ_TOOLS = {"read", "read_file", "readfile", "view", "view_file"}
SEARCH_TOOLS = {"grep", "rg", "search", "search_files", "find"}
TEST_TOOLS = {"test", "run_tests", "pytest", "go_test"}


def percentile(values: list[float], q: float) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    if len(ordered) == 1:
        return float(ordered[0])
    position = (len(ordered) - 1) * q
    low = math.floor(position)
    high = math.ceil(position)
    if low == high:
        return float(ordered[low])
    return float(ordered[low] + (ordered[high] - ordered[low]) * (position - low))


def distribution(values: list[float]) -> dict:
    return {
        "count": len(values),
        "mean": statistics.fmean(values) if values else None,
        "median": statistics.median(values) if values else None,
        "p75": percentile(values, 0.75),
        "p90": percentile(values, 0.90),
    }


def official_resolution(run_dir: Path) -> dict[str, float] | None:
    reports = sorted(run_dir.glob(f"*.{run_dir.name}.json"))
    if not reports:
        return None
    report = json.loads(reports[-1].read_text(encoding="utf-8"))
    resolved = report.get("resolved_ids")
    if not isinstance(resolved, list):
        return None
    resolved_set = {value for value in resolved if isinstance(value, str)}
    return {instance_id: 1.0 for instance_id in resolved_set}


def load_rows(run: dict) -> dict[str, dict]:
    inline = run.get("rows")
    if isinstance(inline, dict):
        return inline
    run_dir = Path(run["run_dir"])
    metrics_path = run_dir / "metrics.jsonl"
    rows = {}
    with metrics_path.open(encoding="utf-8") as handle:
        for line in handle:
            if not line.strip():
                continue
            row = json.loads(line)
            rows[row["instance_id"]] = row
    resolution = official_resolution(run_dir)
    if resolution is not None:
        for instance_id, row in rows.items():
            row["resolved"] = resolution.get(instance_id, 0.0)
    return rows


def tool_family(row: dict, names: set[str], field: str = "tool_calls_by_name") -> float:
    counts = row.get(field, {})
    if not isinstance(counts, dict):
        return 0.0
    return float(sum(
        count for name, count in counts.items()
        if isinstance(name, str) and name.lower() in names
        and isinstance(count, int) and not isinstance(count, bool) and count >= 0
    ))


def metric_value(row: dict, metric: str) -> float | None:
    if metric == "read_calls":
        return tool_family(row, READ_TOOLS)
    if metric == "search_calls":
        return tool_family(row, SEARCH_TOOLS)
    if metric == "test_calls":
        return tool_family(row, TEST_TOOLS)
    if metric == "repeated_read_calls":
        if not isinstance(row.get("repeated_tool_calls_by_name"), dict):
            return None
        return tool_family(row, READ_TOOLS, "repeated_tool_calls_by_name")
    if metric == "repeated_search_calls":
        if not isinstance(row.get("repeated_tool_calls_by_name"), dict):
            return None
        return tool_family(row, SEARCH_TOOLS, "repeated_tool_calls_by_name")
    if metric == "repeated_test_calls":
        if not isinstance(row.get("repeated_tool_calls_by_name"), dict):
            return None
        return tool_family(row, TEST_TOOLS, "repeated_tool_calls_by_name")
    if metric.startswith("semantix_"):
        value = row.get("raw", {}).get(metric)
    else:
        value = row.get(metric)
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return float(value)
    return None


def build_report(manifest: dict) -> dict:
    loaded = []
    by_rep_arm: dict[tuple[int, str], dict[str, dict]] = {}
    for run in manifest.get("runs", []):
        rows = load_rows(run)
        item = dict(run, rows=rows)
        loaded.append(item)
        by_rep_arm[(int(run["repetition"]), run["arm"])] = rows

    repetitions = sorted({int(run["repetition"]) for run in loaded})
    arms = sorted({run["arm"] for run in loaded})
    for repetition in repetitions:
        baseline = by_rep_arm.get((repetition, "A"))
        if baseline is None:
            raise ValueError(f"repetition {repetition} missing arm A baseline")
        baseline_ids = set(baseline)
        for arm in arms:
            rows = by_rep_arm.get((repetition, arm))
            if rows is None:
                raise ValueError(f"repetition {repetition} missing arm {arm}")
            if set(rows) != baseline_ids:
                raise ValueError(
                    f"repetition {repetition} arm {arm} instance set differs from arm A"
                )

    output = {"schema": 1, "repetitions": len(repetitions), "arms": {}}
    for arm in arms:
        absolute = {metric: [] for metric in METRICS}
        deltas = {metric: [] for metric in METRICS}
        instance_count = 0
        for repetition in repetitions:
            rows = by_rep_arm[(repetition, arm)]
            baseline = by_rep_arm[(repetition, "A")]
            instance_count += len(rows)
            for instance_id, row in rows.items():
                for metric in METRICS:
                    value = metric_value(row, metric)
                    base = metric_value(baseline[instance_id], metric)
                    if value is not None:
                        absolute[metric].append(value)
                    if arm != "A" and value is not None and base is not None:
                        deltas[metric].append(value - base)
        resolved_values = absolute["resolved"]
        output["arms"][arm] = {
            "instances": instance_count,
            "resolved_rate": statistics.fmean(resolved_values) if resolved_values else None,
            "metrics": {
                metric: {
                    "absolute": distribution(absolute[metric]),
                    "delta_vs_A": None if arm == "A" else distribution(deltas[metric]),
                }
                for metric in METRICS
            },
        }
    return output


def fmt(value: float | None) -> str:
    return "n/a" if value is None else f"{value:.2f}"


def markdown(report: dict) -> str:
    lines = [
        "| arm | paired n | resolved | Δ executor median/P75/P90 | Δ input median/P75/P90 | Δ tools median/P75/P90 | Δ repeats median/P75/P90 | Δ fuses median/P75/P90 |",
        "|---|---:|---:|---:|---:|---:|---:|---:|",
    ]
    for arm, data in report["arms"].items():
        def delta(metric: str) -> str:
            d = data["metrics"][metric]["delta_vs_A"]
            if d is None:
                return "baseline"
            return "/".join(fmt(d[key]) for key in ("median", "p75", "p90"))
        resolved = data["resolved_rate"]
        lines.append(
            f"| {arm} | {data['instances']} | "
            f"{'n/a' if resolved is None else f'{resolved:.1%}'} | "
            f"{delta('executor_calls')} | {delta('input_tokens')} | {delta('tool_calls')} | "
            f"{delta('repeated_tool_calls')} | {delta('semantix_fuse_turns')} |"
        )
    return "\n".join(lines) + "\n"


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--manifest", required=True)
    ap.add_argument("--format", choices=("json", "md"), default="md")
    ap.add_argument("--out", default="")
    args = ap.parse_args()
    manifest = json.loads(Path(args.manifest).read_text(encoding="utf-8"))
    report = build_report(manifest)
    text = (json.dumps(report, indent=2, ensure_ascii=False) + "\n"
            if args.format == "json" else markdown(report))
    if args.out:
        Path(args.out).write_text(text, encoding="utf-8")
    else:
        print(text, end="")


if __name__ == "__main__":
    main()
