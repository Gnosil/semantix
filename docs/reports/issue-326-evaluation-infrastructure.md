# Issue #326 — authoritative evaluation infrastructure

Status: infrastructure complete; full three-seed SWE-bench Verified rollout remains a separately funded execution step.

## Protocol boundary

The runner exposes two mutually exclusive protocols and records the selection in
`run_config.json` and every Semantix cost record:

| Track | Runner flag | State semantics | Publication rule |
| --- | --- | --- | --- |
| A — capability | `--protocol standard` (default) | fresh checkout and an isolated, empty kernel store for every instance | leaderboard-comparable after official SWE-bench scoring |
| B — reuse economics | `--protocol grouped` | frozen input order, serialized within each repository, repository-isolated store retained across instances | non-standard; report separately from Track A |

`--semantix-memory on|off` selects the paired `full` / `no-kernel` arms. The
harness ablation registry also includes `kernel`, but benchmark memory comparisons
use the explicit runner flag so store lifecycle is unambiguous. A frozen seed store
is rejected under the standard protocol.

## Reproducible artifacts

Each run produces:

- `predictions.jsonl`: patches in official SWE-bench prediction format;
- `cost.jsonl`: per-instance provider usage, USD cost, wall time, errors, empty
  patches, kernel counters, protocol, and native metrics;
- `run_config.json`: CLI protocol parameters plus the runner commit SHA;
- `audit/<instance>.txt`: an opt-in, mode-0600 verbatim record of strict-mode L2 blocks assembled for
  memory-enabled Semantix runs.

The legacy `preds.jsonl` and `metrics.jsonl` aliases remain for existing tooling.
Scoring continues to delegate pass/fail exclusively to
`swebench.harness.run_evaluation`; no project code implements a correctness judge.

## Leakage gate

`scan_injection_leaks.py` compares every audited block against the official dataset
row for that instance. It exits non-zero for an exact gold patch, a `FAIL_TO_PASS`
identifier, an unknown audit file, or a missing expected audit file. The JSON report
is suitable for publishing beside Track B results. Audit files can contain retrieved
task context and must be handled as sensitive experiment artifacts.

## Evidence and remaining execution

Unit tests cover protocol isolation/grouping, config round-trip, private audit-file
creation, complete multi-block append behavior, and fail-closed leakage scanning.
The existing 50-instance single-seed report remains historical evidence only; it
does not satisfy #326's `n >= 3` publication rule. A release claim still requires
the predeclared model, sample, three or more seeds, official Docker evaluation,
confidence intervals, and the Track A/Track B reports generated from those runs.
