# Offline paired memory replay

`memory_shadow_score.py` summarizes recorded outcomes; it never retrieves memory,
calls a model, or decides whether task output is correct. It complements
`TestMemoryShadowReplay` in `kernel/inject`, which runs real candidate admission
against synthetic current, retired, and untagged configuration values. That
fixture uses a scripted consumer, not an LLM, and makes no quality or savings
claim.

Collect exactly one independently scored outcome per anonymous task and arm.
Keep same-task input, model/version, verifier, retry policy, and scoring rule
fixed where applicable. Use `off` for no memory, `current` for the existing
configuration, and `revision_aware` for a shadow policy using independently
verified revision metadata. The fixture's `current` arm has no revision gate,
like the Codex hook; a different host may already have this gate. Do not treat
unversioned history as current just because it is relevant. Keep transcripts,
experiment outputs, and raw measurements local.

JSON schema by example, with synthetic booleans and no timing or costs:

```json
{
  "kind": "synthetic",
  "tasks": [{
    "id": "task-01",
    "arms": {
      "off": {"passed": true},
      "current": {"passed": false},
      "revision_aware": {"passed": true}
    }
  }]
}
```

Use `kind: "recorded"` only to distinguish observations from a fixture. An arm
may include `attempt_cost_microusd`: a list with every attempt's cost including
retries and failures. Include auxiliary costs (retrieval, embeddings, judging,
or verification) in those amounts when part of the comparison. Use a null entry
for an unmeasured attempt, and use zero only if the amount was actually zero.
Include `elapsed_ms` as measured task end-to-end time, including retries, not
time to first token. With missing cost or latency observations, the associated
aggregate is null, not a biased partial estimate. Cost per pass is undefined
when no task passes. p95 uses nearest rank over all logical tasks in that arm.
All flipped task IDs are reported, since newly broken and newly fixed tasks can
cancel out in aggregate.

```sh
python3 scripts/tools/memory_shadow_score.py /path/to/observations.json
python3 -m unittest discover -s scripts/tools -p 'test_memory_shadow_score.py'
go test ./kernel/inject -run '^TestMemoryShadowReplay$'
```
