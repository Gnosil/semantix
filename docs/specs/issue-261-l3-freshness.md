# Spec: L3 age-aware freshness zones (Issue 261)

This increment replaces the gateway's binary post-lookup TTL check with an
age-aware policy inside the L3 candidate loop. Dependency fingerprints remain
the authoritative content-change gate.

## Policy input

The gateway supplies the request's resolved vendor TTL and one request-scoped
Unix timestamp through `cache.Query.Freshness`. A zero or negative TTL keeps
the legacy behavior and disables age classification.

When the policy is active, candidate age is classified as follows:

| Candidate age | Freshness zone | L3 behavior |
|---|---|---|
| `0 <= age <= TTL/2` | Hit | Continue through the normal L3 gates |
| `TTL/2 < age <= TTL` | Grey | Require the configured judge |
| `age > TTL` | Miss | Reject without calling the judge |
| Missing or future `CreatedAt` | Miss | Reject fail-closed |

The 50% boundary is deliberately fixed for this first implementation. It adds
no operator knob before replay data exists to justify one.

## Decision order

For every Result candidate, `L3Decider` applies:

1. semantic Hit/Grey/Miss classification;
2. freshness Hit/Grey/Miss classification;
3. the stricter of the two verdicts;
4. the existing judge path when the combined verdict is Grey;
5. context and model isolation;
6. dependency mtime and fingerprint verification.

A freshness Grey candidate follows the existing grey-zone judge path. A Miss
continues to the next candidate, so one stale high-ranked result cannot hide a
fresh lower-ranked result.

## Compatibility and safety

- Queries without a freshness policy retain legacy CLI behavior.
- Vendor TTL precedence and existing defaults do not change.
- The hard TTL remains a fail-closed upper bound.
- No new dependency, persistence field, or public interface method is added.

## Deferred dependency

Repeated fingerprint failure feedback is not implemented in this increment.
It depends on Issue 267's `SliceReject` producer-to-score channel, which is not
present on `main`. Once that channel lands, fingerprint Miss outcomes can feed
it without changing the freshness verdict defined here.

## Acceptance

- Unknown and expired timestamps fail closed when TTL is active.
- Candidates in the second half of TTL require judge approval.
- Expired candidates never spend a judge call.
- A rejected stale candidate does not block a later fresh candidate.
- Existing callers with no freshness policy remain unchanged.
