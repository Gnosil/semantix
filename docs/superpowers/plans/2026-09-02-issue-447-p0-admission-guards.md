# Issue #447 P0.4 Admission Guards Plan

## Goal

Turn the P0.2 shadow trace into a conservative production admission policy that blocks the two observed negative-transfer paths: wrong-type semantic matches and weak/ambiguous matches from small libraries.

## Production defaults

- injectable types: `Context`, `Memory`;
- `Prompt` and `ToolPattern`: retrieved and diagnosed, never injected;
- `Result`: retrieved and diagnosed, never injected until a distinct successful-evaluation marker exists;
- minimum Project library size: 5 slices;
- minimum distinct non-empty source sessions for the candidate type: 2;
- minimum absolute BM25 score: 0.70;
- minimum cleaned-query coverage: 0.25;
- minimum eligible top-1 minus top-2 margin: 0.15;
- a single eligible candidate is rejected (`runner_up_missing`).

These values are intentionally conservative initial stops. Shadow diagnostics retain exact scores/reasons so later calibration can change constants without changing the event contract.

## Query cleaning

Retrieval uses a deterministic low-authority query projection:

1. remove the transient `<execution-policy>` block;
2. when a SWE-bench `<issue>...</issue>` block exists, use its body and discard the fixed benchmark frame/requirements;
3. remove a small English-only set of framing stopwords while preserving code symbols, CJK terms, test names, and domain terms.

Telemetry records hashes/counts for both original and cleaned query; it does not log raw user text.

## Commit sequence

1. `feat(inject): add replayable admission policy guards`
   - generic injector fields, exact rejection reasons, eligible margin.
2. `fix(semantix): enforce conservative L2 admission defaults`
   - query cleaner, C/M policy, library/session evidence, bridge tests.
3. `docs(semantix): document negative-transfer admission gates`
   - operator contract and verification evidence.

## Acceptance

```powershell
go test ./kernel/inject -count=1
go test ./harness/semantix ./harness/eventwire ./harness/config -count=1
go test ./harness/agent -run TestShadowRetrievalKeepsProviderMessagesByteIdenticalToOff -count=1
git diff --check upstream/main...HEAD
```
