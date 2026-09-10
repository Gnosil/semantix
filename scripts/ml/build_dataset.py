#!/usr/bin/env python3
"""Assemble the reranker dataset (spec §4).

Inputs: a `semantix export` slice JSONL, the gateway retrieval events log,
and optionally a synth_queries.py output. Writes train.jsonl /
heldout.jsonl of {"query","slice_id","text","label","source","at"} rows.

  uv run python build_dataset.py \
      --slices export.jsonl --events events.jsonl --synth synth.jsonl \
      --out datasets/r1
"""

import argparse
import dataclasses
import json
from pathlib import Path

from semantix_ml.dataset import (
    load_slices,
    pairs_from_events,
    pairs_from_synthetic,
    time_split,
)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--slices", required=True)
    ap.add_argument("--events", default=None)
    ap.add_argument("--synth", default=None)
    ap.add_argument("--out", required=True)
    ap.add_argument("--n-neg", type=int, default=2)
    ap.add_argument("--seed", type=int, default=42)
    ap.add_argument("--holdout-frac", type=float, default=0.2)
    ap.add_argument("--synth-holdout-frac", type=float, default=0.1)
    args = ap.parse_args()

    slices = load_slices(args.slices)
    pairs = []
    if args.events and Path(args.events).exists():
        pairs += pairs_from_events(args.events, slices)
    if args.synth and Path(args.synth).exists():
        pairs += pairs_from_synthetic(args.synth, slices, n_neg=args.n_neg, seed=args.seed)
    if not pairs:
        raise SystemExit("build_dataset: no pairs produced (empty inputs?)")

    train, heldout = time_split(
        pairs, holdout_frac=args.holdout_frac, synth_holdout_frac=args.synth_holdout_frac
    )
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    for name, rows in (("train", train), ("heldout", heldout)):
        with open(out / f"{name}.jsonl", "w", encoding="utf-8") as f:
            for p in rows:
                f.write(json.dumps(dataclasses.asdict(p), ensure_ascii=False) + "\n")
    by_src = {}
    for p in pairs:
        by_src[p.source] = by_src.get(p.source, 0) + 1
    print(
        f"build_dataset: slices={len(slices)} pairs={len(pairs)} {by_src} "
        f"train={len(train)} heldout={len(heldout)} -> {out}"
    )


if __name__ == "__main__":
    main()
