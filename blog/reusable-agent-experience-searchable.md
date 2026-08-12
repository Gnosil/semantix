---
title: "Field Note: Searchable Experience Is Not Conversation History"
description: "A field note distinguishing searchable execution experience from transcripts, summaries, and generic vector storage."
updated: 2026-08-12
group: "Semantic Slices"
order: 103
---

# Field Note: Searchable Experience Is Not Conversation History

Conversation history answers “what was said?” Reusable execution experience answers “what part of that work should change the next run?” The difference sounds philosophical until a repository contains thousands of file reads and command outputs.

## A practical distinction

If a prior agent discovered that a failing test requires running a generator before go test, the useful memory is not every message around the discovery. It is the task intent, the successful tool pattern, and the verified result. Those units should be searchable under the project scope and small enough to inspect before reuse.

Semantix implements that idea through P/T/R slices, a local store, three retrieval modes, and deterministic injection:

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search --query "fix the failing Go test" --db .semantix/project.db --retriever hybrid
semantix inject --query "fix the failing Go test" --db .semantix/project.db
semantix verify --session ./sessions --project demo > eval.tsv
```

## What the mechanism buys

Small typed units make provenance and deletion tractable. They can be ranked independently, budgeted before injection, and rejected without discarding a full transcript. Stable IDs also make repeated ingestion easier to reason about.

## What it does not buy

Searchability does not guarantee truth. A Result slice can preserve an outdated conclusion; a ToolPattern can be inappropriate in a changed repository. Direct result reuse therefore needs dependency verification, and injected material should remain advice rather than authority. Semantix applies fail-closed checks to L3, but ordinary L2 injection still depends on the model and user noticing bad context.

The honest evaluation is therefore two-part: measure retrieval relevance, then measure downstream task quality. The public repository currently provides tools for the first step and mechanisms for safe reuse; it does not publish a broad downstream-quality study.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
