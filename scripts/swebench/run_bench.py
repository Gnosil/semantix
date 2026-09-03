#!/usr/bin/env python3
"""Run one harness × one DeepSeek model over a SWE-bench Verified subset.

Adapters share a workspace/prompt/patch pipeline (common.py) and differ only
in how the agent process is invoked and how its native usage telemetry maps
onto the normalized metrics record. Supported harnesses:

  semantix      semantix-agent run -p (built-in memory kernel, --metrics JSON)
  claude-code   claude -p via DeepSeek's Anthropic-compatible endpoint
  codex         codex exec --json via DeepSeek's OpenAI-compatible endpoint
  dsh           DeepSeek Harness headless profile (@deepseek-ai/dsh)
  custom        any CLI harness described by a small JSON spec (see README)

Examples:
  python run_bench.py --harness semantix --model deepseek-chat \
      --dataset data/swebench_verified.jsonl --ids subsets/smoke-1.txt
  python run_bench.py --harness claude-code --model deepseek-chat ... --workers 4
"""

from __future__ import annotations

import argparse
import concurrent.futures as cf
import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

from common import (
    HERE,
    InstanceMetrics,
    Stopwatch,
    build_prompt,
    extract_patch,
    load_instances,
    load_prices,
    prepare_workspace,
    price_run,
    select_instances,
    write_prediction,
    append_jsonl,
)

DEEPSEEK_OPENAI_BASE = "https://api.deepseek.com"
DEEPSEEK_ANTHROPIC_BASE = "https://api.deepseek.com/anthropic"

REPO_OWNER = re.compile(r"^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$")
REPO_NAME = re.compile(r"^[A-Za-z0-9._-]+$")


def repo_identity(inst: dict) -> str:
    """Return the canonical owner/repo identity or fail closed.

    A shared fallback would recreate the contamination this guard prevents, so
    malformed dataset rows stop scheduling instead of entering an unknown db.
    """
    raw = inst.get("repo")
    if not isinstance(raw, str):
        raise ValueError(f"instance {inst.get('instance_id', '<unknown>')} has no repo identity")
    parts = raw.split("/")
    if (len(parts) != 2 or parts[1] in {"", ".", ".."}
            or not REPO_OWNER.fullmatch(parts[0] or "")
            or not REPO_NAME.fullmatch(parts[1] or "")):
        raise ValueError(f"invalid SWE-bench repo identity: {raw!r}")
    return f"{parts[0].lower()}/{parts[1].lower()}"


def repo_store_key(inst: dict) -> str:
    return repo_identity(inst).replace("/", "__", 1)


def mirror_fingerprint_paths(mirror: Path, workspace: Path) -> list[str]:
    """Return existing regular workspace files named by tool-call arguments."""
    workspace = workspace.resolve()
    found: set[str] = set()

    def visit(value, key: str = "") -> None:
        if isinstance(value, dict):
            for child_key, child in value.items():
                visit(child, str(child_key).lower())
            return
        if isinstance(value, list):
            for child in value:
                visit(child, key)
            return
        if isinstance(value, str) and key in {"args", "arguments"}:
            try:
                visit(json.loads(value))
            except json.JSONDecodeError:
                pass
            return
        if not isinstance(value, str) or key not in {
                "path", "paths", "file", "files", "file_path", "filepath", "filename"}:
            return
        candidate = Path(value)
        if not candidate.is_absolute():
            candidate = workspace / candidate
        try:
            if candidate.is_symlink():
                return
            resolved = candidate.resolve(strict=True)
            relative = resolved.relative_to(workspace)
        except (OSError, ValueError):
            return
        if resolved.is_file():
            found.add(relative.as_posix())

    for line in mirror.read_text(encoding="utf-8", errors="replace").splitlines():
        try:
            visit(json.loads(line))
        except json.JSONDecodeError:
            continue
    return sorted(found)


class CountProxy:
    """Per-instance metering proxy (count_proxy.py) for harnesses whose own
    telemetry is unreliable. Records provider-reported usage to a ledger."""

    def __init__(self, upstream: str, ledger: Path):
        self.ledger = ledger
        self.proc = subprocess.Popen(
            [sys.executable, str(HERE / "count_proxy.py"),
             "--upstream", upstream, "--ledger", str(ledger)],
            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
        line = self.proc.stdout.readline().strip()
        if not line.startswith("READY "):
            raise RuntimeError(f"count_proxy failed to start: {line!r}")
        self.port = int(line.split()[1])
        self.base = f"http://127.0.0.1:{self.port}"

    def stop(self) -> list[dict]:
        self.proc.terminate()
        try:
            self.proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.proc.kill()
        rows = []
        if self.ledger.exists():
            with open(self.ledger) as f:
                rows = [json.loads(l) for l in f if l.strip()]
        return rows


def sum_ledger(rows: list[dict]) -> dict:
    rows = [r for r in rows if "usage" in r]  # skip rejection-forensics lines
    total = {"prompt_tokens": 0, "completion_tokens": 0,
             "prompt_cache_hit_tokens": 0, "prompt_cache_miss_tokens": 0,
             "cached_tokens": 0, "calls": len(rows)}
    for row in rows:
        u = row.get("usage", {})
        total["prompt_tokens"] += u.get("prompt_tokens", u.get("input_tokens", 0) or 0)
        total["completion_tokens"] += u.get("completion_tokens", u.get("output_tokens", 0) or 0)
        total["prompt_cache_hit_tokens"] += u.get("prompt_cache_hit_tokens", 0) or 0
        total["prompt_cache_miss_tokens"] += u.get("prompt_cache_miss_tokens", 0) or 0
        details = (u.get("prompt_tokens_details") or
                   u.get("input_tokens_details") or {})
        total["cached_tokens"] += details.get("cached_tokens", 0) or 0
    return total


def normalize_call_counts(value: object) -> dict[str, int]:
    """Return valid non-negative call counts from a usage-by-source object."""
    if not isinstance(value, dict):
        return {}
    out = {}
    for source, bucket in value.items():
        if not isinstance(source, str) or not isinstance(bucket, dict):
            continue
        calls = bucket.get("calls")
        if isinstance(calls, bool) or not isinstance(calls, int) or calls < 0:
            continue
        out[source] = calls
    return out


def normalize_count_map(value: object) -> dict[str, int]:
    """Return valid non-negative counters from a flat string-to-int object."""
    if not isinstance(value, dict):
        return {}
    return {
        name: count
        for name, count in value.items()
        if isinstance(name, str)
        and isinstance(count, int)
        and not isinstance(count, bool)
        and count >= 0
    }


CLI_PATH_FIELDS = (
    "dataset", "ids", "results_dir", "work_dir", "state_dir", "prices",
    "custom_spec", "semantix_bin", "semantix_kernel_bin", "semantix_seed_dir",
    "codex_bin",
)


def resolve_cli_paths(args, base: Path | None = None) -> None:
    """Pin CLI paths before adapters change the child process workspace."""
    base = (base or Path.cwd()).resolve()
    for name in CLI_PATH_FIELDS:
        value = getattr(args, name, "")
        if not value:
            continue
        path = Path(value).expanduser()
        if not path.is_absolute():
            path = base / path
        setattr(args, name, str(path.resolve()))


def clean_env(*, drop_prefixes=(), drop=()) -> dict:
    env = {}
    for k, v in os.environ.items():
        if k in drop or any(k.startswith(p) for p in drop_prefixes):
            continue
        env[k] = v
    return env


class Adapter:
    name = "base"

    def __init__(self, args, run_dir: Path):
        self.args = args
        self.run_dir = run_dir

    def prepare(self) -> None:  # once per run
        pass

    def execution_batches(self, instances: list[dict]) -> list[list[dict]]:
        """Independent batches that may run concurrently.

        The default keeps historical per-instance parallelism. Stateful
        adapters override this to serialize instances that share state.
        """
        return [[inst] for inst in instances]

    def run_instance(self, ws: Path, prompt: str, inst: dict) -> tuple[int | None, dict, str]:
        """Return (exit_code, raw_metrics, error_string)."""
        raise NotImplementedError

    # Map raw metrics into the normalized record. Override per adapter.
    def fill_metrics(self, m: InstanceMetrics, raw: dict) -> None:
        raise NotImplementedError


# ---------------------------------------------------------------------------
# semantix-agent
# ---------------------------------------------------------------------------

def semantix_bench_provider(model: str) -> str:
    return "deepseek-pro" if model.endswith("-pro") else "deepseek-flash"


def semantix_ablation_spec(spec: str, memory_on: bool) -> str:
    """Return the agent ablation spec, labeling the off arm no-kernel."""
    spec = spec.strip()
    if memory_on:
        return "" if spec.lower() == "none" else spec
    if not spec or spec.lower() == "none":
        return "kernel"
    if spec.lower() == "all":
        return spec
    modules = [field.strip().lower() for field in re.split(r"[ ,]+", spec) if field.strip()]
    if "kernel" not in modules:
        spec += ",kernel"
    return spec


class SemantixAdapter(Adapter):
    name = "semantix"

    def prepare(self) -> None:
        # Memory-kernel arm wiring (issue: the original campaign wrote no
        # [semantix] section, so SemantixConfig.Enabled defaulted to false and
        # the whole kernel was off). With --semantix-memory on (the default)
        # every instance gets its OWN home (so session mirrors are attributed
        # race-free under --workers), while each real repo gets its OWN Project
        # store. Same-repo instances are scheduled as one ordered batch; repos
        # remain parallel.
        self.home = self.args.state_path / "semantix-home"
        self.home.mkdir(parents=True, exist_ok=True)
        self.memory_on = self.args.semantix_memory == "on"
        self.kernel_root = self.home / "kernel"
        if self.memory_on:
            self.kernel_root.mkdir(parents=True, exist_ok=True)
            self._seed_kernel_root()
            self.kernel_bin = self.args.semantix_kernel_bin or shutil.which("semantix") or str(
                HERE.parent.parent / "bin" / "semantix")
            if not Path(self.kernel_bin).exists():
                raise SystemExit(
                    f"semantix kernel CLI not found ({self.kernel_bin}); build with "
                    "`go build -o bin/semantix ./cmd/semantix` or pass --semantix-kernel-bin "
                    "(or run with --semantix-memory off)")
        self.binary = self.args.semantix_bin or shutil.which("semantix-agent") or str(
            HERE.parent.parent / "bin" / "semantix-agent"
        )
        if not Path(self.binary).exists():
            raise SystemExit(
                f"semantix-agent binary not found ({self.binary}); build with "
                "`go build -o bin/semantix-agent ./cmd/semantix-agent` or pass --semantix-bin"
            )

    def _seed_kernel_root(self) -> None:
        seed_value = getattr(self.args, "semantix_seed_dir", "")
        if not seed_value:
            return
        source = Path(seed_value).resolve()
        if not source.is_dir():
            raise RuntimeError(f"Semantix seed store directory does not exist: {source}")
        marker = self.kernel_root / ".seed-source.json"
        source_text = str(source)
        if marker.exists():
            recorded = json.loads(marker.read_text(encoding="utf-8"))
            if recorded.get("source") != source_text:
                raise RuntimeError(
                    f"Semantix state was seeded from {recorded.get('source')!r}, "
                    f"not {source_text!r}"
                )
            return
        if any(self.kernel_root.iterdir()):
            raise RuntimeError(
                f"Semantix kernel root already contains unseeded state: {self.kernel_root}"
            )
        shutil.copytree(source, self.kernel_root, dirs_exist_ok=True)
        marker.write_text(
            json.dumps({"schema": 1, "source": source_text}, indent=2) + "\n",
            encoding="utf-8",
        )

    def execution_batches(self, instances: list[dict]) -> list[list[dict]]:
        if not self.memory_on or getattr(self.args, "protocol", "grouped") == "standard":
            return super().execution_batches(instances)
        by_repo: dict[str, list[dict]] = {}
        for inst in instances:
            by_repo.setdefault(repo_identity(inst), []).append(inst)
        return list(by_repo.values())

    def kernel_dir_for(self, inst: dict) -> Path:
        if getattr(self.args, "protocol", "grouped") == "standard":
            return self.kernel_root / "instances" / inst["instance_id"]
        return self.kernel_root / repo_store_key(inst)

    def _write_home(self, home: Path, sessions_dir: Path, kernel_dir: Path | None,
                    workspace_dir: Path | None = None,
                    inject_audit_path: Path | None = None) -> None:
        home.mkdir(parents=True, exist_ok=True)
        workspace_dir = workspace_dir or Path.cwd()
        # Provider keys resolve ONLY from Semantix's global .env (never the
        # process environment) — see ProviderEntry.APIKey in harness/config.
        env_file = home / ".env"
        env_file.write_text(
            f"DEEPSEEK_API_KEY={os.environ.get('DEEPSEEK_API_KEY', 'smoke')}\n")
        env_file.chmod(0o600)
        base = self.args.openai_base or DEEPSEEK_OPENAI_BASE
        provider_selector = semantix_bench_provider(self.args.model)
        effort = f'effort      = "{self.args.effort}"\n' if self.args.effort else ""
        memory_section = ""
        if self.memory_on:
            if kernel_dir is None:
                raise ValueError("memory-on instance requires a repo-specific kernel directory")
            (kernel_dir / ".semantix").mkdir(parents=True, exist_ok=True)
            sessions_dir.mkdir(parents=True, exist_ok=True)
            memory_section = f'''
[semantix]
enabled      = true
inject       = true
mode         = "{self.args.semantix_retrieval_mode}"
budget       = 4096
project_dir  = "{kernel_dir}"
workspace_dir = "{workspace_dir}"
sessions_dir = "{sessions_dir}"
inject_audit_path = "{inject_audit_path or ''}"
'''
        (home / "config.toml").write_text(
            f'''default_model = "{provider_selector}"
{memory_section}
# Benchmark convention: every arm runs in its max-permission mode (codex
# danger-full-access, claude --dangerously-skip-permissions, dsh
# danger-full-access), so the bash OS-sandbox is off here too.
[sandbox]
bash = "off"

[[providers]]
name        = "{provider_selector}"
kind        = "openai"
base_url    = "{base}"
models      = ["{self.args.model}"]
default     = "{self.args.model}"
api_key_env = "DEEPSEEK_API_KEY"
context_window = 128000
{effort}'''
        )
        # Runtime credential resolution reads only $SEMANTIX_HOME/.env (process
        # env is setup-probe only); _write_home materializes the key there.

    def run_instance(self, ws: Path, prompt: str, inst: dict):
        mfile = self.run_dir / "native" / f"{inst['instance_id']}.semantix.json"
        mfile.parent.mkdir(parents=True, exist_ok=True)
        home = self.home / "inst" / inst["instance_id"]
        sessions_dir = home / "kernel-sessions"
        repo = repo_identity(inst) if self.memory_on else ""
        kernel_dir = self.kernel_dir_for(inst) if self.memory_on else None
        inject_audit_path = self.run_dir / "audit" / f"{inst['instance_id']}.txt"
        if self.memory_on:
            inject_audit_path.parent.mkdir(parents=True, exist_ok=True)
            inject_audit_path.touch(exist_ok=True)
            inject_audit_path.chmod(0o600)
        self._write_home(home, sessions_dir, kernel_dir, ws, inject_audit_path)
        env = clean_env()
        env.update({
            "SEMANTIX_HOME": str(home),
            "SEMANTIX_NO_INTRO": "1",
            "SEMANTIX_LANG": "en",
        })
        cmd = [
            self.binary, "run",
            "--dir", str(ws),
            "-p",
            "--permission-mode", "auto",
            "--preset", self.args.preset,
            "--metrics", str(mfile),
            "--model", semantix_bench_provider(self.args.model),
        ]
        ablate = semantix_ablation_spec(self.args.ablate, self.memory_on)
        if ablate:
            cmd += ["--ablate", ablate]
        stdout_text = ""
        stderr_text = ""
        try:
            proc = subprocess.run(cmd, input=prompt, capture_output=True, text=True,
                                  env=env, timeout=self.args.timeout)
            exit_code, err = proc.returncode, ""
            stdout_text = proc.stdout or ""
            stderr_text = proc.stderr or ""
        except subprocess.TimeoutExpired as exc:
            exit_code, err = None, f"timeout after {self.args.timeout}s"
            stdout_text = exc.stdout or ""
            stderr_text = exc.stderr or ""
        if isinstance(stdout_text, bytes):
            stdout_text = stdout_text.decode("utf-8", errors="replace")
        if isinstance(stderr_text, bytes):
            stderr_text = stderr_text.decode("utf-8", errors="replace")
        # Preserve adapter-local diagnostics beside native metrics. Without
        # these files, an early CLI failure produces only zero counters and
        # discards the actual provider/configuration error.
        stem = mfile.with_suffix("")
        Path(str(stem) + ".stdout.txt").write_text(stdout_text, encoding="utf-8")
        Path(str(stem) + ".stderr.txt").write_text(stderr_text, encoding="utf-8")
        raw = {}
        for candidate in (mfile, Path(str(mfile) + ".partial")):
            if candidate.exists():
                raw = json.loads(candidate.read_text())
                break
        if self.memory_on:
            raw["semantix_repo"] = repo
            raw["semantix_project_dir"] = str(kernel_dir)
            raw["semantix_protocol"] = getattr(self.args, "protocol", "grouped")
            raw["semantix_inject_audit"] = str(inject_audit_path)
            raw["extract"] = self._extract_slices(
                sessions_dir, inst["instance_id"], kernel_dir, repo, ws,
                inst.get("base_commit", ""))
        return exit_code, raw, err

    def _extract_slices(self, sessions_dir: Path, instance_id: str,
                        kernel_dir: Path, repo: str, workspace: Path,
                        base_commit: str) -> dict:
        """Close the memory loop: distill this instance's session mirrors into
        the repo-isolated slice library so later same-repo instances can retrieve them. The
        agent never extracts on its own (extraction is `semantix extract` /
        gateway-side by design), so the runner does it here. The sessions dir
        is per-instance, so every mirror in it belongs to this instance; the
        scheduler serializes every same-repo batch, so each append journal has
        exactly one writer while different repo stores remain parallel."""
        mirrors = sorted(sessions_dir.glob("*.jsonl"))
        out = {"mirrors": len(mirrors), "runs": []}
        db = kernel_dir / ".semantix" / "project.db"
        for mirror in mirrors:
            fingerprints = mirror_fingerprint_paths(mirror, workspace)
            ecmd = [self.kernel_bin, "extract",
                    "--input", str(mirror),
                    "--scope", "project",
                    "--project-db", str(db),
                    "--session", instance_id,
                    "--project", repo]
            if base_commit:
                ecmd += ["--base-commit", base_commit]
            if fingerprints:
                ecmd += ["--fingerprint", ",".join(fingerprints)]
            try:
                ep = subprocess.run(ecmd, capture_output=True, text=True, timeout=120,
                                    cwd=workspace)
                out["runs"].append({"mirror": mirror.name, "exit": ep.returncode,
                                    "fingerprints": fingerprints,
                                    "out": (ep.stdout or ep.stderr).strip()[-300:]})
            except Exception as exc:  # extraction is best-effort, never fails the instance
                out["runs"].append({"mirror": mirror.name, "error": str(exc)[:200]})
        return out

    def fill_metrics(self, m: InstanceMetrics, raw: dict) -> None:
        m.input_tokens = raw.get("prompt_tokens", 0)
        m.output_tokens = raw.get("completion_tokens", 0)
        m.cache_hit_tokens = raw.get("cache_hit_tokens", 0)
        m.cache_miss_tokens = raw.get("cache_miss_tokens", 0)
        m.steps = raw.get("steps", 0)
        m.tool_calls = raw.get("tool_calls", 0)
        m.model_calls_by_source = normalize_call_counts(raw.get("usage_by_source"))
        m.executor_calls = m.model_calls_by_source.get("executor", 0)
        m.planner_calls = m.model_calls_by_source.get("planner", 0)
        m.subagent_calls = m.model_calls_by_source.get("subagent", 0)
        m.compaction_calls = m.model_calls_by_source.get("compaction", 0)
        named_sources = {"executor", "planner", "subagent", "compaction"}
        m.other_model_calls = sum(
            calls for source, calls in m.model_calls_by_source.items()
            if source not in named_sources
        )
        m.source_call_total = sum(m.model_calls_by_source.values())
        m.source_call_delta = m.steps - m.source_call_total
        m.provider_retries = raw.get("retries", 0)
        m.compactions = raw.get("compactions", 0)
        m.subagent_runs = raw.get("subagent_runs", 0)
        m.tool_failures = raw.get("tool_failures", 0)
        m.tool_calls_by_name = normalize_count_map(raw.get("tool_calls_by_name"))
        if "repeated_tool_calls" in raw:
            m.repeated_tool_calls = raw.get("repeated_tool_calls")
        if "repeated_tool_calls_by_name" in raw:
            m.repeated_tool_calls_by_name = normalize_count_map(
                raw.get("repeated_tool_calls_by_name")
            )
        m.semantix_fuse_turns = raw.get("semantix_fuse_turns", 0)
        m.semantix_rejected_slices = raw.get("semantix_rejected_slices", 0)
        m.cost_native = raw.get("cost")
        m.cost_native_currency = raw.get("currency", "")


# ---------------------------------------------------------------------------
# Claude Code on DeepSeek's Anthropic-compatible endpoint
# ---------------------------------------------------------------------------

class ClaudeCodeAdapter(Adapter):
    name = "claude-code"

    def prepare(self) -> None:
        # Not under /tmp: some CLIs refuse state dirs in temp paths.
        self.config_dir = self.args.state_path / "claude-home"
        self.config_dir.mkdir(parents=True, exist_ok=True)
        if not os.environ.get("DEEPSEEK_API_KEY") and not self.args.anthropic_base:
            raise SystemExit("DEEPSEEK_API_KEY is required for --harness claude-code")
        if shutil.which("claude") is None:
            raise SystemExit("claude CLI not found (npm install -g @anthropic-ai/claude-code)")

    def run_instance(self, ws: Path, prompt: str, inst: dict):
        env = clean_env(
            drop_prefixes=("CLAUDE_CODE_", "CLAUDE_SESSION_"),
            drop=("ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN",
                  "ANTHROPIC_MODEL", "ANTHROPIC_SMALL_FAST_MODEL", "CLAUDECODE"),
        )
        env.update({
            "ANTHROPIC_BASE_URL": self.args.anthropic_base or DEEPSEEK_ANTHROPIC_BASE,
            "ANTHROPIC_AUTH_TOKEN": os.environ.get("DEEPSEEK_API_KEY", "smoke"),
            "ANTHROPIC_MODEL": self.args.model,
            "ANTHROPIC_SMALL_FAST_MODEL": self.args.model,
            "ANTHROPIC_DEFAULT_SONNET_MODEL": self.args.model,
            "ANTHROPIC_DEFAULT_OPUS_MODEL": self.args.model,
            "ANTHROPIC_DEFAULT_HAIKU_MODEL": self.args.model,
            "CLAUDE_CODE_SUBAGENT_MODEL": self.args.model,
            "CLAUDE_CONFIG_DIR": str(self.config_dir),
            "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
            "DISABLE_TELEMETRY": "1",
            "DISABLE_ERROR_REPORTING": "1",
            "DISABLE_AUTOUPDATER": "1",
            # Containerized benchmark: permit --dangerously-skip-permissions as root.
            "IS_SANDBOX": "1",
        })
        cmd = [
            "claude", "-p",
            "--output-format", "json",
            "--dangerously-skip-permissions",
            "--max-turns", str(self.args.max_turns),
            "--model", self.args.model,
        ]
        try:
            proc = subprocess.run(cmd, input=prompt, capture_output=True, text=True,
                                  cwd=ws, env=env, timeout=self.args.timeout)
            exit_code, err = proc.returncode, ""
        except subprocess.TimeoutExpired:
            return None, {}, f"timeout after {self.args.timeout}s"
        raw = {}
        out = proc.stdout.strip()
        # stdout is one JSON object in -p --output-format json mode; be
        # forgiving about leading noise lines.
        for chunk in (out, out[out.find("{"):]):
            try:
                raw = json.loads(chunk)
                break
            except (ValueError, IndexError):
                continue
        if not raw:
            err = err or f"unparseable claude output: {out[:300]!r} stderr: {proc.stderr[:300]!r}"
        return exit_code, raw, err

    def fill_metrics(self, m: InstanceMetrics, raw: dict) -> None:
        u = raw.get("usage", {})
        direct = u.get("input_tokens", 0)
        creation = u.get("cache_creation_input_tokens", 0)
        read = u.get("cache_read_input_tokens", 0)
        m.input_tokens = direct + creation + read
        m.output_tokens = u.get("output_tokens", 0)
        m.cache_hit_tokens = read
        m.cache_miss_tokens = direct + creation
        m.steps = raw.get("num_turns", 0)
        m.cost_native = raw.get("total_cost_usd")
        m.cost_native_currency = "$" if raw.get("total_cost_usd") is not None else ""
        if raw.get("duration_ms"):
            m.raw.setdefault("duration_api_ms", raw.get("duration_api_ms"))


# ---------------------------------------------------------------------------
# Codex CLI on DeepSeek's OpenAI-compatible endpoint
# ---------------------------------------------------------------------------

class CodexAdapter(Adapter):
    name = "codex"

    def prepare(self) -> None:
        # DeepSeek serves a native Responses API (/v1/responses), so current
        # codex works with wire_api="responses" (the default here). The chat
        # wire needs codex ≤0.80.0 (--codex-bin) and trips DeepSeek's strict
        # tool-message validation on multi-turn histories — avoid it.
        # Per-instance CODEX_HOME (state dir, not /tmp — codex refuses temp
        # dirs) so each instance's config can point at its metering proxy.
        self.config_template = f'''model = "{self.args.model}"
model_provider = "deepseek"
preferred_auth_method = "apikey"
sandbox_mode = "danger-full-access"

[model_providers.deepseek]
name = "DeepSeek"
base_url = "__BASE_URL__"
env_key = "DEEPSEEK_API_KEY"
wire_api = "{self.args.codex_wire_api}"
'''
        self.binary = self.args.codex_bin or shutil.which("codex")
        if not self.binary:
            raise SystemExit("codex CLI not found (npm install -g @openai/codex@0.80.0)")
        if not os.environ.get("DEEPSEEK_API_KEY") and not self.args.openai_base:
            raise SystemExit("DEEPSEEK_API_KEY is required for --harness codex")

    def run_instance(self, ws: Path, prompt: str, inst: dict):
        # codex 0.80's chat wire reports zero token usage, so meter at the
        # wire with a per-instance pass-through proxy and read its ledger.
        native = self.run_dir / "native"
        native.mkdir(parents=True, exist_ok=True)
        proxy = CountProxy(self.args.openai_base or DEEPSEEK_OPENAI_BASE,
                           native / f"{inst['instance_id']}.codex-usage.jsonl")
        codex_home = self.args.state_path / f"codex-home-{inst['instance_id']}"
        codex_home.mkdir(parents=True, exist_ok=True)
        (codex_home / "config.toml").write_text(
            self.config_template.replace("__BASE_URL__", proxy.base + "/v1"))
        env = clean_env(drop=("OPENAI_API_KEY", "OPENAI_BASE_URL"))
        env.update({
            "CODEX_HOME": str(codex_home),
            "DEEPSEEK_API_KEY": os.environ.get("DEEPSEEK_API_KEY", "smoke"),
        })
        cmd = [
            self.binary, "exec",
            "--json",
            "--skip-git-repo-check",
            "--dangerously-bypass-approvals-and-sandbox",
            "-C", str(ws),
            prompt,
        ]
        try:
            proc = subprocess.run(cmd, capture_output=True, text=True, env=env,
                                  timeout=self.args.timeout)
            exit_code, err = proc.returncode, ""
        except subprocess.TimeoutExpired:
            proxy.stop()
            return None, {}, f"timeout after {self.args.timeout}s"
        ledger_rows = proxy.stop()
        raw = {"events_tail": [], "wire_usage": sum_ledger(ledger_rows)}
        usage_total, turns = None, 0
        for line in proc.stdout.splitlines():
            line = line.strip()
            if not line.startswith("{"):
                continue
            try:
                ev = json.loads(line)
            except ValueError:
                continue
            info = ev.get("info") or {}
            if isinstance(info, dict) and "total_token_usage" in info:
                usage_total = info["total_token_usage"]
            elif ev.get("type") in ("turn.completed",) and isinstance(ev.get("usage"), dict):
                turns += 1
                u = ev["usage"]
                if usage_total is None:
                    usage_total = {"input_tokens": 0, "cached_input_tokens": 0, "output_tokens": 0}
                for src, dst in (("input_tokens", "input_tokens"),
                                 ("cached_input_tokens", "cached_input_tokens"),
                                 ("output_tokens", "output_tokens")):
                    usage_total[dst] = usage_total.get(dst, 0) + u.get(src, 0)
            raw["events_tail"] = (raw["events_tail"] + [ev])[-5:]
        raw["usage_total"] = usage_total
        raw["turns"] = turns
        if not raw["wire_usage"]["calls"] and usage_total is None:
            err = err or f"no token usage found; stderr: {proc.stderr[:300]!r}"
        return exit_code, raw, err

    def fill_metrics(self, m: InstanceMetrics, raw: dict) -> None:
        wire = raw.get("wire_usage") or {}
        if wire.get("calls"):
            # Provider-reported usage from the metering proxy (authoritative).
            m.input_tokens = wire["prompt_tokens"]
            m.output_tokens = wire["completion_tokens"]
            hit = wire["prompt_cache_hit_tokens"] or wire["cached_tokens"]
            m.cache_hit_tokens = hit
            m.cache_miss_tokens = (wire["prompt_cache_miss_tokens"]
                                   or max(m.input_tokens - hit, 0))
            m.steps = wire["calls"]
            return
        u = raw.get("usage_total") or {}
        m.input_tokens = u.get("input_tokens", 0)
        cached = u.get("cached_input_tokens", 0)
        m.cache_hit_tokens = cached
        m.cache_miss_tokens = max(m.input_tokens - cached, 0)
        m.output_tokens = u.get("output_tokens", 0)
        m.steps = raw.get("turns", 0)


# ---------------------------------------------------------------------------
# DeepSeek Harness (dsh) headless profile
# ---------------------------------------------------------------------------

class DshAdapter(Adapter):
    name = "dsh"

    def prepare(self) -> None:
        self.dsh_home = self.args.state_path / "dsh-home"
        self.dsh_home.mkdir(parents=True, exist_ok=True)
        # Model override rides the profile patch mechanism; the shipped default
        # is deepseek-v4-flash, so the patch only matters for other models.
        self.patch_file = self.args.state_path / "dsh-model-patch.yml"
        self.patch_file.write_text(
            f"- id: agent-default-model\n"
            f"  config:\n"
            f"    provider: deepseek-official\n"
            f"    model: {self.args.model}\n"
        )
        if shutil.which("dsh") is None:
            raise SystemExit("dsh CLI not found (npm install -g @deepseek-ai/dsh)")
        if not os.environ.get("DEEPSEEK_API_KEY") and not self.args.openai_base:
            raise SystemExit("DEEPSEEK_API_KEY is required for --harness dsh")

    def run_instance(self, ws: Path, prompt: str, inst: dict):
        # dsh's node fetch ignores HTTPS_PROXY env vars, so direct egress is
        # blocked in proxied sandboxes; routing through the metering proxy both
        # fixes that and gives wire-level provider usage (cache fields included).
        native = self.run_dir / "native"
        native.mkdir(parents=True, exist_ok=True)
        proxy = CountProxy(self.args.openai_base or DEEPSEEK_OPENAI_BASE,
                           native / f"{inst['instance_id']}.dsh-usage.jsonl")
        env = clean_env()
        env.update({
            "DSH_HOME": str(self.dsh_home),
            "DSH_PERMISSION_MODE": "danger-full-access",
            "DSH_TELEMETRY_MODE": "DISABLED",
            "DEEPSEEK_API_KEY": os.environ.get("DEEPSEEK_API_KEY", "smoke"),
            "DEEPSEEK_BASE_URL": proxy.base,
        })
        before = self._session_files()
        cmd = ["dsh", "--profile", "headless", "--patch", str(self.patch_file), prompt]
        try:
            proc = subprocess.run(cmd, capture_output=True, text=True, cwd=ws, env=env,
                                  timeout=self.args.timeout)
            exit_code, err = proc.returncode, ""
        except subprocess.TimeoutExpired:
            exit_code, err = None, f"timeout after {self.args.timeout}s"
        ledger_rows = proxy.stop()
        raw = {"stdout_tail": "", "wire_usage": sum_ledger(ledger_rows)}
        try:
            new = [p for p in self._session_files() if p not in before]
            raw.update(self._parse_sessions(new))
        except Exception as e:
            err = (err + f"; session parse failed: {e!r}").strip("; ")
        if exit_code is not None:
            raw["stdout_tail"] = (proc.stdout or "")[-1500:]
            if not raw.get("usage_events") and not raw["wire_usage"]["calls"]:
                err = (err + f"; no usage found; stderr: {proc.stderr[:300]!r}").strip("; ")
        return exit_code, raw, err

    def _session_files(self) -> set:
        return set((self.dsh_home / "sessions").rglob("session.jsonl*")) \
            if (self.dsh_home / "sessions").exists() else set()

    def _parse_sessions(self, files) -> dict:
        totals = {"inputTokens": 0, "outputTokens": 0, "cacheReadTokens": 0,
                  "cacheWriteTokens": 0}
        events = 0
        for path in files:
            data = path.read_bytes()
            if path.name.endswith(".zstd"):
                import io

                import zstandard  # pip install zstandard

                # Sessions are multi-frame streaming zstd; one-shot
                # decompress() stops after the first frame.
                data = zstandard.ZstdDecompressor().stream_reader(
                    io.BytesIO(data)).read()
            for line in data.decode("utf-8", "replace").splitlines():
                line = line.strip()
                if not line:
                    continue
                try:
                    rec = json.loads(line)
                except ValueError:
                    continue
                # One usage chunk per model call: {"type":"assistant/chunk",
                # "data":{"chunk":{"type":"usage","usage":{...}}}}
                chunk = (rec.get("data") or {}).get("chunk") or {}
                if rec.get("type") == "assistant/chunk" and chunk.get("type") == "usage":
                    u = chunk.get("usage") or {}
                    for k in totals:
                        totals[k] += int(u.get(k, 0) or 0)
                    events += 1
        return {"usage_total": totals, "usage_events": events}

    def fill_metrics(self, m: InstanceMetrics, raw: dict) -> None:
        wire = raw.get("wire_usage") or {}
        if wire.get("calls"):
            # Provider-reported usage from the metering proxy (authoritative).
            m.input_tokens = wire["prompt_tokens"]
            m.output_tokens = wire["completion_tokens"]
            hit = wire["prompt_cache_hit_tokens"] or wire["cached_tokens"]
            m.cache_hit_tokens = hit
            m.cache_miss_tokens = (wire["prompt_cache_miss_tokens"]
                                   or max(m.input_tokens - hit, 0))
            m.steps = wire["calls"]
            return
        u = raw.get("usage_total") or {}
        # dsh session usage is Anthropic-shaped: inputTokens excludes cache reads.
        hit = u.get("cacheReadTokens", 0)
        miss = u.get("inputTokens", 0) + u.get("cacheWriteTokens", 0)
        m.cache_hit_tokens = hit
        m.cache_miss_tokens = miss
        m.input_tokens = hit + miss
        m.output_tokens = u.get("outputTokens", 0)
        m.steps = raw.get("usage_events", 0)


# ---------------------------------------------------------------------------
# Custom CLI harness driven by a JSON spec:
# {
#   "command": ["dsh", "run", "--cwd", "{ws}", "--model", "{model}", "--prompt-file", "{prompt_file}"],
#   "env": {"DSH_API_KEY_ENV": "DEEPSEEK_API_KEY"},
#   "usage_file": "{ws}/.dsh/usage.json",          # optional
#   "usage_regex": {                                # optional, applied to stdout
#     "input_tokens": "input[_ ]tokens[=: ]+(\\d+)",
#     "output_tokens": "output[_ ]tokens[=: ]+(\\d+)",
#     "cache_hit_tokens": "cache[_ ]hit[=: ]+(\\d+)"
#   }
# }
# ---------------------------------------------------------------------------

class CustomAdapter(Adapter):
    name = "custom"

    def prepare(self) -> None:
        if not self.args.custom_spec:
            raise SystemExit("--harness custom requires --custom-spec spec.json")
        self.spec = json.loads(Path(self.args.custom_spec).read_text())
        self.name = self.spec.get("name", "custom")

    def run_instance(self, ws: Path, prompt: str, inst: dict):
        prompt_file = self.run_dir / "native" / f"{inst['instance_id']}.prompt.txt"
        prompt_file.parent.mkdir(parents=True, exist_ok=True)
        prompt_file.write_text(prompt)
        subst = {"ws": str(ws), "model": self.args.model, "prompt_file": str(prompt_file),
                 "prompt": prompt}
        cmd = [c.format(**subst) for c in self.spec["command"]]
        env = clean_env()
        env.update({k: v.format(**subst) for k, v in self.spec.get("env", {}).items()})
        try:
            proc = subprocess.run(cmd, capture_output=True, text=True, cwd=ws, env=env,
                                  timeout=self.args.timeout)
            exit_code, err = proc.returncode, ""
        except subprocess.TimeoutExpired:
            return None, {}, f"timeout after {self.args.timeout}s"
        raw = {"stdout_tail": proc.stdout[-2000:], "stderr_tail": proc.stderr[-1000:]}
        usage_file = self.spec.get("usage_file")
        if usage_file:
            p = Path(usage_file.format(**subst))
            if p.exists():
                try:
                    raw["usage"] = json.loads(p.read_text())
                except ValueError:
                    pass
        for key, pattern in self.spec.get("usage_regex", {}).items():
            match = re.search(pattern, proc.stdout) or re.search(pattern, proc.stderr)
            if match:
                raw.setdefault("usage", {})[key] = int(match.group(1))
        return exit_code, raw, err

    def fill_metrics(self, m: InstanceMetrics, raw: dict) -> None:
        u = raw.get("usage", {})
        m.input_tokens = int(u.get("input_tokens", u.get("prompt_tokens", 0)))
        m.output_tokens = int(u.get("output_tokens", u.get("completion_tokens", 0)))
        m.cache_hit_tokens = int(u.get("cache_hit_tokens", u.get("prompt_cache_hit_tokens", 0)))
        miss = u.get("cache_miss_tokens", u.get("prompt_cache_miss_tokens"))
        m.cache_miss_tokens = int(miss) if miss is not None else max(
            m.input_tokens - m.cache_hit_tokens, 0)


ADAPTERS = {
    "semantix": SemantixAdapter,
    "claude-code": ClaudeCodeAdapter,
    "codex": CodexAdapter,
    "dsh": DshAdapter,
    "custom": CustomAdapter,
}


def process_instance(adapter: Adapter, args, run_dir: Path, prices: dict, inst: dict) -> None:
    iid = inst["instance_id"]
    m = InstanceMetrics(run_id=args.run_id, harness=adapter.name, model=args.model,
                        instance_id=iid)
    ws = prepare_workspace(Path(args.work_dir), args.run_id, inst)
    prompt = build_prompt(inst)
    with Stopwatch() as sw:
        try:
            exit_code, raw, err = adapter.run_instance(ws, prompt, inst)
        except Exception as e:  # an adapter crash must not kill the batch
            exit_code, raw, err = None, {}, f"adapter exception: {e!r}"
    m.wall_ms = sw.ms
    m.agent_exit = exit_code
    m.error = err
    m.raw = raw
    try:
        adapter.fill_metrics(m, raw)
    except Exception as e:
        m.error = (m.error + f"; metrics mapping failed: {e!r}").strip("; ")
    patch = ""
    try:
        patch = extract_patch(ws)
    except Exception as e:
        m.error = (m.error + f"; patch extraction failed: {e!r}").strip("; ")
    m.patch_bytes = len(patch.encode())
    m.empty_patch = not patch.strip()
    m.cost_usd = price_run(prices, args.model, m.cache_hit_tokens, m.cache_miss_tokens,
                           m.output_tokens)
    model_name = f"{adapter.name}+{args.model}"
    write_prediction(run_dir / "preds.jsonl", iid, model_name, patch)
    write_prediction(run_dir / "predictions.jsonl", iid, model_name, patch)
    append_jsonl(run_dir / "metrics.jsonl", m.to_json())
    append_jsonl(run_dir / "cost.jsonl", m.to_json())
    rate = m.cache_hit_rate
    print(f"[{iid}] exit={exit_code} wall={m.wall_ms / 1000:.0f}s "
          f"in={m.input_tokens} out={m.output_tokens} "
          f"hit-rate={'n/a' if rate is None else f'{rate:.1%}'} "
          f"patch={m.patch_bytes}B {('ERR: ' + m.error) if m.error else ''}",
          flush=True)
    if not args.keep_ws:
        shutil.rmtree(ws, ignore_errors=True)


def process_batch(adapter: Adapter, args, run_dir: Path, prices: dict,
                  batch: list[dict]) -> None:
    """Run one state-sharing batch sequentially inside a worker."""
    for inst in batch:
        process_instance(adapter, args, run_dir, prices, inst)


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--harness", required=True, choices=sorted(ADAPTERS))
    ap.add_argument("--model", default="deepseek-v4-flash",
                    help="deepseek-v4-flash | deepseek-v4-pro (deepseek-chat/-reasoner retired 2026-07-24)")
    ap.add_argument("--dataset", required=True, help="local SWE-bench Verified jsonl")
    ap.add_argument("--ids", help="file with instance ids, one per line")
    ap.add_argument("--sample", type=int, default=0, help="random sample size (seeded)")
    ap.add_argument("--seed", type=int, default=20260824)
    ap.add_argument("--run-id", default=None)
    ap.add_argument("--results-dir", default=str(HERE / "results"))
    ap.add_argument("--work-dir", default=str(HERE / "work"))
    ap.add_argument("--workers", type=int, default=1,
                    help="parallel workers; semantix memory-on parallelizes repos while "
                    "serializing the selected order within each repo")
    ap.add_argument("--timeout", type=int, default=2400, help="per-instance seconds")
    ap.add_argument("--max-turns", type=int, default=120, help="claude-code turn cap")
    ap.add_argument("--preset", default="balanced", help="semantix preset")
    ap.add_argument("--effort", default="", help="semantix provider effort override")
    ap.add_argument("--ablate", default="", help="semantix --ablate arm (harness-side modules only; "
                    "it does NOT toggle the memory kernel — use --semantix-memory for that)")
    ap.add_argument("--semantix-memory", default="on", choices=("on", "off"),
                    help="semantix memory-kernel arm: on = [semantix] enabled+inject, shared slice "
                    "library across instances, per-instance extract; off = kernel disabled (ablation twin)")
    ap.add_argument("--protocol", default="standard", choices=("standard", "grouped"),
                    help="standard isolates every instance store (leaderboard-comparable); grouped "
                    "orders by repo and preserves that repo's store across instances (non-standard Track B)")
    ap.add_argument("--semantix-retrieval-mode", default="strict",
                    choices=("off", "shadow", "strict"),
                    help="L2 retrieval mode when --semantix-memory=on; shadow records the same "
                    "candidates and admission decisions as strict but leaves provider messages unchanged")
    ap.add_argument("--semantix-bin", default="")
    ap.add_argument("--semantix-kernel-bin", default="",
                    help="path to the semantix kernel CLI (default: bin/semantix) used for slice extraction")
    ap.add_argument("--semantix-seed-dir", default="",
                    help="frozen repo-store root copied once into each memory-on run")
    ap.add_argument("--codex-bin", default="", help="codex binary override (chat wire needs ≤0.80.0)")
    ap.add_argument("--codex-wire-api", default="responses", choices=["responses", "chat"])
    ap.add_argument("--state-dir", default=os.path.expanduser("~/.cache/semantix-swebench"),
                    help="adapter home dirs live here (not /tmp; codex refuses temp dirs)")
    ap.add_argument("--custom-spec", default="")
    ap.add_argument("--prices", default="", help="JSON price table override")
    ap.add_argument("--openai-base", default="", help="override OpenAI-compatible base URL (mock)")
    ap.add_argument("--anthropic-base", default="", help="override Anthropic-compatible base URL (mock)")
    ap.add_argument("--keep-ws", action="store_true")
    args = ap.parse_args()
    resolve_cli_paths(args)

    if args.harness == "semantix" and args.protocol == "standard" and args.semantix_seed_dir:
        ap.error("--semantix-seed-dir requires --protocol grouped; standard runs must start without reusable history")

    if not args.run_id:
        protocol_part = f".{args.protocol}" if args.harness == "semantix" else ""
        args.run_id = f"{args.harness}.{args.model.replace('/', '-')}{protocol_part}.{args.seed}"
    run_dir = Path(args.results_dir) / args.run_id
    run_dir.mkdir(parents=True, exist_ok=True)
    args.state_path = Path(args.state_dir) / args.run_id
    args.state_path.mkdir(parents=True, exist_ok=True)

    rows = load_instances(Path(args.dataset))
    chosen = select_instances(rows, Path(args.ids) if args.ids else None, args.sample, args.seed)

    done = set()
    preds_path = run_dir / "preds.jsonl"
    if preds_path.exists():
        with open(preds_path) as f:
            done = {json.loads(l)["instance_id"] for l in f if l.strip()}
    todo = [r for r in chosen if r["instance_id"] not in done]
    print(f"run_id={args.run_id} harness={args.harness} model={args.model} "
          f"instances={len(chosen)} (skipping {len(chosen) - len(todo)} already done)",
          flush=True)

    adapter = ADAPTERS[args.harness](args, run_dir)
    adapter.prepare()
    batches = adapter.execution_batches(todo)
    prices = load_prices(args.prices or None)

    run_config = dict(vars(args))
    try:
        run_config["runner_commit"] = subprocess.run(
            ["git", "rev-parse", "HEAD"], cwd=HERE.parent.parent,
            check=True, capture_output=True, text=True,
        ).stdout.strip()
    except (OSError, subprocess.SubprocessError):
        run_config["runner_commit"] = "unknown"
    (run_dir / "run_config.json").write_text(json.dumps(run_config, indent=2, default=str))

    if args.workers <= 1:
        for batch in batches:
            process_batch(adapter, args, run_dir, prices, batch)
    else:
        with cf.ThreadPoolExecutor(max_workers=args.workers) as pool:
            futs = [pool.submit(process_batch, adapter, args, run_dir, prices, batch)
                    for batch in batches]
            for fut in cf.as_completed(futs):
                exc = fut.exception()
                if exc:
                    print(f"worker error: {exc!r}", file=sys.stderr, flush=True)

    print(f"done: predictions at {run_dir / 'predictions.jsonl'}; costs at {run_dir / 'cost.jsonl'}", flush=True)


if __name__ == "__main__":
    main()
