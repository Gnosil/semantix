#!/usr/bin/env python3
"""Export MCode's local-runtime-v2 SQLite sessions to Semantix JSONL.

Uses only the Python standard library. Export state belongs to the output
directory; the source database is opened read-only. Run after sessions finish,
with only one exporter writing a given output directory at a time.
"""

import argparse
from contextlib import closing
import hashlib
import json
import os
from pathlib import Path
import re
import sqlite3
import subprocess
import sys
import tempfile


def tool_call(value):
    if (not isinstance(value, dict) or not isinstance(value.get("id"), str)
            or not value["id"] or not isinstance(value.get("name"), str) or not value["name"]):
        raise ValueError("tool call requires an id and name")
    arguments = value.get("arguments", value.get("input", {}))
    if isinstance(arguments, str):
        arguments = json.loads(arguments)
    if not isinstance(arguments, dict):
        raise ValueError("tool arguments must be an object")
    return {"id": value["id"], "name": value["name"], "arguments": arguments}


def message(value):
    if not isinstance(value, dict):
        raise ValueError("message JSON must be an object")
    if value.get("msg_type") == 3:
        return None  # Runtime events are not conversation.
    role = value.get("role")  # The SQL role column is NOT authoritative.
    if role not in ("user", "assistant"):
        return None
    content = value.get("msg_content", "")
    calls = []
    if isinstance(content, str):
        text = content
    elif isinstance(content, list):
        parts = []
        for block in content:
            if not isinstance(block, dict):
                raise ValueError("content block must be an object")
            if block.get("type") == "text":
                if not isinstance(block.get("text"), str):
                    raise ValueError("text block requires text")
                parts.append(block["text"])
            elif block.get("type") == "tool_use":
                calls.append(tool_call(block))
        text = "\n".join(parts)
    else:
        raise ValueError("msg_content must be text or a content-block list")
    explicit = value.get("tool_calls")
    if explicit is None:
        explicit = []
    if not isinstance(explicit, list):
        raise ValueError("tool_calls must be a list")
    calls.extend(tool_call(call) for call in explicit)
    if not text.strip() and not calls:
        return None
    out = {"role": role, "content": text}
    if role == "assistant" and calls:
        unique = {}
        for call in calls:
            if call["id"] in unique and unique[call["id"]] != call:
                raise ValueError("conflicting copies of a tool call")
            unique[call["id"]] = call
        out["tool_calls"] = list(unique.values())
    return out


def memory_messages(text):
    for block in re.split(r"<!--.*?-->", text, flags=re.DOTALL):
        block = block.strip()
        if not block:
            continue
        title = re.search(r"\*\*(.+?)\*\*", block)
        title = title.group(1) if title else block.splitlines()[0].lstrip("# ")
        yield {"role": "user", "content": f"【{title}】\n{block}"}


def write_lines(path, values):
    count = 0
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as stream:
        for value in values:
            stream.write(json.dumps(value, ensure_ascii=False, separators=(",", ":")) + "\n")
            count += 1
        stream.flush()
        os.fsync(stream.fileno())
    return count


def export(database, output_dir, memory=None, sessions=None, memory_only=False, extractor=None, store=None):
    database, output_dir = Path(database).expanduser().resolve(), Path(output_dir).expanduser().resolve()
    state_path = output_dir / "state.json"
    if database == state_path or (memory and Path(memory).expanduser().resolve() == state_path):
        raise ValueError("source paths must not be the export state path")
    if extractor and (store is None or Path(store).expanduser().resolve() in
                      {database, state_path, Path(memory).expanduser().resolve() if memory else database}):
        raise ValueError("extraction requires a store separate from the sources and export state")
    has_state = state_path.exists()
    original = json.loads(state_path.read_text(encoding="utf-8")) if has_state else None
    target = str(Path(store).expanduser().resolve()) if extractor else None
    state = original if has_state else {"version": 1, "database": str(database), "store": target, "sessions": {}, "memory_sha256": None}
    if (not isinstance(state, dict) or state.get("version") != 1 or state.get("database") != str(database)
            or state.get("store") != target
            or not isinstance(state.get("sessions"), dict)
            or any(type(n) is not int or n < 0 for n in state["sessions"].values())):
        raise ValueError("invalid export state or different database/store/export mode; use a separate output directory")
    # Do not mutate the previous state until every source has been validated.
    state = dict(state, sessions=dict(state["sessions"]))
    output_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    report = {"files": [], "messages": 0, "memory_blocks": 0, "sessions": 0}
    with tempfile.TemporaryDirectory(prefix=".mcode-", dir=output_dir) as staging:
        staging = Path(staging)
        files = []
        if not memory_only:
            with closing(sqlite3.connect(database.as_uri() + "?mode=ro", uri=True)) as conn:
                conn.execute("PRAGMA query_only = ON")
                conn.execute("BEGIN")  # One snapshot across sessions, including WAL-backed databases.
                # Validate both required tables before publishing any output.
                conn.execute("SELECT session_id, data_json, rowid FROM local_runtime_message_rows LIMIT 0")
                available = [sid for sid, in conn.execute("SELECT session_id FROM local_runtime_sessions ORDER BY session_id")]
                if any(not isinstance(sid, str) or not sid for sid in available):
                    raise ValueError("session_id must be non-empty text")
                if sessions and set(sessions) - set(available):
                    raise ValueError("requested session not found")
                for sid in available:
                    if sessions and sid not in sessions:
                        continue
                    watermark = state["sessions"].get(sid, 0)
                    last = conn.execute("SELECT COALESCE(MAX(rowid), 0) FROM local_runtime_message_rows WHERE session_id=?", (sid,)).fetchone()[0]
                    if last < watermark:
                        raise ValueError("message rowids moved backwards; use a new output directory for a replaced database")
                    if last == watermark:
                        continue
                    filename = f"mcode-{hashlib.sha256(sid.encode()).hexdigest()}-{watermark + 1}-{last}.jsonl"
                    path = staging / filename

                    def converted():
                        # A separate cursor preserves the outer session iteration.
                        for rowid, data in conn.execute(
                                "SELECT rowid, data_json FROM local_runtime_message_rows WHERE session_id=? AND rowid>? AND rowid<=? ORDER BY rowid",
                                (sid, watermark, last)):
                            try:
                                out = message(json.loads(data))
                            except (ValueError, TypeError) as exc:
                                raise ValueError(f"invalid message at rowid {rowid} ({type(exc).__name__})") from None
                            if out is not None:
                                yield out

                    count = write_lines(path, converted())
                    if count:
                        files.append(path)
                    report["messages"] += count
                    report["sessions"] += 1
                    state["sessions"][sid] = last
        if memory is not None:
            data = Path(memory).expanduser().read_bytes()
            digest = hashlib.sha256(data).hexdigest()
            if digest != state.get("memory_sha256"):
                path = staging / f"mcode-memory-{digest}.jsonl"
                count = write_lines(path, memory_messages(data.decode("utf-8-sig")))
                if count:
                    files.append(path)
                report["memory_blocks"] = count
                state["memory_sha256"] = digest
        if original != state:
            write_lines(staging / "state.json", [state])
            for path in files:
                destination = output_dir / path.name
                os.replace(path, destination)
                report["files"].append(str(destination))
            if extractor:
                for path in report["files"]:
                    subprocess.run([str(extractor), "extract", "--input", path, "--scope", "user",
                                    "--db", str(Path(store).expanduser().resolve()), "--origin", "session-auto",
                                    "--session", Path(path).stem], check=True, stdout=subprocess.DEVNULL)
            os.replace(staging / "state.json", state_path)
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--db", type=Path, default=Path.home() / ".minimax/v2/sqlite/runtime-state.sqlite")
    parser.add_argument("--output-dir", type=Path, default=Path.home() / ".semantix/sessions/mcode")
    parser.add_argument("--session", action="append", help="export only this completed session; repeatable, default: all sessions")
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--memory", type=Path, help="memory file; default: ~/.minimax/memory/user.md if present")
    group.add_argument("--no-memory", action="store_true")
    parser.add_argument("--memory-only", action="store_true", help="export distilled rules without reading the session database")
    parser.add_argument("--extract", action="store_true", help="also run semantix extract; advance state only after successful extraction")
    parser.add_argument("--semantix", default="semantix", help="Semantix CLI executable used with --extract")
    parser.add_argument("--store", type=Path, default=Path.home() / ".semantix/user.db", help="shared user store used with --extract")
    args = parser.parse_args()
    memory = args.memory
    if memory is None and not args.no_memory:
        default = Path.home() / ".minimax/memory/user.md"
        memory = default if default.exists() else None
    if args.memory_only and memory is None:
        parser.error("--memory-only requires an existing memory file")
    try:
        result = export(args.db, args.output_dir, memory, args.session, args.memory_only,
                        args.semantix if args.extract else None, args.store)
    except (OSError, ValueError, sqlite3.Error, subprocess.CalledProcessError) as exc:
        print(f"export_mcode: {exc}", file=sys.stderr)
        return 1
    print(json.dumps(result, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
