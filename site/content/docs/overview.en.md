# Semantix Project Overview

Semantix is a Go-based agent kernel and coding-agent product repository. It turns past sessions into searchable semantic slices, then uses those slices for retrieval, context injection, guarded result reuse, scheduling, prefetch, and bounded feedback-driven adaptation.

## The problem it addresses

Coding agents repeatedly inspect the same repository context, rediscover similar failures, and reconstruct previously verified procedures. A transcript preserves history but does not provide scope, retrieval, invalidation, or acceptance boundaries.

Semantix separates the problem into four steps:

1. Extract reusable slices from session events.
2. Retrieve candidates with lexical, vector, or hybrid search.
3. Inject background context or, behind additional gates, reuse a result.
4. Record hits, waste, and task feedback for constrained tuning.

## Repository map

| Area | Responsibility | Main paths |
|---|---|---|
| CLI | Extraction, retrieval, verification, diagnostics, maintenance | `cmd/semantix` |
| Coding agent | Semantix Agent harness and executable | `cmd/semantix-agent`, `harness` |
| Slices | Types, scopes, storage, compression, eviction | `kernel/slice` |
| Retrieval | BM25, embeddings, fusion, zones | `kernel/bm25`, `kernel/embed`, `kernel/fuse`, `kernel/zone` |
| Reuse | Lookup, injection, L3 gates, fingerprints, promotion | `kernel/lookup`, `kernel/inject`, `kernel/cache`, `kernel/fingerprint`, `kernel/judge`, `kernel/promote` |
| Orchestration | Round plans, prefetch, bounded evolution | `kernel/sched`, `kernel/prefetch`, `kernel/evolve` |
| Gateway | OpenAI-compatible proxy, SSE, upstream routing, usage | `gateway`, `cmd/semantix-gateway` |

## Important boundaries

Semantic similarity is not execution equivalence. A search hit may support L2 context injection, while L3 result reuse additionally needs project-state checks and conservative decision gates.

The repository contains implementations, tests, synthetic replay reports, and deployment examples. These establish inspectable code paths and repository-scoped observations. Cross-team production hit rates, latency changes, and billing outcomes still require validation in each deployment.

## Suggested reading path

Start with **Install and first run**, choose a harness path in **Coding-agent integration**, then read **Slices and cache levels** and **Verification and observability**.
