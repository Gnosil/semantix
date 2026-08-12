---
title: "Architecture Review: Put Execution Policy Beside the Harness"
description: "An architecture review of the case for a separate execution kernel, including integration and failure costs."
updated: 2026-08-12
group: "Scheduling & Harness"
order: 301
---

# Architecture Review: Put Execution Policy Beside the Harness

The proposal under review is not “replace the agent.” It is “move reusable execution policy into a separate local kernel.” That boundary deserves scrutiny because every new layer adds integration and debugging cost.

## Responsibilities

The harness owns the conversation, model calls, permissions, and user interaction. Semantix accepts session events, extracts reusable slices, searches them, emits a bounded injection block, and records usage/evolution signals. It does not choose a foundation model or act as a vector database service.

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search --query "fix the failing Go test" --db .semantix/project.db --retriever hybrid
semantix inject --query "fix the failing Go test" --db .semantix/project.db
semantix verify --session ./sessions --project demo > eval.tsv
```

## Why the boundary can help

A framework-neutral CLI can serve multiple harnesses and keep session data local. Retrieval can fail open without stopping the agent. Deterministic outputs make integration tests and rollback simpler.

## Why the boundary can hurt

The harness must translate its events into supported JSONL, decide when to extract, and place injection at a stable prompt location. Debugging crosses a process boundary. If the harness already has mature memory and observability, a second policy layer may duplicate work.

## Review decision

Approve a pilot when two harnesses need the same reuse logic or when keeping the logic outside a vendor framework is a hard requirement. Reject it for a single short-lived agent with little repeated work. The repository demonstrates the CLI and kernel mechanisms; it does not yet publish production integration data across multiple independent harnesses.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
