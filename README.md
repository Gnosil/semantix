<div align="center">

# Semantix

### Cross-session memory and work reuse for AI coding agents.

**Semantic Caching · Adaptive Scheduling · Speculative Prefetch · Cross-Session Learning**

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg?style=flat-square)](./LICENSE)
[![Version](https://img.shields.io/badge/release-0.7.3-blue?style=flat-square)](https://github.com/Gnosil/semantix/releases)
[![GitHub stars](https://img.shields.io/github/stars/Gnosil/semantix?style=flat-square&logo=github)](https://github.com/Gnosil/semantix/stargazers)
[![GitHub contributors](https://img.shields.io/github/contributors/Gnosil/semantix?style=flat-square&logo=github)](https://github.com/Gnosil/semantix/graphs/contributors)
[![Website](https://img.shields.io/badge/website-semantix.ensureok.ai-168b6d?style=flat-square)](https://semantix.ensureok.ai)

**English** · [简体中文](./README.zh-CN.md) · [Quickstart](./docs/QUICKSTART.md) · [Technical Overview](./docs/TECHNICAL-OVERVIEW.md) · [Website](https://semantix.ensureok.ai)

</div>

<br/>

> **A new coding session often repeats project discovery and tool work. Provider prefix caches can reuse computation for matching prefixes, but do not decide which experience from an earlier session is still relevant or valid.**
>
> Semantix addresses both: a complete coding agent with the memory kernel built in, and a standalone kernel that attaches to an agent already in use.

## Quick start

**Install** — one line, macOS / Linux (arm64 / amd64):

```bash
curl -fsSL https://raw.githubusercontent.com/Gnosil/semantix/main/agent-skill/scripts/install.sh | sh
```

This drops `semantix` + `semantix-agent` into `~/.local/bin` and turns on cross-session memory. **Use it** — start the agent inside any project; that folder becomes the workspace:

```bash
cd ~/your-project
semantix                 # bare command → launch the coding agent here (first run sets up provider / API key)
semantix search "..."    # any subcommand → the memory kernel (search / extract / inject / verify / usage)
```

Pin a version or arch with `... | sh -s -- v0.7.3 arm64`. Other install methods and the full command reference live in [docs/QUICKSTART.md](./docs/QUICKSTART.md).

## Two ways to run it

**`semantix-agent` — the complete agent.** A CLI coding agent shipping with the memory kernel built in: extraction, retrieval, injection and the evolution loop are wired at boot, with provider presets for ~50 endpoints.

**The kernel — attached to an existing agent.** Harness-independent by design; the integration surface is a small set of hooks (tool registration, message interception, session export / event bypass). Three ways in:

| Path | Integration cost | Applies to |
| --- | --- | --- |
| **Agent skill** | `semantix install --target claude-code` | Claude Code |
| **Tool registration** | two tool schemas (`semantix_lookup` / `semantix_inject`) | custom / self-hosted agents |
| **Gateway** | configure upstreams and point the client at the gateway base URL | OpenAI-compatible Chat Completions clients |

Memory integration is designed to fail open: on kernel error the agent falls back to its normal execution path. Direct result reuse is fail-closed: a failed validation rejects the cached result.

## Cross-session memory

A project's build conventions, test layout and service flags often need to be re-established in a new session. Semantix extracts reusable **slices** from finished sessions — task patterns, project knowledge, tool sequences and results — into a local scored library, and reinjects relevant slices when a similar task appears. Each hit carries its retrieval zone (🟢 hit · 🟡 grey · ⚪ miss) and its source session — real `semantix search` / `dashboard` / `verify` output lives in the [reuse visualization walkthrough](./docs/TECHNICAL-OVERVIEW.md#reuse-visualization).

Usage and feedback signals can update slice scores and retrieval thresholds. Type-aware eviction treats reusable project knowledge differently from short-lived results. These mechanisms support tuning; improved precision still needs to be measured on the target workload.

## What the kernel reuses

Semantix connects cross-session memory to three levels of reuse:

| Layer | Current mechanism | What it can save |
| --- | --- | --- |
| **L1 · Provider prefix cache** | Gateway prefix hygiene and provider-aware cache handling help preserve matching prefixes | Repeated prompt computation, subject to the upstream's cache policy |
| **L2 · Context reuse** | Retrieve relevant slices and assemble a budgeted context block with provenance | Repeated project discovery and instructions |
| **L3 · Result reuse** | Return a stored result after relevance, isolation and validity checks pass | The main answer-generation call; grey-zone judging may still call a model |

Three implementation choices are central to this approach:

- **Validity checks for code-related results.** L3 checks captured dependency files using mtimes and SHA-256 fingerprints. The gateway also enforces conversation-context and model isolation and applies an age policy. Dependency-free results require explicit opt-in; gateway ingestion leaves that opt-in off by default. Checks cover captured dependencies, so external state and unrecorded dependencies still need revalidation. See [the L3 decision path](./kernel/cache/l3.go).
- **Separate retrieval from permission to reuse.** Hit / Grey / Miss zones guide admission. L2 drops grey candidates by default, with an optional audit mode; L3 requires judge approval for grey candidates and rejects them when no judge is available. A judge approval still has to pass the isolation and dependency checks. See [L2 admission](./kernel/inject/inject.go) and [retrieval zones](./kernel/zone/zone.go).
- **Use behavior history as well as facts.** Task patterns, project context, tool sequences, results and memory units have different uses. In the bundled agent, tool-pattern learning feeds scheduling and speculative read-only prefetch, with outcome feedback. This execution integration depends on the harness; pointing a client at the gateway alone does not install a tool scheduler. See [the kernel bridge](./harness/semantix/bridge.go).

The default retrieval path is local BM25 and does not require a vector database or an embedding service. CLI search and the gateway also support vector and hybrid retrieval. The default vector backend uses deterministic token-hash embeddings; model embeddings are opt-in and use an OpenAI-compatible embeddings endpoint. The gateway's configured model backend uses HNSW indexing. Hash embeddings do not provide the same semantic capabilities as a trained embedding model. See [retriever construction](./gateway/retriever.go) and [the configuration reference](./docs/QUICKSTART.md).

## Cache hit rate and cost

Provider prefix caches reuse matching prompt prefixes, with token/block granularity, minimum lengths and lifetimes determined by the serving stack. Changes near the start can invalidate much of an otherwise reusable prefix.

`semantix-gateway` removes recognized attribution/billing marker lines from the head of the first system message and sorts tools by name by default. This removes two sources of prefix churn before forwarding. The implementation and tests are in [the prefix-hygiene middleware](./gateway/sanitize.go) and [its tests](./gateway/sanitize_test.go); workload observations are recorded in [the prefix audit](./docs/reports/glm-p0-1-prefix-audit.md).

The design goal is to make the provider cache hittable rather than to work around it:

- **Deterministic context assembly** — admitted slices are ordered by relevance score, with ID as a stable tie-break within each zone. Different queries, scores or metadata can still change the injected bytes; similar meaning alone does not guarantee a prefix-cache hit.
- **Prefix hygiene** — per-request attribution markers are stripped and the tool array is canonicalized by name, removing client enumeration order as a source of invalidation.
- **Provider awareness** — a vendor capability table, cache-control policies and per-model price tables. The gateway's configurable per-vendor L3 age windows are local reuse policy; they do not control the upstream's prefix-cache lifetime.
- **L3 result reuse** — an eligible result is returned without the main generation call, fail-closed; optional grey-zone judging has its own cost.

### Per-provider adaptation

Cache behaviour is determined by the stack serving the model rather than by the model itself, and the spread is wide enough to require per-stack adaptation.

| Endpoint | Prompt-cache hit, steady turn | Where the number comes from |
| --- | --- | --- |
| DeepSeek | **99.8%** | provider-reported cache tokens |
| GLM (measured hosting stacks) | **97.5%** | week-long spike; observed ~97.6% telemetry ceiling |

Both are observed per-turn prompt-token hit rates — `hit / (hit + miss)` over cache token counts returned by the provider. They are workload-specific observations, not guaranteed rates or measures of memory relevance. `semantix-agent` displays the current turn's rate alongside the session average in its status line when the provider reports cache usage.

A week-long GLM study measured the same model behind different hosts and found cache lifetimes differing several-fold: one stack held a prefix for 1–8 minutes with real expiry in `(8, 12]`; another maintained 96–98% for 120 seconds and had fallen to 28% at 301 seconds. These measurements motivate per-stack calibration. The report also documents an observed **~97.6%** telemetry ceiling on the measured stacks, where trailing partial blocks were not counted as cached.

Method and raw runs: [docs/reports/glm-spike-week.md](./docs/reports/glm-spike-week.md) · [docs/reports/glm-p0-1-prefix-audit.md](./docs/reports/glm-p0-1-prefix-audit.md).

Beyond caching, the scheduler learns tool-usage patterns to parallelize eligible calls and to prefetch read-only context during model wait time.

### Evidence and evaluation limits

- **79.8% modeled cost saving** in a synthetic demo, assuming 80% of repeated tool steps can be avoided and estimated tokens per step. This is a cost-model illustration, not a measured production billing reduction — assumptions in [docs/reports/m0-cost-comparison.md](./docs/reports/m0-cost-comparison.md).
- **80% cache hit rate (L3/L2)** on a small demo library (4 extracted sessions) — the one-screen `semantix dashboard` snapshot is captured in the [reuse visualization walkthrough](./docs/TECHNICAL-OVERVIEW.md#reuse-visualization)
- The default replay verdict (`semantix verify`) uses an **auto-classified Hit share of ≥ 70%**. Its output labels this `relevance`, but that statistic is not human-labeled precision. Optional `--labels` calibration supports oracle-based evaluation; real-session relevance validation is tracked in [#58](https://github.com/Gnosil/semantix/issues/58).

**These are replay / demo measurements, not production benchmarks.** The full evidence trail — and everything else technical — lives in [docs/TECHNICAL-OVERVIEW.md](./docs/TECHNICAL-OVERVIEW.md).

## Try it in 30 seconds

Installed via the one-liner above? Extract slices from a past session, then reuse them — this is the memory kernel at work:

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search  --query "fix failing go test" --db .semantix/project.db
semantix inject  --query "fix failing go test" --db .semantix/project.db   # L2 reuse block
semantix verify  --session <session-dir> --project demo                    # replay verdict (auto-Hit share ≥70%)
semantix dashboard                                                         # one-screen snapshot
```

Prefer to build from source (Go 1.26+)? `go build -o semantix ./cmd/semantix && go build -o semantix-agent ./cmd/semantix-agent`. Full command reference and configuration: [docs/QUICKSTART.md](./docs/QUICKSTART.md).

## Integrations

| Agent / harness | Integration path | Status |
| --- | --- | --- |
| Semantix Agent | Built-in bundle: `[semantix] enabled=true` + `semantix_lookup` tool | ✅ shipped since v0.3.0 |
| Claude Code | Tool registration via `semantix_lookup` / `semantix_inject` schemas | ✅ documented (`agent-skill/tools/`) |
| Codex CLI | Opt-in local marketplace plugin with project memory hooks | [Setup and diagnostics](./plugins/semantix-memory/README.md) |
| DeepSeek Harness (DSH) | Local Host bundle for automatic project recall and capture | Verified with [DSH headless](./plugins/semantix-dsh-memory/README.md) |
| LangChain apps | Middleware with two hooks (message rewrite + session extraction) | ✅ documented ([report](./docs/reports/langchain-middleware.md)) |
| Custom / self-hosted | Session bypass: export / event bypass / direct call | ✅ documented (`agent-skill/hooks/session-bypass.md`) |
| OpenAI-compatible Chat Completions clients | `semantix-gateway` in front, configured upstreams and custom base URL | ✅ shipped |
| Milvus (vector DB) | Optional compose stack (docker-compose.milvus.yml) + MCP server; orchestrator does RAG, Semantix caches LLM calls | ✅ documented ([guide](./docs/integrations/milvus.md)) |

Other agents can integrate through tool registration, supported gateway requests or session export. Compatibility depends on the client's protocol and available hooks; the paths above do not imply that every client has a tested adapter or the bundled agent's full execution integration. Concrete integration requests are tracked as [issues](https://github.com/Gnosil/semantix/issues) (template: `.github/ISSUE_TEMPLATE/integration_request.yml`).

## Documentation

| Doc | What's inside |
| --- | --- |
| [ROADMAP.md](./ROADMAP.md) | Shipped / in progress / next (English summary) |
| [docs/TECHNICAL-OVERVIEW.md](./docs/TECHNICAL-OVERVIEW.md) | **Start here** — concepts, cache layers, scheduler, prefetch, evolution loop, architecture, module map, status, roadmap, metrics |
| [docs/QUICKSTART.md](./docs/QUICKSTART.md) | Install, 30-second demo, command reference, configuration |
| [docs/Agent-Infra-架构设计.md](./docs/Agent-Infra-架构设计.md) | Full architecture design (Chinese) |
| [docs/Agile路线图.md](./docs/Agile路线图.md) | Agile 1–3 roadmap and definition-of-done (Chinese) |
| [docs/Security-安全设计.md](./docs/Security-安全设计.md) · [SECURITY.md](./SECURITY.md) | Threat model and security mechanisms |
| [agent-skill/SKILL.md](./agent-skill/SKILL.md) | Self-serve integration for any harness |
| [semantix.ensureok.ai](https://semantix.ensureok.ai) | Website, product docs and the blog |

## Community

- 💬 [GitHub Discussions](https://github.com/Gnosil/semantix/discussions) — Q&A, ideas, show & tell
- 🐛 [Issue tracker](https://github.com/Gnosil/semantix/issues) — `good first issue` is the place to start
- 📖 [Blog](https://semantix.ensureok.ai/blog) — semantic caching and agent-memory write-ups

## Contributing

Semantix is early-stage. Contributions are welcome across semantic caching, retrieval, scheduling, speculative execution, evaluation methodology and harness adapters — see [CONTRIBUTING.md](./CONTRIBUTING.md) for the workflow (branch from `main`, PRs require green `go vet` and `go test -race`). Issues and PRs in English or Chinese are both fine.

**Without writing code:** run `semantix verify` against your own sessions and post the resulting hit rate to [#58](https://github.com/Gnosil/semantix/issues/58). Aggregated community results decide the v1.0 gate.

Architectural assumptions are open to challenge; testing them is part of the work.

## License

[MIT](./LICENSE). [DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix) (MIT).

---

<div align="center">

### Semantix

**Every interaction should make the next one cheaper, faster, and smarter.**

</div>
