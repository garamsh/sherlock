"""Tests of #906's card construction, attach rule, measures and gate on
synthetic records; no test reads the recorded evidence.

    cd docs/research/memory-encoding/profiles && python3 -m unittest screen_test
"""

import unittest

import screen as S


def rec(first, *facts):
    return {"first": first, "last": first + 3, "facts": [{"kind": k, "text": t} for k, t in facts]}


def fact(text, kind="other"):
    return {"kind": kind, "text": text}


class OwnerTest(unittest.TestCase):
    def test_owner_is_the_entity_named_earliest(self):
        names = S.entity_names(["Chloe Daifair", "Saifill Components"])
        self.assertEqual(S.owner(fact("Saifill Components is looked after by Chloe Daifair."), names), "saifill components")
        self.assertEqual(S.owner(fact("Chloe Daifair looks after Saifill Components."), names), "chloe daifair")

    def test_owner_tie_goes_to_the_longer_name(self):
        names = S.entity_names(["Saifill", "Saifill Components"])
        self.assertEqual(S.owner(fact("Saifill Components ships on Mondays."), names), "saifill components")

    def test_participants_never_own_through_the_entity_list(self):
        names = S.entity_names(["assistant", "Tea House"])
        self.assertNotIn("assistant", names)
        self.assertEqual(S.owner(fact("assistant recommended Tea House."), names), "tea house")

    def test_a_leading_the_is_dropped_from_a_name(self):
        names = S.entity_names(["the Bakal office"])
        self.assertEqual(S.owner(fact("Bakal office follows the Vozin rules."), names), "bakal office")

    def test_the_user_named_first_owns_the_fact(self):
        names = S.entity_names(["Raiwairk Motors"])
        self.assertEqual(S.owner(fact("User's mechanic at Raiwairk Motors is Ravi."), names), S.USER)

    def test_an_unnamed_user_fact_goes_to_the_users_card(self):
        self.assertEqual(S.owner(fact("Everyone in the family calls them Saistol.", "user"), {}), S.USER)

    def test_an_unnamed_other_fact_has_no_owner(self):
        self.assertIsNone(S.owner(fact("Everyone in the family calls them Saistol."), {}))
        _, _, dropped = S.build([rec(1, ("other", "Nothing named here."))], {(1, 4): []})
        self.assertEqual([d["text"] for d in dropped], ["Nothing named here."])


class KeyTest(unittest.TestCase):
    def test_the_key_drops_names_values_user_words_and_bookkeeping(self):
        names = S.entity_names(["Chloe Daifair"])
        key = S.attribute_key("Chloe Daifair answers on extension 5828 for the Ronair team, user wants it remembered.", names)
        self.assertEqual(key, frozenset({"answers", "extension", "team", "wants"} - S.BOOKKEEPING))

    def test_the_first_remaining_token_is_kept_even_capitalized(self):
        self.assertEqual(S.attribute_key("Dentist is Nadia Bogea.", {}), frozenset({"dentist"}))

    def test_overlap_threshold_is_half_the_smaller_key(self):
        self.assertTrue(S.same_attribute(frozenset("ab"), frozenset("acd")))
        self.assertFalse(S.same_attribute(frozenset("abc"), frozenset("ade")))
        self.assertFalse(S.same_attribute(frozenset(), frozenset()))


class BuildTest(unittest.TestCase):
    ENTS = {(1, 4): ["user"], (5, 8): ["user"], (9, 12): ["Kofi Soustork"]}

    def test_a_newer_fact_on_the_same_attribute_replaces_the_older(self):
        cards, replaced, _ = S.build([
            rec(1, ("user", "User's dentist is Nadia Bogea.")),
            rec(5, ("user", "User changed dentist to Sofia Cukais.")),
            rec(9, ("user", "User's doctor is Kofi Soustork.")),
        ], self.ENTS)
        texts = [f["text"] for f in cards[S.USER]["facts"]]
        self.assertEqual(texts, ["User changed dentist to Sofia Cukais.", "User's doctor is Kofi Soustork."])
        self.assertEqual([(o["text"], n["text"]) for o, n in replaced], [("User's dentist is Nadia Bogea.", "User changed dentist to Sofia Cukais.")])

    def test_without_overwrite_every_fact_stays(self):
        cards, replaced, _ = S.build([rec(1, ("user", "User's dentist is Nadia Bogea.")), rec(5, ("user", "User changed dentist to Sofia Cukais."))],
                                     self.ENTS, overwrite=False)
        self.assertEqual(len(cards[S.USER]["all"]), 2)
        self.assertEqual(replaced, [])

    def test_the_bound_keeps_the_newest_whole_facts_in_stream_order(self):
        facts = [{"text": t} for t in ("a" * 150, "b" * 150, "c" * 150, "d" * 100)]
        self.assertEqual([f["text"][0] for f in S.bounded(facts, 400)], ["c", "d"])
        self.assertEqual([f["text"][0] for f in S.bounded(facts, 404)], ["b", "c", "d"])
        self.assertEqual([f["text"][0] for f in S.bounded(facts, 403)], ["c", "d"])


class AttachTest(unittest.TestCase):
    ENTS = {(1, 4): ["Chloe Daifair"], (5, 8): ["Tomas Wigrull"], (9, 12): ["User"]}

    def cards(self):
        return S.build([rec(1, ("other", "Chloe Daifair answers on extension 5828.")),
                        rec(5, ("other", "Tomas Wigrull answers on extension 5391.")),
                        rec(9, ("user", "Everyone calls them Saistol."))], self.ENTS)[0]

    def test_only_cards_of_delivered_mids_entities_attach(self):
        owners = S.attach([{"first": 1, "last": 4}], self.ENTS, self.cards())
        self.assertIn("chloe daifair", owners)
        self.assertNotIn("tomas wigrull", owners)

    def test_an_entity_without_a_card_attaches_nothing(self):
        ents = {**self.ENTS, (13, 16): ["Nobody Known"]}
        self.assertEqual(S.attach([{"first": 13, "last": 16}], ents, self.cards()), [S.USER])

    def test_the_users_card_attaches_to_any_delivery(self):
        self.assertIn(S.USER, S.attach([{"first": 5, "last": 8}], self.ENTS, self.cards()))
        self.assertEqual(S.attach([], self.ENTS, self.cards()), [])

    def test_any_mention_reads_every_fact_naming_a_delivered_entity(self):
        records = [rec(1, ("other", "Chloe Daifair answers on extension 5828.")), rec(5, ("other", "Tomas and Chloe Daifair share an office."))]
        got = S.any_mention([{"first": 1, "last": 4}], {(1, 4): ["Chloe Daifair"], (5, 8): ["Tomas"]}, records)
        self.assertEqual(len(got), 2)


class MeasureTest(unittest.TestCase):
    ENTS = {(1, 4): ["Chloe Daifair"], (5, 8): ["Tomas Wigrull"], (9, 12): ["Aiko Veaholl"], (13, 16): ["user"], (17, 20): ["user"]}
    RECORDS = [
        rec(1, ("other", "Chloe Daifair answers on extension 5828.")),
        rec(5, ("other", "Tomas Wigrull answers on extension 5391.")),
        rec(9, ("other", "Aiko Veaholl's predecessor answers on extension 7508.")),
        rec(13, ("user", "User's dentist is Nadia Bogea.")),
        rec(17, ("user", "User changed dentist to Sofia Cukais, not Nadia Bogea.")),
    ]

    def row(self, probe, intervals):
        cards, _, _ = S.build(self.RECORDS, self.ENTS)
        items = [{"first": a, "last": a + 3, "text": ""} for a in intervals]
        return S.probe_row(probe, {"items": items, "context_chars": 100}, cards, self.ENTS, self.RECORDS)

    def test_reach_is_an_attached_card_stating_the_answer(self):
        probe = {"id": "C2-1", "family": "C2", "answer": "5828"}
        self.assertTrue(self.row(probe, [1])["reach"])
        self.assertFalse(self.row(probe, [5])["reach"])

    def test_ku_counts_current_superseded_and_stale(self):
        probe = {"id": "KU-1", "family": "KU", "key": "Sofia Cukais", "obsolete": "Nadia Bogea"}
        r = self.row(probe, [1])
        self.assertEqual((r["current"], r["superseded"], r["stale"]), (True, True, False))
        cards, _, _ = S.build(self.RECORDS, self.ENTS, overwrite=False)
        items = [{"first": 1, "last": 4, "text": ""}]
        r = S.probe_row(probe, {"items": items, "context_chars": 1}, cards, self.ENTS, self.RECORDS)
        self.assertTrue(r["stale"])

    def test_confusion_needs_the_queried_entitys_own_card(self):
        n = {"id": "N-1", "family": "N", "entity": "Chloe Gaimoull", "near_miss": "5391"}
        r = self.row(n, [5])
        self.assertEqual((r["confusion"], r["near_miss_attached"]), (False, True))
        nc = {"id": "NC-1", "family": "NC", "entity": "Aiko Veaholl", "near_miss": "7508"}
        self.assertTrue(self.row(nc, [9])["confusion"])
        r = self.row(nc, [1])
        self.assertEqual((r["confusion"], r["owner_card_states"]), (False, True))

    def test_ku_overwrite_finds_the_replaced_first_statement(self):
        _, replaced, _ = S.build(self.RECORDS, self.ENTS)
        guard = {"sources": [14, 18], "key": "Sofia Cukais", "obsolete": "Nadia Bogea"}
        self.assertEqual(S.ku_overwrite(guard, replaced, self.RECORDS), "replaced")
        _, replaced, _ = S.build(self.RECORDS, self.ENTS, overwrite=False)
        self.assertEqual(S.ku_overwrite(guard, replaced, self.RECORDS), "not replaced")


class GateTest(unittest.TestCase):
    def test_chains_alone_clear_at_ten(self):
        self.assertTrue(S.gate(10, 0, True)["paid_phase_may_be_proposed"])
        self.assertFalse(S.gate(9, 0, True)["paid_phase_may_be_proposed"])

    def test_up_alone_clears_at_five(self):
        self.assertTrue(S.gate(0, 5, True)["paid_phase_may_be_proposed"])
        self.assertFalse(S.gate(0, 4, True)["paid_phase_may_be_proposed"])

    def test_a_failed_owner_audit_voids_a_pass(self):
        g = S.gate(50, 50, False)
        self.assertEqual((g["ceiling_clears"], g["paid_phase_may_be_proposed"]), (True, False))

    def test_the_audit_passes_at_45_of_50(self):
        rows = [{"correct": i < 45} for i in range(50)]
        self.assertTrue(S.audit_result(rows)["passed"])
        rows[44]["correct"] = False
        self.assertFalse(S.audit_result(rows)["passed"])


if __name__ == "__main__":
    unittest.main()
