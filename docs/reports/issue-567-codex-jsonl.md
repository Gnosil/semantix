# Issue #567: Codex turn capture within the input limit

The Codex Stop adapter accepts up to 1 MiB of JSON. When it encoded the
assistant reply as internal JSONL, the default Go encoder expanded each
literal `<` to six bytes. A synthetic Stop event of about 700 KB then exceeded
the extractor's 4 MiB line limit and left its prompt pending.

Disable HTML escaping for the internal transcript encoder. The incoming
payload limit, extractor limit, and hook response encoding are unchanged.

The regression test failed on `7686dc3` with the hook's local-error diagnostic.
With the fix, Stop consumes the pending file and a new session recalls the
captured project prompt. `go test -p 4 -race ./cmd/semantix ./kernel/slice`
passed on Windows amd64 with Go 1.26.5.
