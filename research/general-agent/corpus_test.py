"""Static checks of the corpus corpus.py writes.

    PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s research/general-agent -p '*_test.py'
"""

import json
import re
import unittest
from pathlib import Path

import corpus
import score

HERE = Path(__file__).resolve().parent


def live_steps(f):
    return [s for s in f["steps"] if s["kind"] == "live"]


def messages_between(c, f, first, last):
    """The filler messages seeded between two steps of a family: the
    distance the later step stands from the earlier one, not counting what
    the live turns between them add."""
    return sum(2 * len(c["blocks"][s["index"]]) for s in f["steps"][first + 1:last] if s["kind"] == "block")


class CorpusTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.corpus = corpus.build()
        cls.families = cls.corpus["families"]

    def test_the_committed_corpus_is_what_the_generator_writes(self):
        committed = json.loads((HERE / "corpus.json").read_text())
        self.assertEqual(committed, json.loads(json.dumps(self.corpus)))

    def test_four_kinds_each_with_primary_control_and_reversal_families(self):
        counts = {}
        for f in self.families:
            counts[(f["kind"], f["group"])] = counts.get((f["kind"], f["group"]), 0) + 1
        for kind in corpus.KINDS:
            self.assertEqual(counts[(kind, "primary")], corpus.PRIMARY_PER_KIND)
            self.assertEqual(counts[(kind, "control")], corpus.CONTROL_PER_KIND)
            self.assertEqual(counts[(kind, "reversal")], corpus.REVERSAL_PER_KIND)
        self.assertEqual(len({f["id"] for f in self.families}), len(self.families))

    def test_a_family_asks_three_instances(self):
        for f in self.families:
            self.assertEqual(sorted({s["instance"] for s in live_steps(f)}), [1, 2, 3], f["id"])

    def test_every_check_is_one_the_scorer_knows(self):
        def walk(spec):
            if spec["kind"] in ("all_of", "any_of"):
                for s in spec["of"]:
                    walk(s)
            elif spec["kind"] == "not":
                walk(spec["of"])
            else:
                # An unknown kind raises; a known one returns a bool on an
                # empty turn.
                self.assertIsInstance(score.check(spec, {}, s_task, {}), bool)

        for f in self.families:
            for s in live_steps(f):
                s_task = s["task"]
                walk(s["task"])
                if s["probe"]:
                    walk(s["probe"]["rule"])

    def test_the_correction_is_given_on_instance_one_and_restated_only_by_a_reversal(self):
        for f in self.families:
            corrections = [s["instance"] for s in live_steps(f) if s["correction"]]
            expected = {"control": [], "primary": [1], "reversal": [1, 3]}[f["group"]]
            self.assertEqual(corrections, expected, f["id"])

    def test_probes(self):
        for f in self.families:
            probes = [s["instance"] for s in live_steps(f) if s["probe"]]
            expected = {"control": [1, 2, 3], "primary": [2, 3], "reversal": [3]}[f["group"]]
            self.assertEqual(probes, expected, f["id"])
            if f["group"] == "primary":
                for s in live_steps(f):
                    if s["probe"]:
                        self.assertEqual(s["probe"]["rule"], corpus.RULES[f["rule"]]["check"], f["id"])

    def test_a_probe_does_not_restate_its_rule(self):
        """Control: the probe text never carries the rule's statement, so a
        held rule was kept rather than read; the correction step's text, its
        near miss, is found by the same comparison."""
        for f in self.families:
            if f["group"] != "primary":
                continue
            statement = corpus.RULES[f["rule"]]["statement"]
            correction = [s for s in live_steps(f) if s["correction"]][0]
            self.assertIn(statement, correction["content"], f["id"])
            for s in live_steps(f):
                if s["probe"]:
                    self.assertNotIn(statement, s["content"], f["id"])

    def test_the_correction_has_left_the_window_at_every_probe(self):
        """Control: the filler between the correction and each probe exceeds
        the window, so the correction is outside what a restart restores.
        The near miss, a corpus whose distance is the window less eight, is
        refused by the same comparison."""
        c = self.corpus
        self.assertGreater(c["distance_messages"], c["window"])
        for f in self.families:
            if f["group"] == "control":
                continue
            correction = max(i for i, s in enumerate(f["steps"]) if s["kind"] == "live" and s["correction"]
                             and s["instance"] == 1)
            for i, s in enumerate(f["steps"]):
                if s["kind"] == "live" and s["probe"] and not s["correction"]:
                    self.assertGreater(messages_between(c, f, correction, i), c["window"], f["id"])
        near = dict(c, blocks=[b[:(c["window"] - 8) // 2] for b in c["blocks"]])
        f = self.families[0]
        probe = next(i for i, s in enumerate(f["steps"]) if s["kind"] == "live" and s["probe"])
        self.assertLessEqual(messages_between(near, f, 1, probe), near["window"])

    def test_the_filler_blocks_hold_the_stated_distance(self):
        self.assertEqual(len(self.corpus["blocks"]), corpus.FILLER_BLOCKS)
        for b in self.corpus["blocks"]:
            self.assertEqual(2 * len(b), corpus.DISTANCE_MESSAGES)
            for pair in b:
                self.assertEqual([m["role"] for m in pair], ["user", "assistant"])

    def test_a_reversal_probe_wants_the_new_rule_and_not_the_old(self):
        for f in self.families:
            if f["group"] != "reversal":
                continue
            probe = [s for s in live_steps(f) if s["probe"]][0]
            self.assertEqual(probe["probe"]["rule"], {"kind": "all_of", "of": [
                corpus.RULES[f["new_rule"]]["check"], {"kind": "not", "of": corpus.RULES[f["rule"]]["check"]}]})

    def test_a_continuation_probe_names_neither_the_guide_nor_the_part(self):
        """The next part is known only from the first session, so a probe
        that names it would not test continuation."""
        for f in self.families:
            if f["kind"] != "continuation":
                continue
            parts = live_steps(f)[0]["task"]["parts"]
            for s in live_steps(f)[1:]:
                if s["instance"] > 1:
                    for name in parts:
                        self.assertNotIn(name, s["content"], f["id"])
                    self.assertNotIn(f["domain"], s["content"], f["id"])

    def test_the_file_tasks_have_their_data_and_one_right_answer(self):
        for f in self.families:
            if f["kind"] != "tools":
                continue
            for s in live_steps(f):
                task = s["task"]
                # Every job's files exist in the starting workspace.
                job = re.search(r"\bjob\d\b", s["content"]).group(0)
                self.assertTrue(any(p.startswith(job + "/") for p in f["files"]), f["id"])
                if task["kind"] == "output_number":
                    total = 0
                    for p, content in f["files"].items():
                        if p.startswith(job + "/ledger/"):
                            total += sum(int(ln.split(",")[1]) for ln in content.splitlines()[1:])
                    self.assertEqual(total, task["value"], f["id"])


if __name__ == "__main__":
    unittest.main()
