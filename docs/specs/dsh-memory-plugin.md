# DeepSeek Harness memory plugin

Ship a local DSH Host bundle that reuses the Semantix CLI's project memory
adapter. The runtime requires DSH's `agent/pre-step`, `session/event`, and
`session/flush` extension points and a Semantix executable with `codex-hook`.

- Read the admitted user text from the first step's enter decision. Preserve
  the downstream decision and append source-labeled memory on a hit.
- Pair the latest final assistant text and the admitted prompt by session
  and turn. Use `turn/end` to distinguish completed turns from cancellations,
  errors, and truncation. A session flush waits for any queued write.
- Use the existing adapter's workspace normalization, project isolation,
  pending turn identity, extraction, admission, and payload limits. Keep the
  DSH data root separate from the Codex plugin's data directory.
- Invoke an executable directly with JSON on stdin. Bound subprocess time
  and output. A missing, timed-out, or incompatible executable leaves the
  host turn usable. Sending recalled text to the model is part of the DSH request.
- Removing the bundle removes its event listeners and preserves project data.

Acceptance covers a fresh session's first outbound request, another project,
missing CLI, and bundle removal using DSH headless with a loopback provider.
