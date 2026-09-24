#!/usr/bin/env python3
"""Fine-tune the cross-encoder reranker (spec §3 G2). Needs `train` extras.

Resumes from the current published checkpoint's HF weights when present
(I-3 incremental training); falls back to the base model for the first
round. Saves HF format to --out (export_onnx.py converts it).

  uv run --extra train python train_reranker.py \
      --data datasets/r1/train.jsonl --current train/current \
      --out staging/ckpt/hf --epochs 1
"""

import argparse
import json
from pathlib import Path

DEFAULT_BASE = "BAAI/bge-reranker-v2-m3"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--data", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--current", default=None, help="published checkpoint dir (resume source)")
    ap.add_argument("--base-model", default=DEFAULT_BASE)
    ap.add_argument("--epochs", type=int, default=1)
    ap.add_argument("--batch-size", type=int, default=8)
    ap.add_argument("--lr", type=float, default=2e-5)
    ap.add_argument("--max-length", type=int, default=512)
    args = ap.parse_args()

    import torch
    from sentence_transformers import InputExample
    from sentence_transformers.cross_encoder import CrossEncoder
    from torch.utils.data import DataLoader

    source = args.base_model
    if args.current:
        hf = Path(args.current) / "hf"
        if hf.is_dir():
            source = str(hf)  # incremental: continue from the published weights
    device = "mps" if torch.backends.mps.is_available() else None
    print(f"train_reranker: source={source} device={device or 'auto'}", flush=True)

    samples = []
    with open(args.data, encoding="utf-8") as f:
        for line in f:
            if not line.strip():
                continue
            row = json.loads(line)
            samples.append(InputExample(texts=[row["query"], row["text"]], label=float(row["label"])))
    if not samples:
        raise SystemExit("train_reranker: empty training set")
    print(f"train_reranker: {len(samples)} pairs, {args.epochs} epoch(s)", flush=True)

    model = CrossEncoder(source, num_labels=1, max_length=args.max_length, device=device)
    loader = DataLoader(samples, shuffle=True, batch_size=args.batch_size)
    model.fit(
        train_dataloader=loader,
        epochs=args.epochs,
        warmup_steps=max(10, len(loader) // 10),
        optimizer_params={"lr": args.lr},
        show_progress_bar=True,
    )
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    model.save(str(out))
    print(f"train_reranker: saved HF checkpoint -> {out}")


if __name__ == "__main__":
    main()
