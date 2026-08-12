---
title: "Lab Notes: Reusing One Coding-Agent Session in the Next"
description: "A hands-on lab that captures one session, retrieves it with new wording, and checks the resulting reuse block."
updated: 2026-08-12
group: "Evaluation Guides"
order: 3
---

# Lab Notes: Reusing One Coding-Agent Session in the Next

This is a small laboratory exercise, not a benchmark. The question is narrow: can a useful result produced in session A become inspectable input in session B without replaying the whole transcript?

## Prepare a two-turn fixture

Create a JSONL file whose first user turn asks to diagnose a failing Go test and whose assistant turn records the successful command and outcome. Keep secrets and private repository content out of the fixture.

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search --query "fix the failing Go test" --db .semantix/project.db --retriever hybrid
semantix inject --query "fix the failing Go test" --db .semantix/project.db
semantix verify --session ./sessions --project demo > eval.tsv
```

After extraction, inspect the database file permissions and the returned slice IDs. Semantix derives slice IDs from content, type, and scope, so ingesting identical content should not manufacture a new identity. The file store is local and is designed for 0600 permissions with atomic writes.

## What to observe

The extractor emits three kinds of reusable material: Prompt slices capture the task, ToolPattern slices capture tool sequences, and Result slices capture outcomes. Search should find the relevant unit even when the later query changes wording. Injection should wrap selected material in a marked, budget-bounded block with stable ordering.

Run the injection twice and diff the outputs. A clean diff matters because deterministic prefix content is the precondition for provider-side prefix caching. It also makes failures easier to reproduce.

## Where this lab stops

This fixture proves data flow, not usefulness at scale. It does not establish how often real developers repeat a task, whether injected context improves answer quality, or whether a provider grants a cache hit. The repository’s verify command exists precisely because those questions require held-out real sessions and human labels.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
