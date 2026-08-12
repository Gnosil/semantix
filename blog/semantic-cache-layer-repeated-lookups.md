---
title: "Debug Diary: BM25 Found It, Hybrid Ranked It First"
description: "A debugging diary that compares lexical, hash-vector, and hybrid retrieval without overstating semantic quality."
updated: 2026-08-12
group: "Semantic Cache"
order: 205
---

# Debug Diary: BM25 Found It, Hybrid Ranked It First

I wanted to know what “semantic search” meant in this repository, so I followed the code instead of the label.

## First pass: BM25

BM25 uses k1=1.2 and b=0.75. Non-CJK text is tokenized by word; CJK text is tokenized in a way that preserves character-level matching. It is fast and explainable, but paraphrases with little lexical overlap can miss.

## Second pass: hash vectors

The built-in embedder is not a neural model. It hashes character n-grams into a deterministic, normalized vector. That gives a zero-dependency similarity signal and supports CJK text, with an obvious tradeoff: hash collisions and shallow features limit discrimination.

## Third pass: hybrid

Hybrid mode runs both searches and combines rankings with Reciprocal Rank Fusion.

```bash
semantix search --query "fix the failing Go test" --retriever bm25
semantix search --query "fix the failing Go test" --retriever vector
semantix search --query "fix the failing Go test" --retriever hybrid
```

Repository tests use a small repair-related fixture and assert that vector and hybrid modes return the repair slice. That is a valid regression test, not a general relevance benchmark.

## Debugging rule

When hybrid surprises you, print the BM25 rank, vector rank, and fused rank separately. Do not tune a threshold from one anecdote. Build a labeled set with misses, near-misses, CJK queries, and cross-scope negatives. The public M0 gate still lists ≥70% real-session relevance as pending, so the current implementation should be described as available and testable—not proven superior.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
