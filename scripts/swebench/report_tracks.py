#!/usr/bin/env python3
"""Merge official SWE-bench verdicts with cost.jsonl into Track A/B reports.

Issue #326 splits the authority claim into two tracks that must never be
co-reported in one table:

  * Track A — capability (standard protocol, comparable to leaderboards).
    Every instance solves in isolation: arm `full` (product default) vs
    `no-kernel` (ablation twin proving kernel overhead ≈ 0). Each run directory
    is one agent × model × arm cell; verdicts come from the official
    `swebench.harness.run_evaluation` report (never self-judged).
  * Track B — cost-effectiveness (grouped protocol, explicitly non-standard).
    Same-repo instances run in a fixed course and share a slice store, so
    later instances reuse earlier ones; the signal is the within-repo cost
    curve slope with no resolve-rate regression.

This script never judges patches. It joins `cost.jsonl` (per-instance USD,
written by run_bench.py) with the official report's `resolved_ids` and prints
the two tables separately, plus per-repo Track B curves.

Examples:
  python report_tracks.py --runs results/full.std.20260824 results/nokernel.std.20260824 \\
      --dataset data/swebench_verified.jsonl
  python report_tracks.py --runs results/full.grp.20260824 \\
      --dataset data/swebench_verified.jsonl --out-dir docs/reports/data/issue-326
"""

from __future__ import annotations

import argparse
import json
import random
import statistics
import sys
from pathlib import Path

from common import select_instances

DEFAULT_SEED = 20260824


# ---------------------------------------------------------------------------
# loaders
# ---------------------------------------------------------------------------


def load_costs(run_dir: Path) -> list[dict]:
    """cost.jsonl when present (run_bench >= Issue #326), else derive a lean
    row from metrics.jsonl so older runs still report."""
    costs = run_dir / "cost.jsonl"
    if costs.exists():
        with open(costs, encoding="utf-8") as handle:
            return [json.loads(line) for line in handle if line.strip()]
    rows = []
    metrics = run_dir / "metrics.jsonl"
    if metrics.exists():
        with open(metrics, encoding="utf-8") as handle:
            for line in handle:
                if not line.strip():
                    continue
                m = json.loads(line)
                rows.append({
                    "instance_id": m["instance_id"],
                    "arm": m.get("arm", ""),
                    "protocol": m.get("protocol", ""),
                    "wall_ms": m.get("wall_ms", 0),
                    "steps": m.get("steps", 0),
                    "input_tokens": m.get("input_tokens", 0),
                    "output_tokens": m.get("output_tokens", 0),
                    "cache_hit_tokens": m.get("cache_hit_tokens", 0),
                    "cache_miss_tokens": m.get("cache_miss_tokens", 0),
                    "cost_usd": m.get("cost_usd"),
                    "cost_native": m.get("cost_native"),
                    "empty_patch": m.get("empty_patch", True),
                    "error": m.get("error", ""),
                    "semantix_inject_turns": m.get("semantix_inject_turns"),
                    "semantix_inject_bytes": m.get("semantix_inject_bytes"),
                    "semantix_reuse_hits": m.get("semantix_reuse_hits"),
                })
    return rows


def load_config(run_dir: Path) -> dict:
    path = run_dir / "run_config.json"
    if path.exists():
        return json.loads(path.read_text(encoding="utf-8"))
    return {}


def official_resolution(run_dir: Path) -> dict[str, float] | None:
    """Parse the report left by `python -m swebench.harness.run_evaluation`
    (via evaluate.py). Returns {instance_id: 1.0} for resolved ids or None.
    The newest report wins (re-evaluations overwrite with the same run_id)."""
    candidates = sorted(run_dir.glob(f"*.{run_dir.name}.json"),
                        key=lambda p: (p.stat().st_mtime, p.name))
    if not candidates:
        return None
    try:
        report = json.loads(candidates[-1].read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None
    resolved = report.get("resolved_ids")
    if not isinstance(resolved, list):
        return None
    return {iid: 1.0 for iid in resolved if isinstance(iid, str)}


# ---------------------------------------------------------------------------
# Track A: per-run capability × cost cell
# ---------------------------------------------------------------------------


def percentile(values, q):
    ordered = sorted(values)
    if not ordered:
        return None
    position = (len(ordered) - 1) * q
    low = int(position)
    high = min(low + 1, len(ordered) - 1)
    weight = position - low
    return ordered[low] * (1 - weight) + ordered[high] * weight


def bootstrap_ci(values, seed: int, resamples: int = 2000) -> dict:
    """Deterministic percentile bootstrap 95% CI; None-safe."""
    vals = [v for v in values if v is not None]
    if not vals:
        return {"mean": None, "ci95": [None, None]}
    rng = random.Random(seed)
    means = []
    for _ in range(resamples):
        sample = [vals[rng.randrange(len(vals))] for _ in range(len(vals))]
        means.append(statistics.fmean(sample))
    means.sort()
    lo = means[int(0.025 * len(means))]
    hi = means[int(0.975 * len(means))]
    return {"mean": statistics.fmean(vals), "ci95": [round(lo, 6), round(hi, 6)]}


def track_a_row(run_dir: Path) -> dict:
    cfg = load_config(run_dir)
    rows = load_costs(run_dir)
    resolution = official_resolution(run_dir)
    by_id = {r["instance_id"]: r for r in rows}
    ordered = list(by_id.values())
    n = len(ordered)
    resolved = None
    if resolution is not None:
        resolved = sum(1 for iid in by_id if resolution.get(iid) == 1.0)
    costs = [r.get("cost_usd") for r in ordered]
    walls = [r.get("wall_ms") for r in ordered]
    hit_rates = []
    for r in ordered:
        hit, miss = r.get("cache_hit_tokens", 0), r.get("cache_miss_tokens", 0)
        if hit + miss > 0:
            hit_rates.append(hit / (hit + miss))
    inject_bytes = [r.get("semantix_inject_bytes") for r in ordered
                    if r.get("semantix_inject_bytes") is not None]
    seed = int(cfg.get("seed", DEFAULT_SEED))
    return {
        "run_id": run_dir.name,
        "harness": cfg.get("harness", ""),
        "model": cfg.get("model", ""),
        "arm": cfg.get("arm", "") or (rows[0].get("arm") if rows else ""),
        "protocol": cfg.get("protocol", "") or (rows[0].get("protocol") if rows else ""),
        "n": n,
        "resolved": resolved,
        "resolve_rate": round(resolved / n, 4) if resolved is not None and n else None,
        "resolution": resolution is not None,
        "cost": bootstrap_ci(costs, seed),
        "cost_total_usd": round(sum(c for c in costs if c is not None), 6),
        "wall_s_mean": round(statistics.fmean(walls) / 1000, 1) if walls else None,
        "steps_mean": round(statistics.fmean([r.get("steps", 0) for r in ordered]), 2) if ordered else None,
        "cache_hit_rate_mean": round(statistics.fmean(hit_rates), 4) if hit_rates else None,
        "inject_bytes_total": sum(inject_bytes) if inject_bytes else None,
        "empty_patch": sum(1 for r in ordered if r.get("empty_patch")),
        "errors": sum(1 for r in ordered if r.get("error")),
    }


def format_track_a(rows: list[dict]) -> str:
    if not rows:
        return "_no runs_"
    header = ("| run_id | harness+model | arm | protocol | n | resolved | "
              "resolve | cost/inst USD (95% CI) | total USD | wall s | cache hit |")
    sep = "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |"
    lines = [header, sep]
    for r in rows:
        ci = r["cost"]["ci95"]
        cost = f"{r['cost']['mean']:.4f} [{ci[0]}, {ci[1]}]" if r["cost"]["mean"] is not None else "—"
        rate = f"{r['resolve_rate']:.2%}" if r["resolve_rate"] is not None else "no verdict"
        ch = f"{r['cache_hit_rate_mean']:.1%}" if r["cache_hit_rate_mean"] is not None else "—"
        wall = f"{r['wall_s_mean']}" if r["wall_s_mean"] is not None else "—"
        resolved = r["resolved"] if r["resolved"] is not None else "—"
        lines.append(f"| {r['run_id']} | {r['harness']}+{r['model']} | {r['arm'] or '—'} "
                     f"| {r['protocol'] or '—'} | {r['n']} | {resolved} | {rate} | "
                     f"{cost} | {r['cost_total_usd']} | {wall} | {ch} |")
    lines.append("")
    lines.append("> resolve rate = official harness pass on FAIL_TO_PASS+PASS_TO_PASS; "
                 "cost = model-price run (cost.jsonl); CI = seed-fixed percentile bootstrap.")
    return "\n".join(lines)


# ---------------------------------------------------------------------------
# Track B: within-repo cost curve (grouped protocol only)
# ---------------------------------------------------------------------------


def solve_order(cfg: dict, dataset_rows: list[dict]) -> list[dict]:
    """Recompute the runner's per-instance course from run_config so ordinals
    are reproducible. Falls back to dataset order when the ids file is gone."""
    ids = cfg.get("ids")
    sample = int(cfg.get("sample", 0) or 0)
    seed = int(cfg.get("seed", DEFAULT_SEED))
    ids_file = Path(ids) if ids else None
    if ids_file is not None and not ids_file.exists():
        ids_file = None  # ids file no longer present: approximate
    try:
        return select_instances(dataset_rows, ids_file, sample, seed)
    except SystemExit:
        return dataset_rows


def repo_ordinals(run_dir: Path, dataset_rows: list[dict]) -> dict[str, tuple[str, int]]:
    """instance_id -> (repo, ordinal_within_repo) following the run's own
    selection + repo-grouped course (grouped protocol only; standard protocol
    has no cross-instance course)."""
    cfg = load_config(run_dir)
    if cfg.get("protocol", "grouped") != "grouped":
        return {}
    present = {r["instance_id"] for r in load_costs(run_dir)}
    out: dict[str, tuple[str, int]] = {}
    counter: dict[str, int] = {}
    for row in solve_order(cfg, dataset_rows):
        if row["instance_id"] not in present:
            continue
        repo = row.get("repo", "")
        counter[repo] = counter.get(repo, 0) + 1
        out[row["instance_id"]] = (repo, counter[repo])
    return out


def track_b_rows(run_dir: Path, dataset_rows: list[dict]) -> list[dict]:
    """One row per solved instance of a grouped run, in course order, with the
    within-repo ordinal, running cost and resolution — the material of the
    cost-curve slope check."""
    cfg = load_config(run_dir)
    if cfg.get("protocol", "grouped") != "grouped":
        return []
    if cfg.get("harness") != "semantix" or cfg.get("semantix_memory") != "on":
        return []
    rows = {r["instance_id"]: r for r in load_costs(run_dir)}
    ordinals = repo_ordinals(run_dir, dataset_rows)
    resolution = official_resolution(run_dir) or {}
    out = []
    for iid, (repo, ordinal) in sorted(
            ordinals.items(), key=lambda kv: (kv[1][0], kv[1][1])):
        row = rows.get(iid, {})
        out.append({
            "run_id": run_dir.name, "repo": repo, "ordinal": ordinal,
            "instance_id": iid,
            "cost_usd": row.get("cost_usd"),
            "wall_ms": row.get("wall_ms"),
            "resolved": int(resolution.get(iid, 0.0)),
            "resolution": iid in resolution,
            "inject_bytes": row.get("semantix_inject_bytes"),
        })
    return out


def format_track_b(per_run: list[tuple[Path, list[dict]]]) -> str:
    non_empty = [(p, rows) for p, rows in per_run if rows]
    if not non_empty:
        return ("_no grouped memory-on runs with a per-repo course_ — Track B "
                "needs `--semantix-memory on --protocol grouped` and ≥2 "
                "same-repo instances per repo.")
    chunks = []
    for run_dir, rows in non_empty:
        chunks.append(f"### {run_dir.name}")
        by_repo: dict[str, list[dict]] = {}
        for row in rows:
            by_repo.setdefault(row["repo"], []).append(row)
        for repo, items in by_repo.items():
            first, second = items[:len(items) // 2], items[len(items) // 2:]
            f_cost = statistics.fmean([x["cost_usd"] for x in first if x["cost_usd"] is not None]) if first else None
            s_cost = statistics.fmean([x["cost_usd"] for x in second if x["cost_usd"] is not None]) if second else None
            resolved = sum(x["resolved"] for x in items)
            chunks.append(f"**{repo}** — n={len(items)} resolved={resolved} "
                          f"mean cost 1st half={f_cost:.4f}$ 2nd half={s_cost:.4f}$")
            header = "| # | instance_id | cost USD | resolved | inject bytes |"
            sep = "| --- | --- | --- | --- | --- |"
            chunks.append(header)
            chunks.append(sep)
            for row in items:
                cost = f"{row['cost_usd']:.4f}" if row["cost_usd"] is not None else "—"
                inj = row["inject_bytes"] if row["inject_bytes"] is not None else "—"
                res = "y" if row["resolved"] else ("n" if row["resolution"] else "?")
                chunks.append(f"| {row['ordinal']} | {row['instance_id']} | {cost} | {res} | {inj} |")
        chunks.append("")
    chunks.append("> Track B uses the grouped (non-standard) protocol: same-repo "
                  "instances share a slice store in a fixed course. Never compare "
                  "these numbers against leaderboards (Issue #326 §二).")
    return "\n".join(chunks)


# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------


def collect(run_dirs: list[Path], dataset_rows: list[dict]) -> dict:
    track_a = []
    track_b_per_run = []
    for run_dir in run_dirs:
        track_a.append(track_a_row(run_dir))
        b = track_b_rows(run_dir, dataset_rows)
        if b:
            track_b_per_run.append((run_dir, b))
    return {"track_a": track_a, "track_b": track_b_per_run}


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--runs", nargs="+", required=True)
    ap.add_argument("--dataset", required=True, help="local SWE-bench Verified jsonl "
                    "(needed to recompute Track B course ordinals)")
    ap.add_argument("--format", default="md", choices=["md", "json"])
    ap.add_argument("--out-dir", default="",
                    help="write report.md/report.json/track_b.csv here instead of stdout")
    args = ap.parse_args()

    dataset_rows = []
    with open(Path(args.dataset), encoding="utf-8") as handle:
        dataset_rows = [json.loads(line) for line in handle if line.strip()]
    result = collect([Path(p) for p in args.runs], dataset_rows)

    if args.format == "json":
        payload = json.dumps(result, indent=2, ensure_ascii=False, default=str)
        if args.out_dir:
            out = Path(args.out_dir)
            out.mkdir(parents=True, exist_ok=True)
            (out / "report.json").write_text(payload, encoding="utf-8")
        else:
            print(payload)
        return

    md = "\n".join([
        "# Issue #326 SWE-bench Track A / Track B",
        "",
        "## Track A — capability (standard protocol, official verdict)",
        "",
        format_track_a(result["track_a"]),
        "",
        "## Track B — cost curve (grouped protocol, non-standard)",
        "",
        format_track_b(result["track_b"]),
        "",
    ])
    if args.out_dir:
        out = Path(args.out_dir)
        out.mkdir(parents=True, exist_ok=True)
        (out / "report.md").write_text(md, encoding="utf-8")
        (out / "report.json").write_text(
            json.dumps(result, indent=2, ensure_ascii=False, default=str), encoding="utf-8")
        rows = [r for _, rr in result["track_b"] for r in rr]
        if rows:
            import csv
            with open(out / "track_b.csv", "w", newline="", encoding="utf-8") as fh:
                writer = csv.DictWriter(fh, fieldnames=list(rows[0].keys()))
                writer.writeheader()
                writer.writerows(rows)
        print(f"wrote {out}/report.md, report.json"
              + (", track_b.csv" if rows else ""))
    else:
        print(md)


if __name__ == "__main__":
    main()
