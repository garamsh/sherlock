"""Tests of the rules score.py adds for #900."""

import importlib.util
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("paradigm_score", HERE / "score.py")
S = importlib.util.module_from_spec(spec)
spec.loader.exec_module(S)


def rows(family, applied):
    return {f"{family}-{i:03d}": {"family": family, "applied": a} for i, a in enumerate(applied)}


class Supported(unittest.TestCase):
    def test_a_fact_is_supported_only_by_sources_stating_its_values(self):
        sources = {1: "Tomas Wigrull answers on extension 5391.", 2: "Chloe Gaimoull shares an office with Tomas Wigrull."}
        self.assertFalse(S.supported({"statement": "Chloe Gaimoull answers on extension 5391.", "sources": [1]}, sources))
        self.assertTrue(S.supported({"statement": "Tomas Wigrull answers on extension 5391.", "sources": [1]}, sources))

    def test_the_first_word_is_not_required(self):
        sources = {3: "I've changed dentist: from now on it's Sofia Cukais."}
        self.assertTrue(S.supported({"statement": "The user's dentist is Sofia Cukais.", "sources": [3]}, sources))


class Decision(unittest.TestCase):
    def per(self, r_primary, r_n):
        base = {**rows("C2", [False] * 20), **rows("N", [True] * 10)}
        r = {**rows("C2", r_primary), **rows("N", r_n)}
        return {"S/b4": base, "R/b4-agent": r}

    def test_a_significant_arm_holding_its_guardrails_is_valid(self):
        out = S.compare_live(self.per([True] * 12 + [False] * 8, [True] * 8 + [False] * 2))
        self.assertTrue(out["R/b4-agent"]["significant"])
        self.assertEqual(out["R/b4-agent"]["guard_change"]["N"], -2)
        self.assertTrue(out["R/b4-agent"]["valid"])

    def test_losing_more_than_the_guardrail_allows_invalidates(self):
        out = S.compare_live(self.per([True] * 12 + [False] * 8, [True] * 6 + [False] * 4))
        self.assertTrue(out["R/b4-agent"]["significant"], "the control: the gain alone is significant")
        self.assertFalse(out["R/b4-agent"]["guardrails_hold"])
        self.assertFalse(out["R/b4-agent"]["valid"])

    def test_w_is_never_compared_for_the_decision(self):
        per = self.per([True] * 12 + [False] * 8, [True] * 10)
        per["W/w180k"] = per["R/b4-agent"]
        self.assertNotIn("W/w180k", S.compare_live(per))


if __name__ == "__main__":
    unittest.main()
