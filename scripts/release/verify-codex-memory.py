#!/usr/bin/env python3
"""Offline Codex memory adapter smoke test for a source or release binary."""

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile


ROOT = Path(__file__).resolve().parents[2]
PLUGIN = ROOT / "plugins" / "semantix-memory"
PROMPT = "Remember the canary lighthouse zephyr protocol for integration tests."
REPLY = "Canary lighthouse zephyr protocol uses the synthetic fixture."


def require(condition, message):
    if not condition:
        raise ValueError(message)


def verify_manifest():
    market = json.loads((ROOT / ".agents/plugins/marketplace.json").read_text(encoding="utf-8"))
    require(market["name"] == "semantix-plugins", "unexpected marketplace name")
    entries = [entry for entry in market["plugins"] if entry["name"] == "semantix-memory"]
    require(len(entries) == 1, "expected one semantix-memory marketplace entry")
    require(entries[0]["source"] == {"source": "local", "path": "./plugins/semantix-memory"},
            "marketplace does not point to the local memory bundle")

    manifest = json.loads((PLUGIN / ".codex-plugin/plugin.json").read_text(encoding="utf-8"))
    require(manifest["name"] == "semantix-memory" and manifest["hooks"] == "./hooks/hooks.json",
            "plugin does not reference the expected hooks manifest")
    hooks = json.loads((PLUGIN / "hooks/hooks.json").read_text(encoding="utf-8"))["hooks"]
    require(set(hooks) == {"UserPromptSubmit", "Stop"}, "unexpected memory hook events")
    for name in hooks:
        handlers = [hook for group in hooks[name] for hook in group["hooks"]]
        require(len(handlers) == 1, f"{name}: expected one command")
        handler = handlers[0]
        require(handler["type"] == "command" and handler["command"] == "semantix codex-hook"
                and handler["commandWindows"] == "semantix.exe codex-hook",
                f"{name}: unexpected command wiring")


def verify_binary(binary, expected_version, expected_commit, root):
    work, other = root / "project", root / "other"
    for directory in (work, other):
        directory.mkdir()
        # Mark separate roots even if TMPDIR is under another worktree.
        (directory / ".git").mkdir()
    env = {key: os.environ[key] for key in ("SYSTEMROOT", "WINDIR") if key in os.environ}
    env.update(HOME=str(root), USERPROFILE=str(root), PLUGIN_DATA=str(root / "plugin"))

    def cli(args, payload=None):
        result = subprocess.run([str(binary), *args], input=payload, cwd=work, env=env,
                                capture_output=True, encoding="utf-8", timeout=10)
        require(result.returncode == 0, f"{args[0]} exited {result.returncode}")
        require(not result.stderr.strip(), f"{args[0]} reported an error: {result.stderr.strip()}")
        return json.loads(result.stdout)

    identity = cli(["version", "--json"])["data"]
    require(not expected_version or identity["version"] == expected_version,
            f"binary version {identity['version']} does not match {expected_version}")
    require(not expected_commit or identity["commit"] == expected_commit,
            f"binary commit {identity['commit']} does not match {expected_commit}")

    def hook(event, cwd=work, session="capture", **fields):
        body = dict(hook_event_name=event, cwd=str(cwd), session_id=session, turn_id="once", **fields)
        return cli(["codex-hook"], json.dumps(body))

    require(hook("UserPromptSubmit", prompt=PROMPT) == {}, "unexpected recall on a fresh store")
    require(hook("Stop", last_assistant_message=REPLY) == {}, "Stop must not return model context")
    context = hook("UserPromptSubmit", session="recall", prompt=PROMPT).get("hookSpecificOutput") or {}
    require(context.get("hookEventName") == "UserPromptSubmit"
            and "lighthouse zephyr" in context.get("additionalContext", ""),
            "captured memory did not appear on the next session's first hook")
    require(hook("UserPromptSubmit", cwd=other, session="other", prompt=PROMPT) == {},
            "memory crossed the synthetic project boundary")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("semantix", type=Path, help="path to the binary to test")
    parser.add_argument("--expect-version")
    parser.add_argument("--expect-commit", help="exact commit stamp emitted by the build")
    args = parser.parse_args()
    try:
        binary = args.semantix.resolve(strict=True)
        verify_manifest()
        with tempfile.TemporaryDirectory(prefix="semantix-codex-memory-") as tmp:
            verify_binary(binary, args.expect_version, args.expect_commit, Path(tmp).resolve())
    except (KeyError, OSError, TypeError, ValueError, subprocess.TimeoutExpired) as exc:
        print(f"Codex memory verification failed: {exc}", file=sys.stderr)
        return 1
    print("PASS: Codex manifests, binary identity, synthetic capture, recall and project isolation")
    return 0


if __name__ == "__main__":
    sys.exit(main())
