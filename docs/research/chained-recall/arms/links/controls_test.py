"""Checks of arm L's controls script."""

import unittest

import controls

CORPUS = {"chains": [
    {"id": "C1-01", "k": 1, "hops": [{"source": 10, "key": "Alpha Beta"}]},
    {"id": "C2-01", "k": 2, "hops": [{"source": 20, "key": "Beta Gamma"}, {"source": 30, "key": "Gamma 1234"}]},
]}


def trace(seeds, steps, beyond):
    return {"seeds": seeds, "steps": steps, "beyond_similar": beyond, "beyond_seeds": beyond}


def built(share):
    return {"built": {"notes": {"share_with_entities": share}}}


ORACLE = [{"probe": "C2-01", "hop": 1, "items": [{"first": 17, "last": 20, "text": ""}]},
          {"probe": "C2-01", "hop": 2, "items": [{"first": 29, "last": 32, "text": ""}]}]


class ControlsTest(unittest.TestCase):
    def test_a_walk_that_seeded_stepped_and_reached_past_similarity_passes(self):
        probes = [{"probe": "C1-01", "trace": trace([], 0, [])}, {"probe": "C2-01", "trace": trace(["beta"], 12, [7])}]
        got = controls.controls(CORPUS, built(0.95), probes, ORACLE)
        self.assertTrue(got["passed"])
        self.assertEqual(got["chains"], 1, "only chains of k >= 2 are counted")

    def test_a_walk_of_one_iteration_fails(self):
        # Control for the test above: the same probe, but the walk never left its seeds.
        probes = [{"probe": "C2-01", "trace": trace(["beta"], 1, [7])}]
        got = controls.controls(CORPUS, built(0.95), probes, ORACLE)
        self.assertFalse(got["walked"]["passed"])
        self.assertFalse(got["passed"])

    def test_too_few_mids_with_entities_or_a_missed_oracle_fails(self):
        probes = [{"probe": "C2-01", "trace": trace(["beta"], 12, [7])}]
        self.assertFalse(controls.controls(CORPUS, built(0.89), probes, ORACLE)["entities"]["passed"])
        missed = [ORACLE[0], {"probe": "C2-01", "hop": 2, "items": [{"first": 33, "last": 36, "text": ""}]}]
        self.assertFalse(controls.controls(CORPUS, built(0.95), probes, missed)["oracle"]["passed"])


class AddedTokensTest(unittest.TestCase):
    def call(self, arm, episode, phase, completion, prompt=100, status=200):
        return {"arm": arm, "episode": episode, "phase": phase, "status": status, "completion_tokens": completion, "prompt_tokens": prompt}

    def test_pairs_base_batches_with_the_replication_and_stream_batches_with_the_ledger(self):
        ledger = [
            self.call("L", "b4-00001", "entities-b4", 0, status=429),
            self.call("L", "b4-00001", "entities-b4", 350, 160),
            self.call("L", "b4-00003", "entities-b4", 400, 170),
            self.call("shared", "b4-0001", "synth-b4", 300, 110),
        ]
        base = {1: {"prompt_tokens": 150, "completion_tokens": 300}, 2: {"prompt_tokens": 150, "completion_tokens": 300}}
        got = controls.added_tokens(ledger, base)
        self.assertEqual(got["paired"], 2)
        self.assertEqual(got["output_mean"], 75.0)
        self.assertEqual(got["input_mean"], 35.0)


if __name__ == "__main__":
    unittest.main()
