#!/usr/bin/env python3
import unittest
from pathlib import Path
from types import SimpleNamespace

from run_bench import SemantixAdapter, arm_label, kernel_protocol_state


def args(protocol="grouped", semantix_memory="on", ablate=""):
    return SimpleNamespace(protocol=protocol, semantix_memory=semantix_memory,
                           ablate=ablate, openai_base="http://x/v1",
                           semantix_retrieval_mode="strict")


def inst(iid: str, repo: str) -> dict:
    return {"instance_id": iid, "repo": repo}


class ArmLabelTests(unittest.TestCase):
    def test_full_and_no_kernel_arms(self):
        self.assertEqual(arm_label("semantix", "", True), "full")
        self.assertEqual(arm_label("semantix", "", False), "no-kernel")

    def test_harness_modules_ordered_like_go_modules(self):
        self.assertEqual(arm_label("semantix", "planner,evidence", True),
                         "no-evidence+no-planner")
        self.assertEqual(arm_label("semantix", "all", False),
                         "no-evidence+no-planner+no-subagent+no-retrieval"
                         "+no-compaction+no-kernel")

    def test_memory_flag_only_affects_semantix(self):
        self.assertEqual(arm_label("dsh", "", False), "full")


class ProtocolTests(unittest.TestCase):
    def adapter(self, a) -> SemantixAdapter:
        adapter = SemantixAdapter(a, Path("run"))
        adapter.memory_on = a.semantix_memory == "on"
        # Reuse the production derivation (kernel_protocol_state) instead of
        # re-implementing it, so the tests exercise the same gating lines the
        # runner calls in prepare().
        adapter.kernel_on, adapter.protocol, adapter.standard = kernel_protocol_state(
            adapter.memory_on, a.ablate, a.protocol)
        adapter.arm = arm_label("semantix", a.ablate, adapter.memory_on)
        adapter.kernel_root = Path("/k")
        return adapter

    def test_grouped_keeps_repo_course(self):
        adapter = self.adapter(args())
        chosen = [inst("django-1", "django/django"), inst("flask-1", "pallets/flask"),
                  inst("django-2", "django/django")]
        batches = adapter.execution_batches(chosen)
        self.assertEqual([[x["instance_id"] for x in b] for b in batches],
                         [["django-1", "django-2"], ["flask-1"]])

    def test_standard_protocol_isolates_instances(self):
        adapter = self.adapter(args(protocol="standard"))
        chosen = [inst("django-1", "django/django"), inst("django-2", "django/django")]
        batches = adapter.execution_batches(chosen)
        self.assertEqual([len(b) for b in batches], [1, 1])

    def test_kernel_dir_isolation_standard_vs_grouped(self):
        grouped = self.adapter(args())
        standard = self.adapter(args(protocol="standard"))
        self.assertEqual(grouped.kernel_dir_for(inst("d1", "django/django")),
                         Path("/k/django__django"))
        self.assertEqual(standard.kernel_dir_for(inst("d1", "django/django")),
                         Path("/k/std/d1"))
        self.assertNotEqual(standard.kernel_dir_for(inst("d1", "django/django")),
                            standard.kernel_dir_for(inst("d2", "django/django")))

    def test_memory_off_scheduling_unchanged(self):
        adapter = self.adapter(args(semantix_memory="off"))
        chosen = [inst("a", "django/django"), inst("b", "django/django")]
        self.assertEqual([len(b) for b in adapter.execution_batches(chosen)], [1, 1])

    def test_ablate_kernel_disables_reuse_course_and_protocol(self):
        # boot gates the bridge on Ablation.Off(ablation.Kernel), so
        # --ablate kernel (and --ablate all) with --semantix-memory on still
        # means no live kernel: arm carries no-kernel, protocol must not say
        # grouped, and scheduling must not form a same-repo course.
        for spec in ("kernel", "all"):
            with self.subTest(ablate=spec):
                adapter = self.adapter(args(ablate=spec))
                self.assertEqual(adapter.protocol, "")
                self.assertTrue(adapter.arm.endswith("no-kernel"),
                                adapter.arm)
                if spec == "kernel":
                    self.assertEqual(adapter.arm, "no-kernel")
                chosen = [inst("django-1", "django/django"),
                          inst("django-2", "django/django")]
                batches = adapter.execution_batches(chosen)
                self.assertEqual([len(b) for b in batches], [1, 1])


if __name__ == "__main__":
    unittest.main()
