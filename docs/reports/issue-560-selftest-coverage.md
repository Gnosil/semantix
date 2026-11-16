# Issue #560 closed-loop self-test coverage

## Baseline failure

On `main@d69cdd8ea31b52c8c6b396ca077525057299cbe0`, the closed-loop script extracts two synthetic preference slices, then requires `lookup --query "办案偏好"` to return Hit. The best candidate contains two of the four unique query tokens, below the default Grey threshold introduced by #554.

A binary built from that exact main revision reproduces `SELFTEST FAIL: lookup returned no hit zone`. The same assertion blocks the Go CI job on #559 before vet and race tests execute.

## Updated controls

- Use the complete seeded preference text as the positive query for both lookup and injection.
- Require lookup to produce Hit and injection to produce the reuse block under the default thresholds.
- Keep the earlier low-coverage lookup as a negative control and fail if it produces any Hit.
- Treat a failed negative-control command as a failed self-test.

## Validation

- The original script fails against the main binary.
- The updated script passes against the same binary with `SELFTEST PASS`.
- `bash -n agent-skill/scripts/selftest.sh` passes.
- All nine Python tests under `agent-skill/scripts` pass with that binary.
- `git diff --check` passes.

Validation uses local synthetic transcripts and a temporary memory store. The production zone thresholds remain active throughout the closed-loop check.
