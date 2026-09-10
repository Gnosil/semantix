"""Two-arm comparison aggregates (spec §5 online anchoring, M6).

arm_summary reads one arm directory (usage.jsonl, events.jsonl,
gateway.jsonl) and returns cost + retrieval-behavior + acceptance
aggregates; render_comparison lays two summaries side by side as a
markdown table for the M6 evidence report.
"""

import json
from pathlib import Path

_SKELETON = {
    "requests": 0,
    "tokens_in": 0,
    "tokens_out": 0,
    "injected_tokens": 0,
    "slice_hits": 0,
    "events": 0,
    "admitted": 0,
    "candidates": 0,
    "acceptance": None,
}


def _read_jsonl(path):
    p = Path(path)
    if not p.exists():
        return
    with open(p, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if line:
                try:
                    yield json.loads(line)
                except json.JSONDecodeError:
                    continue


def arm_summary(arm_dir):
    arm_dir = Path(arm_dir)
    s = dict(_SKELETON)
    for row in _read_jsonl(arm_dir / "usage.jsonl"):
        s["requests"] += 1
        s["tokens_in"] += int(row.get("tokens_in") or 0)
        s["tokens_out"] += int(row.get("tokens_out") or 0)
        s["injected_tokens"] += int(row.get("injected_tokens") or 0)
        s["slice_hits"] += int(row.get("slice_hits") or 0)
    for ev in _read_jsonl(arm_dir / "events.jsonl"):
        s["events"] += 1
        for d in ev.get("decisions") or []:
            s["candidates"] += 1
            if d.get("admitted"):
                s["admitted"] += 1
    injected = rejected = 0
    for row in _read_jsonl(arm_dir / "gateway.jsonl"):
        stats = row.get("Stats") or {}
        injected += int(stats.get("Injected") or 0)
        rejected += int(stats.get("Rejected") or 0)
    if injected + rejected > 0:
        s["acceptance"] = injected / (injected + rejected)
    return s


def _fmt(v):
    if v is None:
        return "—"
    if isinstance(v, float):
        return f"{v:.3f}"
    return str(v)


def render_comparison(on, off):
    lines = [
        "| metric | on | off |",
        "|---|---|---|",
    ]
    for key in _SKELETON:
        lines.append(f"| {key} | {_fmt(on.get(key))} | {_fmt(off.get(key))} |")
    return "\n".join(lines) + "\n"
