"""Static checks of corpus.json: the rules every arm's measurement rests on."""

import json
import unittest
from pathlib import Path

import corpus as gen

HERE = Path(__file__).resolve().parent


class CorpusTest(unittest.TestCase):
    def setUp(self):
        self.corpus = json.load(open(HERE / "corpus.json"))
        self.stream = {m["id"]: m for m in self.corpus["stream"]}

    def test_every_hop_exchange_fills_one_batch_of_four_alone(self):
        batches = 0
        for c in self.corpus["chains"]:
            for i, h in enumerate(c["hops"]):
                s = h["source"]
                self.assertEqual((s - 1) % 4, 0, f"{c['id']} hop {i + 1} does not open a batch")
                batch = [self.stream[s + j] for j in range(4)]
                self.assertEqual([m["chain"] for m in batch], [c["id"]] * 4)
                self.assertEqual([m["hop"] for m in batch], [i + 1] * 4)
                self.assertEqual(batch[0]["content"], h["statement"])
                batches += 1
        # Control: the same walk sees a batch that is not a single exchange.
        tail = [m for m in self.corpus["stream"] if m["chain"] == "tail"]
        self.assertGreater(len(tail), 0)
        self.assertEqual(batches, sum(len(c["hops"]) for c in self.corpus["chains"]))

    def test_hops_of_one_chain_share_no_mid_or_group_at_any_setting(self):
        # Batch 8 mids span 8 messages; groups of 8 batch-4 mids span 32.
        for c in self.corpus["chains"]:
            blocks = [(h["source"] - 1) // 32 for h in c["hops"]]
            self.assertEqual(len(set(blocks)), len(blocks), c["id"])
        # Control: two hops in one 32-message block would be caught.
        self.assertEqual(len({(1 - 1) // 32, (29 - 1) // 32}), 1)

    def test_later_hop_words_appear_only_in_the_hop_before(self):
        for c in self.corpus["chains"]:
            self.assertTrue(gen.lexical_ok(c), c["id"])
        # Control: a query naming hop 2's entity breaks the rule.
        c = next(c for c in self.corpus["chains"] if c["k"] == 2)
        leaked = dict(c, query=c["query"] + " " + c["hops"][1]["ends"][1])
        self.assertFalse(gen.lexical_ok(leaked))

    def test_stream_closes_batch_8_and_groups_of_8(self):
        n = self.corpus["base_messages"] + len(self.corpus["stream"])
        self.assertEqual(len(self.corpus["stream"]) % gen.ALIGN, 0)
        self.assertEqual(n % 32, 0)
        ids = [m["id"] for m in self.corpus["stream"]]
        self.assertEqual(ids, list(range(gen.BASE_MESSAGES + 1, gen.BASE_MESSAGES + 1 + len(ids))))

    def test_families_and_guards_have_their_sizes(self):
        counts = {}
        for c in self.corpus["chains"]:
            counts[c["family"]] = counts.get(c["family"], 0) + 1
            self.assertEqual(len(c["hops"]), c["k"])
        self.assertEqual(counts, gen.SIZES)
        guards = {}
        for g in self.corpus["guards"]:
            guards[g["family"]] = guards.get(g["family"], 0) + 1
        self.assertEqual(guards, gen.GUARDS)

    def test_every_kept_chain_passed_the_screen(self):
        screen = {s["id"]: s for s in json.load(open(HERE / "evidence" / "screen.json"))["chains"]}
        for c in self.corpus["chains"]:
            self.assertTrue(screen[c["pool_id"]]["pass"], c["id"])
            self.assertTrue(all(r > screen_top() for r in screen[c["pool_id"]]["ranks"][1:]), c["id"])


def screen_top():
    return json.load(open(HERE / "evidence" / "screen.json"))["top"]


if __name__ == "__main__":
    unittest.main()
