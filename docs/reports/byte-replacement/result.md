# Superseded GLM-4.7 pilot — 2026-09-28

The requested model is **GLM-5.3-Flash**. The numbers below came from
GLM-4.7 and must not be presented as GLM-5.3-Flash results. A rerun using
`glm-5.3-flash` reached the official endpoint, but the provider returned
HTTP 429, code 1113 (insufficient balance or no available resource pack).
No valid GLM-5.3-Flash token or quality comparison is available yet. The
failed request is recorded in `comparison-5.3-flash.jsonl`.

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
