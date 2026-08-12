---
title: "Cost Notebook: What Repository Context Actually Costs Twice"
description: "A transparent cost notebook separating measured events, assumptions, and the data needed for a real savings claim."
updated: 2026-08-12
group: "Semantic Cache"
order: 204
---

# Cost Notebook: What Repository Context Actually Costs Twice

“We saved 79.8%” is an attractive sentence and an incomplete one. This notebook reconstructs what that number means so it can be replaced with real measurements.

## The demonstration

The M0 scenario modeled a first session with six tool calls and a second session receiving three extracted slices in a 43-token L2 block. It assumed 300 completion tokens per recreated tool call and 80% reuse. Under the listed DeepSeek input/output prices, baseline cost was $0.001980 and modeled kernel cost was $0.000399.

| Quantity | Source type |
|---|---|
| Six tool calls | synthetic demo fixture |
| 43 injection tokens | generated demo artifact |
| 300 tokens per tool call | explicit estimate |
| 80% reuse | explicit assumption |
| Provider prices | report input at the time |

## Recalculate with your data

Capture usage events and count actual repeated calls. Use a held-out session set so the same examples do not define and prove the cache. Then compare task success before celebrating lower cost.

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search --query "fix the failing Go test" --db .semantix/project.db --retriever hybrid
semantix inject --query "fix the failing Go test" --db .semantix/project.db
semantix verify --session ./sessions --project demo > eval.tsv
```

## A claim I would publish

“In a synthetic two-session fixture, the listed assumptions produce 79.8% modeled savings; real-session relevance and production savings remain unverified.” That sentence is less exciting and far more citable because every qualifier is visible.

## Sources and limitations

- [Cost report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-cost-comparison.md) — formula, inputs, output, and sensitivity table.
- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
