"""#885's corpus tests (../confirm/corpus_test.py), run over this corpus."""

import importlib.util
import sys
import unittest
from pathlib import Path

import corpus

HERE = Path(__file__).resolve().parent

_spec = importlib.util.spec_from_file_location("chained_recall_confirm_corpus_test", HERE.parent / "confirm" / "corpus_test.py")
_tests = importlib.util.module_from_spec(_spec)
# Its `import corpus as confirm` takes the generator set to this seed and
# directory.
sys.modules["corpus"] = corpus.confirm
try:
    _spec.loader.exec_module(_tests)
finally:
    sys.modules["corpus"] = corpus
_tests.HERE = HERE

CandidatesTest = _tests.CandidatesTest
SelectedTest = _tests.SelectedTest


class SeedTest(unittest.TestCase):
    def test_the_generator_runs_from_this_seed_and_directory(self):
        self.assertEqual(corpus.confirm.SEED, 888)
        self.assertEqual(corpus.confirm.HERE, HERE)
        mine = _tests.load(HERE / "candidates.json")
        theirs = _tests.load(HERE.parent / "confirm" / "candidates.json")
        self.assertNotEqual(mine["chains"][0]["query"], theirs["chains"][0]["query"], "the control: #885's seed wrote another corpus")


if __name__ == "__main__":
    unittest.main()
