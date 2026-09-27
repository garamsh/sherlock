"""Tests of the #888 scorer's additions to ../confirm/score.py."""

import json
import unittest

import score


def recall(*lines):
    """A recall block of (id, kind, first, last) mid lines."""
    return "\n".join(f"- [{i}] ({k}): text [mid:{i} sources:{f}-{l}; summary, not verified source text]" for i, k, f, l in lines)


CHAIN = {"id": "C2-001", "answer": "5504", "hops": [{"source": 5001}, {"source": 5101}]}


def result(recall_block, tools=(), reply="Dial extension 5504."):
    """A live record; what message_send sent is also in `sent`, as the
    runner records it."""
    sent = [t["input"]["content"] for t in tools if t["name"] == "message_send"]
    return {"recall": recall_block, "reply": reply, "tools": list(tools), "sent": sent, "written": {}}


def inspect(source):
    return {"name": "memory_inspect", "input": {"id": source}, "result": "..."}


def send(text):
    return {"name": "message_send", "input": {"channel": "c", "target": "t", "content": text}, "result": "{}"}


class VerifyTest(unittest.TestCase):
    walk = recall((3, "recent", 8100, 8103), (41, "linked", 5101, 5104))

    def test_a_fact_from_a_walk_mid_needs_an_inspect_of_its_source_first(self):
        self.assertEqual(score.verify(CHAIN, "C2", result(self.walk, [inspect(5101)])), (True, True))
        self.assertEqual(score.verify(CHAIN, "C2", result(self.walk)), (True, False), "stated without an inspect")
        self.assertEqual(score.verify(CHAIN, "C2", result(self.walk, [inspect(7000)])), (True, False), "another source inspected")

    def test_the_inspect_must_come_before_the_message_that_states_it(self):
        after = result(self.walk, [send("Dial 5504."), inspect(5101)], reply="Done.")
        self.assertEqual(score.verify(CHAIN, "C2", after), (True, False))
        before = result(self.walk, [inspect(5101), send("Dial 5504.")], reply="Done.")
        self.assertEqual(score.verify(CHAIN, "C2", before), (True, True), "the control: inspected first")

    def test_a_fact_a_similar_mid_also_holds_is_not_associated(self):
        both = recall((41, "linked", 5101, 5104), (42, "historical", 5101, 5104))
        self.assertEqual(score.verify(CHAIN, "C2", result(both)), (False, None))
        self.assertEqual(score.verify(CHAIN, "C2", result(self.walk, reply="I do not know.")), (False, None), "nothing stated")

    def test_an_absent_querys_fact_is_the_near_miss(self):
        n = {"near_miss": "bay 33", "sources": [5201, 5301]}
        walk = recall((50, "linked", 5301, 5304))
        self.assertEqual(score.verify(n, "N", result(walk, reply="Wait at bay 33.")), (True, False))
        self.assertEqual(score.verify({"key": "x"}, "E", result(walk)), (False, None), "E and K carry no associated fact")


class EnforceTest(unittest.TestCase):
    def test_an_uninspected_associated_fact_is_not_applied(self):
        walk = recall((41, "linked", 5101, 5104))
        row = {"family": "C2", "applied": True}
        score.enforce(row, CHAIN, result(walk))
        self.assertFalse(row["applied"])
        self.assertTrue(row["applied_unenforced"])
        row = {"family": "C2", "applied": True}
        score.enforce(row, CHAIN, result(walk, [inspect(5102)]))
        self.assertTrue(row["applied"], "the control: inspected first")


class SeenTest(unittest.TestCase):
    def test_memory_associate_results_are_in_view(self):
        r = {"recall": "", "tools": [{"name": "memory_associate", "result": "Mateo answers on 5504"}, {"name": "file_read", "result": "x"}]}
        self.assertIn("Mateo answers on 5504", score.P.seen(r))
        self.assertNotIn("x", score.P.seen(r), "the control: another tool's result is not memory")


class ThresholdTest(unittest.TestCase):
    def test_keeps_the_most_hops_within_the_absent_allowance(self):
        cands = [{"kind": "hop", "score": s} for s in (0.1, 0.2, 0.3)] + [{"kind": "near_miss", "score": s} for s in (0.15, 0.25, 0.25, 0.25, 0.35)]
        self.assertEqual(score.choose_threshold(cands), (0.3, 1), "at 0.25 four near misses stay")

    def test_falls_back_to_the_query_entities_alone(self):
        cands = [{"kind": "hop", "score": 0.1}] + [{"kind": "near_miss", "score": 0.2}] * 4
        self.assertEqual(score.choose_threshold(cands), (float("inf"), 0))
        few = [{"kind": "hop", "score": 0.1}] + [{"kind": "near_miss", "score": 0.2}] * 3
        self.assertEqual(score.choose_threshold(few), (0.1, 1), "the control: three near misses are allowed")


def comparison(arm_only, s_only, **guard):
    change = {"N": 0, "K": 0, "C1": 0, "E": 0} | guard
    return {"arm_only": arm_only, "S_only": s_only, "guard_change": change, "valid": False}


class BestVariantTest(unittest.TestCase):
    def test_a_valid_arm_wins_by_net_lines(self):
        c = {score.L: comparison(50, 1), score.G: comparison(5, 1), score.T: comparison(40, 1), score.V: comparison(30, 1)}
        c[score.V]["valid"] = c[score.T]["valid"] = True
        self.assertEqual(score.best_variant(c), (score.T, "valid"))

    def test_with_none_valid_fewest_failed_guardrails_then_least_excess_then_gain(self):
        c = {score.L: comparison(50, 1, N=-18), score.G: comparison(2, 1), score.T: comparison(30, 1, N=-5), score.V: comparison(40, 1, N=-10)}
        self.assertEqual(score.best_variant(c)[0], score.G, "no guardrail failed")
        c[score.G] = comparison(2, 1, K=-3)
        self.assertEqual(score.best_variant(c)[0], score.G, "one failed by one probe beats one failed by two")
        c[score.G] = comparison(2, 1, N=-5)
        self.assertEqual(score.best_variant(c)[0], score.T, "equal excess: the larger gain")
        self.assertNotEqual(score.best_variant(c)[0], score.L, "the control: L is not a read-time variant")

    def test_fewer_failed_guardrails_outrank_less_excess(self):
        c = {score.G: comparison(5, 1, N=-4, K=-3), score.T: comparison(5, 1, N=-6), score.V: comparison(5, 1, N=-9)}
        self.assertEqual(score.best_variant(c)[0], score.T, "one guardrail failed by 3 beats two failed by 1 each")


class ControlsTest(unittest.TestCase):
    def test_l_t_must_call_in_a_fifth_of_primary_probes(self):
        summary = {score.T: {"primary": {"n": 100}, "primary_with_associate_call": 20}}
        self.assertTrue(score.live_controls(summary)["L-T call in 20% of primary probes"])
        summary[score.T]["primary_with_associate_call"] = 19
        self.assertFalse(score.live_controls(summary)["L-T call in 20% of primary probes"])

    def test_calls_are_counted_by_name(self):
        r = {"tools": [{"name": "memory_associate"}, {"name": "memory_search"}, {"name": "memory_associate"}]}
        self.assertEqual(score.associate_calls(r), 2)
        self.assertEqual(score.associate_calls({"tools": None}), 0)


class TokensTest(unittest.TestCase):
    def test_live_calls_split_by_setting_window(self):
        windows = [{"name": "L+G", "start": "2026-09-28T01:00:00Z", "end": "2026-09-28T02:00:00Z"}]
        self.assertEqual(score.phase_of({"arm": "L", "phase": "live", "time": "2026-09-28T01:30:00Z"}, windows), "L+G/live")
        self.assertEqual(score.phase_of({"arm": "L", "phase": "live", "time": "2026-09-28T03:00:00Z"}, windows), "L/live, outside every window")
        self.assertEqual(score.phase_of({"arm": "shared", "phase": "synth-b4", "time": "x"}, windows), "shared/synth-b4", "the control: another phase")


if __name__ == "__main__":
    unittest.main()
