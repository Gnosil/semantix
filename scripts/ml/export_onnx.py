#!/usr/bin/env python3
"""Export the HF reranker checkpoint to ONNX (spec §3 G2; `train` extras).

Writes model.onnx + tokenizer.json into --out, the exact layout
rerank_server.py and eval_retrieval.py load.

  uv run --extra train python export_onnx.py --hf staging/ckpt/hf --out staging/ckpt/onnx
"""

import argparse
import shutil
from pathlib import Path


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--hf", required=True, help="HF checkpoint dir from train_reranker.py")
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    from optimum.onnxruntime import ORTModelForSequenceClassification
    from transformers import AutoTokenizer

    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    print(f"export_onnx: exporting {args.hf} ...", flush=True)
    ort_model = ORTModelForSequenceClassification.from_pretrained(args.hf, export=True)
    ort_model.save_pretrained(out)

    # rerank_server loads the fast tokenizer.json directly (no transformers
    # at serve time); make sure it exists in the output.
    if not (out / "tokenizer.json").exists():
        tok = AutoTokenizer.from_pretrained(args.hf)
        tok.save_pretrained(out)
    if not (out / "tokenizer.json").exists():
        raise SystemExit("export_onnx: tokenizer.json missing after export")

    # Normalize the model filename for the loaders.
    if not (out / "model.onnx").exists():
        cands = sorted(out.glob("*.onnx"))
        if not cands:
            raise SystemExit("export_onnx: no .onnx produced")
        shutil.move(str(cands[0]), out / "model.onnx")
    print(f"export_onnx: -> {out}")


if __name__ == "__main__":
    main()
