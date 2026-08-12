---
title: "Integration Recipe: A Memory Sidecar for Any Go Agent Harness"
description: "A concrete integration recipe covering capture, extraction, retrieval, injection, and rollback."
updated: 2026-08-12
group: "Go & Framework Independence"
order: 402
---

# Integration Recipe: A Memory Sidecar for Any Go Agent Harness

This recipe assumes the harness can emit events and amend a prompt. It does not assume a particular agent framework.

## 1. Capture

Map user, assistant, and tool events into newline-delimited JSON. Write only the fields required for extraction and scrub credentials before persistence.

## 2. Extract

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search --query "fix the failing Go test" --db .semantix/project.db --retriever hybrid
semantix inject --query "fix the failing Go test" --db .semantix/project.db
semantix verify --session ./sessions --project demo > eval.tsv
```

Choose project scope for repository knowledge and user scope only for genuinely cross-project preferences. Keep the database path explicit in deployment configuration.

## 3. Retrieve and inject

Use hybrid as an experiment, not a default belief. Log the selected slice IDs, scores, query, and token budget. Put the marked reuse block in one predictable location so it can be removed during an A/B comparison.

## 4. Roll back

If the sidecar exits nonzero or times out, continue without injection. If a selected slice is wrong, record rejection and remove or downweight it. Direct result reuse should remain disabled unless dependency verification is available for that task.

## 5. Verify

Run go vet, go test, a fixture-to-injection smoke test, scope-negative tests, and held-out replay through semantix verify. Measure downstream task completion as well as retrieval relevance.

## Limits

This recipe proves portability of the integration contract, not equal behavior in every harness. Prompt construction, event timing, permissions, and cancellation semantics remain harness-specific work.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
