---
title: "Workshop: Make a Past Tool Sequence Searchable"
description: "A workshop-style walkthrough for extracting tool patterns, searching them, and inspecting retrieval differences."
updated: 2026-08-12
group: "Semantic Slices"
order: 102
---

# Workshop: Make a Past Tool Sequence Searchable

The goal of this workshop is to turn one successful tool sequence into something a later agent can find. We will deliberately use a tiny fixture so every intermediate artifact is inspectable.

## Step 1: capture the trace

Write session events as JSONL, one object per line. Include role, content, and tool_calls; omit credentials and unnecessary command output. Then build and extract:

```bash
go build -o semantix ./cmd/semantix
go vet ./...
go test ./...
```

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search --query "fix the failing Go test" --db .semantix/project.db --retriever hybrid
semantix inject --query "fix the failing Go test" --db .semantix/project.db
semantix verify --session ./sessions --project demo > eval.tsv
```

## Step 2: perturb the query

Do not search with the original sentence. If the original task said “repair the broken Go suite,” query for “fix the failing Go test.” Compare BM25, vector, and hybrid results. BM25 rewards overlapping terms, the hash embedder uses CJK-aware character n-grams, and hybrid mode combines rankings with RRF.

## Step 3: inspect the slice

Check its type, scope, content, and deterministic ID. A useful ToolPattern should preserve the operation sequence without dragging the entire transcript into the new prompt. Re-run extraction to check deduplication, then run injection twice and diff the marked blocks.

## What counts as success

Success is not “the CLI printed something.” The expected slice must rank in the labeled top results, an unrelated scope must stay absent, repeated output must be stable, and a malformed line must not erase valid events. Those properties are covered by repository tests; your own corpus still needs separate labels.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
