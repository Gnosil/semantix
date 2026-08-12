---
title: "Runbook: Stop Paying for Repeated Repository Discovery"
description: "An operational runbook for detecting repeated discovery, capturing evidence, and enabling semantic reuse safely."
updated: 2026-08-12
group: "Semantic Cache"
order: 203
---

# Runbook: Stop Paying for Repeated Repository Discovery

Use this runbook when agent traces show repeated repository searches, configuration reads, or test-discovery commands across sessions.

## 1. Confirm repetition before adding memory

Sample at least ten sessions. Group tool calls by repository and task intent. If repeated work is rare, a semantic cache adds complexity without much return.

## 2. Build and capture safely

```bash
go build -o semantix ./cmd/semantix
go vet ./...
go test ./...
```

Store sanitized session JSONL locally. Semantix’s file store is designed for 0600 files and 0700 directories, atomic replacement, and symlink defenses. That does not remove the operator’s obligation to exclude credentials.

## 3. Extract, retrieve, inject

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search --query "fix the failing Go test" --db .semantix/project.db --retriever hybrid
semantix inject --query "fix the failing Go test" --db .semantix/project.db
semantix verify --session ./sessions --project demo > eval.tsv
```

Run BM25 first because it is easiest to explain, then compare vector and hybrid. Set scope deliberately. Inspect the injection block before enabling it in a harness.

## 4. Define rollback

If retrieval fails, continue without the cache. If injected content degrades tasks, disable injection and clear the affected slices. Do not enable direct L3 reuse until dependency verification is covered for the task type.

## 5. Measure the right outcomes

Track retrieval relevance, repeated tool calls avoided, completion tokens, task success, and user rejection. A lower token count paired with a worse fix is not a win. The project’s synthetic cost report is a useful calculation template; replace every assumed value with observations from your own workload.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
