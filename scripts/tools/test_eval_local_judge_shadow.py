import unittest

from eval_local_judge_shadow import Attempt, Pair, assess, fixture


class LocalJudgeShadowTest(unittest.TestCase):
    def test_false_accepts_reported_without_replacing_reference(self):
        result = assess(*fixture())
        self.assertEqual("synthetic_offline_shadow", result["mode"])
        self.assertIs(result["promotion_enabled"], False)
        self.assertEqual(2, result["reference"]["accepted"])
        self.assertEqual(0, result["reference"]["false_accepts"])
        shadow = result["shadow"]
        self.assertEqual((6, 1, 1), (shadow["evaluated"], shadow["errors"], shadow["timeouts"]))
        self.assertEqual((3, 1, 2), (
            shadow["candidate_approvals"], shadow["true_accepts"], shadow["false_accepts"],
        ))
        self.assertEqual(2 / 6, shadow["false_accept_rate_among_unsafe"])
        self.assertEqual(2 / 3, shadow["false_share_of_approvals"])
        self.assertEqual(2, shadow["disagreements_among_evaluated"])
        self.assertEqual(1, result["by_category"]["negation"]["false_accepts"])
        self.assertEqual(1, result["by_category"]["version"]["false_accepts"])
        self.assertEqual(0, result["by_category"]["scope"]["false_accepts"])
        self.assertEqual(100, shadow["modeled_latency"]["p99_ms"])

    def test_unavailable_scores_count_in_latency_but_never_approve(self):
        pairs = tuple(Pair(str(i), "unsafe", False, False, 200) for i in range(6))
        states = (
            Attempt(1.0, 100), Attempt(None, 90), Attempt(float("inf"), 80),
            Attempt(1.01, 70), Attempt(-0.1, 60), Attempt(True, 50),
        )
        result = assess(pairs, {row.id: attempt for row, attempt in zip(pairs, states)}.__getitem__)
        shadow = result["shadow"]
        self.assertEqual((0, 5, 1), (shadow["evaluated"], shadow["errors"], shadow["timeouts"]))
        self.assertEqual(0, shadow["candidate_approvals"])
        self.assertIsNone(shadow["false_share_of_approvals"])
        self.assertEqual({"p50_ms": 70, "p95_ms": 100, "p99_ms": 100},
                         shadow["modeled_latency"])

    def test_invalid_configuration_fails_before_scoring(self):
        for threshold, timeout in ((-0.1, 100), (1.1, 100), (float("nan"), 100), (0.9, 0)):
            with self.subTest(threshold=threshold, timeout=timeout), self.assertRaises(ValueError):
                assess((Pair("x", "unsafe", False, False, 1),),
                       lambda _: self.fail("invalid fixture tried to score"),
                       threshold, timeout)


if __name__ == "__main__":
    unittest.main()
