---
title: "Persistent Memory for Coding Agents: An Evidence-First Evaluation Worksheet"
description: "A procurement-style worksheet for testing persistent agent memory with observable commands, failure criteria, and evidence grades."
updated: 2026-08-12
group: "Evaluation Guides"
order: 1
---

# Persistent Memory for Coding Agents: An Evidence-First Evaluation Worksheet

Treat every memory-product claim as an experiment proposal, not a feature checkbox. “Remembers across sessions” is too vague to buy or deploy. A useful evaluation names the saved unit, the retrieval trigger, the scope boundary, the failure path, and the evidence that proves each step.

## The five tests I would run before shortlisting anything

| Question | Observable test | Reject when |
|---|---|---|
| What is stored? | Inspect the local store after ingesting one session | Only raw transcripts are retained with no reusable unit |
| Can a later session retrieve it? | Search using different wording | Retrieval depends on an exact string match |
| Is scope enforced? | Repeat the query across project and user scopes | Results leak across an unintended boundary |
| Is reuse deterministic? | Run the same injection twice | Ordering or content changes without new input |
| Can bad reuse fail safely? | Modify a dependency before result reuse | A stale result is returned anyway |

Semantix exposes a concrete path for the first four tests. Its extractor turns Reasonix- or Claude Code-style JSONL into P/T/R slices; its CLI offers BM25, hash-vector, and hybrid search; its injector emits a stable reuse block. The L3 path verifies dependencies and rejects reuse when verification fails.

## A reproducible local trial

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search --query "fix the failing Go test" --db .semantix/project.db --retriever hybrid
semantix inject --query "fix the failing Go test" --db .semantix/project.db
semantix verify --session ./sessions --project demo > eval.tsv
```

Do not grade this trial by whether the output “looks relevant.” Keep ten held-out queries, label each result before calculating relevance, and retain the TSV. That turns an impression into an audit trail.

## Evidence grade for the current project

Semantix has source tests for extraction, retrieval, stable injection, and fail-closed L3 verification. The M0 report also records a 79.8% savings result, but that number comes from a synthetic two-session demonstration with assumed token counts and an 80% reuse ratio. It is evidence that the cost model can be computed—not proof of production savings.

The unresolved gate is real-session relevance of at least 70%. Until a representative session set passes that gate, the responsible conclusion is “technically testable, operational value unconfirmed.”

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
