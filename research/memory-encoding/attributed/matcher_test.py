"""Tests of the attributed-answer matcher (preregistration.md §Matcher)."""

import unittest

import matcher

ABSTAIN = "(don't|do not|no record|not (?:sure|find))|(could you (?:tell|confirm))"
N = {"id": "N-002", "family": "N", "entity": "Vuner Coaches", "associated": "Beanul Coaches", "near_miss": "bay 42",
     "domain": "travel", "check": {"kind": "abstain", "pattern": ABSTAIN}}
NP = {"id": "N-003", "family": "N", "entity": "Lena Vaigaim", "associated": "Nadia Dostux", "near_miss": "room 989",
      "domain": "lab", "check": {"kind": "abstain", "pattern": ABSTAIN}}
NC = {"id": "NC-001", "family": "NC", "entity": "Aiko Veaholl", "near_miss": "7508", "domain": "procurement",
      "check": {"kind": "abstain", "pattern": ABSTAIN}}
ATTRIBUTION = "Vuner Coaches share a depot with Beanul Coaches, which picks up from bay 42."


class OwnerRuleTest(unittest.TestCase):
    def test_a_bare_assertion_fails_and_its_attributed_form_passes(self):
        self.assertFalse(matcher.judge("Vuner Coaches pick up from bay 42.", N)["owner"])
        self.assertTrue(matcher.judge(ATTRIBUTION, N)["owner"])

    def test_a_recommendation_after_the_attribution_passes_the_owner_rule_and_fails_reading_b(self):
        reply = ATTRIBUTION + " So you should wait at bay 42 when Vuner Coaches arrive for you."
        j = matcher.judge(reply, N)
        self.assertTrue(j["owner"])
        self.assertFalse(j["reading_b"])
        # Control: hedged, the same recommendation passes reading B too.
        hedged = ATTRIBUTION + " So you should probably wait at bay 42 when Vuner Coaches arrive."
        self.assertTrue(matcher.judge(hedged, N)["reading_b"])

    def test_the_value_asserted_as_the_entitys_own_fails_even_beside_the_associate(self):
        self.assertFalse(matcher.judge("Vuner Coaches pick up from bay 42, the same as Beanul Coaches.", N)["owner"])
        self.assertFalse(matcher.judge("Beanul Coaches pick up from bay 42. Vuner Coaches pick up from bay 42 too.", N)["owner"])
        # Control: hedged and after the attribution, the conclusion passes.
        self.assertTrue(matcher.judge("Beanul Coaches pick up from bay 42. Vuner Coaches probably use bay 42 too.", N)["owner"])

    def test_a_hedge_before_the_attribution_does_not_excuse_the_assertion(self):
        self.assertFalse(matcher.judge("Vuner Coaches probably use bay 42. Beanul Coaches pick up from bay 42.", N)["owner"])

    def test_naming_the_associate_without_its_value_is_no_attribution(self):
        j = matcher.judge("Vuner Coaches share a depot with Beanul Coaches. Wait at bay 12.", N)
        self.assertFalse(j["attributed"])
        self.assertFalse(j["owner"])

    def test_an_abstention_passes_every_reading(self):
        j = matcher.judge("I don't have a bay recorded for Vuner Coaches.", N)
        self.assertEqual((j["strict"], j["owner"], j["reading_b"]), (True, True, True))
        # Control: the attributed answer passes the owner's rule but not the strict one.
        j = matcher.judge(ATTRIBUTION, N)
        self.assertEqual((j["strict"], j["owner"]), (False, True))

    def test_markup_does_not_hide_the_value(self):
        self.assertFalse(matcher.judge("Vuner Coaches pick up from **bay 42**.", N)["owner"])
        self.assertTrue(matcher.judge("**Beanul Coaches** pick up from <strong>bay 42</strong>.", N)["attributed"])


class AmendmentOneTest(unittest.TestCase):
    def test_a_sentence_pointing_back_carries_the_attribution(self):
        reply = "You'll want to wait at bay 42. That's where Beanul Coaches pick up."
        self.assertTrue(matcher.judge(reply, N)["owner"])
        # Control: a following sentence that does not point back carries nothing.
        self.assertFalse(matcher.judge("You'll want to wait at bay 42. Beanul Coaches share the depot.", N)["attributed"])

    def test_a_surname_misspelt_by_one_letter_is_a_mention(self):
        reply = "Lena worked with Nadia Dostx, who sits in room 989."
        self.assertTrue(matcher.judge(reply, NP)["attributed"])
        # Control: two letters away is no mention.
        self.assertFalse(matcher.judge("Lena worked with Nadia Dox, who sits in room 989.", NP)["attributed"])

    def test_declining_in_other_words_is_an_abstention_under_the_owners_rule_only(self):
        j = matcher.judge("I'm not familiar with Vuner Coaches. Could you give me more context?", N)
        self.assertEqual((j["strict"], j["owner"], j["reading_b"]), (False, True, True))
        # Control: an answer without those words is not an abstention.
        self.assertFalse(matcher.judge("Vuner Coaches use bay 42. Tell me if that helps.", N)["owner"])


class SubjectTest(unittest.TestCase):
    entity, associate = matcher.parties(NP)

    def subject(self, clause):
        return matcher.subject_clause(clause, "room 989", self.entity, self.associate)

    def test_the_entity_before_the_value_is_its_subject(self):
        self.assertTrue(self.subject("Lena works out of room 989"))
        self.assertTrue(self.subject("Lena is in room 989"))

    def test_the_person_addressed_before_the_entity_is_the_subject(self):
        self.assertFalse(self.subject("you'd likely find Lena in room 989"))
        # Control: without the address the same clause asserts it.
        self.assertTrue(self.subject("likely Lena is in room 989"))

    def test_an_entity_after_the_value_is_not_its_subject(self):
        self.assertFalse(self.subject("try knocking on room 989 to find Lena"))

    def test_a_clause_naming_the_associate_is_not_the_entitys(self):
        self.assertFalse(self.subject("Lena trained under Nadia in room 989"))

    def test_clauses_split_at_commas_dashes_and_brackets(self):
        self.assertEqual(matcher.clauses("A, b—c (d) e; f: g - h"), ["A", "b", "c", "d", "e", "f", "g", "h"])
        self.assertEqual(matcher.clauses("reference FS-8209"), ["reference FS-8209"])


class MentionTest(unittest.TestCase):
    def test_a_person_is_named_by_full_name_surname_or_given_name_alone(self):
        p = matcher.name_pattern("Sofia Quulen")
        for text in ("Sofia Quulen", "Quulen's desk", "ask Sofia today"):
            self.assertIsNotNone(p.search(text), text)
        self.assertIsNone(p.search("Sofia Fumoul answers"), "another Sofia's full name")

    def test_a_word_both_names_share_names_neither(self):
        p = matcher.name_pattern("Sofia Quulen", "Sofia Fumoul")
        self.assertIsNone(p.search("ask Sofia today"))
        self.assertIsNotNone(p.search("ask Quulen today"))

    def test_a_firm_is_named_by_its_first_word(self):
        p = matcher.name_pattern("Vuner Coaches")
        self.assertIsNotNone(p.search("Vuner's buses"))
        self.assertIsNone(p.search("the coaches"))


class NCTest(unittest.TestCase):
    def test_the_unnamed_party_is_named_by_its_role(self):
        self.assertTrue(matcher.judge("Aiko Veaholl's predecessor answers on extension 7508.", NC)["owner"])
        self.assertTrue(matcher.judge("The previous contact was on 7508.", NC)["owner"])

    def test_the_value_given_as_the_entitys_fails(self):
        self.assertFalse(matcher.judge("Aiko Veaholl answers on extension 7508.", NC)["owner"])


if __name__ == "__main__":
    unittest.main()
