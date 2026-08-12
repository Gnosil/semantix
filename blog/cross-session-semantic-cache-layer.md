---
title: "Postmortem: Why the Agent Re-read the Same Repository Again"
description: "A postmortem-style analysis of repeated repository discovery and the cache layer that can prevent it."
updated: 2026-08-12
group: "Semantic Cache"
order: 201
---

# Postmortem: Why the Agent Re-read the Same Repository Again

**Incident:** a new coding-agent session repeated the same repository searches, configuration reads, and test discovery completed the day before.

**Impact:** extra tool calls, longer latency, and a second opportunity to reach a different conclusion from unchanged files.

## Root cause

The first session produced useful execution knowledge, but it remained inside a chronological transcript. The next session had no project-scoped retrieval step and therefore treated the repository as unseen.

## Corrective action

Capture the session as JSONL, extract typed slices, retrieve under the project scope, and inject a stable bounded block before repeating discovery:

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search --query "fix the failing Go test" --db .semantix/project.db --retriever hybrid
semantix inject --query "fix the failing Go test" --db .semantix/project.db
semantix verify --session ./sessions --project demo > eval.tsv
```

This is a cache only in a broader sense. BM25 or hybrid search can retrieve semantically related prior work rather than an identical key. The injection is deterministic so unchanged content can also support a provider’s byte-prefix cache.

## Safety action

Cache-layer misses must not stop the agent, so retrieval failure is fail-open. Directly returning a prior result is riskier: L3 is fail-closed and rejects reuse if dependency fingerprints do not match. These two failure policies should not be collapsed into one “memory enabled” switch.

## Evidence and remaining action

The repository includes TestSessionBReusesSessionA, deterministic injection tests, and L3 tests for modified or deleted dependencies. The cost report’s 79.8% saving is a synthetic model with assumed tool-token counts. The remaining corrective action is a real-session replay with labeled relevance and downstream task-quality checks.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
