# Issue #447 P0.3 Repo-Isolated Store Plan

## Goal

Stop cross-repository memory contamination in the standard SWE-bench runner while retaining parallel throughput:

```text
semantix-home/kernel/<owner>__<repo>/.semantix/project.db
```

Instances from the same repository must execute in their existing selected order. Different repositories may execute concurrently up to `--workers`.

## Design

1. Validate the dataset `repo` field as exactly `owner/repo` using path-safe GitHub components. Invalid/missing identities fail before execution instead of sharing an `unknown` store.
2. Resolve one kernel directory per instance from that canonical identity and pass it to both agent configuration and post-instance extraction.
3. Stamp extracted slices with the real `owner/repo` project identity instead of the constant `swebench`.
4. Add an adapter scheduling contract:
   - ordinary adapters return one instance per batch, preserving existing parallel behavior;
   - Semantix memory-off does the same;
   - Semantix memory-on groups instances by repo in first-seen repo order and preserves selected order inside each batch.
5. Submit batches—not individual same-repo instances—to the worker pool. Each batch executes synchronously in its worker; repositories remain parallel.

## Tests first

- canonical repo key and fail-closed invalid identities;
- distinct repos resolve distinct Project stores;
- same-repo interleaved instances become one ordered batch;
- memory-off keeps per-instance batches;
- generated config and extraction command use the same repo-specific directory and project identity.

## Acceptance

```powershell
python -m py_compile scripts/swebench/run_bench.py scripts/swebench/test_repo_isolation.py
python -u scripts/swebench/test_repo_isolation.py -v
python -u scripts/swebench/test_metrics_attribution.py -v
git diff --check upstream/main...HEAD
```
