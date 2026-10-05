#!/usr/bin/env python3
"""Regression tests for current-attempt Semantix native metrics."""

from __future__ import annotations

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock


HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

from run_bench import SemantixAdapter  # noqa: E402


class SemantixAttemptMetricsTest(unittest.TestCase):
    @staticmethod
    def _args() -> SimpleNamespace:
        return SimpleNamespace(
            openai_base="",
            effort="",
            model="deepseek-v4-flash",
            semantix_retrieval_mode="off",
            preset="balanced",
            ablate="",
            timeout=30,
        )

    def _adapter(self, root: Path) -> SemantixAdapter:
        adapter = SemantixAdapter(self._args(), root / "run")
        adapter.binary = "mock-semantix-agent"
        adapter.home = root / "home"
        adapter.memory_on = False
        return adapter

    @staticmethod
    def _metrics_path(root: Path) -> Path:
        return root / "run" / "native" / "i1.semantix.json"

    @staticmethod
    def _archive_files(root: Path) -> list[Path]:
        native = root / "run" / "native"
        return sorted(native.glob("i1.semantix.json.attempts/attempt-*/*"))

    def _run_attempts(self, setup, outputs):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        root = Path(tmp.name)
        mfile = self._metrics_path(root)
        mfile.parent.mkdir(parents=True)
        setup(mfile)
        adapter = self._adapter(root)
        output_iter = iter(outputs)
        calls = []

        def fake_process(cmd, **kwargs):
            calls.append(cmd)
            output = next(output_iter)
            outcome = output(mfile)
            if outcome is not None:
                raise outcome
            return subprocess.CompletedProcess(cmd, 0, stdout="", stderr="")

        with mock.patch("run_bench.subprocess.run", side_effect=fake_process):
            results = [adapter.run_instance(root, "prompt", {"instance_id": "i1"})
                       for _ in outputs]

        self.assertEqual(len(calls), len(outputs))
        return root, results

    def test_stale_complete_is_not_selected_over_fresh_partial(self) -> None:
        stale = b'{"steps": 36, "prompt_tokens": 3600}'
        stale_partial = b'{"steps": 35, "prompt_tokens": 3500}'
        fresh = b'{"steps": 14, "prompt_tokens": 1400}'

        def setup(mfile: Path) -> None:
            mfile.write_bytes(stale)
            Path(str(mfile) + ".partial").write_bytes(stale_partial)

        def timeout_with_partial(mfile: Path):
            Path(str(mfile) + ".partial").write_bytes(fresh)
            return subprocess.TimeoutExpired("mock-semantix-agent", 30)

        root, results = self._run_attempts(setup, [timeout_with_partial])
        self.assertEqual(results[0][1]["steps"], 14)
        archived = self._archive_files(root)
        self.assertEqual([path.read_bytes() for path in archived], [stale, stale_partial])

    def test_stale_only_is_not_selected_when_retry_writes_no_metrics(self) -> None:
        stale = b'{"steps": 36, "prompt_tokens": 3600}'

        def setup(mfile: Path) -> None:
            mfile.write_bytes(stale)

        def timeout_without_output(mfile: Path):
            return subprocess.TimeoutExpired("mock-semantix-agent", 30)

        root, results = self._run_attempts(setup, [timeout_without_output])
        self.assertEqual(results[0][1], {})
        self.assertEqual(self._archive_files(root)[0].read_bytes(), stale)

    def test_fresh_complete_remains_preferred(self) -> None:
        complete = b'{"steps": 27, "prompt_tokens": 2700}'
        partial = b'{"steps": 9, "prompt_tokens": 900}'

        def write_outputs(mfile: Path) -> None:
            mfile.write_bytes(complete)
            Path(str(mfile) + ".partial").write_bytes(partial)

        root, results = self._run_attempts(lambda mfile: None, [write_outputs])
        self.assertEqual(results[0][1]["steps"], 27)
        self.assertEqual(self._archive_files(root), [])

    def test_malformed_complete_falls_back_to_fresh_partial(self) -> None:
        partial = b'{"steps": 8, "prompt_tokens": 800}'

        def write_outputs(mfile: Path) -> None:
            mfile.write_bytes(b'{malformed')
            Path(str(mfile) + ".partial").write_bytes(partial)

        root, results = self._run_attempts(lambda mfile: None, [write_outputs])
        self.assertEqual(results[0][1]["steps"], 8)
        self.assertEqual(Path(str(self._metrics_path(root))).read_bytes(), b'{malformed')

    def test_retries_archive_every_earlier_metric_bytes(self) -> None:
        first = b'{"steps": 11, "prompt_tokens": 1100}'
        second = b'{"steps": 22, "prompt_tokens": 2200}'
        third = b'{"steps": 33, "prompt_tokens": 3300}'

        def write_complete(payload: bytes):
            def writer(mfile: Path) -> None:
                mfile.write_bytes(payload)
            return writer

        def write_partial(mfile: Path) -> None:
            Path(str(mfile) + ".partial").write_bytes(second)

        root, results = self._run_attempts(
            lambda mfile: None,
            [write_complete(first), write_partial, write_complete(third)],
        )
        self.assertEqual([result[1].get("steps", 0) for result in results], [11, 22, 33])
        archived = self._archive_files(root)
        self.assertEqual([path.read_bytes() for path in archived], [first, second])
        self.assertEqual(self._metrics_path(root).read_bytes(), third)


if __name__ == "__main__":
    unittest.main()
