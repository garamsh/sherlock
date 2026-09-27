"""Tests of Kr's recency and fill in screen.py and of the gate in score.py; no server is called."""
import unittest

import score
import screen
from study import Unit, deliver, number, owners, values_in_order


class KrRecencyTest(unittest.TestCase):
    def stores(self, distractors):
        """A B4 and a K store over a 16-message replication, with distractor B4 mids after it."""
        b4 = number([Unit(f, f + 3, 'b4') for f in (1, 5, 9, 13)] + [Unit(f, l, 'd') for f, l in distractors])
        k = number([Unit(1, 16, 'b16')] + [Unit(f, l, 'd') for f, l in distractors])
        return b4, k

    def test_recency_is_batch4_sized_when_the_replication_is_newest(self):
        b4, k = self.stores([])
        recent = screen.kr_recency(b4[-2:][::-1], k)
        self.assertEqual([(u.first, u.last) for u in recent], [(13, 16), (9, 12)])
        # Control: K's own two newest units would be its B16 value.
        self.assertEqual(k[-1].last - k[-1].first + 1, 16)

    def test_a_distractor_recency_unit_is_the_k_stores_own_and_is_delivered_once(self):
        b4, k = self.stores([(17, 20), (21, 24)])
        recent = screen.kr_recency(b4[-2:][::-1], k)
        self.assertIs(recent[0], k[-1])
        ranked = values_in_order([b4[-1], b4[0]], owners(b4, k))
        out = deliver(recent, ranked, 10 ** 6)
        self.assertEqual([(u.first, u.last) for u in out], [(21, 24), (17, 20), (1, 16)])


class StatedTest(unittest.TestCase):
    def test_a_query_is_stated_when_a_delivered_text_states_its_value(self):
        units = number([Unit(1, 16, 'The router password is now Netgear2024.'), Unit(17, 20, 'Talked about plants.')])
        item = {'key': 'Netgear2024', 'new_key': 'Netgear2024', 'old_key': 'Netgear123'}
        q = {'family': 'correction', 'evidence': [3], 'obsolete': [18]}
        self.assertEqual(screen.stated(units, q, item), {'stated': True, 'covered_stated': True, 'stale_stated': False})
        # Control: the same units without the stating text state nothing.
        self.assertFalse(screen.stated(units[1:], q, item)['stated'])

    def test_a_value_stated_outside_the_evidence_unit_is_stated_but_not_covered(self):
        units = number([Unit(1, 4, 'Nothing here.'), Unit(5, 8, 'Kiwi says hello.')])
        q = {'family': 'exact', 'evidence': [2], 'obsolete': []}
        self.assertEqual(screen.stated(units, q, {'key': 'Kiwi'}), {'stated': True, 'covered_stated': False})

    def test_a_stale_correction_states_the_old_value_without_the_new(self):
        item = {'key': 'Netgear2024', 'new_key': 'Netgear2024', 'old_key': 'Netgear123'}
        q = {'family': 'correction', 'evidence': [3], 'obsolete': [18]}
        old = number([Unit(17, 20, 'My router password is Netgear123.')])
        self.assertTrue(screen.stated(old, q, item)['stale_stated'])
        both = number([Unit(1, 4, 'Now Netgear2024.'), Unit(17, 20, 'Was Netgear123.')])
        self.assertFalse(screen.stated(both, q, item)['stale_stated'])

    def test_an_absent_query_counts_its_near_miss_value_stated(self):
        q = {'family': 'absent', 'evidence': [], 'obsolete': []}
        self.assertTrue(screen.stated(number([Unit(1, 4, 'I read 200 pages.')]), q, {'key': '200 pages'})['near_miss_stated'])
        self.assertFalse(screen.stated(number([Unit(1, 4, 'I read 20 pages.')]), q, {'key': '200 pages'})['near_miss_stated'])


def query(evidence=(), obsolete=(), near_miss=()):
    return {'evidence': list(evidence), 'obsolete': list(obsolete), 'near_miss': list(near_miss)}


class MeasuresTest(unittest.TestCase):
    def test_hit_stale_and_near_miss_follow_the_delivered_intervals(self):
        q = query(evidence=[10], obsolete=[3])
        self.assertEqual(score.measures([[9, 12]], q), {'hit': True, 'stale': False, 'near_miss': False})
        self.assertEqual(score.measures([[1, 4]], q), {'hit': False, 'stale': True, 'near_miss': False})
        self.assertEqual(score.measures([[1, 4], [9, 12]], q)['stale'], False)
        self.assertTrue(score.measures([[5, 8]], query(near_miss=[8]))['near_miss'])
        self.assertFalse(score.measures([[5, 7]], query(near_miss=[8]))['near_miss'])


def measure(hit=True, stale=False, near_miss=False):
    return {'hit': hit, 'stale': stale, 'near_miss': near_miss,
            'stated': hit, 'stale_stated': stale, 'near_miss_stated': near_miss}


class GateTest(unittest.TestCase):
    def per(self, kr_primary_losses=0, kr_absent_exposed=0, kr_stale=0):
        """280 primary and 30 absent queries where S4 and B4 hit every primary query and Kr loses some."""
        families = {f: [f'{f}-{i}' for i in range(70)] for f in score.PRIMARY}
        families['absent'] = [f'absent-{i}' for i in range(30)]
        other = {i: measure() for f in score.PRIMARY for i in families[f]}
        other.update({i: measure(hit=False) for i in families['absent']})
        kr = dict(other)
        lost = [i for f in ('paraphrase', 'implicit') for i in families[f]][:kr_primary_losses]
        kr.update({i: measure(hit=False) for i in lost})
        kr.update({i: measure(hit=False, near_miss=True) for i in families['absent'][:kr_absent_exposed]})
        kr.update({i: measure(hit=False, stale=True) for i in families['correction'][:kr_stale]})
        return {'Kr': kr, 'S4': other, 'B4': other}, families

    def test_the_gate_passes_within_the_margin_and_fails_below_it(self):
        per, families = self.per(kr_primary_losses=2)
        checks, holds = score.gate(per, families)
        self.assertGreaterEqual(checks['S4']['delivery']['interval'][0], score.MARGIN)
        self.assertTrue(holds)
        per, families = self.per(kr_primary_losses=20)
        checks, holds = score.gate(per, families)
        self.assertLess(checks['S4']['delivery']['interval'][0], score.MARGIN)
        self.assertFalse(checks['S4']['margin_holds'])
        self.assertFalse(holds)

    def test_a_guardrail_fails_beyond_its_allowance_and_holds_at_it(self):
        per, families = self.per(kr_absent_exposed=score.ALLOWANCE)
        checks, holds = score.gate(per, families)
        self.assertEqual(checks['B4']['guardrails']['absent_near_miss'], score.ALLOWANCE)
        self.assertTrue(holds)
        per, families = self.per(kr_absent_exposed=score.ALLOWANCE + 1)
        checks, holds = score.gate(per, families)
        self.assertTrue(checks['S4']['margin_holds'])
        self.assertFalse(checks['S4']['guardrails_hold'])
        self.assertFalse(holds)

    def test_the_stated_gate_reads_the_stated_measures(self):
        per, families = self.per()
        for i in families['paraphrase'][:20]:
            per['Kr'][i] = dict(per['Kr'][i], stated=False)
        self.assertTrue(score.gate(per, families)[1])
        checks, holds = score.gate(per, families, score.STATED)
        self.assertFalse(checks['S4']['margin_holds'])
        self.assertFalse(holds)

    def test_stale_corrections_count_against_their_guardrail(self):
        per, families = self.per(kr_stale=score.ALLOWANCE + 1)
        checks, holds = score.gate(per, families)
        self.assertEqual(checks['S4']['guardrails']['correction_stale'], score.ALLOWANCE + 1)
        self.assertFalse(holds)


if __name__ == '__main__':
    unittest.main()
