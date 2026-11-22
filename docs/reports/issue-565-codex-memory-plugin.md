# Codex memory plugin validation (Issue #565)

## Host smoke test

Windows x64, Codex CLI 0.159.3, and a Semantix CLI built from this branch.
The test used an isolated Codex home and a loopback Responses API stub that
returned a final-answer message. The invocation enabled hook execution for
the test; the interactive `/hooks` trust flow was not exercised.

1. Added the local marketplace and installed
   `semantix-memory@semantix-plugins`.
2. Ran a turn with a synthetic project fact. The first outbound model request
   contained no Semantix memory. After the final answer, the project store
   existed and the pending capture had been consumed.
3. Started a fresh Codex process in the same project. The loopback provider
   observed recalled memory in the first model request of that session.
4. Repeated the turn without the new Semantix binary on PATH. Codex still
   completed the request without injecting memory.
5. Removed the installed plugin and repeated the turn with Semantix available.
   The request completed without automatic recall.

Windows hook entries use `commandWindows` so the host can invoke the native
executable. The packaged default command is used on other platforms.

## Automated coverage

The hook tests use production retrieval and extraction with temporary stores.
They cover project isolation including Git subdirectories, recall in a new
session, duplicate Stop events, malformed input, expired or incomplete turns,
store failures, and a bounded wait when the project writer holds the lock.
