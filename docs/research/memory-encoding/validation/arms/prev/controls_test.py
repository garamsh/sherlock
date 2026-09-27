"""Tests of P's controls: the hop pairs, the continuing share and the token
pairing."""

import unittest

import controls

# Two stream batches after a base of one: mids 2 and 3. C2-001's hops open
# batches 3 and 2, in that order, so its pair is linked when mid 3 links
# mid 2 whichever hop comes first.
CORPUS = {
    "stream": [{"id": i} for i in range(5, 13)],
    "chains": [
        {"id": "C1-001", "family": "C1", "k": 1, "hops": [{"source": 5}]},
        {"id": "C2-001", "family": "C2", "k": 2, "hops": [{"source": 9}, {"source": 5}]},
    ],
}


class HopPairsTest(unittest.TestCase):
    def test_a_pair_is_linked_from_the_later_mid_whichever_hop_comes_first(self):
        recorded = [{"first": 5, "given": [1], "links": []}, {"first": 9, "given": [2, 1], "links": [2]}]
        got = controls.hop_pairs(CORPUS, recorded)
        self.assertEqual({"C2": {"pairs": 1, "linked": 1, "given": 1}}, got["families"], "a one-hop chain has no pair")
        self.assertEqual(1.0, got["pooled"]["share"])

    def test_a_pair_given_but_not_chosen_is_not_linked(self):
        recorded = [{"first": 5, "given": [1], "links": []}, {"first": 9, "given": [2, 1], "links": [1]}]
        got = controls.hop_pairs(CORPUS, recorded)
        self.assertEqual({"pairs": 1, "linked": 0, "given": 1, "share": 0.0}, got["pooled"])


class ContinuingTest(unittest.TestCase):
    def test_counts_batches_with_a_link_over_every_stream_batch(self):
        recorded = [{"first": 5, "links": [1]}, {"first": 9, "links": [], "failed": "malformed continues"}]
        self.assertEqual({"stream_batches": 2, "recorded": 2, "failed": 1, "continuing": 1, "share": 0.5},
                         controls.continuing(CORPUS, recorded))
        self.assertEqual(0.5, controls.continuing(CORPUS, recorded[:1])["share"], "an unrecorded batch counts as not continuing")


class AddedTokensTest(unittest.TestCase):
    def test_pairs_each_call_with_the_shipped_call_of_its_batch(self):
        ledger = [
            {"phase": "synth-b4", "episode": "b4-0001", "status": 200, "prompt_tokens": 300, "completion_tokens": 200},
            {"phase": "synth-b4", "episode": "b4-0002", "status": 200, "prompt_tokens": 310, "completion_tokens": 190},
            {"phase": "links-b4-prev", "episode": "b4-00002", "status": 200, "prompt_tokens": 400, "completion_tokens": 210},
            {"phase": "links-b4-prev", "episode": "b4-00003", "status": 200, "prompt_tokens": 0, "completion_tokens": 0, "cached": True},
            {"phase": "links-b4-prev", "episode": "b4-00003", "status": 200, "prompt_tokens": 430, "completion_tokens": 180},
        ]
        got = controls.added_tokens(ledger, "b4-prev", base_batches=1)
        self.assertEqual({"paired": 2, "input_mean": 110.0, "input_median": 110.0, "output_mean": 0.0, "output_median": 0.0}, got)


if __name__ == "__main__":
    unittest.main()
