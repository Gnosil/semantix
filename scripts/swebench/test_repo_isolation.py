#!/usr/bin/env python3
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

import common
from run_bench import Adapter, SemantixAdapter, process_batch, repo_store_key


def inst(iid: str, repo: str) -> dict:
    return {"instance_id": iid, "repo": repo}


class RepoIdentityTests(unittest.TestCase):
    def test_repo_store_key_is_canonical_and_path_safe(self):
        self.assertEqual(repo_store_key(inst("a", "django/django")), "django__django")
        self.assertEqual(repo_store_key(inst("b", "PyData/XArray")), "pydata__xarray")

    def test_repo_store_key_rejects_missing_or_unsafe_identity(self):
        for bad in ({}, {"repo": "django"}, {"repo": "../django"}, {"repo": "bad_owner/repo"},
                    {"repo": "a/b/c"}, {"repo": "a\\b"}):
            with self.subTest(bad=bad):
                with self.assertRaises(ValueError):
                    repo_store_key(bad)


class RepoSchedulingTests(unittest.TestCase):
    def test_memory_on_groups_by_repo_and_preserves_selected_order(self):
        adapter = SemantixAdapter(SimpleNamespace(), Path("run"))
        adapter.memory_on = True
        chosen = [
            inst("django-1", "django/django"),
            inst("flask-1", "pallets/flask"),
            inst("django-2", "django/django"),
            inst("flask-2", "pallets/flask"),
        ]
        batches = adapter.execution_batches(chosen)
        self.assertEqual([[x["instance_id"] for x in batch] for batch in batches],
                         [["django-1", "django-2"], ["flask-1", "flask-2"]])

    def test_memory_off_and_other_adapters_keep_instance_parallelism(self):
        chosen = [inst("a", "django/django"), inst("b", "django/django")]
        semantix = SemantixAdapter(SimpleNamespace(), Path("run"))
        semantix.memory_on = False
        self.assertEqual([len(x) for x in semantix.execution_batches(chosen)], [1, 1])
        self.assertEqual([len(x) for x in Adapter(SimpleNamespace(), Path("run")).execution_batches(chosen)], [1, 1])

    def test_process_batch_runs_instances_sequentially_in_batch_order(self):
        chosen = [inst("a", "django/django"), inst("b", "django/django")]
        observed = []
        with mock.patch("run_bench.process_instance",
                        side_effect=lambda _a, _args, _run, _prices, item: observed.append(item["instance_id"])):
            process_batch(Adapter(SimpleNamespace(), Path("run")), SimpleNamespace(),
                          Path("run"), {}, chosen)
        self.assertEqual(observed, ["a", "b"])


class RepoStoreTests(unittest.TestCase):
    def test_repo_cache_clone_is_portable_and_atomic(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)

            def fake_clone(command, **_kwargs):
                Path(command[-1]).mkdir()
                return SimpleNamespace(returncode=0, stdout="", stderr="")

            with mock.patch("common._run", side_effect=fake_clone) as run:
                cache = common.ensure_repo_cache(root, "django/django")
            self.assertTrue(cache.is_dir())
            self.assertEqual(run.call_count, 1)

    def test_prepare_workspace_removes_stale_tree_without_shell_rm(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            inst_row = {
                "instance_id": "django-1",
                "repo": "django/django",
                "base_commit": "abc123",
            }
            stale = root / "ws" / "run" / "django-1"
            stale.mkdir(parents=True)
            (stale / "leftover").write_text("stale", encoding="utf-8")
            cache = root / "cache.git"
            cache.mkdir()
            with mock.patch("common.ensure_repo_cache", return_value=cache), \
                    mock.patch("common._run") as run:
                workspace = common.prepare_workspace(root, "run", inst_row)
            self.assertFalse((workspace / "leftover").exists())
            self.assertEqual(run.call_count, 4)

    def adapter(self, root: Path) -> SemantixAdapter:
        args = SimpleNamespace(openai_base="http://127.0.0.1:8139/v1", effort="",
                               model="deepseek-v4-flash", semantix_retrieval_mode="strict")
        adapter = SemantixAdapter(args, root / "run")
        adapter.memory_on = True
        adapter.kernel_root = root / "semantix-home" / "kernel"
        adapter.kernel_bin = str(root / "semantix")
        return adapter

    def test_distinct_repos_generate_distinct_project_dirs_and_config(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            adapter = self.adapter(root)
            django_dir = adapter.kernel_dir_for(inst("d", "django/django"))
            flask_dir = adapter.kernel_dir_for(inst("f", "pallets/flask"))
            self.assertNotEqual(django_dir, flask_dir)
            self.assertEqual(django_dir.name, "django__django")
            self.assertEqual(flask_dir.name, "pallets__flask")

            home = root / "home"
            adapter._write_home(home, root / "sessions", django_dir)
            config = (home / "config.toml").read_text()
            self.assertIn(f'project_dir  = "{django_dir}"', config)
            self.assertNotIn(str(flask_dir), config)

    def test_extraction_uses_same_repo_store_and_real_project_identity(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            adapter = self.adapter(root)
            sessions = root / "sessions"
            sessions.mkdir()
            (sessions / "turn.jsonl").write_text("{}\n")
            kernel_dir = adapter.kernel_dir_for(inst("d", "django/django"))
            completed = SimpleNamespace(returncode=0, stdout="stored=1", stderr="")
            with mock.patch("run_bench.subprocess.run", return_value=completed) as run:
                result = adapter._extract_slices(sessions, "d", kernel_dir, "django/django")
            self.assertEqual(result["mirrors"], 1)
            command = run.call_args.args[0]
            self.assertEqual(command[command.index("--project") + 1], "django/django")
            self.assertEqual(Path(command[command.index("--project-db") + 1]),
                             kernel_dir / ".semantix" / "project.db")


if __name__ == "__main__":
    unittest.main()
