"""Tests of the decision rule evaluate.py applies; no file is read."""
import unittest

import evaluate

IDS = [f'q{i}' for i in range(100)]


def arm(hit_count):
    return {q: i < hit_count for i, q in enumerate(IDS)}


def storage(ratios):
    return [{'scale': s, 'arm': a, 'units': 1, 'replication_stored_chars': r, 'replication_source_chars': 1,
             'stored_chars': r, 'source_chars': 1} for s in ('s0', 's4') for a, r in ratios.items()]


class StatisticsTest(unittest.TestCase):
    def test_mcnemar_is_two_sided_and_one_without_discordance(self):
        self.assertEqual(evaluate.mcnemar(0, 0), 1.0)
        self.assertAlmostEqual(evaluate.mcnemar(10, 0), 2 * 0.5 ** 10)
        self.assertEqual(evaluate.mcnemar(3, 7), evaluate.mcnemar(7, 3))

    def test_holm_is_monotone_in_the_raw_order(self):
        self.assertEqual(evaluate.holm({'a': 0.01, 'b': 0.04, 'c': 0.03}), {'a': 0.03, 'c': 0.06, 'b': 0.06})


class DecisionTest(unittest.TestCase):
    def hits(self, s0, s4):
        return {(s, a): arm(n) for s, row in (('s0', s0), ('s4', s4)) for a, n in row.items()}

    def test_without_measurable_degradation_the_decisive_scale_is_1x(self):
        hits = self.hits({'S4': 70, 'B4': 70, 'B8': 70}, {'S4': 68, 'B4': 60, 'B8': 50})
        result = evaluate.decide(hits, IDS, storage({'B4': 0.7, 'B8': 0.5}), ['S4', 'B4', 'B8'])
        self.assertFalse(result['stresses_retrieval'])
        self.assertEqual(result['decisive_scale'], 's0')
        self.assertEqual(result['comparisons']['B8']['hits'], 70)

    def test_a_significant_loss_of_s4_makes_the_largest_scale_decisive(self):
        hits = self.hits({'S4': 70, 'B4': 70, 'B8': 70}, {'S4': 50, 'B4': 50, 'B8': 70})
        result = evaluate.decide(hits, IDS, storage({'B4': 0.7, 'B8': 0.5}), ['S4', 'B4', 'B8'])
        self.assertTrue(result['stresses_retrieval'])
        self.assertEqual(result['decisive_scale'], 's4')
        self.assertTrue(result['comparisons']['B8']['delivers_more'])
        self.assertEqual(result['recommendation'], 'B8')

    def test_an_equal_arm_is_recommended_only_when_it_stores_less(self):
        hits = self.hits({'S4': 70, 'B4': 70, 'B8': 70}, {'S4': 70, 'B4': 70, 'B8': 70})
        cheaper = evaluate.decide(hits, IDS, storage({'B4': 0.7, 'B8': 0.5}), ['S4', 'B4', 'B8'])
        dearer = evaluate.decide(hits, IDS, storage({'B4': 0.7, 'B8': 0.9}), ['S4', 'B4', 'B8'])
        self.assertEqual(cheaper['recommendation'], 'B8')
        self.assertIsNone(dearer['recommendation'])

    def test_an_arm_whose_interval_admits_a_large_loss_is_not_no_worse(self):
        hits = self.hits({'S4': 70, 'B4': 70, 'B8': 60}, {'S4': 70, 'B4': 70, 'B8': 60})
        result = evaluate.decide(hits, IDS, storage({'B4': 0.7, 'B8': 0.5}), ['S4', 'B4', 'B8'])
        self.assertLess(result['comparisons']['B8']['interval'][0], evaluate.NO_WORSE)
        self.assertIsNone(result['recommendation'])


class HitTest(unittest.TestCase):
    def test_the_hit_table_scores_the_delivered_intervals(self):
        queries = [{'id': 'a', 'evidence': [6]}, {'id': 'b', 'evidence': []}]
        rows = [{'scale': 's0', 'arm': 'B4', 'query': 'a', 'delivered': [[1, 4], [5, 8]]},
                {'scale': 's0', 'arm': 'B8', 'query': 'a', 'delivered': [[1, 4]]},
                {'scale': 's0', 'arm': 'B4', 'query': 'b', 'delivered': [[5, 8]]}]
        self.assertEqual(evaluate.hit_table(rows, queries), {('s0', 'B4'): {'a': True}, ('s0', 'B8'): {'a': False}})


if __name__ == '__main__':
    unittest.main()
