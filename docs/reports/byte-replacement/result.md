# GLM-5.3-Flash byte-replacement comparison — 2026-09-28

Three synthetic, explicitly managed project-context questions were run through
the official GLM Coding Plan Chat Completion endpoint using `glm-5.3-flash`.
Each question used the same model, output limit and sampling settings in all
arms. The policy text was the same, but its experiment prefix included the arm
name; this small prompt difference means the earlier A/B/D comparison was not
perfectly controlled. Arm B adds the actual Semantix L2 block rendered by
`kernel/inject.Injector.BuildHits`; arm D is constructed by the Python runner,
which compacts the bounded source JSON and omits L2. It does not call the Go
`buildSamplingRequest` replacement branch. The nine requests, answers
and provider usage records are in `comparison-5.3-flash-plan-aligned.jsonl`.
The earlier `comparison-5.3-flash-plan.jsonl` used `project="demo"` in the
managed message but `project="replacement-experiment"` in L2 provenance. The
aligned report fixes that prerequisite, but neither live report exercises the
Go runtime gate; a separate focused Go check covers that branch.

The runner now uses the same system message for all arms of a case. Its first
rerun request returned HTTP 429 / code 1302, before any comparable results
were collected; the one-row attempt is in
`comparison-5.3-flash-plan-same-prompt-attempt.jsonl`. The token table below
still reports the earlier arm-labeled run and should not be treated as a
same-prompt result.

A separate `run_runtime.py` now captures A/B/D requests through actual
`Agent.Run` calls with a recording provider before sending them to GLM. Its
three-case dry run confirmed all nine request shapes, including strict L2 and
the Go replacement branch, with the same system message and current question
within each case. No live model result has been collected from these captured
requests yet. The current Go branch preserves the slice provenance in a short
reference while omitting the duplicate body; the earlier 56.8% token figure
was measured on the Python arm that removed the entire L2 block, so it is not
an estimate for this revised runtime behavior. Across the three dry-run cases,
the captured message content totals 6,081 bytes in strict mode and 4,154
bytes in replace mode (**31.7% fewer bytes**); off mode totals 3,476 bytes.
These are UTF-8 request-content bytes, not provider token counts or charges.

| Arm | Correct | Prompt | Cached prompt | Noncached prompt | Completion | Total |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| A original context | 3/3 | 646 | 64 | 582 | 304 | 950 |
| B original + L2 injection | 3/3 | 1253 | 64 | 1189 | 354 | 1607 |
| D replacement | 3/3 | 541 | 64 | 477 | 326 | 867 |

D used **56.8% fewer provider-reported input tokens than B** and **16.3%
fewer than A**. Total input plus output tokens fell 46.0% versus B. The
noncached input and completion counts also fell versus B; cached input was
equal. The provider did not report a monetary charge or Coding Plan point cost,
so this report does not claim a measured currency or plan-quota saving. Under
the same nonnegative per-token rates by category, D's aggregate charge would
be lower than B's; no such conclusion follows versus A because D used more
completion tokens than A.

This is one small structured-context run, not proof that arbitrary prose,
tool outputs, conflicting facts or multi-turn tasks retain their behavior.
The runner does not execute an ordinary Semantix agent session. PR #518 adds a
provider-request replacement path for a matching managed source block, but
no production source currently emits that block automatically. The saved
transcript remains unchanged.

The harness writes a session mirror, while slice extraction into the L2
project library is a separate step. The SWE-bench runner performs that step
after each instance before the next one in the same repository. A request
against an empty library cannot measure replacement opportunities. The older
10-instance SWE-bench pilot did exercise real cross-instance injection and
reported 6,932,300 input tokens and $0.0980 with memory on, versus 4,353,870
and $0.0544 with memory off (`docs/reports/swe-pilot-two-arm.md`). That is
evidence that the former injection setup could increase cost, not a
GLM-5.3-Flash baseline or an estimate for today's stricter admission rules.

The earlier attempt on the ordinary API endpoint returned HTTP 429 / code
1113; its failed request remains in `comparison-5.3-flash.jsonl`. GLM Coding
Plan uses a separate endpoint. Official model and endpoint references:
https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5.3-flash and
https://docs.bigmodel.cn/cn/coding-plan/quick-start.

## Existing session mirror census

The read-only `probe_local.go` tool inspected 544 local Semantix SWE coding
session mirrors from 85 DeepSeek-v4-Flash experiment variants. One session
could not be extracted because its record exceeded the extractor's line limit;
the remaining sessions yielded 537 Context and 404 Result slices. The probe
compared each slice body with every non-system message in its own session, an
upper bound on exact duplicate opportunities rather than a runtime hit rate.
Context matched **0/537** messages. Result matched **394/404**, but **0/404**
were marked verified by the current extraction rule, so none would pass the
current L2 Result admission gate. None of the 42,491 rows contained the
`verification` or `workspace_mutation` host fields; the zero verified count
reflects missing host evidence, not proof that no task ran a successful test.
No managed-context marker occurred in any message. The corpus uses an older
model and session format, not GLM-5.3-Flash, and does not establish answer
quality or cost.

This rules out treating the controlled JSON saving as an observed benefit on
that corpus. A broader replacement must first locate an admitted slice that
duplicates provider-visible content and preserve its untrusted provenance;
otherwise the existing injection should remain.

The repository's `harness/agent/memory_flow_e2e_test.go` exercises the real
agent request path with verified history from earlier sessions. Its new session
does not contain the historical command in the `off` or `shadow` provider
request; `strict` adds that knowledge through L2. Thus a blanket removal of
L2 would lose information in this concrete workflow. Replacement can preserve
the available facts only when the provider request already holds the same
source content, or when a shorter host-owned representation demonstrably
retains what the task needs. The controlled JSON case establishes only the
first, explicitly constructed condition.

## Earlier GLM-4.7 pilot

The numbers below came from GLM-4.7 and must not be presented as
GLM-5.3-Flash results.

Three synthetic, explicitly managed project-context questions were sent through
the official `open.bigmodel.cn` chat endpoint. Each task used identical model,
policy, question, output limit and temperature across the three arms. Arm B's
extra message was assembled by Semantix `kernel/inject.Injector.BuildHits`; arm D
replaced the bounded source context with compact JSON retaining every field and
value. Raw requests, answers and provider usage are in `comparison.jsonl`.

| Arm | Correct / 3 | Prompt tokens | Cached | Completion tokens |
| --- | ---: | ---: | ---: | ---: |
| A original context | 3 | 619 | 0 | 103 |
| B original + L2 injection | 3 | 1220 | 0 | 103 |
| D replacement | 3 | 512 | 0 | 103 |

Replacement reduced provider-reported input tokens by **58.0% versus current
injection** and **17.3% versus no injection**. This improvement came from
removing duplicate context and compact serialization, not cache reuse. With
equal output tokens, no cache hit, and identical model tariff, D is cheaper
than both A and B. Using the repository's August 2026 official-pricing snapshot
for this GLM-4.7 length tier (¥2 per million input; ¥8 per million output),
the estimated total for these three requests is ¥0.003264 (B), ¥0.002062 (A),
and ¥0.001848 (D): **43.4% less than B, 10.4% less than A**. These are
conditional tariff estimates, not verified account charges or current tariff.

This result supports the narrow mechanism: an already available, fully
structured context block can be replaced with the same facts and save tokens.
It does not show equivalent behavior for arbitrary prose, mixed tool output,
conflicting facts, or multi-turn work. The script does not modify Semantix's
production request assembly. Extending it would require a real managed block
and a fail-open path at `context.prepare`, with the original transcript kept.

Pricing snapshot: `docs/reports/glm-spike-week.md` (section 4). Official API
reference: https://docs.bigmodel.cn/cn/guide/develop/http/introduction.
Coding Plan endpoint and tool scope:
https://docs.bigmodel.cn/cn/coding-plan/quick-start.
