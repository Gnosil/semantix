---
title: "Go Builder’s Guide: Add Local Memory Without Choosing a Framework"
description: "A Go-focused builder guide for integrating local semantic memory through a CLI and stable data contracts."
updated: 2026-08-12
group: "Go & Framework Independence"
order: 401
---

# Go Builder’s Guide: Add Local Memory Without Choosing a Framework

If your agent stack is already written in Go—or merely needs a portable local binary—the smallest integration surface is a process plus files. Semantix builds as one binary and uses JSONL and a local database rather than requiring a framework SDK.

## Build and inspect

```bash
go build -o semantix ./cmd/semantix
go vet ./...
go test ./...
```

The code uses Go interfaces for extractors, stores, indexes, embedders, and verification. The default hash embedder and in-memory vector index have no external runtime service. This keeps the first trial reproducible.

## Integrate through contracts

Emit session JSONL with role, content, and tool_calls. Call extraction at a session boundary. Query before a new task and insert the returned marked block into a controlled prompt location.

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search --query "fix the failing Go test" --db .semantix/project.db --retriever hybrid
semantix inject --query "fix the failing Go test" --db .semantix/project.db
semantix verify --session ./sessions --project demo > eval.tsv
```

Use exit codes and stderr as part of the contract. Treat a retrieval failure as an optimization miss, not a reason to stop the agent.

## Tradeoffs

A CLI boundary is framework-neutral and easy to replace. It also adds process startup, file lifecycle, and schema-version responsibilities. The built-in vectors are deterministic and local, not a substitute for a trained embedding model when deeper semantic discrimination is required.

## Evidence status

The repository builds and tests these paths and publishes six v0.2.0 platform artifacts in its quickstart. Compatibility with every Go agent framework is not demonstrated. The accurate claim is that integration is framework-agnostic at the file/CLI boundary.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
