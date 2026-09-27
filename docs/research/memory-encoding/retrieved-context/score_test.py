"""Tests for score.py: carried reach, the controls, stale copies, the
screen's links, admission and the decision.

    cd docs/research/memory-encoding/retrieved-context && PYTHONDONTWRITEBYTECODE=1 python3 -m unittest score_test
"""

import unittest

import score as S


def item(first, last, text, level="mid"):
    return {"id": first, "level": level, "kind": "historical", "first": first, "last": last, "text": text}


HOP1 = {"source": 5001, "key": "Zevai Components Chloe Traike"}
HOP2 = {"source": 7001, "key": "Chloe Traike 6200"}


class CarriedReach(unittest.TestCase):
    def test_a_later_mid_that_states_the_hop_carries_it(self):
        later = item(7001, 7004, "Chloe Traike, who looks after Zevai Components, is on 6200.")
        self.assertTrue(S.carried([later], HOP1), "the hop's entities stated by a mid that does not cover it")
        self.assertTrue(S.carried([later], HOP2))

    def test_an_item_that_does_not_state_both_entities_does_not(self):
        self.assertFalse(S.carried([item(5001, 5004, "An account manager was named.")], HOP1))
        self.assertFalse(S.carried([item(5001, 5004, "Chloe Traike was named.")], HOP1))

    def test_score_setting_keeps_strict_reach_beside_carried(self):
        c = {"chains": [{"id": "C2-1", "family": "C2", "k": 2, "hops": [HOP1, HOP2]}], "guards": []}
        items = [item(7001, 7004, "Chloe Traike, who looks after Zevai Components, is on 6200.")]
        probes = [{"probe": "C2-1", "items": items, "context_chars": 10}]
        rows = S.score_setting(c, probes, [])
        self.assertTrue(rows["C2-1"]["reached"])
        self.assertFalse(rows["C2-1"]["reached_strict"], "#890's rule needs hop 1's own mid")


def rec(first, messages, mids, **kw):
    return {"first": first, "last": first + 3, "messages": messages, "mids": [{"id": m[0], "first": m[1], "last": m[2]} for m in mids], **kw}


class RetrievalControl(unittest.TestCase):
    def test_passes_when_everything_read_ends_before_the_batch(self):
        recorded = {9: rec(9, [1, 8], [(1, 1, 4), (2, 5, 8)], summary="s"), 13: rec(13, [12], [(3, 9, 12)], summary="s")}
        self.assertTrue(S.retrieval_control(recorded, [9, 13])["passed"])

    def test_fails_on_a_source_at_or_after_the_batch(self):
        for recorded in ({9: rec(9, [9], [], summary="s")}, {9: rec(9, [], [(3, 9, 12)], summary="s")}):
            got = S.retrieval_control(recorded, [9])
            self.assertEqual(got["late"], 1)
            self.assertFalse(got["passed"])

    def test_fails_over_the_bounds_missing_batches_or_too_many_failures(self):
        self.assertFalse(S.retrieval_control({9: rec(9, list(range(9)), [], summary="s")}, [9])["passed"])
        self.assertFalse(S.retrieval_control({9: rec(9, [], [(i, i, i) for i in range(5)], summary="s")}, [9])["passed"])
        self.assertFalse(S.retrieval_control({9: rec(9, [], [], summary="s")}, [9, 13])["passed"])
        firsts = list(range(1, 41, 4))
        recorded = {f: rec(f, [], [], summary="s") for f in firsts}
        self.assertTrue(S.retrieval_control(recorded, firsts)["passed"], "one failure in ten is 90% accepted")
        recorded[1]["failed"] = recorded[5]["failed"] = "no answer"
        self.assertFalse(S.retrieval_control(recorded, firsts)["passed"])


class LineControl(unittest.TestCase):
    def test_the_line_is_the_rendering_of_what_was_named(self):
        r = rec(9, [], [(7, 1, 4)], related={"entities": ["Chloe Traike"], "mids": [7]}, line="Related to: Chloe Traike; earlier mids 7.")
        self.assertTrue(S.line_control({9: r})["passed"])
        self.assertEqual(S.render({"entities": [], "mids": []}), "")

    def test_fails_on_an_unretrieved_mid_a_different_line_or_a_summary(self):
        cases = [
            rec(9, [], [(7, 1, 4)], related={"entities": [], "mids": [8]}, line="earlier mids 8."),
            rec(9, [], [], related={"entities": ["Chloe Traike"], "mids": []}, line="Related to: Chloe Traike, 6200."),
            rec(9, [], [], related={"entities": [], "mids": []}, line="", summary="a summary"),
        ]
        for r in cases:
            self.assertFalse(S.line_control({9: r})["passed"])


class ShownControl(unittest.TestCase):
    def test_every_stream_mid_shows_the_stored_summary(self):
        records = [{"items": [item(4801, 4804, "S one."), item(1, 4, "base")]}]
        self.assertTrue(S.shown_control(records, {4801: "S one."})["passed"])

    def test_the_comparison_can_fail(self):
        records = [{"items": [item(4801, 4804, "S one.")]}]
        self.assertFalse(S.shown_control(records, {4801: "RW one."})["passed"], "S's records fail against RW's summaries")

    def test_a_shown_line_fails(self):
        shown = "S one.\nRelated to: Chloe Traike."
        records = [{"items": [item(4801, 4804, shown)]}]
        got = S.shown_control(records, {4801: shown}, {4801: "Related to: Chloe Traike."})
        self.assertEqual((got["equal"], got["line_shown"]), (1, 1), "the text matches, and the line is in it")
        self.assertFalse(got["passed"])


KU = {"id": "KU-1", "family": "KU", "sources": [5001, 7001], "obsolete": "Nadia Bogea", "key": "Sofia Cukais"}


class StaleCopies(unittest.TestCase):
    def test_counts_later_mids_restating_the_superseded_value(self):
        texts = {
            5001: "My dentist is Nadia Bogea.",
            7001: "Changed dentist from Nadia Bogea to Sofia Cukais.",
            7005: "Dentist Nadia Bogea again.",
            7009: "Sofia Cukais replaced Nadia Bogea.",
            7013: "Unrelated.",
        }
        got = S.stale_copies({"guards": [KU]}, texts)
        self.assertEqual(got["copies"], 2, "the correction's own mid and earlier ones are not after it")
        self.assertEqual(got["copies_without_new"], 1)
        self.assertEqual(got["items_with_copies"], 1)


    def test_a_delivered_copy_after_the_correction_is_a_stale_failure(self):
        self.assertTrue(S.stale_delivered([item(7005, 7008, "Dentist Nadia Bogea again.")], KU))
        self.assertFalse(S.stale_delivered([item(7001, 7004, "Changed dentist from Nadia Bogea to Sofia Cukais.")], KU),
                         "the correction's own mid records the change")
        self.assertFalse(S.stale_delivered([item(5001, 5004, "My dentist is Nadia Bogea.")], KU), "the first statement is no copy")
        self.assertFalse(S.stale_delivered([item(7005, 7008, "Sofia Cukais is the dentist.")], KU))

    def test_a_stale_copy_is_never_ku_reach(self):
        c = {"chains": [], "guards": [dict(KU, evidence=[7001])]}
        probes = [{"probe": "KU-1", "items": [item(7005, 7008, "Sofia Cukais replaced Nadia Bogea.")], "context_chars": 1}]
        row = S.score_setting(c, probes, [])["KU-1"]
        self.assertFalse(row["reached"], "a later mid stating the new value does not cover the correction")
        self.assertTrue(row["stale_delivered"])


class Links(unittest.TestCase):
    def test_a_later_hop_finds_the_earlier_by_message_or_by_mid(self):
        c = {"chains": [{"id": "C2-1", "family": "C2", "hops": [{"source": 7001}, {"source": 5001}]},
                        {"id": "C2-2", "family": "C2", "hops": [{"source": 5101}, {"source": 7101}]},
                        {"id": "C2-3", "family": "C2", "hops": [{"source": 5201}, {"source": 7201}]}],
             "guards": [dict(KU, sources=[5301, 7301])]}
        recorded = {7001: rec(7001, [5001], []), 7101: rec(7101, [], [(9, 5101, 5104)]), 7201: rec(7201, [5202], [(9, 5205, 5208)]),
                    7301: rec(7301, [5301], [])}
        got = S.links(c, recorded)
        self.assertEqual(got["C2"], {"pairs": 3, "by_message": 1, "by_mid": 1, "either": 2}, "hops listed in either order")
        self.assertEqual(got["KU"]["either"], 1)


def base_rows(n, reached):
    return {f"C2-{i}": {"family": "C2", "reached": i < reached} for i in range(n)}


class Admission(unittest.TestCase):
    def test_margin_needs_ten_percent_and_significance(self):
        base = base_rows(100, 0)
        arm = base_rows(100, 10)
        got = S.compare(arm, base, ("C2",))
        self.assertEqual((got["arm"], got["S"], got["arm_only"]), (10, 0, 10))
        self.assertTrue(S.margin_met(got))
        self.assertFalse(S.margin_met(S.compare(base_rows(100, 9), base, ("C2",))), "9 of 100 is short of the margin")
        self.assertFalse(S.margin_met(dict(got, p=0.06)), "the margin without significance")
        self.assertFalse(S.margin_met(S.compare(base_rows(100, 10), base_rows(100, 10), ("C2",))), "no gain over S")


def live_rows(fam, n, applied):
    return {f"{fam}-{i}": {"family": fam, "applied": i < applied} for i in range(n)}


class Decision(unittest.TestCase):
    def per(self, gain):
        rows = {**live_rows("C2", 50, 20 + gain), **live_rows("C3", 50, 10), **live_rows("N", 60, 50)}
        return rows

    def test_rw_key_is_named_before_rw(self):
        per = {"S": self.per(0), "RW": self.per(20), "RW-key": self.per(20)}
        controls = {"RW": {"passed": True}, "RW-key": {"passed": True}}
        got = S.compare_live(per, controls)
        self.assertTrue(got["RW"]["valid"] and got["RW-key"]["valid"])
        self.assertEqual(S.decide(got), "RW-key")

    def test_a_failed_control_voids_an_arm(self):
        per = {"S": self.per(0), "RW": self.per(20), "RW-key": self.per(20)}
        got = S.compare_live(per, {"RW": {"passed": True}, "RW-key": {"passed": False}})
        self.assertFalse(got["RW-key"]["valid"])
        self.assertEqual(S.decide(got), "RW")

    def test_a_broken_guardrail_voids_an_arm(self):
        lossy = self.per(20)
        lossy.update(live_rows("N", 60, 46))
        got = S.compare_live({"S": self.per(0), "RW": lossy}, {"RW": {"passed": True}})
        self.assertEqual(got["RW"]["guard_change"]["N"], -4)
        self.assertFalse(got["RW"]["valid"])
        self.assertIsNone(S.decide(got))


if __name__ == "__main__":
    unittest.main()
