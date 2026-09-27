"""Tests of the #896 scorer's own rules: pointer and SC value reach, each
arm's controls, the live controls and the decision."""

import unittest

import score


def item(first, text, kind="historical", iid=None):
    return {"id": iid if iid is not None else first, "level": "mid", "kind": kind, "first": first, "last": first + 3, "text": text}


HOP = {"statement": "Chloe Traike answers on extension 6200.", "ends": ["Chloe Traike", "6200"], "key": "Chloe Traike 6200", "source": 9}
POINTER = 'Linked through "chloe traike"; content not shown, read its sources with memory_inspect.'


class ReachTest(unittest.TestCase):
    def test_a_pointer_through_one_of_the_hops_entities_reaches_it(self):
        self.assertEqual(score.hop_reached([item(9, POINTER, "linked")], HOP), (True, True))

    def test_a_pointer_through_another_entity_does_not(self):
        other = 'Linked through "zevai components"; content not shown, read its sources with memory_inspect.'
        self.assertEqual(score.hop_reached([item(9, other, "linked")], HOP), (True, False))
        # Control: the text rule still counts a summary that states the hop.
        self.assertEqual(score.hop_reached([item(9, "Chloe Traike is on extension 6200.")], HOP), (True, True))

    def test_a_pointer_elsewhere_reaches_nothing(self):
        self.assertEqual(score.hop_reached([item(21, POINTER, "linked")], HOP), (False, False))

    def test_sc_counts_the_value_and_keeps_890s_strict_count(self):
        c = {"chains": [{"id": "SC-001", "family": "SC", "k": 1, "answer": "4864", "intro": {"source": 5},
                         "hops": [{"statement": "The best line for them is extension 4864.", "ends": ["Oscar Goudre", "4864"],
                                   "key": "Oscar Goudre 4864", "source": 9}]}], "guards": []}
        rows = score.score_setting(c, [{"probe": "SC-001", "items": [item(9, "The best line for them is extension 4864.")], "context_chars": 1}], [])
        self.assertTrue(rows["SC-001"]["reached"])
        self.assertFalse(rows["SC-001"]["reached_strict"])
        rows = score.score_setting(c, [{"probe": "SC-001", "items": [item(9, "The best line is an extension.")], "context_chars": 1}], [])
        self.assertFalse(rows["SC-001"]["reached"], "the value must be stated")


class EntitiesTest(unittest.TestCase):
    ents = score.Entities([{"first": 1, "entities": ["The Drudoull build", "Saifill Components"]},
                           {"first": 5, "entities": ["Tomas Wigrull's", "5391", "tomas wigrull"]}])

    def test_normalises_as_the_arm_stores_names(self):
        self.assertEqual(self.ents.subject(1), "drudoull build")
        self.assertEqual(self.ents.by_first[5], ["tomas wigrull", "5391"])

    def test_seeds_are_whole_names_among_the_texts_words(self):
        self.assertEqual(self.ents.seeds("[msg-C3-001] owner: An order for the Drudoull build?"), ["drudoull build"])
        self.assertEqual(self.ents.seeds("the Drudoull"), [], "a part of a name is no seed")


GUARDS = {"guards": [
    {"id": "N-001", "family": "N", "entity": "Chloe Gaimoull", "associated": "Tomas Wigrull", "near_miss": "5391", "sources": [5245, 6665]},
    {"id": "NC-001", "family": "NC", "entity": "Aiko Veaholl", "near_miss": "7508", "sources": [7201, 7205]}], "chains": []}


def attributed(subject, seeds, rest="Tomas Wigrull answers on extension 5391."):
    return score.attribution(subject, seeds) + rest


class OfflineControlTest(unittest.TestCase):
    ents = score.Entities([{"first": 6665, "entities": ["Tomas Wigrull", "5391"]}, {"first": 5245, "entities": ["Chloe Gaimoull", "Tomas Wigrull"]}])

    def probe(self, text, subject="tomas wigrull"):
        return [{"probe": "N-001", "items": [item(6665, text, "linked", iid=7)],
                 "trace": {"seeds": ["chloe gaimoull"], "subjects": {"7": subject}}}]

    def test_attribution_passes_when_present_and_correct(self):
        out = score.attribution_control(GUARDS, self.probe(attributed("tomas wigrull", ["chloe gaimoull"])), self.ents)
        self.assertTrue(out["passed"], out)

    def test_attribution_fails_when_missing_or_wrong(self):
        self.assertFalse(score.attribution_control(GUARDS, self.probe("Tomas Wigrull answers on extension 5391."), self.ents)["passed"])
        wrong = self.probe(attributed("chloe gaimoull", ["chloe gaimoull"]), subject="chloe gaimoull")
        self.assertFalse(score.attribution_control(GUARDS, wrong, self.ents)["passed"])
        # A well-formed attribution to a subject the store holds, but not
        # the associated entity, fails on correctness alone.
        other = score.Entities([{"first": 6665, "entities": ["Sofia Kell", "5391"]}])
        misattributed = self.probe(attributed("sofia kell", ["chloe gaimoull"]), subject="sofia kell")
        out = score.attribution_control(GUARDS, misattributed, other)
        self.assertEqual(out["attributed"], out["walk_items"], "the setup: present and agreeing with the store")
        self.assertFalse(out["passed"])

    def test_attribution_presence_is_checked_beyond_the_n_item(self):
        ents = score.Entities([{"first": 6665, "entities": ["Tomas Wigrull", "5391"]}, {"first": 7205, "entities": ["predecessor", "7508"]}])
        nc = {"probe": "NC-001", "items": [item(7205, "The predecessor answers on 7508.", "linked", iid=9)],
              "trace": {"seeds": ["aiko veaholl"], "subjects": {"9": "predecessor"}}}
        n = self.probe(attributed("tomas wigrull", ["chloe gaimoull"]))
        out = score.attribution_control(GUARDS, n + [nc], ents)
        self.assertEqual((out["n_correct"], out["nc_subject_not_queried"]), (1, 1), "the setup: correct on both")
        self.assertFalse(out["passed"], "the NC walk item lacks its attribution")
        nc["items"][0]["text"] = score.attribution("predecessor", ["aiko veaholl"]) + nc["items"][0]["text"]
        self.assertTrue(score.attribution_control(GUARDS, n + [nc], ents)["passed"], "the control: with it, the control passes")

    def test_pointer_control_fails_on_walk_text(self):
        self.assertTrue(score.pointer_control([{"items": [item(9, POINTER, "linked")]}])["passed"])
        self.assertFalse(score.pointer_control([{"items": [item(9, POINTER, "linked"), item(13, "Chloe Traike is on 6200.", "linked")]}])["passed"])
        self.assertFalse(score.pointer_control([{"items": [item(9, "a similar mid")]}])["passed"], "no walk item, nothing shown")

    def test_key_text_control_compares_stream_items_with_s(self):
        summaries = {4801: "S's summary."}
        self.assertTrue(score.key_text_control([{"items": [item(4801, "S's summary.")]}], summaries)["passed"])
        self.assertFalse(score.key_text_control([{"items": [item(4801, "C's summary.")]}], summaries)["passed"])
        self.assertTrue(score.key_text_control([{"items": [item(4801, "S's summary."), item(4805, POINTER, "linked")]}], summaries, skip_linked=True)["passed"])

    def test_ranking_control_needs_a_tenth_of_probes_to_differ(self):
        base = [{"probe": str(i), "items": [item(1, "")]} for i in range(10)]
        same = [{"probe": str(i), "items": [item(1, "")]} for i in range(10)]
        self.assertFalse(score.ranking_control(same, base)["passed"])
        same[0] = {"probe": "0", "items": [item(5, "")]}
        self.assertTrue(score.ranking_control(same, base)["passed"])

    def test_check_control_fails_on_a_walk_delivery_left_unchecked(self):
        c = {"chains": [{"id": "C2-001", "family": "C2"}], "guards": []}
        ran = {"walk": [2], "ran": True, "named": [1], "connected": True}
        self.assertTrue(score.check_control(c, [{"probe": "C2-001", "trace": {"check": ran}}], [])["passed"])
        skipped = {"walk": [2], "ran": False, "named": [], "connected": False}
        self.assertFalse(score.check_control(c, [{"probe": "C2-001", "trace": {"check": ran}}], [{"probe": "C2-001", "trace": {"check": skipped}}])["passed"])

    def test_margin_is_890s(self):
        self.assertTrue(score.margin_met({"n": 100, "arm": 12, "S": 2, "arm_only": 10, "S_only": 0, "p": 0.002}))
        self.assertFalse(score.margin_met({"n": 100, "arm": 11, "S": 2, "arm_only": 9, "S_only": 0, "p": 0.004}), "9 of 100 is under the margin")
        self.assertFalse(score.margin_met({"n": 100, "arm": 12, "S": 2, "arm_only": 12, "S_only": 2, "p": 0.06}))


def line(iid, kind, text, first):
    return f"- [{iid}] ({kind}): {text} [mid:{iid} sources:{first}-{first + 3}; summary, not verified source text]"


class LiveTest(unittest.TestCase):
    corpus = {"chains": [{"id": "C2-001", "family": "C2", "query": "Who handles Zevai Components?"}],
              "guards": [dict(GUARDS["guards"][0], query="What number do I dial to reach Chloe Gaimoull?")]}
    ents = score.Entities([{"first": 6665, "entities": ["Tomas Wigrull", "5391"]}, {"first": 5245, "entities": ["Chloe Gaimoull", "Tomas Wigrull"]}])

    def test_recall_lines_parse_the_block(self):
        recall = "## Recalled memories\n" + line(7, "linked", POINTER, 9) + "\n- [notice] (check): No stored memory states this fact."
        self.assertEqual(score.recall_lines(recall), [(7, "linked", POINTER, 9, 12)])

    def test_live_pointer_control_reads_linked_lines(self):
        ok = [{"probe": "C2-001", "recall": line(7, "linked", POINTER, 9)}]
        bad = [{"probe": "C2-001", "recall": line(7, "linked", "Chloe Traike is on 6200.", 9)}]
        self.assertTrue(score.live_controls("L-ptr", self.corpus, ok, {}, self.ents, [], [])["passed"])
        self.assertFalse(score.live_controls("L-ptr", self.corpus, bad, {}, self.ents, [], [])["passed"])

    def test_live_attribution_control_recomputes_seeds_from_the_task(self):
        good = [{"probe": "N-001", "recall": line(7, "linked", attributed("tomas wigrull", ["chloe gaimoull"]), 6665)}]
        plain = [{"probe": "N-001", "recall": line(7, "linked", "Tomas Wigrull answers on extension 5391.", 6665)}]
        self.assertTrue(score.live_controls("L-attr", self.corpus, good, {}, self.ents, [], [])["passed"])
        self.assertFalse(score.live_controls("L-attr", self.corpus, plain, {}, self.ents, [], [])["passed"])

    def test_live_attribution_presence_is_checked_on_nc(self):
        corpus = {"chains": [], "guards": [dict(GUARDS["guards"][0], query="What number do I dial to reach Chloe Gaimoull?"),
                                           dict(GUARDS["guards"][1], query="What number do I dial to reach Aiko Veaholl?")]}
        ents = score.Entities([{"first": 6665, "entities": ["Tomas Wigrull", "5391"]}, {"first": 7205, "entities": ["predecessor"]},
                               {"first": 7201, "entities": ["Aiko Veaholl"]}, {"first": 5245, "entities": ["Chloe Gaimoull", "Tomas Wigrull"]}])
        n = {"probe": "N-001", "recall": line(7, "linked", attributed("tomas wigrull", ["chloe gaimoull"]), 6665)}
        bare = {"probe": "NC-001", "recall": line(9, "linked", "The predecessor answers on 7508.", 7205)}
        self.assertFalse(score.live_controls("L-attr", corpus, [n, bare], {}, ents, [], [])["passed"])
        told = {"probe": "NC-001", "recall": line(9, "linked", score.attribution("predecessor", ["aiko veaholl"]) + "The predecessor answers on 7508.", 7205)}
        self.assertTrue(score.live_controls("L-attr", corpus, [n, told], {}, ents, [], [])["passed"])

    def test_live_check_control_needs_the_ledger_to_hold_the_checks(self):
        rec = {"probe": "N-001", "opened": "t2", "call": "recall", "walk": [7], "ran": True, "named": [], "connected": False}
        stale = {"probe": "N-001", "opened": "t1", "call": "recall", "walk": [7], "ran": False, "named": [], "connected": False}
        results = [{"probe": "N-001", "recall": ""}]
        call = {"arm": "L", "phase": "check", "episode": "N-001"}
        self.assertTrue(score.live_controls("L+chk", self.corpus, results, {}, self.ents, [stale, rec], [call])["passed"], "only the final attempt counts")
        self.assertFalse(score.live_controls("L+chk", self.corpus, results, {}, self.ents, [rec], [])["passed"])

    def test_decide_takes_the_valid_arm_with_the_fewest_lines(self):
        def arm(sig, failed, gain):
            return {"significant": sig, "guardrails_failed": failed, "guardrails_hold": not failed, "lost_beyond": len(failed),
                    "primary": {"arm_only": gain, "S_only": 0}}
        comps = {"L-attr": arm(True, [], 40), "C-key": arm(True, [], 10), "L+chk": arm(True, ["N"], 30)}
        out = score.decide(comps, {"L-attr": True, "C-key": True, "L+chk": True})
        self.assertEqual(out["best_valid"], "C-key")
        out = score.decide(comps, {"L-attr": True, "C-key": False, "L+chk": True})
        self.assertEqual(out["best_valid"], "L-attr", "a failed control voids an arm")
        none = score.decide({"L+chk": arm(True, ["N"], 30), "L-ptr": arm(False, [], 5)}, {"L+chk": True, "L-ptr": True})
        self.assertIsNone(none["best_valid"])
        self.assertEqual(none["ranking_if_none_valid"], ["L-ptr", "L+chk"], "fewest guardrails failed first")


if __name__ == "__main__":
    unittest.main()
