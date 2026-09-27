# Byte replacement research protocol

Status: in progress. Baseline revision: `f527fd7` (2026-09-27).

## Question and pre-registered gates

Can replacing explicitly bounded, Semantix-managed context reduce actual GLM
input tokens and total estimated cost without degrading task correctness?
Issue #493's canonical-prefix hypothesis is a separate mechanism: a cached input
token is still an input token. Report token reduction and cache discounts separately.

Before collecting comparative results, use these gates:

- At least 60 paired held-out tasks, with exact expected outputs and altered-fact
  counterexamples. No observed replacement-only failures. Report paired uncertainty;
  zero failures alone does not prove universal semantic equivalence.
- At least 30% aggregate provider-reported input-token reduction versus both the
  no-memory and current-injection arms; at least 20% lower complete estimated cost.
- Include output/reasoning, preprocessing/judge calls, retries and failed requests
  where usage is reported. Missing billable usage is unknown, never zero.
- Cost estimates use verified official tariffs with dated evidence; they are not
  account invoices. Unknown prices block a cost-success claim.
- Synthetic structured-context results establish feasibility only. Follow with
  repository-grounded tasks and multi-turn/tool-use checks before recommending
  production use. Do not describe a hand-constructed compression ratio as a
  general production saving.

## Arms

A: original context, no L2 injection.
B: original context plus `kernel/inject.Injector.BuildHits` output, preserving its
metadata/trust wrapper. This isolates assembly with controlled retrieved candidates;
it does not measure retrieval quality or the entire agent.
C: replace the original bounded context with stable, complete canonical prose.
D: replace it with a compact complete representation of the same facts.

Same official endpoint, model, tool schemas, system policy, output limit, thinking
mode and task for every arm. Disable L3. Randomize arm order within tasks and record
it. Distinct experiment/arm prefixes prevent accidental cross-arm cache warming.
Record cold-like first observations and intentional warm repeats separately; absent
provider eviction controls, do not claim a proven cold cache or TTL expiry.

## Replacement boundary

Only explicitly managed declarative blocks with a complete supported schema may
be replaced. A plan binds project, source revision, block identity, original SHA256,
canonical version and complete fact equivalence. Unknown fields, conflicting
duplicates, unsupported syntax, stale/foreign source, ambiguous targets and changed
source hashes cause rejection with the original request intact. Similarity scores
are not equivalence evidence. No rewriting current user instructions, system policy,
reasoning, arbitrary tool results, or authority levels. Preserve original transcript;
freeze the request projection across retries.

## Current source audit

- `harness/agent/sampling_request.go`: `buildSamplingRequest` adds L2 before the last
  user message, then role projection, `context.prepare`, `provider.request`, freezing.
  `prependSemantixHistory` retains all original messages. Existing interception is
  a possible experimental seam; no new history store is needed.
- `kernel/inject/inject.go`: `BuildHits` reuses production admission and rendering;
  `formatSliceItem` includes score, source, commit, origin and creation time. Stable
  text at this block alone does not establish a stable provider prefix.
- `gateway/pipeline.go`: L2 injection and L3 reuse are distinct. Bytes/4 fields are
  estimates and cannot establish actual token reduction. L3 bypass must not count
  as a prompt-cache gain.

## Official endpoint preflight

One authorized request to `https://open.bigmodel.cn/api/paas/v4/chat/completions`
with `glm-4.7`, temperature 0, thinking disabled and max_tokens 32 returned `OK`.
Provider usage: prompt 10, cached 0, completion 2, reasoning 0, total 12.
This proves endpoint access and usage telemetry only; it proves no savings.
Credentials were entered through a no-echo prompt and were not saved.

Official endpoint reference:
https://docs.bigmodel.cn/cn/guide/develop/http/introduction
