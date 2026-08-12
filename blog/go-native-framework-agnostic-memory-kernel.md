---
title: "Code Review: Is the Memory Kernel Really Framework-Agnostic?"
description: "A code-review checklist testing whether Semantix is genuinely independent of agent frameworks."
updated: 2026-08-12
group: "Go & Framework Independence"
order: 403
---

# Code Review: Is the Memory Kernel Really Framework-Agnostic?

“Framework-agnostic” should be reviewable. I use three tests: no framework types in the kernel API, a neutral interchange format, and a failure path that leaves the host usable.

## Test 1: inspect imports and interfaces

The kernel packages define Go interfaces and types for slices, stores, retrieval, injection, and verification. They do not import a named agent framework. That supports source-level independence.

## Test 2: inspect the boundary

Ingest accepts Reasonix-style JSONL, but the required concepts—roles, content, and tool calls—are generic enough for adapters. Output is CLI text or JSON rather than an SDK object owned by one framework.

```bash
go build -o semantix ./cmd/semantix
go vet ./...
go test ./...
```

## Test 3: remove it

Cache operations are designed to fail open. A harness should be able to skip injection when the process fails and continue its normal loop. If your integration cannot do that, the deployment is coupled even if the source packages are not.

## Review finding

The implementation is framework-neutral at the kernel and CLI boundary. The phrase does not mean “zero integration work.” Each harness still needs event mapping, prompt placement, lifecycle handling, and security review. Published repository evidence covers a Reasonix-style path and design notes for others; it is not a compatibility matrix across the market.

For a real adoption review, I would require a second adapter built by someone who did not design the kernel. That exercise would reveal whether the JSONL contract is genuinely sufficient or whether hidden Reasonix assumptions still live in event ordering, tool-call naming, cancellation, or prompt placement. Until that independent adapter exists, “framework-agnostic” is a supported architectural property, not a broad interoperability result.

## Sources and limitations

- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
