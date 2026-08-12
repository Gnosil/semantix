---
title: "From Noisy Tool Traces to Three Reusable Slice Types"
description: "An annotated teardown of how prompt, tool-pattern, and result slices are derived from agent session JSONL."
updated: 2026-08-12
group: "Semantic Slices"
order: 101
---

# From Noisy Tool Traces to Three Reusable Slice Types

A tool trace mixes several kinds of information: what the user wanted, which operations the agent attempted, and what finally worked. Saving the whole trace preserves context but makes later retrieval noisy. Semantix’s extractor makes a specific editorial choice: split the trace into Prompt, ToolPattern, and Result slices.

## An annotated example

Suppose a session contains a request to fix a Go test, followed by rg, file reads, an edit, and go test. The reusable units are not equivalent:

- **Prompt slice**: the normalized task intent;
- **ToolPattern slice**: ordered tool-call n-grams, space-joined so BM25 can tokenize them;
- **Result slice**: the final assistant outcome when one exists.

The extractor reads one JSON object per line and tolerates unknown fields. Slice IDs are content-derived hashes that mix type and scope, making duplicates stable and cross-scope collisions less likely.

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search --query "fix the failing Go test" --db .semantix/project.db --retriever hybrid
semantix inject --query "fix the failing Go test" --db .semantix/project.db
semantix verify --session ./sessions --project demo > eval.tsv
```

## Why three types are better than one summary

Different later queries want different evidence. “How did we diagnose this?” should favor a ToolPattern. “What was the accepted fix?” should favor a Result. “Have we seen this request?” should favor a Prompt. A single generated summary hides those distinctions and introduces another model call before retrieval can even begin.

## Boundary conditions

The extractor does not prove that every turn boundary is the correct semantic boundary. The M0 gate explicitly leaves real-session relevance unresolved and proposes changing from turn-level to subtask-level extraction if relevance is below 70%. The current split is a testable baseline, not a final ontology.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
