---
title: "Control-Loop Walkthrough: What the Kernel Does Before a Tool Call"
description: "A control-loop walkthrough separating shipped retrieval behavior from scheduling and prefetch direction."
updated: 2026-08-12
group: "Scheduling & Harness"
order: 304
---

# Control-Loop Walkthrough: What the Kernel Does Before a Tool Call

Start with the shipped path, not the roadmap diagram.

## Today’s path

Historical sessions are captured as JSONL. The extractor creates typed slices. Search ranks them with BM25, deterministic hash vectors, or hybrid fusion. Injection selects relevant slices and emits a stable marked block for the harness.

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search --query "fix the failing Go test" --db .semantix/project.db --retriever hybrid
semantix inject --query "fix the failing Go test" --db .semantix/project.db
semantix verify --session ./sessions --project demo > eval.tsv
```

The CLI also exposes verification and usage reporting. L3 can reuse verified read-only results when dependency checks pass.

## The broader control-loop direction

The architecture describes intent-aware scheduling, concurrency, speculative prefetch, and an evolution loop that adjusts policy from outcomes. Those ideas explain why the project calls itself a kernel, but they should not be presented as equally mature shipped capabilities.

## The key interface question

A harness integration needs explicit answers: when are events flushed, which user text forms the retrieval query, where is the reuse block inserted, how is it removed, and which errors are non-blocking? If those contracts remain implicit, the kernel becomes difficult to debug regardless of retrieval quality.

## Practical conclusion

Use Semantix today as a testable retrieval-and-reuse side layer. Evaluate scheduling and prefetch as roadmap capabilities against their own code and tests. This distinction makes the current CLI useful without requiring a reader to accept every architectural ambition as complete.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
