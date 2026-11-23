#!/usr/bin/env python3
"""Summarize complete paired offline memory replay outcomes without model calls."""
import argparse
import json
from pathlib import Path
import re

ARMS = ("off", "current", "revision_aware")
COMPARISONS = (("off", "current"), ("off", "revision_aware"), ("current", "revision_aware"))
ID = re.compile(r"[A-Za-z0-9_.:-]{1,80}\Z")


def measured_int(value):
    return type(value) is int and value >= 0


def validate(document):
    if not isinstance(document, dict) or document.get("kind") not in ("synthetic", "recorded"):
        raise ValueError("kind must be synthetic or recorded")
    tasks = document.get("tasks")
    if not isinstance(tasks, list) or not tasks:
        raise ValueError("expected a nonempty tasks array")
    seen = set()
    for i, task in enumerate(tasks, 1):
        if not isinstance(task, dict):
            raise ValueError(f"invalid task at row {i}")
        key = task.get("id")
        if not isinstance(key, str) or ID.fullmatch(key) is None or key in seen:
            raise ValueError(f"invalid or duplicate anonymous task id at row {i}")
        seen.add(key)
        arms = task.get("arms")
        if not isinstance(arms, dict) or set(arms) != set(ARMS):
            raise ValueError(f"incomplete or mismatched arm set at row {i}")
        for arm in ARMS:
            obs = arms[arm]
            if not isinstance(obs, dict) or type(obs.get("passed")) is not bool:
                raise ValueError(f"missing Boolean passed at row {i}, arm {arm}")
            costs = obs.get("attempt_cost_microusd")
            if costs is not None and (not isinstance(costs, list) or not costs or
                                      any(x is not None and not measured_int(x) for x in costs)):
                raise ValueError(f"invalid cost observation at row {i}, arm {arm}")
            elapsed = obs.get("elapsed_ms")
            if elapsed is not None and not measured_int(elapsed):
                raise ValueError(f"invalid elapsed observation at row {i}, arm {arm}")
    return tasks


def score(document):
    tasks = validate(document)
    n = len(tasks)
    summary = {}
    for arm in ARMS:
        observations = [task["arms"][arm] for task in tasks]
        passed = sum(obs["passed"] for obs in observations)
        totals = []
        elapsed = []
        for obs in observations:
            costs = obs.get("attempt_cost_microusd")
            if costs is not None and None not in costs:
                totals.append(sum(costs))
            if obs.get("elapsed_ms") is not None:
                elapsed.append(obs["elapsed_ms"])
        total = sum(totals) if len(totals) == n else None
        index95 = (95 * n + 99) // 100 - 1  # nearest-rank p95 across all tasks
        summary[arm] = {
            "passed": passed,
            "failed": n - passed,
            "known_cost_tasks": len(totals),
            "total_cost_microusd": total,
            "cost_per_pass_microusd": total / passed if total is not None and passed else None,
            "known_latency_tasks": len(elapsed),
            "p95_elapsed_ms": sorted(elapsed)[index95] if len(elapsed) == n else None,
        }
    comparisons = {}
    for before, after in COMPARISONS:
        fixed, broken = [], []
        for task in tasks:
            a, b = task["arms"][before]["passed"], task["arms"][after]["passed"]
            if not a and b:
                fixed.append(task["id"])
            elif a and not b:
                broken.append(task["id"])
        comparisons[f"{before}_to_{after}"] = {
            "newly_fixed": fixed,
            "newly_broken": broken,
        }
    return {"kind": document["kind"], "paired_tasks": n,
            "arms": summary, "comparisons": comparisons}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("observations", type=Path)
    args = parser.parse_args()
    try:
        print(json.dumps(score(json.loads(args.observations.read_text(encoding="utf-8"))), indent=2))
    except (OSError, ValueError, OverflowError) as exc:
        parser.error(str(exc))


if __name__ == "__main__":
    main()
