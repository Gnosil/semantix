#!/usr/bin/env python3
"""Cold-start synthetic query generation (spec §4, GPL-style).

For each slice, asks an OpenAI-compatible chat endpoint for 1-3 retrieval
queries a developer might type that this slice should answer. Output JSONL:
{"query","slice_id"}. One-shot bootstrap tooling — quality is checked
downstream by the held-out gate, never assumed (I-4). Stdlib-only HTTP.

Endpoint from env: SEMANTIX_SYNTH_BASE_URL, SEMANTIX_SYNTH_MODEL,
SEMANTIX_SYNTH_API_KEY (same shape as the embed env triplet).

  SEMANTIX_SYNTH_BASE_URL=https://qianfan.baidubce.com/v2 \
  SEMANTIX_SYNTH_MODEL=ernie-4.5-turbo-128k \
  SEMANTIX_SYNTH_API_KEY=... \
  uv run python synth_queries.py --slices export.jsonl --out synth.jsonl
"""

import argparse
import json
import os
import time
import urllib.request
from pathlib import Path

from semantix_ml.dataset import load_slices

PROMPT = """你是检索评测数据构造器。下面是一个知识切片的内容。请生成 {n} 个开发者可能输入的检索 query，要求：能被这个切片回答、口吻自然简短（关键词或短句）、与切片语言一致（中文切片出中文 query）。只输出 JSON 数组，例如 ["query1", "query2"]。

切片内容：
{content}"""


def chat(base_url, model, api_key, content, n):
    req = urllib.request.Request(
        base_url.rstrip("/") + "/chat/completions",
        data=json.dumps(
            {
                "model": model,
                "messages": [{"role": "user", "content": PROMPT.format(n=n, content=content[:2000])}],
                "temperature": 0.7,
            }
        ).encode(),
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {api_key}"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=60) as resp:
        body = json.load(resp)
    text = body["choices"][0]["message"]["content"].strip()
    # Tolerate markdown fences around the array.
    if text.startswith("```"):
        text = text.strip("`")
        text = text[text.find("[") :]
    queries = json.loads(text[text.find("[") : text.rfind("]") + 1])
    return [q.strip() for q in queries if isinstance(q, str) and q.strip()]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--slices", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--per-slice", type=int, default=3)
    ap.add_argument("--min-content-chars", type=int, default=24)
    ap.add_argument("--limit", type=int, default=0, help="stop after N slices (0 = all)")
    args = ap.parse_args()

    base_url = os.environ.get("SEMANTIX_SYNTH_BASE_URL", "")
    model = os.environ.get("SEMANTIX_SYNTH_MODEL", "")
    api_key = os.environ.get("SEMANTIX_SYNTH_API_KEY", "")
    if not (base_url and model and api_key):
        raise SystemExit(
            "synth_queries: set SEMANTIX_SYNTH_BASE_URL / SEMANTIX_SYNTH_MODEL / SEMANTIX_SYNTH_API_KEY"
        )

    slices = load_slices(args.slices)
    done = set()
    out_path = Path(args.out)
    if out_path.exists():  # resume: don't re-pay for generated slices
        with open(out_path, encoding="utf-8") as f:
            for line in f:
                if line.strip():
                    done.add(json.loads(line)["slice_id"])

    n_slices = n_queries = 0
    with open(out_path, "a", encoding="utf-8") as f:
        for sid, s in sorted(slices.items()):
            if sid in done or len(s["text"]) < args.min_content_chars:
                continue
            if args.limit and n_slices >= args.limit:
                break
            try:
                queries = chat(base_url, model, api_key, s["text"], args.per_slice)
            except Exception as e:
                print(f"synth_queries: {sid}: {e} (skipped)", flush=True)
                time.sleep(2)
                continue
            for q in queries[: args.per_slice]:
                f.write(json.dumps({"query": q, "slice_id": sid}, ensure_ascii=False) + "\n")
                n_queries += 1
            n_slices += 1
            if n_slices % 20 == 0:
                print(f"synth_queries: {n_slices} slices, {n_queries} queries", flush=True)
    print(f"synth_queries: done — {n_slices} slices, {n_queries} queries -> {out_path}")


if __name__ == "__main__":
    main()
