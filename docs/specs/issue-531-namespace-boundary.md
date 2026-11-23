# Draft: local slice compatibility and a shared-store boundary (#531)

Status: review proposal. This does not enable shared storage, change local IDs, migrate a library, or introduce account code.

## Stable local contract

Local extractor IDs remain the lowercase hex of the first eight SHA-256 bytes of `[type byte, scope byte] || sanitized content`. Already-imported IDs remain opaque; do not require all existing IDs to have a particular width. Keep local JSONL/journals, metadata sidecars, and emitted prompt bytes unchanged. The golden local compatibility test captures the current derivation and storage round trip. A hash is a lookup key, not a security boundary.

## Proposed shared identity

Make the shared storage key a **structured pair** `(authenticated tenant namespace, slice identity)`; do not concatenate it into `Slice.ID` or a local filename. The tenant component is server-derived, opaque, stable, and unrelated to a mutable email, display name, request header, or workspace path. Personal local operation has no cloud namespace. An authenticated request without a resolved tenant fails closed and never falls back to local or global objects. A project/collection stays an additional bound on visibility. Keep identity width/version explicit in shared storage; decide 64 vs 128 bits before committing to that durable format. Same-tenant collisions, regardless of width, must not overwrite unequal canonical content or falsely report a dedup hit.

## Accounts and reusable objects (#524, #492)

- Account linking is opt-in and separate from sync. Login, logout, and switching accounts must not move or delete local stores or upload local data. Require explicit, account-scoped sync consent and clear cloud session state on logout/switch. Device authorization, email flows, credential storage, and deployed identity providers are separately reviewed work.
- Shared result candidates use a tenant- and project-bound index keyed by context fingerprint, resolved model version, and gate-policy generation; the stored object also retains source slice identity, freshness, dependency fingerprints, and explicit dependency-free reuse indication. Lookup never converts similarity or store presence into authorization to return an answer: reapply the existing `kernel/cache` L3 gates including freshness, model/context match, and dependency verification. Missing, stale, or unverified metadata is a miss. Publish promotions only with durable verification; a reject label does not broaden discovery or cache eligibility.
- Minimal storage interface after authorization design: scoped `FindCandidates`, `PutVerified` using create-or-compare semantics, and scoped `Invalidate`. Callers cannot supply a cross-tenant read prefix. Invalidation/deletion covers derived vectors and labels where consent and retention require it. Training export is separate, opt-in, sanitized, and grouped by source project/session; no raw text or credentials in key material or audit logs.

## Review and acceptance gates

1. Local lifecycle and replay are byte-compatible: no migration, no key rewriting, and no prefix-cache regression. Maintain old-reader round trips.
2. With a synthetic store, the same slice cannot be retrieved across two tenants or two project collections, including on cache hits and while switching accounts. Unknown tenant, absent provenance, changed context/model/dependencies, or ambiguous content must not serve a cached result.
3. Before a shared store rollout, choose tenant principal/collection policy, supported identity version, object store/revocation and deletion semantics, and whether any opt-in import is needed. Prefer copying only explicitly approved records into a new cloud namespace, leaving local stores untouched. Do not migrate an existing store without a reviewed cross-index/state-file manifest, dry run, and rollback plan.
