# Issue #447 P0 Metrics Attribution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Expose Semantix executor, planner, subagent, compaction, other-model, retry, and per-tool call counts in every normalized SWE-bench instance record and aggregate report so memory ON/OFF step deltas are attributable.

**Architecture:** Keep `harness/cli/run_metrics.go` as the authoritative producer: it already emits `steps`, `usage_by_source`, `retries`, `compactions`, and `tool_calls_by_name`. Add a narrow normalization layer in the Semantix SWE-bench adapter, store both the complete source map and stable convenience counters, then aggregate those fields in `report.py`. This package does not alter the agent loop, retrieval, injection, or model requests.

**Tech Stack:** Python 3 standard library (`dataclasses`, `unittest`, `tempfile`, `json`); existing Go metrics JSON contract.

**Spec:** `docs/specs/semantix-memory-negative-transfer.md` §5.1 (PR #448) and parent tracking issue #447 §6 P0.1.

## Global Constraints

- Preserve existing `steps` semantics: every billed model call, regardless of source.
- Preserve backward compatibility with metrics JSON that lacks `usage_by_source`, retry, compaction, or per-tool maps.
- Keep unknown/future usage sources instead of dropping them.
- Never infer executor calls by subtracting named auxiliary sources; use the producer's explicit `executor` bucket.
- Report reconciliation explicitly: `source_call_total` and `source_call_delta = steps - source_call_total`.
- Add no runtime dependencies.
- This package is observability-only and must not change provider request bytes or agent behavior.

---

### Task 1: Normalize Semantix call-source telemetry per instance

**Files:**
- Modify: `scripts/swebench/common.py:58-78`
- Modify: `scripts/swebench/run_bench.py:271-279`
- Create: `scripts/swebench/test_metrics_attribution.py`

**Interfaces:**
- Consumes: raw `run --metrics` keys `steps`, `usage_by_source`, `retries`, `compactions`, `subagent_runs`, `tool_failures`, and `tool_calls_by_name`.
- Produces: `InstanceMetrics.model_calls_by_source: dict[str, int]`, `executor_calls`, `planner_calls`, `subagent_calls`, `compaction_calls`, `other_model_calls`, `source_call_total`, `source_call_delta`, `provider_retries`, `compactions`, `subagent_runs`, `tool_failures`, and `tool_calls_by_name`.

- [x] **Step 1: Write failing normalization tests**

Create `scripts/swebench/test_metrics_attribution.py` using `unittest`. Insert the script directory into `sys.path`, import `InstanceMetrics` and `SemantixAdapter`, instantiate the adapter without running `prepare`, and cover:

```python
def test_semantix_fill_metrics_attributes_every_model_call(self):
    raw = {
        "steps": 11,
        "usage_by_source": {
            "executor": {"calls": 5},
            "planner": {"calls": 1},
            "subagent": {"calls": 2},
            "compaction": {"calls": 1},
            "classifier": {"calls": 2},
        },
        "retries": 3,
        "compactions": 1,
        "subagent_runs": 2,
        "tool_calls": 8,
        "tool_failures": 1,
        "tool_calls_by_name": {"read_file": 4, "grep": 3, "bash": 1},
    }
    metrics = self.new_metrics()
    self.adapter.fill_metrics(metrics, raw)
    self.assertEqual(metrics.executor_calls, 5)
    self.assertEqual(metrics.planner_calls, 1)
    self.assertEqual(metrics.subagent_calls, 2)
    self.assertEqual(metrics.compaction_calls, 1)
    self.assertEqual(metrics.other_model_calls, 2)
    self.assertEqual(metrics.source_call_total, 11)
    self.assertEqual(metrics.source_call_delta, 0)
```

Also cover an old record without `usage_by_source` (`source_call_total == 0`, `source_call_delta == steps`) and malformed/negative source buckets being ignored rather than crashing.

- [x] **Step 2: Run the test and confirm RED**

Run:

```powershell
python scripts/swebench/test_metrics_attribution.py -v
```

Expected: FAIL because the new `InstanceMetrics` fields do not exist.

- [x] **Step 3: Extend the normalized metrics schema**

Add these defaulted fields to `InstanceMetrics` after `tool_calls`:

```python
model_calls_by_source: dict[str, int] = field(default_factory=dict)
executor_calls: int = 0
planner_calls: int = 0
subagent_calls: int = 0
compaction_calls: int = 0
other_model_calls: int = 0
source_call_total: int = 0
source_call_delta: int = 0
provider_retries: int = 0
compactions: int = 0
subagent_runs: int = 0
tool_failures: int = 0
tool_calls_by_name: dict[str, int] = field(default_factory=dict)
```

Keep the existing defaults so old adapters remain source-compatible.

- [x] **Step 4: Add defensive call-map normalization**

In `run_bench.py`, add a private helper near `sum_ledger`:

```python
def normalize_call_counts(value: object) -> dict[str, int]:
    if not isinstance(value, dict):
        return {}
    out = {}
    for source, bucket in value.items():
        if not isinstance(source, str) or not isinstance(bucket, dict):
            continue
        calls = bucket.get("calls")
        if isinstance(calls, bool) or not isinstance(calls, int) or calls < 0:
            continue
        out[source] = calls
    return out
```

Use the same non-negative integer rule for `tool_calls_by_name` values; preserve unknown names.

- [x] **Step 5: Populate source and trajectory counters**

In `SemantixAdapter.fill_metrics`, retain existing token/cost assignments, then populate the new fields. Define the named set as `{executor, planner, subagent, compaction}`; sum every other source into `other_model_calls`. Set:

```python
m.source_call_total = sum(m.model_calls_by_source.values())
m.source_call_delta = m.steps - m.source_call_total
```

Copy `retries` to `provider_retries`; do not rename or mutate the raw payload stored under `m.raw`.

- [x] **Step 6: Run the normalization tests and confirm GREEN**

Run:

```powershell
python scripts/swebench/test_metrics_attribution.py -v
```

Expected: all tests PASS.

- [x] **Step 7: Commit the normalized per-instance metrics**

```powershell
git add scripts/swebench/common.py scripts/swebench/run_bench.py scripts/swebench/test_metrics_attribution.py
git commit -m "feat(swebench): attribute semantix model calls by source"
```

---

### Task 2: Aggregate attribution fields in benchmark reports

**Files:**
- Modify: `scripts/swebench/report.py:18-75`
- Modify: `scripts/swebench/test_metrics_attribution.py`

**Interfaces:**
- Consumes: normalized JSONL fields produced by Task 1.
- Produces: aggregate totals for named model-call sources, unknown sources, retries, compactions, subagent runs, tool failures, and tool names; Markdown includes a compact call-source column.

- [x] **Step 1: Write failing report aggregation tests**

Use `tempfile.TemporaryDirectory` to create a run directory with two `metrics.jsonl` rows. Assert:

```python
self.assertEqual(aggregate["executor_calls"], 8)
self.assertEqual(aggregate["planner_calls"], 2)
self.assertEqual(aggregate["subagent_calls"], 3)
self.assertEqual(aggregate["compaction_calls"], 2)
self.assertEqual(aggregate["other_model_calls"], 4)
self.assertEqual(aggregate["provider_retries"], 3)
self.assertEqual(aggregate["tool_calls_by_name"], {"bash": 2, "grep": 4})
```

Add one backward-compatibility row with no new keys and assert `load_run` treats them as zero.

- [x] **Step 2: Run the report tests and confirm RED**

Run:

```powershell
python scripts/swebench/test_metrics_attribution.py -v
```

Expected: FAIL because `report.load_run` omits attribution totals.

- [x] **Step 3: Add backward-compatible aggregators**

Add small helpers in `report.py`:

```python
def sum_int(metrics: list[dict], key: str) -> int:
    return sum(m.get(key, 0) for m in metrics)

def sum_count_maps(metrics: list[dict], key: str) -> dict[str, int]:
    out = {}
    for row in metrics:
        for name, count in row.get(key, {}).items():
            out[name] = out.get(name, 0) + count
    return dict(sorted(out.items()))
```

Use them in `load_run` for every new counter and `model_calls_by_source`/`tool_calls_by_name`.

- [x] **Step 4: Add a compact Markdown call breakdown**

Add one column named `calls E/P/S/C/O` formatted as `executor/planner/subagent/compaction/other`, plus `tools` and `retry`. Keep JSON output fully structured.

- [x] **Step 5: Run report tests and confirm GREEN**

Run:

```powershell
python scripts/swebench/test_metrics_attribution.py -v
```

Expected: all tests PASS.

- [x] **Step 6: Commit report aggregation**

```powershell
git add scripts/swebench/report.py scripts/swebench/test_metrics_attribution.py
git commit -m "feat(swebench): report model call attribution"
```

---

### Task 3: Document and verify the attribution contract

**Files:**
- Modify: `scripts/swebench/README.md:68-90`
- Modify: `docs/specs/swebench-memory-arm.md`

**Interfaces:**
- Consumes: Task 1/2 field names and semantics.
- Produces: operator-facing documentation that distinguishes total steps, source-attributed calls, compaction attempts, retries, and tool calls.

- [x] **Step 1: Document the JSON contract**

Add a table defining:

- `steps`: total billed model calls;
- `model_calls_by_source`: authoritative complete source map;
- `executor_calls`, `planner_calls`, `subagent_calls`, `compaction_calls`: convenience counters;
- `other_model_calls`: sum of classifier/title/capability-router/recovery-reviewer/goal-evaluator and future sources;
- `source_call_delta`: `steps - source_call_total`, expected zero for current complete metrics;
- `provider_retries`: transport retry events, not billed calls unless a usage event exists;
- `compactions`: compaction attempts, distinct from `compaction_calls`;
- `tool_calls_by_name`: completed tool calls by canonical name.

- [x] **Step 2: Run formatting and focused tests**

Run:

```powershell
python -m py_compile scripts/swebench/common.py scripts/swebench/run_bench.py scripts/swebench/report.py scripts/swebench/test_metrics_attribution.py
python scripts/swebench/test_metrics_attribution.py -v
git diff --check
```

Expected: all commands exit 0.

- [x] **Step 3: Run relevant Go producer tests**

Run:

```powershell
go test ./harness/cli -run 'TestUsageBySource|TestMetrics|TestRunMetrics' -count=1
```

Expected: PASS. This confirms the producer contract consumed by Python still works.

- [x] **Step 4: Record the unrelated baseline failure**

Run:

```powershell
go test ./cmd/semantix -run '^TestDispatchExitCodeContract$' -count=1
```

Expected on baseline `eceb763`: FAIL at `main_test.go:291` because `run(nil, ...)` returns 0 instead of the test's expected usage code 2. This package does not touch `cmd/semantix`; do not claim a clean full-suite baseline.

- [x] **Step 5: Commit documentation**

```powershell
git add scripts/swebench/README.md docs/specs/swebench-memory-arm.md
git commit -m "docs(swebench): define call attribution metrics"
```

- [x] **Step 6: Final branch verification**

Run:

```powershell
git diff --check upstream/main...HEAD
python scripts/swebench/test_metrics_attribution.py -v
go test ./harness/cli -run 'TestUsageBySource|TestMetrics|TestRunMetrics' -count=1
git status --short
```

Expected: no diff errors, Python tests PASS, focused Go tests PASS, and only planned files are modified/committed.
