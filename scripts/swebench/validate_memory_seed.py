#!/usr/bin/env python3
"""Fail-closed preflight for a frozen repo-isolated Semantix memory seed."""

from __future__ import annotations

import argparse
import hashlib
import json
from collections import defaultdict
from pathlib import Path


INJECTABLE_TYPES = {"context", "memory", "result"}
TYPE_NAMES = {0: "prompt", 1: "context", 2: "tool_pattern", 3: "result", 4: "memory"}


def read_jsonl(path: Path):
    if not path.exists():
        return
    for number, line in enumerate(path.read_text(encoding="utf-8", errors="replace").splitlines(), 1):
        if not line.strip():
            continue
        try:
            yield json.loads(line)
        except json.JSONDecodeError as exc:
            raise ValueError(f"{path}:{number}: malformed JSON: {exc.msg}") from exc


def load_store(path: Path) -> dict[str, dict]:
    entries: dict[str, dict] = {}
    for row in read_jsonl(path) or ():
        if row.get("ID"):
            entries[row["ID"]] = row
    journal = Path(str(path) + ".journal")
    journal_rows = list(read_jsonl(journal) or ())
    if journal_rows:
        header = journal_rows.pop(0)
        stat = path.stat()
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        if (header.get("j") != 1 or header.get("bsize") != stat.st_size
                or header.get("bmtime") != stat.st_mtime_ns
                or header.get("bsha") != digest):
            raise ValueError(f"{journal}: journal header does not match base store")
    for row in journal_rows:
        op = row.get("op")
        if op == "put" and isinstance(row.get("s"), dict) and row["s"].get("ID"):
            entries[row["s"]["ID"]] = row["s"]
        elif op == "del":
            entries.pop(row.get("id"), None)
    return entries


def expected_repos(dataset: Path, ids_path: Path) -> dict[str, list[str]]:
    ids = {line.strip() for line in ids_path.read_text(encoding="utf-8").splitlines()
           if line.strip() and not line.lstrip().startswith("#")}
    repos: dict[str, list[str]] = defaultdict(list)
    seen: set[str] = set()
    for row in read_jsonl(dataset) or ():
        instance_id = row.get("instance_id")
        if instance_id not in ids:
            continue
        repo = row.get("repo")
        if not isinstance(repo, str) or repo.count("/") != 1:
            raise ValueError(f"{instance_id}: invalid repo identity {repo!r}")
        repos[repo.lower()].append(instance_id)
        seen.add(instance_id)
    missing = sorted(ids - seen)
    if missing:
        raise ValueError(f"IDs absent from dataset: {', '.join(missing)}")
    return dict(sorted(repos.items()))


def type_name(row: dict) -> str:
    value = row.get("Type")
    return TYPE_NAMES.get(value, str(value).lower()) if isinstance(value, int) else str(value).lower()


def meta_value(row: dict, snake: str, go_name: str):
    meta = row.get("Meta") or {}
    return meta.get(snake, meta.get(go_name, ""))


def validate(seed_dir: Path, dataset: Path, ids_path: Path,
             min_library: int = 5, min_source_sessions: int = 2) -> dict:
    repos = expected_repos(dataset, ids_path)
    report = {"ok": True, "seed_dir": str(seed_dir), "repos": {}, "errors": []}
    for repo, instance_ids in repos.items():
        key = repo.replace("/", "__", 1)
        db = seed_dir / key / ".semantix" / "project.db"
        store_error = ""
        try:
            entries = load_store(db) if db.exists() else {}
        except ValueError as exc:
            entries = {}
            store_error = str(exc)
        sessions_by_type: dict[str, set[str]] = defaultdict(set)
        injectable: list[dict] = []
        missing_commit: list[str] = []
        verified_results = 0
        for row in entries.values():
            kind = type_name(row)
            if kind not in INJECTABLE_TYPES:
                continue
            status = meta_value(row, "result_status", "ResultStatus")
            if kind == "result" and str(status).lower() != "verified":
                continue
            if kind == "result":
                verified_results += 1
            injectable.append(row)
            session = meta_value(row, "source_session", "SourceSession")
            if session:
                sessions_by_type[kind].add(str(session))
            if not meta_value(row, "base_commit", "BaseCommit"):
                missing_commit.append(str(row.get("ID", "<unknown>")))

        eligible_types = sorted(kind for kind, sessions in sessions_by_type.items()
                                if len(sessions) >= min_source_sessions)
        errors: list[str] = []
        if not db.exists():
            errors.append("missing_store")
        elif store_error:
            errors.append("store_invalid")
        if len(entries) < min_library:
            errors.append("library_too_small")
        if not eligible_types:
            errors.append("type_sources_too_few")
        if missing_commit:
            errors.append("commit_unknown")
        repo_report = {
            "instances": len(instance_ids), "store": str(db), "slices": len(entries),
            "injectable": len(injectable), "verified_results": verified_results,
            "source_sessions_by_type": {k: len(v) for k, v in sorted(sessions_by_type.items())},
            "eligible_types": eligible_types, "missing_base_commit": sorted(missing_commit),
            "store_error": store_error, "errors": errors,
        }
        report["repos"][repo] = repo_report
        report["errors"].extend(f"{repo}:{error}" for error in errors)
    report["ok"] = not report["errors"]
    report["summary"] = {
        "repos": len(repos),
        "ready_repos": sum(not item["errors"] for item in report["repos"].values()),
        "instances": sum(len(items) for items in repos.values()),
    }
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--seed-dir", type=Path, required=True)
    parser.add_argument("--dataset", type=Path, required=True)
    parser.add_argument("--ids", type=Path, required=True)
    parser.add_argument("--min-library", type=int, default=5)
    parser.add_argument("--min-source-sessions", type=int, default=2)
    parser.add_argument("--json-out", type=Path)
    args = parser.parse_args()
    report = validate(args.seed_dir, args.dataset, args.ids,
                      args.min_library, args.min_source_sessions)
    rendered = json.dumps(report, indent=2, ensure_ascii=False) + "\n"
    if args.json_out:
        args.json_out.parent.mkdir(parents=True, exist_ok=True)
        args.json_out.write_text(rendered, encoding="utf-8")
    print(rendered, end="")
    return 0 if report["ok"] else 3


if __name__ == "__main__":
    raise SystemExit(main())
