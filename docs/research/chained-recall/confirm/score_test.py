"""Tests of the #885 scorer's additions to ../score.py."""

import unittest

import score


def rows(family, applied):
    return {f"{family}-{i:02d}": {"family": family, "applied": a} for i, a in enumerate(applied)}


def arm(primary, n=60, k=40, c1=30, e=30):
    """Per-probe rows: `primary` applied C2 probes of 100 primary, and n, k,
    c1, e applied guard probes."""
    out = {}
    out.update(rows("C2", [i < primary for i in range(100)]))
    out.update(rows("N", [i < n for i in range(60)]))
    out.update(rows("K", [i < k for i in range(40)]))
    out.update(rows("C1", [i < c1 for i in range(30)]))
    out.update(rows("E", [i < e for i in range(30)]))
    return out


class DecisionTest(unittest.TestCase):
    def test_both_valid_takes_the_smaller_arm(self):
        per = {"S/b4": arm(5, n=50), "L/b4-model": arm(60, n=47), "L/b4-model-a": arm(55, n=50)}
        c = score.compare(per)
        self.assertTrue(c["L/b4-model"]["valid"])
        self.assertTrue(c["L/b4-model-a"]["valid"])
        self.assertEqual(score.smallest_valid(c), "L/b4-model")

    def test_a_guard_loss_past_its_allowance_invalidates_the_arm(self):
        per = {"S/b4": arm(5, n=50, k=35), "L/b4-model": arm(60, n=46), "L/b4-model-a": arm(55, n=50, k=33)}
        c = score.compare(per)
        self.assertEqual(c["L/b4-model"]["guard_change"]["N"], -4)
        self.assertFalse(c["L/b4-model"]["valid"], "N loses 4, 3 allowed")
        self.assertEqual(c["L/b4-model-a"]["guard_change"]["K"], -2)
        self.assertTrue(c["L/b4-model-a"]["valid"], "the control: K loses 2, 2 allowed")
        self.assertEqual(score.smallest_valid(c), "L/b4-model-a")

    def test_c1_and_e_allow_three_probes(self):
        per = {"S/b4": arm(5, c1=30, e=30), "L/b4-model": arm(60, c1=27, e=27), "L/b4-model-a": arm(60, c1=26)}
        c = score.compare(per)
        self.assertTrue(c["L/b4-model"]["guardrails_hold"])
        self.assertFalse(c["L/b4-model-a"]["guardrails_hold"])

    def test_no_significant_gain_leaves_no_arm(self):
        per = {"S/b4": arm(5), "L/b4-model": arm(7), "L/b4-model-a": arm(6)}
        c = score.compare(per)
        self.assertFalse(c["L/b4-model"]["significant"])
        self.assertIsNone(score.smallest_valid(c))


class LabelTest(unittest.TestCase):
    def test_every_walk_line_must_carry_the_label(self):
        good = "- [4] (historical): a\n- [9] (associated): " + score.LABEL + ' "x", not matched to the question: b [mid:9 sources:1-4]'
        self.assertTrue(score.walk_in_recall(good))
        self.assertTrue(score.label_ok(good))
        self.assertFalse(score.label_ok(good + "\n- [11] (linked): c"), "an unlabelled walk line")
        self.assertFalse(score.label_ok("- [9] (associated): b"), "a line without the label text")
        self.assertFalse(score.walk_in_recall("- [4] (historical): a"), "the control: no walk line")


if __name__ == "__main__":
    unittest.main()
