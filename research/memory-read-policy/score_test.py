"""Check artifact scoring against correct and independently broken exports."""
import unittest

import score


GOOD = '''```python
import csv
import io

def render_export(rows):
    output = io.StringIO()
    writer = csv.writer(output, lineterminator='\\n')
    writer.writerow(['label', 'total'])
    for label, count in rows:
        if not label.startswith('_'):
            writer.writerow([label, count])
    return output.getvalue()
```'''


class ArtifactTest(unittest.TestCase):
    def test_detects_missing_contract_facets(self):
        good = score.artifact_check(GOOD)
        for field in ['runnable', 'privacy', 'current_header', 'lf_only',
                      'order_and_quoting', 'stdlib_csv_and_stringio']:
            self.assertTrue(good[field], field)
        mutations = [
            ('privacy', "if not label.startswith('_'):", 'if True:'),
            ('current_header', "['label', 'total']", "['label', 'count']"),
            ('lf_only', ", lineterminator='\\n'", ''),
        ]
        for field, before, after in mutations:
            with self.subTest(field=field):
                broken = score.artifact_check(GOOD.replace(before, after))
                self.assertFalse(broken[field])


if __name__ == '__main__':
    unittest.main()
