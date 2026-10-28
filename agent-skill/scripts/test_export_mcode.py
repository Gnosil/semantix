from contextlib import closing
import hashlib
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from export_mcode import export


class MCodeExportTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.db = self.root / "runtime.sqlite"
        self.out = self.root / "export"
        with closing(sqlite3.connect(self.db)) as conn, conn:
            conn.executescript("""
                CREATE TABLE local_runtime_sessions(session_id TEXT PRIMARY KEY);
                CREATE TABLE local_runtime_message_rows(session_id TEXT, role TEXT, data_json TEXT, created_at_ms INTEGER);
                INSERT INTO local_runtime_sessions VALUES ('a'), ('b');
            """)

    def add(self, sid, value, sql_role="assistant"):
        with closing(sqlite3.connect(self.db)) as conn, conn:
            conn.execute("INSERT INTO local_runtime_message_rows VALUES (?, ?, ?, 0)",
                         (sid, sql_role, json.dumps(value, ensure_ascii=False)))

    @staticmethod
    def read(paths):
        return [json.loads(line) for path in paths
                for line in Path(path).read_text(encoding="utf-8").splitlines()]

    def test_authoritative_role_multiple_sessions_tools_and_incremental_runs(self):
        self.add("a", {"role": "user", "msg_type": 1, "msg_content": "修复字幕偏移"})
        self.add("a", {"role": "assistant", "msg_type": 2, "msg_content": [
            {"type": "text", "text": "查看字幕"},
            {"type": "tool_use", "id": "c1", "name": "bash", "input": {"cmd": "cat subtitles.srt"}},
        ], "tool_calls": [{"id": "c1", "name": "bash", "arguments": {"cmd": "cat subtitles.srt"}}]})
        # Tool-only assistant messages must survive too (for ToolPattern).
        self.add("a", {"role": "assistant", "msg_type": 2, "msg_content": "", "tool_calls": [
            {"id": "c2", "name": "bash", "arguments": {"cmd": "python align.py"}}]})
        self.add("a", {"role": "user", "msg_type": 3, "msg_content": "todo_updated"})
        self.add("a", {"role": "assistant", "msg_type": 2, "msg_content": "已修复字幕偏移"})
        self.add("b", {"role": "user", "msg_type": 1, "msg_content": "另一个会话"})
        before = self.db.read_bytes()
        first = export(self.db, self.out)
        rows = self.read(first["files"])
        self.assertEqual(first["sessions"], 2)
        self.assertEqual(first["messages"], 5)
        self.assertEqual([r["content"] for r in rows], ["修复字幕偏移", "查看字幕", "", "已修复字幕偏移", "另一个会话"])
        self.assertEqual(rows[0]["role"], "user")
        self.assertEqual(len(rows[1]["tool_calls"]), 1)
        self.assertEqual(self.db.read_bytes(), before)
        self.assertEqual(export(self.db, self.out)["files"], [])
        self.add("a", {"role": "user", "msg_type": 1, "msg_content": "新增任务"})
        appended = export(self.db, self.out)
        self.assertEqual(appended["messages"], 1)
        self.assertEqual(self.read(appended["files"]), [{"role": "user", "content": "新增任务"}])

    def test_memory_changes_without_new_session_rows_and_preserves_tail(self):
        memory = self.root / "user.md"
        memory.write_text("**字幕规则**\n保持时间轴。\n\n应用方式：修复字幕偏移。\n<!-- entry -->\n**偏好**\n先运行测试。", encoding="utf-8")
        first = export(self.db, self.out, memory)
        self.assertEqual(first["memory_blocks"], 2)
        rows = self.read(first["files"])
        self.assertEqual(len(rows), 2)
        self.assertEqual(rows[0]["content"].replace("\r\n", "\n"), "【字幕规则】\n**字幕规则**\n保持时间轴。\n\n应用方式：修复字幕偏移。")
        self.assertTrue(all(r["role"] == "user" for r in rows))
        self.assertEqual(export(self.db, self.out, memory)["files"], [])
        memory.write_text("**字幕规则**\n保持时间轴，使用新的对齐命令。", encoding="utf-8")
        changed = export(self.db, self.out, memory)
        self.assertEqual(changed["messages"], 0)
        self.assertEqual(changed["memory_blocks"], 1)

    def test_failure_does_not_advance_state_or_publish_other_sessions(self):
        self.add("a", {"role": "user", "msg_content": "initial"})
        export(self.db, self.out)
        before = {p.name: p.read_bytes() for p in self.out.iterdir()}
        self.add("a", {"role": "user", "msg_content": "valid new message"})
        with closing(sqlite3.connect(self.db)) as conn, conn:
            conn.execute("INSERT INTO local_runtime_message_rows VALUES ('b', 'assistant', '{broken', 0)")
        with self.assertRaisesRegex(ValueError, "invalid message at rowid"):
            export(self.db, self.out)
        self.assertEqual({p.name: p.read_bytes() for p in self.out.iterdir()}, before)

    def test_missing_database_schema_and_memory_leave_state_unchanged(self):
        export(self.db, self.out)
        before = (self.out / "state.json").read_bytes()
        with self.assertRaises(OSError):
            export(self.db, self.out, self.root / "missing.md")
        self.assertEqual((self.out / "state.json").read_bytes(), before)
        with closing(sqlite3.connect(self.db)) as conn, conn:
            conn.execute("DROP TABLE local_runtime_message_rows")
        with self.assertRaises(sqlite3.Error):
            export(self.db, self.out)
        self.assertEqual((self.out / "state.json").read_bytes(), before)
        missing = self.root / "does-not-exist.sqlite"
        with self.assertRaises(sqlite3.Error):
            export(missing, self.root / "other")
        self.assertFalse(missing.exists())

    def test_selection_safe_filenames_memory_only_and_private_files(self):
        self.add("a", {"role": "user", "msg_content": "selected"})
        self.add("b", {"role": "user", "msg_content": "not selected"})
        report = export(self.db, self.out, sessions=["a"])
        self.assertEqual(self.read(report["files"])[0]["content"], "selected")
        with self.assertRaisesRegex(ValueError, "not found"):
            export(self.db, self.out, sessions=["missing"])
        memory = self.root / "user.md"
        memory.write_text("**Preference**\nUse targeted tests.", encoding="utf-8")
        report = export(self.root / "absent.sqlite", self.root / "memory-only", memory, memory_only=True)
        self.assertEqual(report["memory_blocks"], 1)
        if os.name != "nt":
            for path in (self.root / "memory-only").iterdir():
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)

    def test_replaced_database_and_corrupt_state_fail_loudly(self):
        self.add("a", {"role": "user", "msg_content": "first"})
        export(self.db, self.out)
        with closing(sqlite3.connect(self.db)) as conn, conn:
            conn.execute("DELETE FROM local_runtime_message_rows")
        with self.assertRaisesRegex(ValueError, "backwards"):
            export(self.db, self.out)
        for invalid in (None, [], {}, {"version": 99}):
            (self.out / "state.json").write_text(json.dumps(invalid), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "invalid export state"):
                export(self.db, self.out)

    def test_wal_rows_are_visible_without_writing_the_source(self):
        with closing(sqlite3.connect(self.db)) as writer, writer:
            writer.execute("PRAGMA journal_mode=WAL")
            writer.execute("INSERT INTO local_runtime_message_rows VALUES ('a', 'assistant', ?, 0)",
                           (json.dumps({"role": "user", "msg_content": "committed WAL message"}),))
            writer.commit()
            before = {suffix: hashlib.sha256(Path(str(self.db) + suffix).read_bytes()).hexdigest()
                      for suffix in ("", "-wal")}
            report = export(self.db, self.out)
            self.assertEqual(self.read(report["files"])[0]["content"], "committed WAL message")
            self.assertEqual(before, {suffix: hashlib.sha256(Path(str(self.db) + suffix).read_bytes()).hexdigest()
                                      for suffix in ("", "-wal")})

    def test_extraction_failure_keeps_watermark_and_retry_reuses_batch(self):
        self.add("a", {"role": "user", "msg_content": "new task"})
        store = self.root / "shared.db"
        with patch("export_mcode.subprocess.run", side_effect=subprocess.CalledProcessError(1, "semantix")):
            with self.assertRaises(subprocess.CalledProcessError):
                export(self.db, self.out, extractor="semantix", store=store)
        self.assertFalse((self.out / "state.json").exists())
        files = sorted(self.out.glob("*.jsonl"))
        self.assertEqual(len(files), 1)
        with patch("export_mcode.subprocess.run") as run:
            retry = export(self.db, self.out, extractor="semantix", store=store)
        self.assertEqual(retry["files"], [str(p) for p in files])
        self.assertEqual(run.call_count, 1)
        self.assertTrue((self.out / "state.json").exists())
        with self.assertRaisesRegex(ValueError, "different database/store/export mode"):
            export(self.db, self.out, extractor="semantix", store=self.root / "another.db")
        with self.assertRaisesRegex(ValueError, "different database/store/export mode"):
            export(self.db, self.out)

    @unittest.skipUnless(os.environ.get("SEMANTIX_BIN"), "set SEMANTIX_BIN for real CLI integration")
    def test_export_extract_and_cross_agent_lookup(self):
        self.add("a", {"role": "user", "msg_content": "修复字幕偏移"})
        for i in range(2):
            self.add("a", {"role": "assistant", "msg_content": "", "tool_calls": [
                {"id": f"c{i}", "name": "bash", "arguments": {"cmd": "python align.py"}}]})
        self.add("a", {"role": "assistant", "msg_content": "字幕偏移已修复"})
        memory = self.root / "user.md"
        memory.write_text("**字幕偏好**\n字幕对齐时保留时间轴，并先检查字幕偏移。", encoding="utf-8")
        binary = os.environ["SEMANTIX_BIN"]
        store = self.root / "shared-user.db"
        export(self.db, self.out, memory, extractor=binary, store=store)
        def stored_slices():
            result = subprocess.run([binary, "export", "--scope", "user", "--db", str(store)],
                                    check=True, capture_output=True, encoding="utf-8", cwd=self.root)
            return [json.loads(line) for line in result.stdout.splitlines()]

        stored = stored_slices()
        # The on-disk Slice enum is Prompt=0, Context=1, ToolPattern=2, Result=3.
        self.assertTrue({0, 2, 3}.issubset({r["Type"] for r in stored}), stored)
        count = len(stored)
        self.assertEqual(export(self.db, self.out, memory, extractor=binary, store=store)["files"], [])
        other_agent = self.root / "other-agent"
        other_agent.mkdir()
        found = subprocess.run([binary, "lookup", "--query", "字幕偏好 时间轴", "--scope", "user", "--db", str(store), "--json"],
                               check=True, capture_output=True, cwd=other_agent, encoding="utf-8")
        self.assertIn("保留时间轴", found.stdout)
        self.assertEqual(len(stored_slices()), count)
        old_ids = {r["ID"] for r in stored}
        self.add("a", {"role": "user", "msg_content": "新增独立需求：调整配色"})
        appended = export(self.db, self.out, memory, extractor=binary, store=store)
        self.assertEqual(appended["messages"], 1)
        self.assertEqual(len({r["ID"] for r in stored_slices()} - old_ids), 1)


if __name__ == "__main__":
    unittest.main()
