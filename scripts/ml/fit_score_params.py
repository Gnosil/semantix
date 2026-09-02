#!/usr/bin/env python3
"""Fit ScoreParams by replay (spec §3 G3).

Takes two slice-export snapshots: t0 (features) and t1 (outcome). A slice's
label is 1 when its LastUsed/Hits/Injected advanced between the snapshots —
"was reused later" — anchored purely on external usage (I-4). One clamped
coordinate step from the currently deployed params (I-3), written as the
snake_case JSON that `semantix gc --score-params` loads.

  uv run python fit_score_params.py --t0 export-t0.jsonl --t1 export-t1.jsonl \
      --current train/current/score_params.json --out train/current/score_params.json
"""

import argparse
import json
import time
from pathlib import Path

from semantix_ml.dataset import load_slices
from semantix_ml.score_params import DEFAULTS, fit


def usage_key(row):
    stats = row.get("Stats") or {}
    return (
        int(stats.get("LastUsed") or 0),
        int(stats.get("Hits") or 0),
        int(stats.get("Injected") or 0),
    )


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--t0", required=True, help="earlier slice export (features)")
    ap.add_argument("--t1", required=True, help="later slice export (outcomes)")
    ap.add_argument("--current", default=None, help="currently deployed score_params.json")
    ap.add_argument("--out", required=True)
    ap.add_argument("--max-rel-step", type=float, default=0.2)
    ap.add_argument("--now", type=int, default=None, help="feature timestamp (default: t0 max LastUsed)")
    args = ap.parse_args()

    t0 = {sid: v["raw"] for sid, v in load_slices(args.t0).items()}
    t1 = {sid: v["raw"] for sid, v in load_slices(args.t1).items()}

    rows, labels = {}, {}
    for sid, row in t0.items():
        later = t1.get(sid)
        if later is None:
            continue  # evicted between snapshots: outcome unknowable, skip
        rows[sid] = row
        labels[sid] = 1 if usage_key(later) > usage_key(row) else 0

    current = dict(DEFAULTS)
    if args.current and Path(args.current).exists():
        current.update(json.loads(Path(args.current).read_text()))

    now = args.now
    if now is None:
        now = max((usage_key(r)[0] for r in rows.values()), default=int(time.time()))

    n_pos = sum(labels.values())
    fitted = fit(rows, labels, now, current, max_rel_step=args.max_rel_step)
    Path(args.out).parent.mkdir(parents=True, exist_ok=True)
    Path(args.out).write_text(json.dumps(fitted, indent=2) + "\n")
    print(
        f"fit_score_params: n={len(rows)} pos={n_pos} neg={len(rows) - n_pos}\n"
        f"  current: {current}\n  fitted:  {fitted}\n  -> {args.out}"
    )


if __name__ == "__main__":
    main()
