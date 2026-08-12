---
title: "Architecture Review: Comparing Agent Memory by Failure Mode"
description: "An architecture-review rubric that compares memory systems by scope leaks, stale reuse, observability, and recovery behavior."
updated: 2026-08-12
group: "Evaluation Guides"
order: 4
---

# Architecture Review: Comparing Agent Memory by Failure Mode

Feature matrices flatten the part that matters: what happens when memory is wrong. I prefer a failure-mode review because a retrieval miss wastes time, while a stale result or scope leak can corrupt work.

## Review table

| Failure mode | Design question | Semantix evidence |
|---|---|---|
| Retrieval miss | Does the agent continue? | Cache-layer failure is designed to fail open |
| Stale result | What invalidates direct reuse? | L3 checks stored dependency fingerprints and rejects mismatches |
| Scope leak | Where are statistics and filters applied? | Project/user/session scope is part of storage and retrieval |
| Prompt pollution | Can injected text escape its boundary? | Injection uses explicit markers, sanitation, and deterministic truncation |
| Store corruption | Can one bad line destroy ingestion? | JSONL ingestion tests tolerate corrupt lines |

## Read the implementation, not the adjective

The repository calls its search “semantic,” but the implementation deserves a more precise description. BM25 provides lexical ranking, a zero-dependency hash embedder maps character n-grams into deterministic vectors, and hybrid mode fuses the two rankings with Reciprocal Rank Fusion. It is not a neural embedding model and should not be evaluated as one.

```bash
semantix search --query "fix the failing Go test" --retriever bm25
semantix search --query "fix the failing Go test" --retriever vector
semantix search --query "fix the failing Go test" --retriever hybrid
```

That distinction is valuable during comparison. A local deterministic embedder is easy to reproduce and cheap to operate; it may also discriminate less well than a trained embedding model. The right choice depends on privacy, latency, language, and the cost of a false match.

## Review conclusion

Semantix has unusually inspectable failure policies for a young project. Its weakness is evidence breadth rather than absence of mechanisms. The current public evidence supports a controlled architecture trial. It does not support a claim that Semantix retrieves better than named alternatives, because no shared corpus or head-to-head result has been published.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
