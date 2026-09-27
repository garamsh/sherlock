"""Static checks of the episode files corpus.py writes."""

import json
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent


def load(name):
    return json.load(open(HERE / name))


class UnmixedTest(unittest.TestCase):
    """Issue #870: every evidence statement opens a batch, and what shares
    its batch is its own exchange. Seeded batches are checked here; a live
    statement's batch is closed at run time by the align step after it, and
    the harness checks the stored batch (harness/run.go unmixedBatch)."""

    def setUp(self):
        self.corpus = load("unmixed-episodes.json")

    def walk(self, episode):
        """Yield (offset, kind, payload) with offsets in messages since the
        base; None after a live turn until an align step closes its batch."""
        offset = 0
        for step in episode["steps"]:
            kind = step["kind"]
            if kind == "block":
                yield offset, kind, None
                offset = None if offset is None else offset + 2 * len(self.corpus["blocks"][step["index"]])
            elif kind == "pad":
                yield offset, kind, None
                offset = None if offset is None else offset + 2
            elif kind == "seed":
                for p in step["pairs"]:
                    yield offset, "pair", p
                    offset = None if offset is None else offset + 2
            elif kind == "live":
                yield offset, kind, step["content"]
                offset = None
            elif kind == "align":
                yield offset, kind, None
                offset = 0  # the harness closes the batch, so what follows starts one

    def test_every_statement_opens_a_batch_of_its_own_exchange(self):
        for e in self.corpus["episodes"]:
            if "oracle" not in e:
                continue
            steps = list(self.walk(e))
            found = False
            for i, (offset, kind, payload) in enumerate(steps):
                if kind == "pair" and payload[0]["content"] == e["oracle"]:
                    found = True
                    self.assertIsNotNone(offset, e["id"])
                    self.assertEqual(offset % 4, 0, f"{e['id']}: seeded statement opens a batch")
                    nxt = steps[i + 1]
                    self.assertEqual(nxt[1], "pair", e["id"])
                    self.assertIn(nxt[2][0]["content"], e["followups"], f"{e['id']}: the batch's other pair is its follow-up")
                if kind == "live" and payload == e["oracle"]:
                    found = True
                    self.assertIsNotNone(offset, e["id"])
                    self.assertEqual(offset % 4, 0, f"{e['id']}: live statement opens a batch")
                    self.assertEqual(steps[i + 1][1], "align", f"{e['id']}: an align step closes its batch")
            self.assertTrue(found, f"{e['id']}: the evidence statement is in the episode")

    def test_every_live_turn_but_the_probe_is_aligned(self):
        for e in self.corpus["episodes"]:
            kinds = [s["kind"] for s in e["steps"]]
            lives = [i for i, k in enumerate(kinds) if k == "live"]
            for i in lives[:-1]:
                self.assertEqual(kinds[i + 1], "align", f"{e['id']} step {i}")

    def test_the_control_holds_on_the_mixed_corpus(self):
        """The same walk finds #864's evidence batches mixed, so it can fail."""
        self.corpus = load("confirm-episodes.json")
        mixed = 0
        for e in self.corpus["episodes"]:
            steps = list(self.walk(e))
            for i, (offset, kind, payload) in enumerate(steps):
                if kind == "live" and payload == e.get("oracle") and steps[i + 1][1] != "align":
                    mixed += 1
        self.assertGreater(mixed, 0)


if __name__ == "__main__":
    unittest.main()
