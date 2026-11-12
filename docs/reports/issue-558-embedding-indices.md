# Issue #558 embedding response alignment

## Audit scope and baseline

The audit checked current issues and open pull requests, then traced model embedding through CLI vector caching and gateway batch insertion on `main@d69cdd8ea31b52c8c6b396ca077525057299cbe0`. The current hybrid lookup PR #556 does not change the shared model embedder.

The response schema includes an embedding index. The adapter discarded that field and associated vectors with input texts by response-array position. Both CLI caching and gateway batch insertion rely on input-order results.

[OpenAI embedding response schema](https://developers.openai.com/api/reference/resources/embeddings/methods/create)

## Reproduction and impact

For two input texts, a mock response returns index 1 with vector `[0,4]`, then index 0 with vector `[3,0]`. The baseline outputs normalized vectors `[[0,1],[1,0]]`, attaching each vector to the other input. The expected input-order result is `[[1,0],[0,1]]`.

The baseline also accepts duplicate, negative, out-of-range and partially omitted indices as successful remote results. A gateway regression reproduces the wrong slice ranking after batch insertion. CLI regressions show that both disposable caches and persisted embeddings can retain a legacy mismatched vector.

All reproductions use synthetic data and local HTTP test servers.

## Fix and compatibility

- Decode explicit indices and restore input order before returning vectors.
- Validate index presence, range and uniqueness. Invalid mappings enter the existing whole-batch fallback and observable fallback callback.
- Fully unindexed compatible endpoints retain positional response handling.
- Advance the embedding cache provenance version so older positional vectors are recomputed through existing cache invalidation. The first query against an older library can incur fresh embedding calls.
- Existing slice records and embedding storage formats continue to round-trip.

## Verification

The response-index regression fails on the baseline for reversed responses and five invalid-index cases. The gateway batch-ranking regression also fails with the baseline embedder. Both legacy-cache regressions fail before the provenance version changes.

Regression coverage includes normalized output ordering, whole-batch fallback, legacy unindexed responses, gateway batch search and refresh of both legacy embedding storage paths.

- `go test -p 4 -race ./kernel/... ./cmd/semantix ./gateway` passed.
- `go vet -p 4 ./...` passed.
- `git diff --check` passed.

Whole-module race acceptance is tracked separately in the PR and CI.
