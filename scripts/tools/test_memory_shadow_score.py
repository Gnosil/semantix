import unittest

from memory_shadow_score import ARMS, score


def replay_task(task_id, outcomes):
    return {"id": task_id, "arms": dict(zip(ARMS, outcomes))}


def result(passed, costs=None, elapsed=None):
    return {"passed": passed, "attempt_cost_microusd": costs, "elapsed_ms": elapsed}


class MemoryShadowScoreTest(unittest.TestCase):
    def test_paired_flips_survive_equal_aggregate_pass_counts(self):
        report = score({"kind": "synthetic", "tasks": [
            replay_task("kept", [result(True, [1], 10), result(False, [2, 3], 40),
                                 result(True, [0], 12)]),
            replay_task("rescued", [result(False, [4], 20), result(True, [6], 35),
                                    result(True, [0], 15)]),
        ]})
        self.assertEqual(report["paired_tasks"], 2)
        self.assertEqual(report["comparisons"]["off_to_current"], {
            "newly_fixed": ["rescued"], "newly_broken": ["kept"],
        })
        self.assertEqual(report["comparisons"]["current_to_revision_aware"], {
            "newly_fixed": ["kept"], "newly_broken": [],
        })
        self.assertEqual(report["arms"]["current"]["cost_per_pass_microusd"], 11)
        self.assertEqual(report["arms"]["current"]["p95_elapsed_ms"], 40)
        self.assertEqual(report["arms"]["revision_aware"]["total_cost_microusd"], 0)

    def test_missing_measurements_do_not_become_zero_or_bias_the_sample(self):
        report = score({"kind": "synthetic", "tasks": [
            replay_task("one", [result(False, [0], 0), result(True, [2, None], 10),
                                result(True)]),
            replay_task("two", [result(False, [7], 10), result(True, [3]),
                                result(True)]),
        ]})
        off = report["arms"]["off"]
        self.assertEqual(off["total_cost_microusd"], 7)
        self.assertIsNone(off["cost_per_pass_microusd"])
        current = report["arms"]["current"]
        self.assertEqual(current["known_cost_tasks"], 1)
        self.assertEqual(current["known_latency_tasks"], 1)
        self.assertIsNone(current["cost_per_pass_microusd"])
        self.assertIsNone(current["p95_elapsed_ms"])

    def test_rejects_unpaired_duplicate_and_malformed_outcomes(self):
        task = replay_task("safe", [result(True), result(True), result(True)])
        with self.assertRaisesRegex(ValueError, "duplicate anonymous task id"):
            score({"kind": "recorded", "tasks": [task, task]})
        for arm, fields, reason in (
            ("off", {"passed": 1}, "Boolean"),
            ("current", result(True, []), "cost"),
            ("revision_aware", result(True, [1], -1), "elapsed"),
        ):
            malformed = dict(task, arms=dict(task["arms"], **{arm: fields}))
            with self.subTest(arm=arm), self.assertRaisesRegex(ValueError, reason):
                score({"kind": "recorded", "tasks": [malformed]})
        del task["arms"]["revision_aware"]
        with self.assertRaisesRegex(ValueError, "arm set"):
            score({"kind": "synthetic", "tasks": [task]})
