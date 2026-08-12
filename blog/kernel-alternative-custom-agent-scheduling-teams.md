---
title: "Team Decision Record: Buy a Kernel Layer or Keep the Custom Glue?"
description: "A team decision record comparing shared kernel policy with custom per-harness scheduling and memory glue."
updated: 2026-08-12
group: "Scheduling & Harness"
order: 303
---

# Team Decision Record: Buy a Kernel Layer or Keep the Custom Glue?

**Decision status:** pilot, with exit criteria.

## Context

Two agent harnesses need project-scoped reuse. Each currently has ad hoc transcript search and no shared evaluation protocol. The team can consolidate that logic in a framework-neutral kernel or continue maintaining separate integrations.

## Option A: keep custom glue

This minimizes new dependencies and preserves native harness behavior. It is the better choice when requirements differ sharply or only one harness matters.

## Option B: adopt Semantix as a local side layer

The team gains one JSONL ingest format, P/T/R extraction, BM25/vector/hybrid retrieval, deterministic injection, verification tooling, and local storage.

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search --query "fix the failing Go test" --db .semantix/project.db --retriever hybrid
semantix inject --query "fix the failing Go test" --db .semantix/project.db
semantix verify --session ./sessions --project demo > eval.tsv
```

The cost is process integration, mapping harness events, and ownership of a young project.

## Pilot exit criteria

1. At least 500 slices from representative sessions.
2. Human-labeled top-result relevance of at least 70%.
3. No cross-project retrieval in negative tests.
4. Task success does not decline with injection enabled.
5. The team can disable the layer without blocking either harness.

## Decision rationale

The repository passes mechanism-level tests and provides a runnable CLI. The real-data relevance gate remains open. A time-boxed pilot is therefore justified; a platform-wide mandate is not. If two extraction-granularity trials remain below the relevance gate, the project’s own M0 decision rule calls for a stop/review.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
