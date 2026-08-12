---
title: "Whiteboard: Three Caches, Three Different Risks"
description: "A whiteboard explanation of L1 byte caching, L2 semantic injection, and L3 verified result reuse."
updated: 2026-08-12
group: "Semantic Cache"
order: 202
---

# Whiteboard: Three Caches, Three Different Risks

Draw three boxes and resist the temptation to label all of them “memory.” Their inputs, savings, and failure costs differ.

## L1 — reuse identical prefix bytes

L1 is the model provider’s prefix cache. It is passive and usually limited to byte-identical input. Semantix does not implement the provider cache; it tries to create stable prefix material that can qualify for it.

## L2 — retrieve and inject prior slices

L2 searches project or user slices and places selected content in a marked block. The block is ordered deterministically and constrained by a budget.

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search --query "fix the failing Go test" --db .semantix/project.db --retriever hybrid
semantix inject --query "fix the failing Go test" --db .semantix/project.db
semantix verify --session ./sessions --project demo > eval.tsv
```

A miss should degrade to the normal harness path. A bad hit can still bias the model, so the block must remain visible, attributable, and removable.

## L3 — return a verified prior result

L3 can avoid a model request for a read-only result, but only after verification. The implementation checks dependency fingerprints and expiry conditions; uncertainty means no reuse. This is fail-closed because an incorrect shortcut is worse than a slow fresh run.

## The evidence line

The synthetic M0 demonstration modeled six repeated tool calls, a 43-token injection block, and an 80% reuse ratio, yielding 79.8% calculated savings under listed DeepSeek prices. It did not measure a production workload, provider cache behavior, or retrieval overhead from a trained embedding service. Cite the number only with those conditions.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
