<div align="center">

# Semantix

### A self-evolving agent kernel that learns how you work.

**Semantic Caching · Adaptive Scheduling · Speculative Prefetch · Cross-Session Learning**

[![License: FSL-1.1-MIT](https://img.shields.io/badge/License-FSL--1.1--MIT-blue.svg?style=flat-square)](./LICENSE)
[![Status](https://img.shields.io/badge/status-v0.3.1-green?style=flat-square)](#project-status)
[![Version](https://img.shields.io/badge/release-0.3.1-blue?style=flat-square)](https://github.com/Gnosil/semantix/releases)
[![GitHub stars](https://img.shields.io/github/stars/Gnosil/semantix?style=flat-square\&logo=github)](https://github.com/Gnosil/semantix/stargazers)
[![GitHub contributors](https://img.shields.io/github/contributors/Gnosil/semantix?style=flat-square\&logo=github)](https://github.com/Gnosil/semantix/graphs/contributors)
[![Website](https://img.shields.io/badge/website-semantix.ensureok.ai-168b6d?style=flat-square)](https://semantix.ensureok.ai)

**English** | [简体中文](./README.zh-CN.md)

</div>

<br/>

> **Agents should not start from zero every session.**
>
> Semantix sits between an AI agent harness and its resources. It learns what you reuse, how you work, and what you are likely to need next — then turns that knowledge into semantic cache hits, smarter scheduling, safe prefetch, and lower-cost agent execution.

Semantix is designed to work with agent harnesses such as **DeepSeek-Reasonix**, Claude Code-style runtimes, and future custom agent systems.

The goal is not simply to reduce tokens.

The goal is to:

> **avoid unnecessary computation while preserving or improving agent quality.**

---

## Why Semantix?

Modern agent harnesses have become increasingly good at managing long-running sessions.

They can maintain context, call tools, compact history, recover from failures, and take advantage of provider-side prefix caching.

But most optimization still stops at the **session boundary**.

A new session often means:

* rebuilding project context
* repeating similar tool calls
* re-processing familiar instructions
* rediscovering the same files
* paying again for work the agent has effectively already done
* ignoring the user's historical workflow patterns

A traditional agent often behaves like this:

```text
Session A ────────────X
                      context ends

Session B ────────────X
                      context rebuilt

Session C ────────────X
                      similar work repeated
```

Semantix introduces a persistent optimization layer:

```text
Session A ───────┐
                 │
Session B ───────┼────► Semantix
                 │          │
Session C ───────┘          │
                            ▼
                    Semantic knowledge
                    Behavioral patterns
                    Reusable results
                    Cacheable context
                            │
                            ▼
                    Better next session
```

Instead of only asking:

> **How do we keep this session efficient?**

Semantix asks:

> **What has this user done before, what are they likely to do next, and what work can safely be reused?**

---

# Reuse Visualization

> **Cross-session reuse you can see.** Semantix renders the three reuse signals — hit rate, cost saved, source session — as one-glance CLI output (the Agile 1 DoD "reuse visualization", shipped as U28–U31). Every block below is **real output** captured from a small demo library (4 extracted sessions); the real-data measurement lives in [`docs/reports/m0-cost-comparison.md`](./docs/reports/m0-cost-comparison.md) (**79.8%** cost saved on synthetic replay).

One-screen snapshot — `semantix dashboard`:

```text
$ semantix dashboard

  semantix dashboard — reuse snapshot
  ------------------------------------------------

  💰 Cost savings
     paid        $ 0.0060
     baseline    $ 0.0141
     saved       $ 0.0080  (56.99%)
     ██████████████░░░░░░░░░░

  🎯 Cache hit rate (L3/L2)
     4 / 5 turns  (80.00%)
     L3 1 · L2 3
     ███████████████████░░░░░

  🗂 Zone distribution (library replay)
     hit  ████ 4   grey ██████ 6   miss  0

  📦 Slice library
     10 slices · 3 cross-session sessions
```

Every hit carries its zone icon and source session — `semantix search` / `semantix lookup`:

```text
$ semantix search --query "fix failing go test"
1. 🟢 score=4.331011 zone=hit id=619551c54af5437a scope=project from:2026-08-14-c9d4
   fix failing go test after refactor
2. 🟢 score=3.852740 zone=hit id=73b12bb117664106 scope=project from:2026-08-13-b7c2
   fix failing go test in kernel slice extractor
3. 🟢 score=3.852740 zone=hit id=adfecdd9bff0db2d scope=project from:2026-08-12-a1f3
   fix failing go test in kernel slice store
🎯 3/3 hits in 3 sessions
```

Replay gate with zone bars and a one-line verdict — `semantix verify`:

```text
$ semantix verify --session ./sessions
session	turn	score	zone	top1_content	query
session-2026-08-14-d2e5.jsonl	3	7.1261	✅hit	add shell completion for bash and zsh	add shell completion for zsh
session-2026-08-14-d2e5.jsonl	4	0.0000	❌miss		design a brand new logo
# done: 4 replayed turns; zones hit=3 grey=0 miss=1 grey_ratio=0.0% (target 30.0%)
# zones: hit ██████░░ grey ░░░░░░░░ miss ██░░░░░░
# ✅ PASS relevance=75.0% (≥70%)
```

Per-session cost meter — `semantix usage`:

```text
$ semantix usage
💰 节省成本  ██████░░░░  $0.008017
📈 节省率    57.0%
🧠 L3 复用   1
📦 命中切片  10
…
```

Icon legend (two icon families — retrieval zones vs. the verify gate):

| Where                                | Icons                     | Meaning                                                          |
| ------------------------------------ | ------------------------- | ---------------------------------------------------------------- |
| Retrieval zone (`search` / `lookup`) | 🟢 hit · 🟡 grey · ⚪ miss | 🟢 clearly reusable · 🟡 verify before reuse · ⚪ do not reuse    |
| Replay table (`verify` TSV)          | ✅hit · 🟡grey · ❌miss    | top-1 zone verdict for each replayed turn                        |
| Gate verdict (`verify` tail)         | ✅ PASS · ⚠ WARN · ❌ FAIL | ✅ relevance ≥ 70% · ⚠ grey ratio over target · ❌ under the bar |

---

# Features

## Semantic Slice Library

> **Status: shipped (v0.3.1)** — the extractor (P/T/R slices), BM25 + hybrid retrieval, and a file-backed store are implemented in `kernel/` with tests; local embeddings (Ollama/bge-m3) and an ANN index are the next step.

At the center of Semantix is the **Semantic Slice Library (SSL)**.

Semantix extracts reusable semantic units from historical agent sessions and stores them in a persistent vector-indexed library.

Rather than treating an entire conversation as one large memory object, Semantix separates reusable information into different kinds of slices.

| Slice       | Meaning               | Used for              | Example                           |
| ----------- | --------------------- | --------------------- | --------------------------------- |
| **P-Slice** | Prompt / task pattern | L2 context injection  | `run tests before committing`     |
| **C-Slice** | Context knowledge     | L2 context injection  | project structure, build commands |
| **T-Slice** | Tool behavior pattern | scheduling / prefetch | `grep → readFile → editFile`      |
| **R-Slice** | Reusable result       | L3 result reuse       | repeated lookup or explanation    |
| **M-Slice** | Memory unit           | retrieval / evolution | user or project preferences       |

A slice is not automatically permanent.

Slices can be scored using signals such as:

* historical hit rate
* recency
* intent relevance
* successful injection
* user corrections
* explicit feedback
* reuse frequency

Low-value slices decay.

Useful slices become easier to retrieve.

The objective is for the library to become:

> **more precise — not simply larger.**

---

# Three-Layer Semantic Cache

> **Status: partially shipped** — L2 deterministic injection (`semantix inject`) and L3 verified reuse (`kernel/fingerprint` + `kernel/judge` + `kernel/promote`, accepted per `docs/reports/issue-08-acceptance.md`) are implemented; real-harness end-to-end validation is pending.

Semantix adds semantic reuse on top of existing provider-side prefix caching.

```text
┌────────────────────────────────────────┐
│ L3 · Verified Result Reuse             │
│ Reuse work without a model call        │
├────────────────────────────────────────┤
│ L2 · Semantic Slice Injection          │
│ Meaning → stable canonical context     │
├────────────────────────────────────────┤
│ L1 · Provider Prefix / Byte Cache      │
│ Reuse identical prompt prefixes        │
└────────────────────────────────────────┘
```

Each layer has a different cost and risk profile.

---

## L1 — Provider Prefix Cache

Most provider-side prefix caches operate on identical or sufficiently stable prompt prefixes.

If the beginning of a request remains byte-stable, the provider may be able to reuse previously computed work.

This is fast and low risk.

But it has an important limitation:

```text
similar meaning ≠ identical bytes
```

Two prompts can mean almost the same thing while producing completely different cache keys.

Semantix keeps L1 as the foundation and tries to make the higher layers **feed it**.

---

## L2 — Semantic Slice Injection

This is one of the central ideas behind Semantix.

Suppose two different sessions have semantically similar tasks:

```text
Session A:
"Before finishing, run the Go test suite."

Session B:
"Make sure all Go tests pass before you're done."
```

The meaning is similar.

The bytes are not.

A normal prefix cache may treat them as unrelated.

Semantix instead retrieves a previously stored canonical slice:

```text
[workflow:test-before-finish]
Run `go test ./...` before marking the task complete.
```

That stable slice can then be inserted deterministically into the model context.

The flow becomes:

```text
semantic similarity
        │
        ▼
retrieve canonical slice
        │
        ▼
stable deterministic content
        │
        ▼
identical prompt bytes
        │
        ▼
provider prefix-cache hit
```

In other words:

> **Semantic caching does not replace the provider cache. It feeds it.**

To preserve cache stability, L2 injection should be deterministic:

* stable serialization
* stable ordering
* whole-slice injection
* deterministic retrieval
* injection-set freezing
* controlled updates

Changing the order or representation of otherwise identical slices can destroy the cache benefit.

---

## L3 — Verified Result Reuse

The highest cache layer can potentially skip a model request entirely.

For example:

```text
User asks:
"Where is the authentication middleware defined?"
```

If Semantix has already answered the same read-only question and the relevant files have not changed, it may be possible to reuse the existing result.

But:

> **semantic similarity is not semantic equivalence.**

For this reason, L3 is intentionally conservative.

Reuse can require checks such as:

* matching intent
* matching input fingerprint
* matching project
* unchanged dependent files
* file SHA / modification validation
* freshness window
* read-only task classification
* no unknown session state

L3 should be **fail-closed**.

If Semantix cannot prove that reuse is safe, it should send the task through the normal agent loop.

Write operations are never blindly replayed.

---

# Adaptive Kernel Scheduler

> **Status: shipped (MVP)** — `kernel/sched.RuleDecider` implements parallel groups, a behavior-learning gate, model tiering, and prefetch hints (see `docs/Agile路线图.md` H3); provider-level model switching is planned.

Semantix is not only a cache.

It also acts as a resource scheduler around the agent loop.

For every task, the kernel can consider:

```text
What is the user's intent?

Which context is useful?

Which model should handle the task?

Which tools are likely to be needed?

Which operations can run concurrently?

What can safely be reused?

What should be prefetched?

How much latency / token budget should be spent?
```

Inputs can include:

* task intent
* semantic slice matches
* historical tool patterns
* task complexity
* historical success rate
* model performance
* available resources
* latency budget
* token budget
* project state

The scheduler can then produce decisions such as:

```text
Model tier
    +
Context injection
    +
Tool concurrency
    +
Cache reuse
    +
Prefetch plan
```

The priority order is:

> **Correctness → Cache Reuse → Concurrency → Prefetch**

Optimization should never come before task correctness.

---

# Speculative Prefetch

> **Status: shipped (MVP)** — `kernel/prefetch` ships `Planner` (offline, Issue 62) + `MatrixPrefetcher` (online, hit/waste feedback) plus a `Runner`; wired into the scheduler via `prefetch.AsPlanFunc` (see `docs/Agile路线图.md` H5).

Agent execution contains a surprising amount of waiting.

The system may be waiting for:

* LLM streaming
* network requests
* tool output
* file indexing
* embeddings
* external APIs

During that time, local resources are often idle.

Semantix attempts to use this waiting time to prepare likely next-step information.

---

## Learning Tool Patterns

T-Slices represent observed tool sequences.

For example, historical behavior might look like:

```text
grep
  │
  │ 87%
  ▼
readFile
  │
  │ 72%
  ▼
editFile
  │
  │ 81%
  ▼
test
```

The percentages represent learned transition tendencies, not hardcoded workflow rules.

Semantix can use several signals:

### Tool transition patterns

```text
P(tool B | tool A)
```

Example:

```text
grep → readFile
```

### File-path patterns

A project may repeatedly follow workflows such as:

```text
package.json
      ↓
src/
      ↓
tests/
```

### Semantic similarity

A current tool call may resemble a historical call whose next step is known.

---

## Conservative Prefetch

Prefetch is fundamentally speculative.

Therefore Semantix should primarily prefetch **read-only** resources.

Good candidates include:

* next-turn semantic slice assembly
* embedding queries
* index lookups
* safe metadata retrieval
* read-only context preparation

Operations with side effects must not be speculated.

Semantix should never decide:

```text
"You usually edit this file next,
so I edited it for you."
```

Instead:

```text
"You usually inspect this file next,
so I prepared the relevant context."
```

---

## Learning From Waste

Each speculative operation can be classified as:

```text
prefetch hit
```

or:

```text
prefetch waste
```

Repeatedly inaccurate predictions reduce that predictor's weight.

Therefore the prefetcher should learn two things:

> **when to predict**

and

> **when not to predict.**

---

# Self-Evolving Optimization

> **Status: shipped (MVP)** — slice value scoring + capacity eviction are live: hit/injection accounting feeds `weight = recency · frequency · injection-success · feedback` (`kernel/slice` scorer), and `gc` / gateway startup enforce a capped, archived library. `kernel/evolve` remains an EWMA-tuning MVP (retrieval threshold only; signal wiring pending).

Semantix is designed around a feedback loop.

Every interaction generates signals.

Those signals influence future decisions.

```text
User behavior
      │
      ▼
Semantic slices
      │
      ▼
Retrieval + scheduling
      │
      ▼
Agent execution
      │
      ▼
Cost / latency / success / corrections
      │
      ▼
Feedback
      │
      ▼
Parameter updates
      │
      └──────────────────────► next interaction
```

Possible feedback signals include:

* L1 cache hit / miss
* L2 slice hit / miss
* L3 result reuse
* token cost
* latency
* task success
* user edits
* context pollution
* prefetch hit
* prefetch waste
* explicit approve / reject
* retries
* rollback events

---

## Online Adaptation

Lightweight statistics can adapt continuously.

For example:

* semantic similarity thresholds
* injection budgets
* prefetch thresholds
* model-tier selection
* concurrency limits

EWMA-style updates can allow the system to react to changing usage patterns without retraining a large model.

---

## Offline Optimization

More expensive optimization can happen periodically.

Examples:

* refresh embeddings
* retrain T-Slice transition matrices
* search better thresholds
* archive stale slices
* evaluate slice quality
* run ablation tests

The long-term goal is:

> **The system should improve because you use it.**

---

# Architecture

Semantix is designed as a kernel independent of any specific agent harness.

```text
┌─────────────────────────────────────────────────────────┐
│                     Agent Harness                        │
│                                                         │
│   Reasonix · Claude Code-style agents · custom agents   │
└──────────────────────────┬──────────────────────────────┘
                           │
                    events │ decisions
                           │
┌──────────────────────────▼──────────────────────────────┐
│                    SEMANTIX KERNEL                       │
│                                                         │
│  ┌─────────────────┐      ┌──────────────────────────┐ │
│  │ Intent          │      │ Semantic Slice Library   │ │
│  │ Perception      │      │ P / C / T / R / M       │ │
│  └────────┬────────┘      └────────────┬─────────────┘ │
│           │                            │               │
│  ┌────────▼────────┐      ┌────────────▼─────────────┐ │
│  │ Adaptive        │◄────►│ Semantic Cache          │ │
│  │ Scheduler       │      │ L1 / L2 / L3           │ │
│  └────────┬────────┘      └────────────┬─────────────┘ │
│           │                            │               │
│  ┌────────▼────────┐      ┌────────────▼─────────────┐ │
│  │ Evolution       │◄────►│ Speculative Prefetch    │ │
│  │ Engine          │      │ T-Slice prediction      │ │
│  └─────────────────┘      └──────────────────────────┘ │
│                                                         │
└──────────────────────────┬──────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│                    Resource Layer                        │
│                                                         │
│ LLM APIs · Tools · Files · Embeddings · Local Indexes   │
└─────────────────────────────────────────────────────────┘
```

The kernel observes the harness through an adapter/event layer.

It can then return optimization decisions without owning the agent's primary reasoning loop.

This allows Semantix to remain portable across different agent runtimes.

---

# How a Request Flows

A typical request can follow this path:

```text
User Request
      │
      ▼
┌────────────────────┐
│ Intent Perception  │
└─────────┬──────────┘
          │
          ▼
┌────────────────────┐
│ Semantic Retrieval │
└─────────┬──────────┘
          │
     ┌────┴──────────────────────┐
     │                           │
     ▼                           ▼
  L3 hit                      L2 hit
     │                           │
     ▼                           ▼
 Validate                   Stable slices
     │                           │
     ▼                           │
Reuse result                     │
                                 ▼
                       ┌────────────────────┐
                       │ Kernel Scheduler   │
                       └─────────┬──────────┘
                                 │
                    ┌────────────┼────────────┐
                    │            │            │
                    ▼            ▼            ▼
                  model      concurrency   prefetch
                    │            │            │
                    └────────────┼────────────┘
                                 │
                                 ▼
                         ┌──────────────┐
                         │ Agent Loop   │
                         └──────┬───────┘
                                │
                                ▼
                         Task Completed
                                │
                                ▼
                    Observe execution signals
                                │
                                ▼
                       ┌────────────────┐
                       │ Evolution      │
                       │ Engine         │
                       └───────┬────────┘
                               │
                               ▼
                         Update slices /
                          parameters
```

---

# Semantix + Reasonix

Reasonix is the initial architecture baseline and first integration target for Semantix. Since v0.3.0 the release bundle ships both binaries together (reasonix + semantix) with a shared install script and example config.

The two projects solve different layers of the agent-infrastructure problem.

| Capability                      | Reasonix                  | Semantix                   |
| ------------------------------- | ------------------------- | -------------------------- |
| Primary scope                   | Individual agent session  | Cross-session optimization |
| Agent execution loop            | ✅                         | Uses existing harness      |
| Stable prompt structure         | ✅                         | Builds on it               |
| Prefix-cache awareness          | ✅                         | ✅ feeds it                 |
| Context compaction              | ✅                         | Uses harness capability    |
| Cross-session semantic slices   | —                         | ✅                          |
| Semantic cache                  | Limited / session-focused | ✅ L1/L2/L3                 |
| User behavior learning          | —                         | ✅                          |
| Tool-pattern learning           | —                         | ✅                          |
| Adaptive scheduling             | Mostly static rules       | ✅ learned decisions        |
| Speculative prefetch            | —                         | ✅                          |
| Result reuse                    | Limited                   | ✅ verified L3              |
| Long-term optimization feedback | —                         | ✅                          |
| Self-evolving behavior          | —                         | ✅                          |

Reasonix asks:

> **How can this agent session run efficiently?**

Semantix asks:

> **How can every previous session make the next session better?**

Together:

```text
Semantix
   │
   ├─ Cross-session learning
   ├─ Semantic reuse
   ├─ Adaptive scheduling
   ├─ Prefetch
   └─ Evolution
   │
   ▼
Reasonix
   │
   ├─ Agent loop
   ├─ Context management
   ├─ Tool execution
   ├─ Prefix-cache stability
   └─ Provider interaction
   │
   ▼
LLM Provider
```

Semantix is not designed to replace Reasonix.

It is designed to make harnesses like Reasonix more efficient over time.

---

# Future Agent Integrations

Semantix's kernel is designed to stay harness-independent — "one kernel, many harnesses" (see [`docs/Agent-Infra-架构设计.md`](./docs/Agent-Infra-架构设计.md)). The integration surface is a small set of hooks (tool registration, message interception, session export / event bypass), so a new coding agent can be attached without touching the kernel.

| Agent / harness            | Integration path                                                             | Status                                    |
| -------------------------- | ---------------------------------------------------------------------------- | ----------------------------------------- |
| DeepSeek-Reasonix          | Built-in bundle: `[semantix] enabled=true` + `semantix_lookup` tool           | ✅ shipped since v0.3.0                   |
| Claude Code                | Tool registration via `semantix_lookup` / `semantix_inject` schemas           | ✅ path documented (`agent-skill/tools/`) |
| LangChain apps             | Middleware with two hooks (message rewrite + session extraction)              | ✅ path documented (`docs/reports/langchain-middleware.md`) |
| Custom / self-hosted agent | Session bypass: export / event bypass / direct call                          | ✅ path documented (`agent-skill/hooks/session-bypass.md`) |
| OpenAI Codex CLI           | Tool registration (function calling) + session export                         | 🔜 candidate — same path as Claude Code   |
| Cursor                     | Session export + context hook                                                 | 🔜 candidate                               |
| Windsurf                   | Session export + context hook                                                 | 🔜 candidate                               |
| GitHub Copilot (agent mode)| Function-calling tool registration                                            | 🔜 candidate                               |
| Gemini CLI                 | Tool registration + session export                                            | 🔜 candidate                               |
| Cline / Continue / Aider   | Tool registration or session-bypass                                           | 🔜 candidate                               |

Adapters are thin and share the same kernel surface; priorities are driven by where users actually work, so the list above is a candidate set rather than a commitment. If you maintain or use one of these agents and want a concrete integration, open an [integration request](https://github.com/Gnosil/semantix/issues) — the repo ships a template (`.github/ISSUE_TEMPLATE/integration_request.yml`).

---

# Design Principles

## Correctness Comes First

Semantix follows one priority order:

> **Correctness → Cache Reuse → Concurrency → Prefetch**

A cheaper wrong answer is not an optimization.

---

## Stable Prefixes Matter

Provider prefix caches depend heavily on stable request prefixes.

Semantix therefore treats:

* canonical serialization
* stable ordering
* deterministic retrieval
* injection freezing

as first-class infrastructure.

A semantic cache should not constantly mutate the exact bytes it is trying to make cacheable.

---

## Semantic Similarity Is Not Equivalence

Two pieces of content can have high embedding similarity while still differing in important details.

Therefore:

```text
L1 → low risk

L2 → controlled contextual reuse

L3 → strict verification
```

The higher the potential optimization benefit, the stronger the validation should be.

---

## Read Before Write

Speculative operations must remain safe.

Read-only prediction can be useful.

Speculative mutation is dangerous.

Semantix may preload a likely file.

It should never silently modify that file because history suggests the user usually does so next.

---

## Fail Open

Semantix is an optimization layer.

It should never become a dependency that prevents the underlying agent from functioning.

If:

* embeddings fail
* the ANN index is unavailable
* semantic retrieval times out
* optimization confidence is low
* internal state becomes inconsistent

Semantix should simply fall back to the original harness.

```text
Semantix failure
       │
       ▼
Skip optimization
       │
       ▼
Normal agent execution
```

Optimization failure must not become agent failure.

---

## Every Optimization Must Be Reversible

Semantic injection, reuse, scheduling, and prefetch are probabilistic decisions.

Every major mechanism should therefore be:

* observable
* explainable
* measurable
* traceable
* independently disableable

A self-evolving system must also be able to recognize when it is evolving in the wrong direction.

---

## Privacy by Design

The Semantic Slice Library may contain:

* source code
* project context
* user workflow patterns
* historical instructions
* internal file paths

The intended architecture therefore favors:

* local storage
* local indexing
* configurable retention
* sanitization
* project/user isolation
* clear deletion controls

Cross-project reuse must avoid leaking project-specific secrets or paths.

---

# Project Status

> **Semantix v0.3.1 is released** (2026-08-13), and **M2 CLI v2 (U19–U27) shipped** (2026-08-14): command tree, config wiring, `--json` envelope, shell completion, doctor, install, gc/export/import. The remaining gate before scaling up is **real-data validation of the cross-session hit rate**.

Architecture specification v2 is complete.

The project is implemented incrementally so each major optimization mechanism is evaluated independently.

Current status:

```text
Architecture v2                    ✅

Agile 1 · First downloadable agent   🚧 M0 ✅ · M1 near-complete (gate #58) · CLI v2 (U19–U27) ✅
  · Observability (P0)               ✅  kernel/event + kernel/usage
  · Semantic Slice Library (P1)      🚧  extract + BM25/hybrid shipped ✅ · local embeddings + ANN pending
  · Semantic Cache (P2)              🚧  L2 + L3 shipped ✅ · real-harness e2e pending
  · bundle + reuse visualization     🚧  v0.3.1 shipped; CLI reuse viz (U28–U31) ✅ · H4 UI pending

Agile 2 · Self-evolving loop          🚧 kernel-side MVP landed (M1-U18b); harness side pending
  · Adaptive Scheduler (P3)           ✅  kernel/sched.RuleDecider (MVP)
  · Speculative Prefetch (P4)         ✅  Planner + MatrixPrefetcher + Runner (MVP)
  · Value scoring & eviction          ✅  kernel/slice scorer + capped/archived gc
  · Evolution Loop (P5)               ✅  kernel/evolve (MVP); closed-loop wiring pending
  · H2 ResourceLayer / H3 orchestration ⏳ blueprint only

Agile 3 · Multi-harness ecosystem     ⏳  paths documented (agent-skill/); no adapter shipped
```

**First integration target shipped**: since v0.3.0 the release bundle packages **reasonix + semantix** together (per-platform archives, install script, example config).

**Gate**: M0-Gate passed conditionally (2026-08-09) — technical feasibility and cost savings (79.8% on synthetic replay, `docs/reports/m0-cost-comparison.md`) are verified; **real-data cross-session hit rate ≥ 70%** (`semantix verify`) is the remaining gate, per `docs/reports/m0-gate.md`.

**Agile roadmap**: Agile 1 (first downloadable, brandable agent) is in progress — M0 shipped, M1 nearly complete, with #58 (real-data hit rate) as the remaining gate; Agile 2 (self-evolving loop) and Agile 3 (multi-harness ecosystem) are defined in [`docs/Agile路线图.md`](./docs/Agile路线图.md).

---

# Roadmap

Execution is organized in **Agile cycles** — one downloadable milestone per Agile (see [`docs/Agile路线图.md`](./docs/Agile路线图.md)). The technical phases P0–P5 map onto them as follows:

| Agile | Milestone                                               | Technical scope                              | Status                                                                 |
| ----- | ------------------------------------------------------- | -------------------------------------------- | ---------------------------------------------------------------------- |
| **1** | First downloadable, brandable agent (v1.0)              | P0–P2 + bundle + reuse visualization (H4)     | 🚧 M0 ✅ · M1 near-complete · gate #58 · CLI v2 (U19–U27) ✅             |
| **2** | Self-evolving loop — kernel orchestrates the harness    | P3–P5 + H2 ResourceLayer + H3 orchestration  | 🚧 kernel-side MVP landed (M1-U18b); harness side pending               |
| **3** | Multi-harness ecosystem                                 | CLI install / serve / adapter contribution    | ⏳ paths documented; not started                                        |

### Technical phases (P0–P5) detail

| Phase  | Deliverable                                                                     | Status                                                              | In Agile |
| ------ | ------------------------------------------------------------------------------- | ------------------------------------------------------------------- | -------- |
| **P0** | Observability layer — harness adapter, event stream, baseline metrics           | ✅ shipped — `kernel/event`, `kernel/usage`                          | 1        |
| **P1** | Semantic Slice Library — extraction, embeddings, ANN index, project/user stores | 🚧 extraction + BM25/hybrid shipped ✅; local embeddings + ANN pending | 1        |
| **P2** | Semantic cache — stable L2 injection, verified L3 reuse, pollution detection    | 🚧 L2 + L3 shipped ✅; real-harness e2e pending                      | 1        |
| **P3** | Adaptive scheduler — intent classification, concurrency learning, model tier    | ✅ `kernel/sched.RuleDecider` MVP (M1-U18b); learning overlay pending | 2        |
| **P4** | Speculative prefetch — T-Slice prediction, path patterns, budget control        | ✅ Planner + MatrixPrefetcher + Runner MVP (M1-U18b)                 | 2        |
| **P5** | Evolution loop — online adaptation, offline optimization, ablation              | ✅ slice scoring/eviction shipped (`kernel/slice`); `kernel/evolve` MVP, closed-loop wiring + ablation pending | 2        |

Each stage should remain independently measurable.

The first major implementation hypothesis is:

> **Can deterministic semantic slice injection convert cross-session semantic similarity into provider-side prefix-cache hits without reducing task quality?**

---

# Target Metrics

The following numbers are **design targets**. Where a number has already been measured (see the Verification column), it comes from a specific report — none are claimed as reproducible benchmark results yet.

| Metric                               |                         Target | Verification                                                        |
| ------------------------------------ | -----------------------------: | ------------------------------------------------------------------- |
| Cross-session L2 cache hit rate      |                          ≥ 40% | pending real data (`semantix verify`)                               |
| Combined L1 + L2 cached input tokens |                          ≥ 90% | pending                                                             |
| Cost per task                        |   ≥ 50% reduction vs. baseline | 79.8% on synthetic replay (`docs/reports/m0-cost-comparison.md`)    |
| Prefetch utilization                 | ≥ 30% of eligible wait windows | n/a — P4 not implemented                                            |
| Context pollution rate               |                           ≤ 5% | pending                                                             |
| End-to-end latency                   |                     ≤ baseline | pending real-harness e2e                                            |

These targets are intended to guide implementation and evaluation.

They should not be interpreted as achieved performance until reproducible benchmarks are published.

---

# Evaluation Philosophy

Semantix should not be evaluated only by token reduction.

A useful optimization layer has to measure at least four things simultaneously:

```text
Cost
  +
Latency
  +
Task Quality
  +
Reliability
```

An optimization that reduces token cost but causes:

* more retries
* worse answers
* stale context
* incorrect tool actions
* higher user correction rates

is not a successful optimization.

The target is:

> **lower effective computation per successful task.**

---

# What Semantix Is Not

Semantix is **not**:

* a foundation model
* another coding model
* a prompt compressor
* a vector database wrapper
* a replacement for an agent harness
* blind replay of previous tool actions
* a generic memory database

It is:

> **an optimization and learning kernel around agent execution.**

---

# Documentation

Detailed architecture documents are available in [`docs/`](./docs).

### Quickstart

[`docs/QUICKSTART.md`](./docs/QUICKSTART.md) — install (release binary or source build), 30-second demo, command reference, security conventions.

### Agent Skill

[`agent-skill/SKILL.md`](./agent-skill/SKILL.md) — self-serve integration for any harness (Reasonix fork / LangChain / Claude Code / custom): install + selftest scripts, tool schemas, session-bypass hooks.

### Website

[`site/`](./site) — marketing site and product docs (Next.js), including the blog on semantic caching and cross-session reuse.

### Architecture

[`docs/Agent-Infra-架构设计.md`](./docs/Agent-Infra-架构设计.md)

Full architecture design covering:

* problem definition
* Reasonix baseline analysis
* Semantic Slice Library
* L1 / L2 / L3 cache
* scheduling
* speculative prefetch
* evolution
* risks
* roadmap
* metrics

### End-to-End Flow

[`docs/总体架构-流程树.md`](./docs/总体架构-流程树.md)

Structured request lifecycle covering:

```text
user request
→ intent
→ semantic retrieval
→ scheduling
→ agent loop
→ tools
→ completion
→ feedback
→ evolution
```

---

# Contributing

Semantix is still early.

Contributions, criticism, experiments, and architecture discussions are welcome.

Particularly useful areas include:

* semantic caching
* KV / prefix-cache optimization
* context engineering
* semantic retrieval
* agent scheduling
* speculative execution
* agent memory
* model routing
* behavioral learning
* local embeddings
* ANN indexing
* evaluation methodology
* Reasonix integration
* other harness adapters

If you disagree with an architectural assumption, open an issue.

Testing the assumptions is part of building the project.

---

# Acknowledgements

Semantix uses **DeepSeek-Reasonix** as its initial architecture baseline and first intended integration target.

Reasonix's work on:

* cache-stable agent execution
* context maintenance
* event-driven architecture
* tool orchestration
* session memory
* loop engineering

directly informed several Semantix design decisions.

Semantix extends those ideas toward persistent, cross-session optimization.

---

# License

Semantix is licensed under the **Functional Source License, Version 1.1, MIT Future License (FSL-1.1-MIT)**.

Each release becomes available under the **MIT License** on the second anniversary of its release date, according to the terms of FSL-1.1-MIT.

See [`LICENSE`](./LICENSE) for the full license terms.

---

<div align="center">

### Semantix

**Every interaction should make the next one cheaper, faster, and smarter.**

</div>
