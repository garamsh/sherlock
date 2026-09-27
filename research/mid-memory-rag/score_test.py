"""Check that scoring can fail: evidence outside returned intervals scores zero, and the verdict follows the pairs."""
import unittest

import score

MIDS = {1: {'SourceFirst': 1, 'SourceLast': 8}, 2: {'SourceFirst': 9, 'SourceLast': 16}}


def rows(b_only, a_only, ties=0):
    out = []
    for family_index, (b, a) in enumerate([(1, 0)] * b_only + [(0, 1)] * a_only + [(1, 1)] * ties):
        family = score.PRIMARY[family_index % len(score.PRIMARY)]
        out.append({'family': family, 'arms': {'A': {'delivered_hit': a}, 'B': {'delivered_hit': b}}})
    return out


def table(per_query):
    t = {}
    for arm in ['A', 'B']:
        t[arm] = {f: {'delivered_hits': sum(r['arms'][arm]['delivered_hit'] for r in per_query if r['family'] == f)}
                  for f in score.PRIMARY}
        t[arm]['correction']['newest_delivered'] = t[arm]['correction']['delivered_hits']
    return t


PASSING = {'oracle': {'A': True, 'B': True}, 'similarity_differs_from_recency': True, 'similarity_top1_distinct': True}
LIVE = {'A_inspect_works': True, 'B_inspect_works': True}


class ScoreTest(unittest.TestCase):
    def test_reach_credits_only_sources_inside_returned_intervals(self):
        spans = score.intervals([2], MIDS)
        self.assertEqual(score.reach([12], spans), 1)
        self.assertEqual(score.reach([3], spans), 0)
        self.assertEqual(score.first_rank([3], score.intervals([2, 1], MIDS)), 2)

    def test_verdict_follows_paired_counts_and_controls(self):
        # Wins spread across families keep every guardrail inside its bound.
        balanced = rows(8, 0, 30)
        self.assertEqual(score.verdict(table(balanced), balanced, PASSING, LIVE)['verdict'], 'valid')
        reverse = rows(0, 8, 30)
        self.assertEqual(score.verdict(table(reverse), reverse, PASSING, LIVE)['verdict'], 'not valid')
        even = rows(3, 3, 30)
        self.assertEqual(score.verdict(table(even), even, PASSING, LIVE)['verdict'], 'undetermined')
        # More B wins without significance (4 against 1, p = 0.375) stays undetermined.
        leaning = rows(4, 1, 30)
        self.assertEqual(score.verdict(table(leaning), leaning, PASSING, LIVE)['verdict'], 'undetermined')
        failed = dict(PASSING, oracle={'A': True, 'B': False})
        self.assertEqual(score.verdict(table(balanced), balanced, failed, LIVE)['verdict'], 'undetermined')

    def test_sign_test_matches_binomial(self):
        self.assertAlmostEqual(score.sign_test(8, 0), 2 / 256)
        self.assertEqual(score.sign_test(3, 3), 1.0)


if __name__ == '__main__':
    unittest.main()
