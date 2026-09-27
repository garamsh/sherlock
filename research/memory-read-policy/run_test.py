"""Check that collection cannot spend past its pre-request reservations."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import run


class Response:
    def __enter__(self):
        return self

    def __exit__(self, *args):
        return False

    def read(self):
        return json.dumps({'usage': {'prompt_tokens': 10, 'completion_tokens': 5},
                           'choices': [{'message': {'content': 'done'}}]}).encode()


class BudgetTest(unittest.TestCase):
    def test_reservation_refuses_before_http_with_accepted_control(self):
        for field, value in [('input', 119999), ('completion', 20000), ('calls', [{}] * 48)]:
            with self.subTest(field=field), tempfile.TemporaryDirectory() as directory:
                meter = run.Meter(Path(directory))
                with patch.dict(run.os.environ, {'MINIMAX_KEY': 'unit-test'}), patch(
                        'urllib.request.urlopen', return_value=Response()) as http:
                    meter.chat('control', [], [], 20)
                    self.assertEqual(meter.input, 10)
                    self.assertEqual(meter.completion, 5)
                    http.reset_mock()
                    setattr(meter, field, value)
                    with self.assertRaisesRegex(RuntimeError, 'reservation refused'):
                        meter.chat('refusal', [], [], 20)
                    http.assert_not_called()

    def test_context_refuses_with_small_request_control(self):
        with tempfile.TemporaryDirectory() as directory:
            meter = run.Meter(Path(directory))
            with patch.dict(run.os.environ, {'MINIMAX_KEY': 'unit-test'}), patch(
                    'urllib.request.urlopen', return_value=Response()) as http:
                meter.chat('control', [], [], 20)
                http.reset_mock()
                with self.assertRaisesRegex(RuntimeError, 'reservation refused'):
                    meter.chat('oversize', [{'role': 'user', 'content': 'x' * 14000}], [], 20)
                http.assert_not_called()


if __name__ == '__main__':
    unittest.main()
