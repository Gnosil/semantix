"""Training-pair construction (spec §4).

Sources:
- retrieval events (gateway [retrieval] events_log): admitted candidates
  are positives; candidates rejected for *relevance* reasons (zone_miss,
  below_min_score, coverage_low) are hard negatives. Governance rejections
  (budget, type_not_allowed, library gates, origin floor, ...) say nothing
  about relevance and are excluded.
- synthetic pairs (synth_queries.py output): the generated query is a
  positive for its source slice; negatives are sampled from other slices.

Slice content joins in from a `semantix export` JSONL (Content is base64 —
Go []byte JSON encoding).
"""

import base64
import hashlib
import json
import random
from dataclasses import dataclass

# Relevance-informative rejection reasons (inject.CandidateDecision.Reason).
HARD_NEGATIVE_REASONS = {"zone_miss", "below_min_score", "coverage_low"}


@dataclass
class Pair:
    query: str
    slice_id: str
    text: str
    label: int
    source: str  # "events" | "synthetic"
    at: int


def _read_jsonl(path):
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if line:
                yield json.loads(line)


def load_slices(path):
    """Slice export JSONL → {id: {"text": decoded content, "raw": row}}."""
    out = {}
    for row in _read_jsonl(path):
        sid = row.get("ID")
        if not sid:
            continue
        content = row.get("Content") or ""
        try:
            text = base64.b64decode(content).decode("utf-8", errors="replace")
        except Exception:
            text = ""
        out[sid] = {"text": text, "raw": row}
    return out


def pairs_from_events(events_path, slices):
    pairs = []
    for ev in _read_jsonl(events_path):
        query = ev.get("query") or ""
        at = int(ev.get("at") or 0)
        if not query:
            continue
        for d in ev.get("decisions") or []:
            sid = d.get("id")
            if not sid or sid not in slices:
                continue  # evicted since logging — no content to train on
            reason = d.get("reason") or ""
            if d.get("admitted"):
                label = 1
            elif reason in HARD_NEGATIVE_REASONS:
                label = 0
            else:
                continue  # governance rejection: not a relevance signal
            pairs.append(
                Pair(
                    query=query,
                    slice_id=sid,
                    text=slices[sid]["text"],
                    label=label,
                    source="events",
                    at=at,
                )
            )
    return pairs


def pairs_from_synthetic(synth_path, slices, n_neg=2, seed=42):
    rng = random.Random(seed)
    all_ids = sorted(slices)
    pairs = []
    for row in _read_jsonl(synth_path):
        query = row.get("query") or ""
        sid = row.get("slice_id") or ""
        if not query or sid not in slices:
            continue
        pairs.append(
            Pair(query=query, slice_id=sid, text=slices[sid]["text"], label=1, source="synthetic", at=0)
        )
        candidates = [x for x in all_ids if x != sid]
        rng.shuffle(candidates)
        for neg_id in candidates[:n_neg]:
            pairs.append(
                Pair(
                    query=query,
                    slice_id=neg_id,
                    text=slices[neg_id]["text"],
                    label=0,
                    source="synthetic",
                    at=0,
                )
            )
    return pairs


def _synth_holdout(query, frac):
    """Stable hash-based holdout membership: survives re-runs and dataset
    growth (a query never migrates between train and held-out)."""
    h = int.from_bytes(hashlib.sha256(query.encode()).digest()[:8], "big")
    return (h % 10_000) < frac * 10_000


def time_split(pairs, holdout_frac=0.2, synth_holdout_frac=0.1):
    """Events split by time (latest holdout_frac of *events*, so the gate
    always evaluates on data strictly newer than anything trained on);
    synthetic pairs split by stable query hash."""
    events = [p for p in pairs if p.source == "events"]
    synth = [p for p in pairs if p.source != "events"]

    train, heldout = [], []
    if events:
        cutov = sorted({p.at for p in events})
        n_hold = max(1, int(len(cutov) * holdout_frac))
        threshold = cutov[len(cutov) - n_hold]
        for p in events:
            (heldout if p.at >= threshold else train).append(p)
    for p in synth:
        (heldout if _synth_holdout(p.query, synth_holdout_frac) else train).append(p)
    return train, heldout
