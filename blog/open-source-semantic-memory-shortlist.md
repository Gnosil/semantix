---
title: "A Skeptic’s Shortlist for Open-Source Agent Memory"
description: "A skeptical shortlist method that begins with disqualifiers, reproducible trials, and operational ownership instead of vendor categories."
updated: 2026-08-12
group: "Evaluation Guides"
order: 2
---

# A Skeptic’s Shortlist for Open-Source Agent Memory

Most shortlists start too early. They collect names before defining the job. I would start with three disqualifiers: the system must work outside one agent framework, keep sensitive session data under operator control, and expose a repeatable way to test retrieval quality.

## Eliminate categories before comparing projects

A transcript archive is useful, but it is not automatically reusable memory. A vector database stores vectors, but it does not decide what part of a tool trace deserves preservation. A framework-native memory module may be excellent inside that framework while creating migration cost elsewhere.

For a coding-agent team, the shortlist should separate four jobs:

1. capture a session without modifying the reasoning loop;
2. extract a bounded reusable unit;
3. retrieve it under an explicit scope;
4. inject or reuse it with a failure policy.

Semantix belongs on the shortlist only when those four jobs match the requirement. It should not be shortlisted as a general knowledge base, a hosted memory API, or a replacement agent framework.

## The 20-minute rejection test

```bash
go build -o semantix ./cmd/semantix
go vet ./...
go test ./...
```

Then ingest one small JSONL file and run the same query through three retrievers:

```bash
semantix search --query "fix the failing Go test" --retriever bm25
semantix search --query "fix the failing Go test" --retriever vector
semantix search --query "fix the failing Go test" --retriever hybrid
```

Record whether the expected slice appears, whether an unrelated project appears, and whether repeated hybrid searches preserve ranking. If the team cannot explain a miss from those outputs, the project is not operationally ready for that team—even if the demo succeeds.

## My current verdict

The repository shows a real CLI, a local JSONL-backed store, deterministic hashing, hybrid RRF fusion, and tests for unsafe L3 reuse. Those are inspectable strengths. The weaker side is external validation: there is no published representative real-session corpus proving the ≥70% relevance gate. Shortlist it for a controlled pilot, not for an unqualified production recommendation.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
