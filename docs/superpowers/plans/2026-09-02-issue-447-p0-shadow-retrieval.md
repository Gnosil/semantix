# Issue #447 P0.2 Shadow Retrieval Implementation Plan

> Scope: make L2 retrieval observable before tightening admission. This change does not tune thresholds or add new slice allowlists.

## Goal

Add `off | shadow | strict` retrieval modes. `shadow` must execute the same retrieval and admission path as `strict`, emit replayable diagnostics, and return no injection text so provider-visible request bytes remain identical to `off`.

## Compatibility

- `mode = ""` preserves the legacy `inject` boolean (`inject=true` => `strict`, otherwise `off`).
- An explicit mode takes precedence over `inject`.
- `Enabled=false` always resolves to `off`.
- Unknown explicit values fail closed to `off`.

## Work packages

1. **Exact admission trace in `kernel/inject`**
   - Split retrieval from assembly so the bridge can inspect the exact top-k hits without running BM25 twice.
   - Record every candidate's score, query coverage, zone, admission outcome, and exact rejection reason.
   - Keep the existing deterministic block and budget behavior unchanged.

2. **Mode and per-turn diagnostics in `harness/semantix`**
   - Resolve the effective mode once in `NewBridge`.
   - Attach library size, repository identity, HEAD commit, redacted query summaries, top-1/top-2 margin, candidate provenance, final order, role, bytes, and decision.
   - In `shadow`, run the same retrieval/build path but suppress text, injection stats, and `SliceInject` events.

3. **Wire and configuration contract**
   - Add `semantix.mode` to config rendering and boot wiring.
   - Carry retrieval diagnostics through the existing `kernel_cache` event wire shape.

4. **Regression proof**
   - RED tests first for exact rejection reasons, mode compatibility, shadow/no-injection behavior, strict behavior, and JSON wire fields.
   - Verify focused packages and `git diff --check`.

## Acceptance commands

```powershell
go test ./kernel/inject ./harness/semantix ./harness/eventwire ./harness/config ./harness/boot -count=1
go test ./harness/agent -run 'TestPrependSystemBlock|TestSampling' -count=1
git diff --check upstream/main...HEAD
```

## Provider-byte invariant

The only Semantix input to provider request assembly is `turn.injectBlock`. `off` returns an empty `InjectResult`; `shadow` returns the same empty `Text` after recording diagnostics. Therefore both modes leave the message list untouched. A regression test compares the serialized provider-visible projection for `off` and `shadow` while confirming that only shadow emits retrieval diagnostics.
