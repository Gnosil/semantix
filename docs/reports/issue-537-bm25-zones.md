# Issue #537 BM25 L2 zone regression

## Behavior

Native BM25 hits carry the fraction of unique query tokens found in each candidate. `zone.ClassifyHit` requires coverage to meet `tau_high` for Hit and `tau_low` for Grey, in addition to the existing relative-score and raw-score absolute-floor checks. A large raw BM25 score and first-place rank therefore need independent lexical support.

The metadata is computed during index search and excluded from JSON. Search, lookup, probe, L2 injection and L2 decisions use the shared classifier. Corpus-calibrated absolute thresholds continue to apply.

Both absolute floors set to zero explicitly select relative-only classification. The harness already uses that configuration for reference-history admission, including long tasks referencing short cards. The regression suite exercises this policy separately.

Coverage follows the existing tokenizer, including CJK character tokens. It is a lexical heuristic and cannot establish semantic equivalence. Vector, fused and legacy index hits without coverage use their existing score classifier.

## Regression evidence

- A Chinese negative control matches only 3 of 12 query tokens. Its raw score exceeds the default absolute Hit floor, but its new verdict is Miss.
- A complete matching query produces Hit, including a query with repeated tokens.
- CLI search and lookup, kernel lookup, L2 decisions and injection exercise both cases through their normal entry points.
- Unit checks cover threshold boundaries, calibrated absolute floors, weak relative scores, invalid coverage and explicit relative-only mode.
- The gateway tool-call preservation fixture now includes both query words so it still exercises actual injection.

## Local validation

- `go vet -p 2 ./...` passed.
- `go test -p 2 -race ./kernel/... ./cmd/semantix ./gateway ./harness/semantix` passed.
- `go test -p 2 ./harness/agent -run 'TestMemoryDelivery|TestMemoryFlow'` passed.
- `git diff --check` passed.
- `go test -p 2 -race ./...` completed with failures in nine harness packages. All kernel packages, CLI, gateway, Agent and the harness Semantix bridge passed. Remaining failures include unavailable Windows symlink privileges, missing Git Bash/POSIX commands, shell-dependent golden output, statusline refresh and stale temporary-directory cleanup. Full-suite acceptance remains outstanding.
- The initial unrestricted full-suite run encountered Windows paging-file exhaustion, unavailable symlink privileges and POSIX-shell launch failures. Full-suite acceptance remains subject to those environment constraints.

Validation used a D-drive temporary directory and build cache because the C drive had no free space.
