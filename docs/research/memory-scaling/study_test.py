"""Tests of the delivery, budget, dedupe, leak and tier rules study.py applies; no server is called."""
import unittest

import study
from study import Unit


def units(*spans, text='x' * 10):
    return study.number([Unit(first, last, text) for first, last in spans])


class DeliveryTest(unittest.TestCase):
    def test_a_query_is_hit_when_an_evidence_source_lies_inside_a_delivered_interval(self):
        self.assertTrue(study.covers([(5, 8)], [8]))
        self.assertTrue(study.covers([(5, 8)], [5]))
        self.assertFalse(study.covers([(5, 8)], [4, 9]))

    def test_the_first_rank_is_the_first_unit_covering_evidence(self):
        ranked = units((1, 4), (5, 8), (9, 12))
        self.assertEqual(study.first_rank(ranked[::-1], [6]), 2)
        self.assertIsNone(study.first_rank(ranked, [13]))


class BudgetTest(unittest.TestCase):
    def test_no_turn_exceeds_the_budget_and_a_later_unit_that_fits_is_still_taken(self):
        small = units((1, 4), (5, 8))
        large = study.number([Unit(9, 12, 'y' * 500)])[0]
        budget = small[0].size() + small[1].size()
        out = study.deliver([], [small[0], large, small[1]], budget)
        self.assertEqual([u.first for u in out], [1, 5])
        self.assertLessEqual(sum(u.size() for u in out), budget)

    def test_the_budget_admits_a_unit_that_fits_exactly(self):
        store = units((1, 4))
        self.assertEqual(study.deliver([], store, store[0].size()), store)

    def test_a_unit_size_counts_the_rendered_suffix(self):
        unit = units((1, 4))[0]
        self.assertEqual(unit.size(), 10 + len(' [mid:1 sources:1-4; summary, not verified source text]'))

    def test_only_the_first_depth_ranked_units_are_tried(self):
        store = units((1, 4), (5, 8), (9, 12))
        self.assertEqual(study.deliver([], store, 10 ** 6, depth=2), store[:2])


class DedupeTest(unittest.TestCase):
    def test_a_recency_unit_ranked_again_is_delivered_once(self):
        store = units((1, 4), (5, 8), (9, 12))
        out = study.deliver([store[2]], [store[2], store[0]], 10 ** 6)
        self.assertEqual(out, [store[2], store[0]])

    def test_the_shipped_rule_deduplicates_and_stops_at_seven(self):
        store = units(*[(4 * i + 1, 4 * i + 4) for i in range(10)])
        recent = store[-2:][::-1]
        out = study.shipped(recent, [store[-1]] + store[:8])
        self.assertEqual(len(out), 7)
        self.assertEqual(len({id(u) for u in out}), 7)
        self.assertEqual(out[:3], [store[-1], store[-2], store[0]])

    def test_keys_under_one_value_deliver_it_once_in_first_hit_order(self):
        keys = units((1, 4), (5, 8), (17, 20))
        values = units((1, 16), (17, 32))
        value_of = study.owners(keys, values)
        self.assertEqual(study.values_in_order([keys[1], keys[2], keys[0]], value_of), [values[0], values[1]])


class LeakTest(unittest.TestCase):
    def test_a_planted_value_is_found_case_insensitively(self):
        patterns = study.leak_patterns(['Kiwi'])
        self.assertIsNotNone(study.leaked(['my parrot kiwi talks'], patterns))

    def test_a_value_flanked_by_a_letter_or_digit_is_not_a_leak(self):
        patterns = study.leak_patterns(['47'])
        self.assertIsNone(study.leaked(['desk K47 and room 470'], patterns))
        self.assertIsNotNone(study.leaked(['extension 47.'], patterns))


class RankingTest(unittest.TestCase):
    def test_fusion_ranks_a_unit_in_both_lists_above_one_in_either(self):
        a, b, c = units((1, 4), (5, 8), (9, 12))
        dense, lexical = [a, b], [c, b]
        rank = {id(a): 0, id(b): 1, id(c): 2}
        self.assertEqual(study.fuse(dense, lexical, rank), [b, a, c])

    def test_the_lexical_query_drops_stop_words_and_repeats(self):
        self.assertEqual(study.lexical_query('What is the Wi-Fi password, the password?'),
                         '"wi" OR "fi" OR "password"')

    def test_the_aged_tier_keeps_old_batches_at_16_and_the_newest_window_at_4(self):
        rep4 = [Unit(4 * i + 1, 4 * i + 4, 'b4') for i in range(12)]
        rep16 = [Unit(16 * i + 1, 16 * i + 16, 'b16') for i in range(3)]
        tier = study.aged(rep4, rep16, newest=48, window=20)
        self.assertEqual([(u.first, u.last) for u in tier], [(1, 16), (17, 20), (21, 24), (25, 28), (29, 32),
                                                             (33, 36), (37, 40), (41, 44), (45, 48)])
        self.assertEqual([u.text for u in study.aged(rep4, rep16, newest=100, window=20)], ['b16'] * 3)


if __name__ == '__main__':
    unittest.main()
