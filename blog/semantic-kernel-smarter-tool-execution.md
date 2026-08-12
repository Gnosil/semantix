---
title: "Threat Model: When Reused Agent Context Becomes an Attack Surface"
description: "A threat-model view of semantic injection, stale results, credentials, local storage, and safe degradation."
updated: 2026-08-12
group: "Scheduling & Harness"
order: 302
---

# Threat Model: When Reused Agent Context Becomes an Attack Surface

Memory changes the threat model because text from an earlier session can influence a later one. A useful design must answer who wrote the slice, which scope can read it, and what happens when its assumptions expire.

## Assets and adversaries

Assets include session JSONL, repository paths, stored results, and provider credentials. Threats include prompt content that escapes an injection boundary, cross-project retrieval, stale direct reuse, symlink attacks on local stores, and accidental formula execution in TSV exports.

## Implemented controls

Semantix sanitizes ANSI/C1 characters, escapes injection markers, guards TSV formula prefixes, writes local stores atomically with restrictive permissions, and includes scope in slice identity. L3 checks dependency fingerprints and rejects reuse when a file changes or disappears.

```bash
go build -o semantix ./cmd/semantix
go vet ./...
go test ./...
```

## Failure policy

Retrieval and embedding are optimization layers; failure should skip them and continue the harness. Permission or verification uncertainty is a safety boundary; failure should reject the shortcut. This split is summarized as cache fail-open, security fail-closed.

## Residual risk

L2 content is still untrusted input to a model. Sanitization cannot make an outdated instruction correct, and Windows permission semantics differ from Unix modes. Operators must avoid ingesting secrets and should test scope isolation on their deployment. The security document also lists sandbox and credential-management items that remain checklist work, so this is not a certification claim.

## Sources and limitations

- [Security design](https://github.com/Gnosil/semantix/blob/main/docs/Security-%E5%AE%89%E5%85%A8%E8%AE%BE%E8%AE%A1.md) — threat list, controls, and open checklist.
- [Quickstart](https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md) — commands and supported release paths.
- [M0 gate report](https://github.com/Gnosil/semantix/blob/main/docs/reports/m0-gate.md) — what passed, what is conditional, and what remains unverified.
- [Source and tests](https://github.com/Gnosil/semantix) — implementation is the final authority.
