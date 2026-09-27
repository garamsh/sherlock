"""Check that collection cannot spend past its reservations and that arms share the production merge."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import run


class Response:
    status = 200

    def __enter__(self):
        return self

    def __exit__(self, *args):
        return False

    def read(self):
        return json.dumps({'usage': {'prompt_tokens': 10, 'completion_tokens': 5},
                           'choices': [{'message': {'content': 'done'}}]}).encode()


class MeterTest(unittest.TestCase):
    def test_reservation_refuses_before_http_with_accepted_control(self):
        for field, value in [('input', run.LIMITS['input'] - 10), ('output', run.LIMITS['output'] - 10)]:
            with self.subTest(field=field), tempfile.TemporaryDirectory() as directory:
                meter = run.Meter(Path(directory))
                with patch.dict(run.os.environ, {'MINIMAX_KEY': 'unit-test'}), patch(
                        'urllib.request.urlopen', return_value=Response()) as http:
                    meter.chat('control', {'messages': []}, 20)
                    self.assertEqual((meter.input, meter.output), (10, 5))
                    http.reset_mock()
                    setattr(meter, field, value)
                    with self.assertRaisesRegex(RuntimeError, 'reservation refused'):
                        meter.chat('refusal', {'messages': []}, 20)
                    http.assert_not_called()

    def test_resume_counts_recorded_usage(self):
        with tempfile.TemporaryDirectory() as directory:
            with patch.dict(run.os.environ, {'MINIMAX_KEY': 'unit-test'}), patch(
                    'urllib.request.urlopen', return_value=Response()):
                run.Meter(Path(directory)).chat('first', {'messages': []}, 20)
            resumed = run.Meter(Path(directory))
            self.assertEqual((len(resumed.calls), resumed.input, resumed.output), (1, 10, 5))


class MergeTest(unittest.TestCase):
    def test_merge_puts_recent_first_dedupes_and_caps(self):
        recent = [{'ID': 300}, {'ID': 299}]
        relevant = [{'ID': 299}] + [{'ID': i} for i in range(1, 10)]
        self.assertEqual([m['ID'] for m in run.merge(recent, relevant)], [300, 299, 1, 2, 3, 4, 5])

    def test_cosine_rank_orders_by_best_query_vector(self):
        order, _ = run.cosine_rank([[1, 0], [0, 1]], [[0.6, 0.8], [1, 0], [0.8, 0.6]])
        self.assertEqual(order[0], 1)
        order, _ = run.cosine_rank([[0, 1]], [[0.6, 0.8], [1, 0], [0.8, 0.6]])
        self.assertEqual(order, [0, 2, 1])


if __name__ == '__main__':
    unittest.main()
