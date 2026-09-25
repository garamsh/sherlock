"""Check the replication's selection, hybrid merge, interval, decision rule and call log."""
import json
import re
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import replication as R
import replication_score as S
import run_test


class SelectionTest(unittest.TestCase):
    def test_contains_respects_value_boundaries(self):
        self.assertTrue(R.contains('The code is QX-12, fine.', 'qx-12'))
        self.assertFalse(R.contains('The code is QX-123.', 'QX-12'))

    def test_key_leaking_into_another_chat_bans_the_item_and_takes_the_next(self):
        def item(i):
            return {'id': f'exact-{i}', 'failure': None, 'key': f'K{i}Z', 'fact_sentence': f'the locker code number is K{i}Z'}
        chats = {f'exact-{i}': {'messages': [{'content': f'the locker code is K{i}Z'}]} for i in range(5)}
        # exact-1 restates exact-0's fact: a leak. exact-3 and exact-4 carry a key with at most one other long word.
        chats['exact-1']['messages'].append({'content': 'the old locker code was K0Z'})
        chats['exact-4']['messages'].append({'content': 'is K3Z the code'})
        chats['exact-3']['messages'].append({'content': 'K2Z'})
        words = lambda text: set(re.findall(r'[a-z0-9]+', text.lower())) - {'the', 'is', 'was'}
        with patch.object(R, 'PER_FAMILY', 3):
            selected, dropped = R.select({'exact': [item(i) for i in range(5)]}, chats, words)
        self.assertEqual([i['id'] for i in selected['exact']], ['exact-1', 'exact-2', 'exact-3'])
        self.assertEqual(dropped, [{'id': 'exact-0', 'reason': 'key appears in another selected chat'}])


class MergeTest(unittest.TestCase):
    def test_hybrid_alternates_starting_with_shipped_and_dedupes(self):
        a = [[1, 8], [9, 16], [17, 24]]
        b = [[9, 16], [25, 32], [33, 40]]
        self.assertEqual(R.interleave(a, b), [[1, 8], [9, 16], [25, 32], [17, 24], [33, 40]])

    def test_deliver_keeps_recent_first_dedupes_and_caps(self):
        recent = [[41, 48], [33, 40]]
        relevant = [[33, 40]] + [[i, i + 7] for i in range(1, 49, 8)]
        self.assertEqual(R.deliver(recent, relevant), [[41, 48], [33, 40], [1, 8], [9, 16], [17, 24], [25, 32]])
        longer = [[i, i + 7] for i in range(49, 200, 8)]
        self.assertEqual(len(R.deliver(recent, longer)), 7)


class RuleTest(unittest.TestCase):
    def test_paired_interval_matches_exact_binomial(self):
        low, high = S.paired_interval(10, 0, 100)
        self.assertAlmostEqual(high, 0.10, places=6)
        self.assertAlmostEqual(low, 10 * (2 * 0.6915028 - 1) / 100, places=4)

    def test_decision_separates_gain_null_and_ambiguity(self):
        self.assertEqual(S.decide(40, 10, 280, True, True)[0], 'valid')
        self.assertEqual(S.decide(40, 10, 280, False, True)[0], 'undetermined')
        self.assertEqual(S.decide(20, 20, 280, True, True)[0], 'not valid')
        self.assertEqual(S.decide(60, 45, 280, True, True)[0], 'undetermined')
        self.assertEqual(S.decide(40, 10, 280, True, False)[0], 'undetermined')


class MeterTest(unittest.TestCase):
    def test_jsonl_log_resumes_usage_and_hides_regenerable_requests(self):
        with tempfile.TemporaryDirectory() as directory:
            with patch.dict(R.run.os.environ, {'MINIMAX_KEY': 'unit-test'}), patch(
                    'urllib.request.urlopen', return_value=run_test.Response()):
                meter = R.Meter(Path(directory), R.LIMITS)
                meter.call('synthesis-n8-1-0', {'messages': ['secret-free body']}, 20)
                meter.call('design-x', {'messages': []}, 20)
            resumed = R.Meter(Path(directory), R.LIMITS)
            self.assertEqual((len(resumed.calls), resumed.input, resumed.output), (2, 20, 10))
            lines = [json.loads(l) for l in (Path(directory) / 'calls.jsonl').read_text().splitlines()]
            self.assertTrue(all('sha256' in l['request'] for l in lines if l['cell'].startswith('synthesis-')))
            self.assertTrue(all('messages' in l['request'] for l in lines if l['cell'].startswith('design-')))

    def test_rate_limited_answer_is_retried_then_refused_by_the_ceiling(self):
        class Limited(run_test.Response):
            def read(self):
                return json.dumps({'base_resp': {'status_code': 1002}, 'usage': {'prompt_tokens': 1,
                                                                                   'completion_tokens': 0}}).encode()
        with tempfile.TemporaryDirectory() as directory, patch.object(R, 'BACKOFF', [0, 0]), patch.dict(
                R.run.os.environ, {'MINIMAX_KEY': 'unit-test'}), patch('urllib.request.urlopen', return_value=Limited()):
            meter = R.Meter(Path(directory), R.LIMITS)
            with self.assertRaisesRegex(RuntimeError, 'transient failures exhausted'):
                meter.call('design-y', {'messages': []}, 20)
            self.assertEqual(len(meter.calls), 3)
            meter.input = R.LIMITS['input']
            with self.assertRaisesRegex(RuntimeError, 'reservation refused'):
                meter.call('design-z', {'messages': []}, 20)


if __name__ == '__main__':
    unittest.main()
