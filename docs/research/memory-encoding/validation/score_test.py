"""Tests of the #890 scorer's own rules: the families it adds, the offline
margin and the decision."""

import unittest

import score


def item(first, last, text, kind="historical"):
    return {"id": first, "level": "mid", "kind": kind, "first": first, "last": last, "text": text}


CORPUS = {
    "chains": [
        {"id": "SC-001", "family": "SC", "k": 1, "intro": {"source": 5},
         "hops": [{"statement": "They answer on extension 4417.", "ends": ["Mara Quelrand", "4417"], "key": "Mara Quelrand 4417", "source": 9}]},
        {"id": "UP-001", "family": "UP", "k": 1, "hops": [{"statement": "My horse is Wouvull.", "ends": ["Wouvull"], "key": "Wouvull", "source": 13}]},
    ],
    "guards": [
        {"id": "KU-001", "family": "KU", "evidence": [21], "sources": [17, 21], "key": "Ines Brast", "obsolete": "Hugo Tell"},
        {"id": "NC-001", "family": "NC", "entity": "Mara Quelrand", "near_miss": "7010", "sources": [25, 29]},
    ],
}


def probe(pid, items):
    return {"probe": pid, "items": items, "context_chars": 100}


class OfflineTest(unittest.TestCase):
    def rows(self, sc, up, ku, nc):
        probes = [probe("SC-001", sc), probe("UP-001", up), probe("KU-001", ku), probe("NC-001", nc)]
        return score.score_setting(CORPUS, probes, [])

    def test_sc_needs_the_evidence_mid_to_name_the_subject(self):
        named = self.rows([item(9, 12, "Mara Quelrand answers on extension 4417.")], [], [], [])
        bare = self.rows([item(9, 12, "They answer on extension 4417.")], [], [], [])
        self.assertTrue(named["SC-001"]["reached"])
        self.assertFalse(bare["SC-001"]["reached"])
        # Control: interval reach holds in both; only the statement differs.
        self.assertEqual(named["SC-001"]["interval"], bare["SC-001"]["interval"])

    def test_up_needs_the_facts_word_from_the_facts_batch(self):
        self.assertTrue(self.rows([], [item(13, 16, "User's horse: Wouvull")], [], [])["UP-001"]["reached"])
        self.assertFalse(self.rows([], [item(1, 4, "User's horse: Wouvull")], [], [])["UP-001"]["reached"])

    def test_ku_counts_the_new_name_and_the_old_alone(self):
        new = self.rows([], [], [item(21, 24, "Now it's Ines Brast.")], [])["KU-001"]
        old = self.rows([], [], [item(17, 20, "My doctor is Hugo Tell.")], [])["KU-001"]
        self.assertTrue(new["reached"] and not new["old_only"])
        self.assertTrue(old["old_only"] and not old["reached"])

    def test_nc_marks_the_near_miss_beside_the_entity(self):
        apart = self.rows([], [], [], [item(29, 32, "The predecessor is on 7010.")])["NC-001"]
        joined = self.rows([], [], [], [item(29, 32, "Context: Mara Quelrand. The predecessor is on 7010.")])["NC-001"]
        self.assertTrue(apart["near_miss_in_view"] and not apart["near_miss_with_entity"])
        self.assertTrue(joined["near_miss_with_entity"])


def live_rows(family, applied):
    return {f"{family}-{i:03d}": {"family": family, "applied": a} for i, a in enumerate(applied)}


class DecisionTest(unittest.TestCase):
    def per(self, arm_up, s_up, arm_n, s_n):
        base = {**live_rows("UP", s_up), **live_rows("N", s_n)}
        arm = {**live_rows("UP", arm_up), **live_rows("N", arm_n)}
        return {"S/b4": base, "F/b4": arm}

    def test_a_significant_gain_with_guards_held_is_valid(self):
        per = self.per([True] * 20 + [False] * 30, [False] * 50, [True] * 60, [True] * 60)
        a = score.compare_live(per)["F/b4"]
        self.assertTrue(a["significant"] and a["guardrails_hold"] and a["valid"])

    def test_losing_more_absent_probes_than_allowed_invalidates(self):
        lost = score.ALLOWED_LOSS["N"] + 1
        per = self.per([True] * 20 + [False] * 30, [False] * 50, [False] * lost + [True] * (60 - lost), [True] * 60)
        a = score.compare_live(per)["F/b4"]
        self.assertTrue(a["significant"])
        self.assertFalse(a["guardrails_hold"] or a["valid"])
        # Control: one probe fewer lost holds.
        per = self.per([True] * 20 + [False] * 30, [False] * 50, [False] * (lost - 1) + [True] * (61 - lost), [True] * 60)
        self.assertTrue(score.compare_live(per)["F/b4"]["valid"])

    def test_the_smallest_valid_arm_is_named(self):
        score.NET_LINES.update({"F/b4": 120, "C/b4-w8": 50})
        try:
            for order in (("F/b4", "C/b4-w8"), ("C/b4-w8", "F/b4")):
                self.assertEqual(score.smallest_valid({n: {"valid": True} for n in order}), "C/b4-w8", order)
            self.assertEqual(score.smallest_valid({"F/b4": {"valid": True}, "C/b4-w8": {"valid": False}}), "F/b4")
            self.assertIsNone(score.smallest_valid({"F/b4": {"valid": False}}))
        finally:
            score.NET_LINES.clear()


class CheckTest(unittest.TestCase):
    def test_the_check_counts_needed_sources_and_the_oracle(self):
        c = {"chains": [{"id": "C2-001", "family": "C2"}, {"id": "SC-001", "family": "SC"}]}
        passing = {"chains": [{"id": "C2-001", "raw": [1, 30], "oracle": [1, 2]}, {"id": "SC-001", "raw": [40], "oracle": [3]}]}
        self.assertTrue(score.check_rows(passing, c)["passed"])
        # A chain's first hop is free; an SC evidence is not.
        leaking = {"chains": [{"id": "C2-001", "raw": [1, 30], "oracle": [1, 2]}, {"id": "SC-001", "raw": [2], "oracle": [3]}]}
        self.assertEqual(score.check_rows(leaking, c)["leaks"], {"SC": 1})
        blind = {"chains": [{"id": "C2-001", "raw": [1, 30], "oracle": [9, 9]}, {"id": "SC-001", "raw": [40], "oracle": [9]}]}
        self.assertFalse(score.check_rows(blind, c)["passed"])


if __name__ == "__main__":
    unittest.main()
