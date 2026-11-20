# Codex memory plugin (Issue #565)

Status: approved for the first Codex integration.

## Installation

Ship a Codex plugin manifest and bundled command hooks for `UserPromptSubmit`
and `Stop`. The hooks invoke a local adapter backed by the existing Semantix
CLI and store. Enabling the plugin and approving its hooks enables automatic
memory. Disabling the plugin stops both hooks and preserves stored memory.
Document the supported Codex version and Semantix binary prerequisite.

This first integration covers Codex, project-scoped context reuse, and completed
turn ingestion. Other hosts, MCP packaging, and result replay remain outside
this proposal.

## Read and write contract

- `UserPromptSubmit` receives `prompt`, `cwd`, `session_id`, and `turn_id`.
  Resolve the project from the canonical workspace root and use its isolated
  store for both recall and ingestion. Save the prompt temporarily in a
  plugin-owned local directory under the project, session, and turn identity.
  Run the existing L2 injector once before the first model request for this
  turn. Use a two-second Codex hook timeout and default Semantix admission
  thresholds. Reject grey-zone candidates through the existing injector.
- Return a bounded Semantix context block as `additionalContext` on an accepted
  hit. Codex supplies that field as developer context, so label recalled text
  as untrusted historical data and quote it using the existing safe rendering
  rules. For a miss, invalid input, timeout, or unavailable store, return a
  successful no-op. Keep diagnostic text out of stdout.
- `Stop` pairs the pending prompt with a nonempty `last_assistant_message`
  for the same project, session, and turn. Feed the pair to the existing
  extractor as user and assistant JSONL records. Emit an empty JSON object
  on stdout and let Codex finish the turn.
- Serialize duplicate writes for the same turn and use a stable extraction
  identity so retries, including a retry after a partial failure, do not
  duplicate memory. Treat pending prompts older than 24 hours as expired and
  clean them on later activity. Keep at most 128 pending prompts per project.
  Bound input size, keep files private to the local user, and omit raw prompts
  and replies from diagnostics. The adapter reads only the documented hook
  payload and does not require transcript file access or network access.

## Acceptance

- Install and trust the plugin in Codex. Complete one turn with a known
  project fact, then start a fresh session and verify that the next related
  prompt's first model request already contains the recalled fact. Verify
  that disabling the plugin stops both capture and recall.
- Verify separate projects cannot recall one another's facts. Cover repeated
  `Stop` events, concurrent turns, missing assistant text, and stale pending
  state with fixture-based tests.
- Simulate a missing executable and a timeout. Verify Codex completes the
  turn without injected memory or a continuation. Record the tested Codex
  version, operating system, and sanitized results in the acceptance report.

## References

- [Issue #565](https://github.com/Gnosil/semantix/issues/565)
- [Codex hook contract](https://learn.chatgpt.com/docs/hooks)
- [Semantix session ingestion](../../agent-skill/hooks/session-bypass.md)
