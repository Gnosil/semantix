#!/usr/bin/env python3
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace

from run_bench import SemantixAdapter


class ShadowConfigTests(unittest.TestCase):
    def test_write_home_emits_explicit_shadow_mode(self):
        adapter = SemantixAdapter.__new__(SemantixAdapter)
        adapter.args = SimpleNamespace(
            openai_base="http://127.0.0.1:8139/v1",
            effort="",
            model="deepseek-v4-flash",
            semantix_retrieval_mode="shadow",
        )
        adapter.memory_on = True
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            adapter.kernel_root = root / "kernel"
            kernel_dir = adapter.kernel_dir_for(
                {"instance_id": "django-1", "repo": "django/django"})
            adapter._write_home(root / "home", root / "sessions", kernel_dir)
            config = (root / "home" / "config.toml").read_text()
        self.assertIn('mode         = "shadow"', config)
        self.assertIn(f'project_dir  = "{kernel_dir}"', config)
        self.assertIn("enabled      = true", config)


if __name__ == "__main__":
    unittest.main()
