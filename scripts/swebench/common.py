"""Shared plumbing for the SWE-bench Verified harness comparison.

One benchmark run = one harness × one model × one instance subset. Every
adapter produces the same two artifacts so score / tokens / cache hit rate /
wall time are comparable across harnesses:

  results/<run_id>/predictions.jsonl  SWE-bench prediction format
  results/<run_id>/cost.jsonl         one normalized cost/usage record per instance

Legacy aliases (preds.jsonl and metrics.jsonl) are emitted for existing tools.

The normalized record is deliberately small; each harness's native metrics
payload is preserved verbatim under "raw" for audit.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import time
from contextlib import contextmanager
from dataclasses import dataclass, field, asdict
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO_ROOT = HERE.parent.parent

# DeepSeek per-1M-token USD prices used to compute a comparable cost when a
# harness cannot price the run itself. Off-peak rates from the 2026-08-16
# price table; peak windows (01:00-04:00 and 06:00-10:00 UTC) double them.
# Override with --prices path/to.json ({"model": {"cache_hit": x,
# "cache_miss": y, "output": z}}) — verify against
# https://api-docs.deepseek.com/quick_start/pricing before publishing numbers.
DEFAULT_PRICES = {
    "deepseek-v4-flash": {"cache_hit": 0.007, "cache_miss": 0.22, "output": 0.66},
    "deepseek-v4-pro": {"cache_hit": 0.022, "cache_miss": 0.66, "output": 1.98},
    # Retired 2026-07-24 but kept for replaying old runs.
    "deepseek-chat": {"cache_hit": 0.028, "cache_miss": 0.28, "output": 0.42},
    "deepseek-reasoner": {"cache_hit": 0.028, "cache_miss": 0.28, "output": 0.42},
}


def load_prices(path: str | None) -> dict:
    prices = dict(DEFAULT_PRICES)
    if path:
        prices.update(json.loads(Path(path).read_text()))
    return prices


def price_run(prices: dict, model: str, cache_hit: int, cache_miss: int, output: int) -> float | None:
    for key in (model, model.split("/")[-1]):
        if key in prices:
            p = prices[key]
            return (
                cache_hit * p["cache_hit"] + cache_miss * p["cache_miss"] + output * p["output"]
            ) / 1e6
    return None


@dataclass
class InstanceMetrics:
    run_id: str
    harness: str
    model: str
    instance_id: str
    wall_ms: int = 0
    input_tokens: int = 0            # prompt tokens, cache hits included
    output_tokens: int = 0
    cache_hit_tokens: int = 0
    cache_miss_tokens: int = 0
    steps: int = 0                   # model calls
    tool_calls: int = 0
    model_calls_by_source: dict[str, int] = field(default_factory=dict)
    executor_calls: int = 0
    planner_calls: int = 0
    subagent_calls: int = 0
    compaction_calls: int = 0
    other_model_calls: int = 0
    source_call_total: int = 0
    source_call_delta: int = 0
    provider_retries: int = 0
    compactions: int = 0
    subagent_runs: int = 0
    tool_failures: int = 0
    tool_calls_by_name: dict[str, int] = field(default_factory=dict)
    # None means the selected harness/binary predates repeat attribution;
    # zero means attribution ran and observed no repeated signature.
    repeated_tool_calls: int | None = None
    repeated_tool_calls_by_name: dict[str, int] | None = None
    semantix_fuse_turns: int = 0
    semantix_rejected_slices: int = 0
    cost_usd: float | None = None    # computed from DeepSeek prices when possible
    cost_native: float | None = None # what the harness itself reported
    cost_native_currency: str = ""
    patch_bytes: int = 0
    empty_patch: bool = True
    agent_exit: int | None = None
    error: str = ""
    raw: dict = field(default_factory=dict)

    @property
    def cache_hit_rate(self) -> float | None:
        denom = self.cache_hit_tokens + self.cache_miss_tokens
        if denom <= 0:
            return None
        return self.cache_hit_tokens / denom

    def to_json(self) -> str:
        d = asdict(self)
        d["cache_hit_rate"] = self.cache_hit_rate
        return json.dumps(d, ensure_ascii=False)


# ---------------------------------------------------------------------------
# Dataset
# ---------------------------------------------------------------------------

VERIFIED_HF_ID = "SWE-bench/SWE-bench_Verified"


def fetch_dataset(out_path: Path) -> Path:
    """Download SWE-bench Verified to a local jsonl (requires huggingface.co egress)."""
    from datasets import load_dataset  # lazy: heavy import

    ds = load_dataset(VERIFIED_HF_ID, split="test")
    out_path.parent.mkdir(parents=True, exist_ok=True)
    ds.to_json(str(out_path))
    return out_path


def load_instances(dataset_path: Path) -> list[dict]:
    rows = []
    with open(dataset_path) as f:
        for line in f:
            line = line.strip()
            if line:
                rows.append(json.loads(line))
    return rows


def select_instances(rows: list[dict], ids_file: Path | None, sample: int, seed: int) -> list[dict]:
    if ids_file:
        wanted = [l.strip() for l in ids_file.read_text().splitlines() if l.strip() and not l.startswith("#")]
        by_id = {r["instance_id"]: r for r in rows}
        missing = [w for w in wanted if w not in by_id]
        if missing:
            raise SystemExit(f"instance ids not in dataset: {missing}")
        return [by_id[w] for w in wanted]
    if sample and sample < len(rows):
        import random

        rng = random.Random(seed)
        return rng.sample(sorted(rows, key=lambda r: r["instance_id"]), sample)
    return rows


# ---------------------------------------------------------------------------
# Workspace management: bare cache clone once per repo, cheap local clone per
# instance, checkout at base_commit.
# ---------------------------------------------------------------------------


def _run(cmd: list[str], cwd: Path | None = None, check: bool = True, env: dict | None = None,
         timeout: int | None = None) -> subprocess.CompletedProcess:
    return subprocess.run(cmd, cwd=cwd, check=check, env=env, timeout=timeout,
                          capture_output=True, text=True)


@contextmanager
def exclusive_file_lock(path: Path):
    """Hold a one-byte advisory lock on POSIX and Windows."""
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a+b") as lock:
        if os.name == "nt":
            import msvcrt

            lock.seek(0, os.SEEK_END)
            if lock.tell() == 0:
                lock.write(b"\0")
                lock.flush()
            while True:
                lock.seek(0)
                try:
                    msvcrt.locking(lock.fileno(), msvcrt.LK_NBLCK, 1)
                    break
                except OSError:
                    time.sleep(0.05)
            try:
                yield
            finally:
                lock.seek(0)
                msvcrt.locking(lock.fileno(), msvcrt.LK_UNLCK, 1)
        else:
            import fcntl

            fcntl.flock(lock, fcntl.LOCK_EX)
            try:
                yield
            finally:
                fcntl.flock(lock, fcntl.LOCK_UN)


def ensure_repo_cache(work_dir: Path, repo: str) -> Path:
    cache = work_dir / "repos" / (repo.replace("/", "__") + ".git")
    if cache.exists():
        return cache
    cache.parent.mkdir(parents=True, exist_ok=True)
    # Serialize concurrent clones (workers race on first use of a repo);
    # clone to a temp path so a killed clone never leaves a half-built cache.
    with exclusive_file_lock(cache.with_suffix(".lock")):
        if not cache.exists():
            tmp = cache.with_suffix(".tmp")
            if tmp.exists():
                shutil.rmtree(tmp)
            _run(["git", "clone", "--bare", f"https://github.com/{repo}.git", str(tmp)])
            tmp.rename(cache)
    return cache


def prepare_workspace(work_dir: Path, run_id: str, inst: dict) -> Path:
    ws = work_dir / "ws" / run_id / inst["instance_id"]
    if ws.exists():
        shutil.rmtree(ws)
    cache = ensure_repo_cache(work_dir, inst["repo"])
    # --shared is safe: workspaces are throwaway and never gc'd independently.
    _run(["git", "clone", "--shared", "--no-checkout", str(cache), str(ws)])
    _run(["git", "checkout", "-q", inst["base_commit"]], cwd=ws)
    _run(["git", "config", "user.email", "swebench@example.invalid"], cwd=ws)
    _run(["git", "config", "user.name", "swebench-runner"], cwd=ws)
    return ws


def extract_patch(ws: Path) -> str:
    """Diff of everything the agent left in the working tree (staged or not,
    new files included), relative to base_commit."""
    _run(["git", "add", "-A"], cwd=ws)
    diff = _run(["git", "diff", "--cached", "--no-color"], cwd=ws).stdout
    return diff


# ---------------------------------------------------------------------------
# Prompt — identical across harnesses so the comparison isolates the harness.
# ---------------------------------------------------------------------------

# Wording note: several harnesses extract natural-language constraints from the
# prompt with substring matching (semantix-agent's task policy treats phrases
# like "do not modify" / "no tests" as global write/test bans — see
# harness/taskpolicy parseConstraints). Scoped requirements below are phrased
# positively so no harness reads them as a blanket mutation ban.
PROMPT_TEMPLATE = """You are working in a git checkout of the {repo} repository at commit {base_commit}. Resolve the GitHub issue below.

<issue>
{problem_statement}
</issue>

Requirements:
- Find the root cause and implement a complete fix by editing non-test source files.
- Keep every existing test file exactly as it is; adding new test files is unnecessary.
- Leave all changes uncommitted in the working tree.
- The full test suite may be too heavy for this environment; verify what you can and finish once the fix is in place.
"""


def build_prompt(inst: dict) -> str:
    return PROMPT_TEMPLATE.format(
        repo=inst["repo"],
        base_commit=inst["base_commit"],
        problem_statement=inst["problem_statement"].strip(),
    )


# ---------------------------------------------------------------------------
# Output writers
# ---------------------------------------------------------------------------


def append_jsonl(path: Path, obj_line: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with open(path, "a") as f:
        f.write(obj_line.rstrip("\n") + "\n")


def write_prediction(path: Path, instance_id: str, model_name: str, patch: str) -> None:
    append_jsonl(path, json.dumps({
        "instance_id": instance_id,
        "model_name_or_path": model_name,
        "model_patch": patch,
    }, ensure_ascii=False))


class Stopwatch:
    def __enter__(self):
        self.t0 = time.monotonic()
        return self

    def __exit__(self, *exc):
        self.ms = int((time.monotonic() - self.t0) * 1000)
