# Semantix memory for Codex CLI

[简体中文](./README.zh-CN.md)

Use a Semantix executable built from the same revision as this checkout and a
Codex CLI that supports bundled plugin hooks. If using a release binary, also
check out that release's source tag for the local marketplace files. Start in
the terminal and environment you use to launch Codex:

```sh
codex --version
command -v semantix             # PowerShell: Get-Command semantix.exe -All
semantix version --json
semantix codex-hook --help
```

- **Command missing:** put the matching executable on the Codex process's
  PATH, then restart Codex. If several copies are installed, make sure the
  first one is the build you intend to use.
- **Unknown `codex-hook`:** the CLI and checkout do not match. For a release
  build, its reported commit should match `git rev-parse --short=12 HEAD` here.
  A dev build may say `unknown`; rebuild it from this checkout. The hook
  adapter is not present in every Semantix release.
- **Plugin installs but never runs:** open a fresh session, use `/hooks` to
  review and trust both `UserPromptSubmit` and `Stop`, and confirm both are
  enabled. A successful install does not establish trust. Avoid sharing
  prompt, transcript, or memory files when reporting a setup issue.

From the Semantix checkout, add the local marketplace and plugin:

```sh
codex plugin marketplace add .
codex plugin add semantix-memory@semantix-plugins
```

Open a new session and use `/hooks` to review and trust both local commands.

The prior [loopback report](../../docs/reports/issue-565-codex-memory-plugin.md) documents
Codex CLI `0.159.3` on Windows. The repository's offline verification below
does not exercise the interactive trust UI or establish host compatibility.

```sh
python3 scripts/release/verify-codex-memory.py /path/to/semantix
```

Run this from the checkout root with the executable you want to check. It
validates the marketplace and hook manifests and checks synthetic capture,
recall in a fresh session and project isolation with temporary data. It does
not start Codex, call a provider or read your existing memory.
