#!/usr/bin/env python3
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

import common
import run_bench
from run_bench import (
    Adapter,
    SemantixAdapter,
    process_batch,
    repo_store_key,
    semantix_ablation_spec,
)


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
    def test_memory_off_arm_is_named_no_kernel(self):
        self.assertEqual(semantix_ablation_spec("", False), "kernel")
        self.assertEqual(semantix_ablation_spec("planner", False), "planner,kernel")
        self.assertEqual(semantix_ablation_spec("kernel", False), "kernel")
        self.assertEqual(semantix_ablation_spec("all", False), "all")
        self.assertEqual(semantix_ablation_spec("planner", True), "planner")

    def test_standard_protocol_isolates_every_instance(self):
        adapter = SemantixAdapter(SimpleNamespace(protocol="standard"), Path("run"))
        adapter.memory_on = True
        adapter.kernel_root = Path("/state/kernel")
        chosen = [inst("django-1", "django/django"), inst("django-2", "django/django")]

        self.assertEqual(adapter.execution_batches(chosen), [[chosen[0]], [chosen[1]]])
        self.assertNotEqual(adapter.kernel_dir_for(chosen[0]), adapter.kernel_dir_for(chosen[1]))

    def test_grouped_protocol_shares_only_within_repo(self):
        adapter = SemantixAdapter(SimpleNamespace(protocol="grouped"), Path("run"))
        adapter.memory_on = True
        adapter.kernel_root = Path("/state/kernel")
        first = inst("django-1", "django/django")
        second = inst("django-2", "django/django")
        other = inst("flask-1", "pallets/flask")

        self.assertEqual(adapter.execution_batches([first, other, second]), [[first, second], [other]])
        self.assertEqual(adapter.kernel_dir_for(first), adapter.kernel_dir_for(second))
        self.assertNotEqual(adapter.kernel_dir_for(first), adapter.kernel_dir_for(other))

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
    def test_frozen_seed_store_is_copied_once_and_resume_keeps_new_slices(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            seed = root / "seed"
            seed_db = seed / "django__django" / ".semantix" / "project.db.journal"
            seed_db.parent.mkdir(parents=True)
            seed_db.write_text("frozen seed\n", encoding="utf-8")
            adapter = SemantixAdapter.__new__(SemantixAdapter)
            adapter.args = SimpleNamespace(semantix_seed_dir=str(seed))
            adapter.kernel_root = root / "kernel"
            adapter.kernel_root.mkdir()

            adapter._seed_kernel_root()
            copied = adapter.kernel_root / "django__django" / ".semantix" / "project.db.journal"
            self.assertEqual(copied.read_text(encoding="utf-8"), "frozen seed\n")

            copied.write_text("frozen seed\nnew slice\n", encoding="utf-8")
            adapter._seed_kernel_root()
            self.assertEqual(copied.read_text(encoding="utf-8"), "frozen seed\nnew slice\n")

    def test_frozen_seed_store_refuses_to_mix_with_unmarked_state(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            seed = root / "seed"
            seed.mkdir()
            adapter = SemantixAdapter.__new__(SemantixAdapter)
            adapter.args = SimpleNamespace(semantix_seed_dir=str(seed))
            adapter.kernel_root = root / "kernel"
            adapter.kernel_root.mkdir()
            (adapter.kernel_root / "existing").write_text("untracked state", encoding="utf-8")

            with self.assertRaisesRegex(RuntimeError, "already contains unseeded state"):
                adapter._seed_kernel_root()

    def test_relative_cli_paths_are_resolved_before_workspace_chdir(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            args = SimpleNamespace(
                dataset="data/verified.jsonl",
                ids="subsets/ids.txt",
                results_dir="results",
                work_dir="work",
                state_dir="state",
                prices="",
                custom_spec="",
                semantix_bin="bin/semantix-agent",
                semantix_kernel_bin="bin/semantix",
                semantix_seed_dir="seed",
                codex_bin="",
            )

            run_bench.resolve_cli_paths(args, root)

            for name in (
                "dataset", "ids", "results_dir", "work_dir", "state_dir",
                "semantix_bin", "semantix_kernel_bin", "semantix_seed_dir",
            ):
                self.assertTrue(Path(getattr(args, name)).is_absolute(), name)
            self.assertEqual(args.results_dir, str(root / "results"))
            self.assertEqual(args.state_dir, str(root / "state"))
            self.assertEqual(args.prices, "")

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
            workspace = root / "workspace"
            workspace.mkdir()
            adapter._write_home(home, root / "sessions", django_dir, workspace)
            config = (home / "config.toml").read_text()
            self.assertIn(f'project_dir  = "{django_dir}"', config)
            self.assertIn(f'workspace_dir = "{workspace}"', config)
            self.assertNotIn(str(flask_dir), config)

    def test_extraction_uses_same_repo_store_and_real_project_identity(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            adapter = self.adapter(root)
            sessions = root / "sessions"
            sessions.mkdir()
            workspace = root / "workspace"
            source = workspace / "pkg" / "cache.py"
            source.parent.mkdir(parents=True)
            source.write_text("value = 1\n", encoding="utf-8")
            (sessions / "turn.jsonl").write_text(
                '{"role":"assistant","tool_calls":[{"id":"1","name":"read_file",'
                '"arguments":"{\\"path\\":\\"pkg/cache.py\\"}"}]}\n', encoding="utf-8")
            kernel_dir = adapter.kernel_dir_for(inst("d", "django/django"))
            completed = SimpleNamespace(returncode=0, stdout="stored=1", stderr="")
            with mock.patch("run_bench.subprocess.run", return_value=completed) as run:
                result = adapter._extract_slices(
                    sessions, "d", kernel_dir, "django/django", workspace, "abc123")
            self.assertEqual(result["mirrors"], 1)
            command = run.call_args.args[0]
            self.assertEqual(command[command.index("--project") + 1], "django/django")
            self.assertEqual(Path(command[command.index("--project-db") + 1]),
                             kernel_dir / ".semantix" / "project.db")
            self.assertEqual(command[command.index("--base-commit") + 1], "abc123")
            self.assertEqual(command[command.index("--fingerprint") + 1], "pkg/cache.py")


if __name__ == "__main__":
    unittest.main()
