# Issue #447 A–D memory matrix pilot

Date: 2026-09-02

This report records the first end-to-end pilot of the paired memory matrix. It is
pipeline evidence, not a causal estimate of memory quality.

## Setup

- Dataset: SWE-bench Verified
- Instance: `django__django-13195`
- Repetitions: 1
- Maximum turns: 40
- Arm order: A → B → C → D
- State and work directories: isolated per arm
- A/B/C: current agent; D: pre-policy legacy agent

The four arm runs completed and produced patches. Official resolved scoring was
not part of this pilot, so resolved is reported as `n/a`.

## Observed metrics

| Arm | Policy | Executor calls | Input tokens | Tool calls | Repeated tool calls | Wall time (ms) | Cost (USD) | Patch bytes |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| A | memory off | 43 | 1,305,440 | 48 | 0 | 176,061 | 0.02691086 | 2,200 |
| B | shadow retrieval | 51 | 1,608,715 | 60 | 0 | 222,067 | 0.032217724 | 4,947 |
| C | strict current policy | 38 | 1,006,020 | 46 | 1 | 164,577 | 0.024804972 | 2,662 |
| D | legacy all-type policy | 76 | 2,980,415 | 87 | n/a | 303,511 | 0.05220650 | 4,947 |

Paired deltas against A:

| Arm | Δ executor | Δ input tokens | Δ tool calls | Δ repeated calls |
|---|---:|---:|---:|---:|
| B | +8 | +303,275 | +12 | 0 |
| C | -5 | -299,420 | -2 | +1 |
| D | +33 | +1,674,975 | +39 | n/a |

`n/a` for D repeated calls is intentional: the legacy binary did not emit the
attribution field. Missing telemetry must not be normalized to a real zero.

## Causal boundary

Each memory-on arm started with an empty, isolated store. B, C, and D recorded no
`semantix_inject_turns` during the instance and extracted three slices only after
the run. Therefore the large D-minus-A delta occurred without memory injection
and cannot be attributed to an injected slice or replacement policy.

This observation adds a third live hypothesis to the two product hypotheses:

1. Sparse or poorly matched slices may cross the admission threshold and cause
   intent drift.
2. Excessive replacement may remove constraints, inducing rework or repeated
   execution.
3. Model/provider/runtime variance can produce a large step delta even when no
   memory was injected.

The pilot supports the measurement pipeline but does not rank these hypotheses.

## Issues found by the pilot

The actual run exposed and drove fixes for:

- cross-platform workspace locking and cleanup;
- persistence of Semantix CLI arguments, exit code, stdout, and stderr;
- selection of the repository's canonical DeepSeek model alias;
- separation of missing repeated-call attribution from a measured zero.

## Next acceptance gate

Run the frozen 50-instance subset for at least three repetitions after warming
an isolated store with earlier instances from the same repository. Pair every
comparison by `(repetition, instance_id)` and require all of the following before
claiming causality:

1. a recorded injection event;
2. the exact admitted slice IDs, scores, bytes, and replacement decision;
3. a statistically stable regression in executor calls, input tokens, repeated
   tool calls, or resolved rate;
4. replay or slice-removal evidence that the regression disappears when the
   implicated slice is shadowed or excluded.
