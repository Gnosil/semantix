# Issue #447 — P0.2 Shadow Retrieval

## Purpose

Shadow retrieval measures what the L2 memory path *would* inject without changing the provider request. It is the calibration step before adding small-library, slice-type, coverage, margin, and verification gates.

## Configuration

```toml
[semantix]
enabled = true
mode = "shadow" # off | shadow | strict
```

| Mode | Retrieves | Emits diagnostics | Adds `[semantix-reuse]` to provider messages |
|---|---:|---:|---:|
| `off` | no | no | no |
| `shadow` | yes | yes | no |
| `strict` | yes | yes | yes, when candidates are admitted |

Compatibility: when `mode` is absent, the existing `inject` boolean remains authoritative (`true` maps to `strict`; `false` maps to `off`). An invalid explicit value fails closed to `off`.

## Per-turn record

The existing `kernel_cache` event carries a `retrieval` object with:

- effective mode, Project-scope library size, repository directory, and HEAD commit;
- SHA-256/byte/token summaries for the pre/post-cleaning query (currently identical because no query cleaner is enabled);
- top-1 minus top-2 BM25 margin;
- score-ordered candidates with slice ID/type, source session, project, origin, verification status, absolute BM25 score, query coverage, zone, admission boolean, and exact reason;
- canonical final slice order, actual injection byte count, message role, and terminal decision.

Candidate reasons are stable replay inputs: `admitted`, `below_min_score`, `zone_grey`, `zone_miss`, `sanitized_empty`, `origin_below_floor`, `budget`, and `nil_slice`.

`verified` is deliberately `unknown` in this phase. Existing slice provenance is not a successful-evaluation marker and must not be relabelled as verification.

## Provider-byte invariant

The agent request builder only inserts Semantix content when `InjectResult.Text` is non-empty. Shadow executes the same search, zone, sanitization, provenance, and budget path as strict, but returns empty `Text`, does not increment injection statistics, does not emit `SliceInject`, and disables speculative injection warm-up. A regression test serializes the resulting provider message projection and requires byte equality between `off` and `shadow`.

## What this phase does not change

- BM25 parameters or zone thresholds;
- allowed slice types;
- library-size, coverage, or margin admission thresholds;
- Result-slice verification policy;
- provider prompt content in legacy `inject=true` (`strict`) mode.

Those controls follow after shadow traces establish score and failure distributions.
