# Semantix memory for DeepSeek Harness

This local DSH bundle recalls project memory before the first model request
and captures completed text turns. It uses a Semantix CLI built from a version
that includes `codex-hook`. Put that executable on the DSH process's `PATH`.
The headless workflow has been verified with DSH `0.1.5-rc.1`.

From the Semantix checkout, install the bundle in the profile you use:

```sh
dsh plugin --profile headless add ./plugins/semantix-dsh-memory
dsh --profile headless "Describe the project convention we used before"
```

Run the second command from your project directory. For another profile, use
its name in both commands. Remove the bundle with:

```sh
dsh plugin --profile headless remove @semantix/dsh-memory
```

Memory uses the canonical Git workspace root or, outside Git, the session
working directory. The DSH store defaults to `~/.semantix/dsh`, separate from
the Codex plugin store. Set `SEMANTIX_DSH_DATA_DIR` to an absolute directory to
override it. Uninstalling the bundle preserves this directory. To search one
project's store explicitly, use `semantix search --scope project --db
<project-memory.jsonl> --query <query>`.

The plugin sends admitted user text and a completed turn's final assistant
text to the local Semantix process. Recalled text is labeled as historical
data when inserted into a model request. Failed or unavailable recall lets
DSH continue. The plugin uses the CLI's existing payload and context limits.

To repeat the headless verification from the checkout, run
`python smoke.py <dsh-lib-bin.js> <semantix-bin-directory>` from this directory.
It installs and removes the bundle in a temporary DSH profile and uses a
loopback model stub.
