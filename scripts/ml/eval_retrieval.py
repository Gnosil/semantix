#!/usr/bin/env python3
"""Held-out evaluation + release gate (spec §5).

Groups held-out pairs by query (positives = label-1 slice ids), ranks each
query's candidate texts with the scorer under test, and reports mean
NDCG@5 / MRR@5 plus the gate decision against a reference metrics file.

Scorers:
  --model <onnx-dir>   score with the exported reranker (needs `serve` extras)
  --baseline           token-overlap ranking — the no-reranker reference
                       (identity of the lexical route, dependency-free)

  uv run python eval_retrieval.py --data datasets/r1/heldout.jsonl --baseline \
      --report reports/baseline.json
  uv run --extra serve python eval_retrieval.py --data datasets/r1/heldout.jsonl \
      --model staging/ckpt/onnx --reference train/current/metrics.json \
      --report reports/candidate.json
"""

import argparse
import json
from collections import defaultdict
from pathlib import Path

from semantix_ml.gate import decide
from semantix_ml.metrics import evaluate_ranking


def load_pairs(path):
    rows = []
    with open(path, encoding="utf-8") as f:
        for line in f:
            if line.strip():
                rows.append(json.loads(line))
    return rows


def overlap_score(query, text):
    q = set(query.lower().split())
    t = set(text.lower().split())
    if not q:
        return 0.0
    return len(q & t) / len(q)


class OnnxScorer:
    def __init__(self, model_dir):
        import numpy as np
        import onnxruntime as ort
        from tokenizers import Tokenizer

        self.np = np
        self.tok = Tokenizer.from_file(str(Path(model_dir) / "tokenizer.json"))
        self.tok.enable_truncation(max_length=512)
        self.sess = ort.InferenceSession(str(Path(model_dir) / "model.onnx"))
        self.input_names = {i.name for i in self.sess.get_inputs()}

    def score(self, query, texts):
        np = self.np
        encs = [self.tok.encode(query, t) for t in texts]
        maxlen = max(len(e.ids) for e in encs)
        ids = np.array([e.ids + [0] * (maxlen - len(e.ids)) for e in encs], dtype=np.int64)
        mask = np.array(
            [e.attention_mask + [0] * (maxlen - len(e.attention_mask)) for e in encs],
            dtype=np.int64,
        )
        feeds = {"input_ids": ids, "attention_mask": mask}
        if "token_type_ids" in self.input_names:
            feeds["token_type_ids"] = np.zeros_like(ids)
        logits = self.sess.run(None, feeds)[0].reshape(-1)
        return [1.0 / (1.0 + float(np.exp(-x))) for x in logits]


def rank_queries(rows, scorer):
    groups = defaultdict(lambda: {"cands": [], "relevant": set()})
    for r in rows:
        g = groups[r["query"]]
        g["cands"].append((r["slice_id"], r["text"]))
        if r["label"] == 1:
            g["relevant"].add(r["slice_id"])
    rankings = []
    for query, g in sorted(groups.items()):
        # A query with no positive or no negative can't measure ranking.
        if not g["relevant"] or len(g["cands"]) < 2:
            continue
        if scorer is None:
            scored = [(overlap_score(query, t), sid) for sid, t in g["cands"]]
        else:
            scores = scorer.score(query, [t for _, t in g["cands"]])
            scored = list(zip(scores, [sid for sid, _ in g["cands"]]))
        scored.sort(key=lambda x: (-x[0], x[1]))
        rankings.append(([sid for _, sid in scored], g["relevant"]))
    return rankings


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--data", required=True)
    ap.add_argument("--model", default=None, help="ONNX model dir (exclusive with --baseline)")
    ap.add_argument("--baseline", action="store_true")
    ap.add_argument("--reference", default=None, help="metrics.json to gate against")
    ap.add_argument("--report", default=None, help="write metrics(+gate) JSON here")
    ap.add_argument("--k", type=int, default=5)
    args = ap.parse_args()
    if bool(args.model) == args.baseline:
        raise SystemExit("eval_retrieval: pass exactly one of --model / --baseline")

    scorer = OnnxScorer(args.model) if args.model else None
    metrics = evaluate_ranking(rank_queries(load_pairs(args.data), scorer), k=args.k)
    out = {"metrics": metrics, "scorer": args.model or "baseline", "k": args.k}

    if args.reference:
        reference = json.loads(Path(args.reference).read_text())
        reference = reference.get("metrics", reference)
        d = decide(metrics, reference)
        out["gate"] = {"publish": d.publish, "reason": d.reason, "reference": reference}
        print(f"gate: {'PUBLISH' if d.publish else 'REJECTED'} — {d.reason}")
    print(f"metrics: ndcg@{args.k}={metrics['ndcg']:.4f} mrr@{args.k}={metrics['mrr']:.4f} n={metrics['n']}")

    if args.report:
        Path(args.report).parent.mkdir(parents=True, exist_ok=True)
        Path(args.report).write_text(json.dumps(out, indent=2) + "\n")
    if args.reference and not out["gate"]["publish"]:
        raise SystemExit(3)  # distinct exit for "gate rejected"


if __name__ == "__main__":
    main()
