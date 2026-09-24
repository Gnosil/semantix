#!/usr/bin/env python3
"""Loopback rerank sidecar (spec §3 B; `serve` extras).

  POST /rerank {"query": "...", "documents": [{"id","text"},...], "top_n": N}
  -> {"results": [{"id","score"},...]}   score = sigmoid(logit) ∈ [0,1], desc

Serves train/current (the registry symlink); SIGHUP re-resolves and reloads
it, so publish/rollback take effect without dropping the listener — model
switches land on a request boundary (freeze-window discipline, spec §6).
Loopback only, mirroring embed_server.py: no auth by design, never bind
beyond 127.0.0.1.
"""

import argparse
import json
import math
import signal
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

STATE = {"scorer": None, "version": None}
LOCK = threading.Lock()
MODEL_DIR = None


def load_scorer():
    import numpy as np
    import onnxruntime as ort
    from tokenizers import Tokenizer

    d = Path(MODEL_DIR).resolve()

    class Scorer:
        version = d.name

        def __init__(self):
            self.np = np
            self.tok = Tokenizer.from_file(str(d / "tokenizer.json"))
            self.tok.enable_truncation(max_length=512)
            self.sess = ort.InferenceSession(str(d / "model.onnx"))
            self.inputs = {i.name for i in self.sess.get_inputs()}

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
            if "token_type_ids" in self.inputs:
                feeds["token_type_ids"] = np.zeros_like(ids)
            logits = self.sess.run(None, feeds)[0].reshape(-1)
            return [1.0 / (1.0 + math.exp(-float(x))) for x in logits]

    return Scorer()


def reload_scorer(*_):
    scorer = load_scorer()
    with LOCK:
        STATE["scorer"] = scorer
    print(f"rerank_server: loaded {scorer.version}", flush=True)


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        if not self.path.endswith("/rerank"):
            self.send_error(404)
            return
        n = int(self.headers.get("Content-Length", 0))
        try:
            body = json.loads(self.rfile.read(n) or b"{}")
            query = body.get("query") or ""
            docs = body.get("documents") or []
            if not query or not docs:
                raise ValueError("query and documents are required")
            with LOCK:
                scorer = STATE["scorer"]
            scores = scorer.score(query, [d.get("text") or "" for d in docs])
            ranked = sorted(
                ({"id": d.get("id"), "score": s} for d, s in zip(docs, scores)),
                key=lambda r: (-r["score"], r["id"] or ""),
            )
            top_n = body.get("top_n") or len(ranked)
            payload = json.dumps({"results": ranked[: int(top_n)]}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
        except Exception as e:  # any failure → 500; the gateway fail-softs
            self.send_error(500, str(e))

    def log_message(self, fmt, *args):
        pass


def main():
    global MODEL_DIR
    ap = argparse.ArgumentParser()
    ap.add_argument("--addr", default="127.0.0.1:8689")
    ap.add_argument("--model-dir", required=True, help="ONNX dir or the train/current symlink")
    args = ap.parse_args()
    MODEL_DIR = args.model_dir
    reload_scorer()
    signal.signal(signal.SIGHUP, lambda *_: threading.Thread(target=reload_scorer).start())
    host, _, port = args.addr.rpartition(":")
    host = host or "127.0.0.1"
    if host not in ("127.0.0.1", "localhost", "::1"):
        raise SystemExit("rerank_server: refusing to bind beyond loopback")
    httpd = ThreadingHTTPServer((host, int(port)), Handler)
    print(f"rerank_server: ready on {host}:{port} (SIGHUP reloads)", flush=True)
    httpd.serve_forever()


if __name__ == "__main__":
    main()
