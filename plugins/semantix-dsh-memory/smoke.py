"""Verify the installed DSH bundle against a local fake provider.

Usage: python smoke.py <dsh lib/bin.js> <directory containing semantix>
"""

import http.server
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import threading


PROMPT = "Remember the cobalt lighthouse convention for project fixture."
ANSWER = "Synthetic answer: cobalt lighthouse convention uses a fixture."
MARKER = "Semantix project memory follows"


class Provider(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        self.server.requests.append(body)
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        for delta, finish in [({"role": "assistant", "content": ANSWER}, None), ({}, "stop")]:
            chunk = {
                "id": "chatcmpl-smoke", "object": "chat.completion.chunk",
                "created": 1, "model": body["model"],
                "choices": [{"index": 0, "delta": delta, "finish_reason": finish}],
            }
            self.wfile.write(("data: " + json.dumps(chunk) + "\n\n").encode())
        self.wfile.write(b"data: [DONE]\n\n")

    def log_message(self, *_):
        pass


def verify(root, server, dsh, semantix_dir):
    bundle = Path(__file__).resolve().parent
    memory = root / "memory"
    workspace = root / "project"
    workspace.mkdir()
    other = root / "other"
    other.mkdir()
    env = dict(os.environ, DSH_HOME=str(root / "home"), SEMANTIX_DSH_DATA_DIR=str(memory),
               DSH_TELEMETRY_MODE="DISABLED", DEEPSEEK_API_KEY="synthetic",
               DEEPSEEK_BASE_URL=f"http://127.0.0.1:{server.server_port}",
               PATH=f"{semantix_dir}{os.pathsep}{os.environ['PATH']}")
    semantix = shutil.which("semantix", path=str(semantix_dir))
    if not semantix:
        raise RuntimeError("semantix executable missing from the supplied directory")

    def command(*args, cwd=workspace, settings=env):
        result = subprocess.run(args, cwd=cwd, env=settings, capture_output=True,
                                text=True, encoding="utf-8", errors="replace", timeout=60)
        assert result.returncode == 0, (result.stdout, result.stderr)
        return result.stdout

    def run_turn(cwd=workspace, settings=env):
        server.requests.clear()
        output = command("node", dsh, "--profile", "headless", PROMPT, cwd=cwd, settings=settings)
        assert ANSWER in output and server.requests, output
        first = json.dumps(server.requests[0])
        assert PROMPT in first, first
        return MARKER in first

    command("node", dsh, "plugin", "--profile", "headless", "add", str(bundle))
    assert not run_turn(), "first run should have an empty project store"
    db, = memory.glob("projects/*/memory.jsonl")
    saved = command(semantix, "search", "--db", str(db), "--query", ANSWER, "--json")
    assert any(row["content"] == ANSWER for row in json.loads(saved)["data"]), saved
    assert run_turn(), "fresh session did not recall memory in its first request"
    assert not run_turn(other), "memory crossed project boundaries"

    without_cli = os.pathsep.join(part for part in env["PATH"].split(os.pathsep)
                                  if not shutil.which("semantix", path=part))
    assert not run_turn(settings=dict(env, PATH=without_cli)), "missing CLI injected memory"
    command("node", dsh, "plugin", "--profile", "headless", "remove", "@semantix/dsh-memory")
    assert not run_turn(), "removed bundle still recalled memory"
    assert db.is_file(), "removing the bundle deleted the project store"


if __name__ == "__main__":
    dsh_path, cli_dir = [Path(arg).resolve(strict=True) for arg in sys.argv[1:]]
    with tempfile.TemporaryDirectory() as temp, http.server.ThreadingHTTPServer(("127.0.0.1", 0), Provider) as server:
        server.requests = []
        threading.Thread(target=server.serve_forever, daemon=True).start()
        try:
            verify(Path(temp), server, dsh_path, cli_dir)
        finally:
            server.shutdown()
    print("PASS: capture, first-request recall, project isolation, missing CLI, bundle removal")
