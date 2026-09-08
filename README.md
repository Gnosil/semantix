<div align="center">

# Semantix

### Cross-session memory for AI coding agents — and a much smaller bill.

**Semantic Caching · Adaptive Scheduling · Speculative Prefetch · Cross-Session Learning**

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg?style=flat-square)](./LICENSE)
[![Version](https://img.shields.io/badge/release-0.7.3-blue?style=flat-square)](https://github.com/Gnosil/semantix/releases)
[![GitHub stars](https://img.shields.io/github/stars/Gnosil/semantix?style=flat-square&logo=github)](https://github.com/Gnosil/semantix/stargazers)
[![GitHub contributors](https://img.shields.io/github/contributors/Gnosil/semantix?style=flat-square&logo=github)](https://github.com/Gnosil/semantix/graphs/contributors)
[![Website](https://img.shields.io/badge/website-semantix.ensureok.ai-168b6d?style=flat-square)](https://semantix.ensureok.ai)

**English** · [简体中文](./README.zh-CN.md) · [Quickstart](./docs/QUICKSTART.md) · [Technical Overview](./docs/TECHNICAL-OVERVIEW.md) · [Website](https://semantix.ensureok.ai)

</div>

<br/>

> **A coding agent loses its entire context when a session ends, and a provider's prefix cache hits only on byte-identical prompts — a single edit near the head invalidates everything after it.**
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

Pin a version or arch with `... | sh -s -- v0.7.2 arm64`. Other install methods and the full command reference live in [docs/QUICKSTART.md](./docs/QUICKSTART.md).

## Two ways to run it

**`semantix-agent` — the complete agent.** A CLI coding agent shipping with the memory kernel built in: extraction, retrieval, injection and the evolution loop are wired at boot, with provider presets for ~50 endpoints.

**The kernel — attached to an existing agent.** Harness-independent by design; the integration surface is a small set of hooks (tool registration, message interception, session export / event bypass). Three ways in:

| Path | Integration cost | Applies to |
| --- | --- | --- |
| **Agent skill** | `semantix install --target claude-code` | Claude Code |
| **Tool registration** | two tool schemas (`semantix_lookup` / `semantix_inject`) | custom / self-hosted agents |
| **Gateway** | one base URL, no code change | any OpenAI-compatible client |

Fail-open throughout: on kernel error the agent falls back to its normal execution path.

## Cross-session memory

A project's build conventions, test layout and service flags have to be re-established in every new session. Semantix extracts reusable **slices** from finished sessions — task patterns, project knowledge, tool sequences, verified results — into a local scored library, and reinjects the relevant ones when a similar task appears. Each hit carries its retrieval zone (🟢 hit · 🟡 grey · ⚪ miss) and its source session — real `semantix search` / `dashboard` / `verify` output lives in the [reuse visualization walkthrough](./docs/TECHNICAL-OVERVIEW.md#reuse-visualization).

Hits, misses and manual corrections all feed back into slice scores and retrieval thresholds, so precision improves with use rather than volume alone. Type-aware eviction discards stale results first and retains project knowledge.

## Cache hit rate and cost

Provider prefix caches match byte-exactly: a hit requires the prompt to be identical to the previous one byte for byte, and a single differing character near the head invalidates everything after it.

The impact of that failure mode is routinely underestimated. Claude Code writes a per-request billing marker into the head of the system prompt. First-party endpoints strip it server-side; third-party endpoints do not, so there the line invalidates the cache from the first token. The spec behind the prefix-hygiene middleware attributes a **133× difference in hit rate** to that single header, and records cache spend rising **4–5×** when stripping is disabled. `semantix-gateway` strips it by default.

The design goal is to make the provider cache hittable rather than to work around it:

- **Byte-stable injection** — retrieved slices are ordered by ID rather than by score, so semantically similar requests produce byte-identical prefixes.
- **Prefix hygiene** — per-request attribution markers are stripped and the tool array is canonicalized by name, removing client enumeration order as a source of invalidation.
- **Provider awareness** — a vendor capability table, per-vendor cache lifetimes (DeepSeek's 24-hour on-disk context cache, Anthropic's 5-minute ephemeral window), budget-aware `cache_control` breakpoint placement, and per-model price tables.
- **L3 result reuse** — a verified result is returned without a model call, fail-closed.

### Per-provider adaptation

Cache behaviour is determined by the stack serving the model rather than by the model itself, and the spread is wide enough to require per-stack adaptation.

| Endpoint | Prompt-cache hit, steady turn | Where the number comes from |
| --- | --- | --- |
| DeepSeek | **99.8%** | provider-reported cache tokens |
| GLM | **97.5%** | week-long spike; ~97.6% telemetry ceiling |

Both are per-turn prompt-token hit rates — `hit / (hit + miss)` over cache token counts returned by the provider, not estimates. `semantix-agent` displays the current turn's rate alongside the session average in its status line, so the figure is verifiable on any workload.

A week-long GLM study measured the same model behind different hosts and found cache lifetimes differing several-fold: one stack held a prefix for 1–8 minutes with real expiry in `(8, 12]`; another maintained 96–98% for 120 seconds and had fallen to 28% at 301 seconds. This is why the TTL table is per-vendor rather than a single global value, and why GLM hit-rate telemetry carries a documented **~97.6%** reporting ceiling — trailing partial blocks never count as cached.

Method and raw runs: [docs/reports/glm-spike-week.md](./docs/reports/glm-spike-week.md) · [docs/reports/glm-p0-1-prefix-audit.md](./docs/reports/glm-p0-1-prefix-audit.md).

Beyond caching, the scheduler learns tool-usage patterns to parallelize eligible calls and to prefetch read-only context during model wait time.

### Measured

- **79.8% cost saved** on a synthetic replay comparison — methodology in [docs/reports/m0-cost-comparison.md](./docs/reports/m0-cost-comparison.md)
- **80% cache hit rate (L3/L2)** on a small demo library (4 extracted sessions) — the one-screen `semantix dashboard` snapshot is captured in the [reuse visualization walkthrough](./docs/TECHNICAL-OVERVIEW.md#reuse-visualization)
- A replay gate (`semantix verify`) enforces **≥ 70% relevance**; validating that hit rate on real user sessions is the open v1.0 gate — [#58](https://github.com/Gnosil/semantix/issues/58)

**These are replay / demo measurements, not production benchmarks.** The full evidence trail — and everything else technical — lives in [docs/TECHNICAL-OVERVIEW.md](./docs/TECHNICAL-OVERVIEW.md).

## Try it in 30 seconds

Installed via the one-liner above? Extract slices from a past session, then reuse them — this is the memory kernel at work:

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search  --query "fix failing go test" --db .semantix/project.db
semantix inject  --query "fix failing go test" --db .semantix/project.db   # L2 reuse block
semantix verify  --session <session-dir> --project demo                    # replay gate (≥70%)
semantix dashboard                                                         # one-screen snapshot
```

Prefer to build from source (Go 1.26+)? `go build -o semantix ./cmd/semantix && go build -o semantix-agent ./cmd/semantix-agent`. Full command reference and configuration: [docs/QUICKSTART.md](./docs/QUICKSTART.md).

## Integrations

| Agent / harness | Integration path | Status |
| --- | --- | --- |
| Semantix Agent | Built-in bundle: `[semantix] enabled=true` + `semantix_lookup` tool | ✅ shipped since v0.3.0 |
| Claude Code | Tool registration via `semantix_lookup` / `semantix_inject` schemas | ✅ documented (`agent-skill/tools/`) |
| LangChain apps | Middleware with two hooks (message rewrite + session extraction) | ✅ documented ([report](./docs/reports/langchain-middleware.md)) |
| Custom / self-hosted | Session bypass: export / event bypass / direct call | ✅ documented (`agent-skill/hooks/session-bypass.md`) |
| Any OpenAI-compatible client | `semantix-gateway` in front, custom base URL | ✅ shipped |

Cursor, Windsurf, Codex CLI, Gemini CLI, Copilot agent mode and Cline / Continue / Aider all reach the kernel through one of the paths above; none requires kernel changes. Priorities follow observed usage — concrete integration requests are tracked as [issues](https://github.com/Gnosil/semantix/issues) (template: `.github/ISSUE_TEMPLATE/integration_request.yml`).

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
