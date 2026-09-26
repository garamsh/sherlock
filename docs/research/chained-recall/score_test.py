"""Checks of the per-hop scorer."""

import unittest

import score

HOP = {"source": 4805, "key": "Hourer Motors Pavel Kobri"}


def item(first, last, text):
    return {"id": 1, "level": "mid", "kind": "historical", "first": first, "last": last, "text": text}


class HopTest(unittest.TestCase):
    def test_a_hop_is_reached_when_an_item_holds_it_and_states_both_ends(self):
        self.assertEqual(score.hop_reached([item(4805, 4808, "At Hourer Motors the mechanic is Pavel Kobri.")], HOP), (True, True))

    def test_holding_the_statement_without_its_entities_is_interval_reach_only(self):
        # Control for the test above: the same interval, a summary that lost a name.
        self.assertEqual(score.hop_reached([item(4805, 4808, "The user trusts a mechanic at Hourer Motors.")], HOP), (True, False))

    def test_stating_the_entities_outside_the_interval_is_not_reach(self):
        self.assertEqual(score.hop_reached([item(4809, 4812, "Hourer Motors, Pavel Kobri.")], HOP), (False, False))


class ChainTest(unittest.TestCase):
    def corpus(self):
        return {"chains": [{"id": "C2-01", "family": "C2", "k": 2, "hops": [
            {"source": 4801, "key": "Alpha Beta"}, {"source": 4901, "key": "Beta 1234"}]}],
            "guards": [{"id": "E-01", "family": "E", "evidence": [10], "key": "Kiwi"},
                       {"id": "N-01", "family": "N", "evidence": []}]}

    def test_a_chain_needs_every_hop(self):
        probes = [{"probe": "C2-01", "items": [item(4801, 4804, "Alpha and Beta")], "context_chars": 10},
                  {"probe": "E-01", "items": [item(9, 12, "Kiwi says hi")], "context_chars": 5},
                  {"probe": "N-01", "items": [], "context_chars": 0}]
        chains, guards = score.score_setting(self.corpus(), probes, [])
        self.assertEqual(chains["C2-01"]["stated"], [True, False])
        self.assertFalse(chains["C2-01"]["chain"])
        self.assertTrue(guards["E-01"]["reached"])
        self.assertIsNone(guards["N-01"]["reached"])
        probes[0]["items"].append(item(4901, 4904, "Beta is on 1234"))
        chains, _ = score.score_setting(self.corpus(), probes, [])
        self.assertTrue(chains["C2-01"]["chain"])



class LiveTest(unittest.TestCase):
    def test_applied_reads_the_reply_messages_and_files(self):
        check = {"kind": "contains", "value": "REF-KQZA418"}
        self.assertTrue(score.applied(check, {"reply": "done", "written": {"note.txt": "Ref REF-KQZA418\nOn schedule."}}))
        self.assertFalse(score.applied(check, {"reply": "On schedule."}), "the control: an artifact without the reference")

    def test_following_counts_searches_for_a_name_an_earlier_view_showed(self):
        score.PROBE_NAMES["C2-01"] = {"Hourer", "Motors"}
        result = {"probe": "C2-01", "recall": "At Hourer Motors the mechanic is Pavel Kobri.", "tools": [
            {"name": "memory_search", "input": {"query": "Hourer Motors mechanic"}, "result": "[]"},
            {"name": "memory_search", "input": {"query": "Pavel Kobri keys"}, "result": "[]"}]}
        self.assertEqual(score.followed(result), 1)

    def test_arm_items_are_linked_or_group_lines_of_the_recall_block(self):
        self.assertTrue(score.arm_items({"recall": "- [12] (linked): a summary"}))
        self.assertTrue(score.arm_items({"recall": "- [group:3] (historical): a group"}))
        self.assertFalse(score.arm_items({"recall": "- [12] (historical): a summary"}), "the control: a shipped line")


if __name__ == "__main__":
    unittest.main()
