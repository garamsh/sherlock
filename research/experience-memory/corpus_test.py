"""Static checks of the corpus corpus.py writes."""

import json
import unittest
from pathlib import Path

import corpus
import score

HERE = Path(__file__).resolve().parent


class CorpusTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.corpus = corpus.build()

    def test_fifty_families_of_three_instances(self):
        families = self.corpus["families"]
        self.assertEqual(len(families), 50)
        kinds = {}
        for f in families:
            kinds[f["family"]] = kinds.get(f["family"], 0) + 1
        self.assertEqual(kinds["primary"], 40)
        self.assertEqual(kinds["control"], 5)
        self.assertEqual(kinds["reversal"], 5)

    def test_a_family_asks_three_instances_of_one_kind_of_task(self):
        for f in self.corpus["families"]:
            instances = [s["instance"] for s in f["steps"] if s["kind"] == "live"]
            self.assertEqual(sorted(set(instances)), [1, 2, 3], f["id"])

    def test_the_correction_is_given_once_on_the_first_instance(self):
        for f in self.corpus["families"]:
            if f["family"] == "control":
                self.assertEqual([s for s in f["steps"] if s["correction"]], [], f["id"])
                continue
            corrections = [s for s in f["steps"] if s["correction"]]
            self.assertTrue(corrections, f["id"])
            # A primary family corrects once, on the first instance; a
            # reversal family corrects there and again on the third, where
            # the rule changes.
            expected = [1] if f["family"] == "primary" else [1, 3]
            self.assertEqual(sorted({s["instance"] for s in corrections}), expected, f["id"])

    def test_the_correction_turn_asks_for_the_same_task_again(self):
        """The turn that applies the rule is a turn of the same task, so a
        case or a lesson stored under that turn's task is about the task the
        next instance asks for."""
        for f in self.corpus["families"]:
            if f["family"] == "control":
                continue
            first = [s for s in f["steps"] if s["kind"] == "live" and s["instance"] == 1]
            self.assertEqual(len(first), 2, f["id"])
            self.assertTrue(first[1]["correction"], f["id"])
            self.assertIn(first[0]["content"], first[1]["content"], f["id"])

    def test_a_primary_family_probes_the_second_and_third_instance(self):
        for f in self.corpus["families"]:
            if f["family"] != "primary":
                continue
            probes = [s for s in f["steps"] if s["probe"]]
            self.assertEqual([s["instance"] for s in probes], [2, 3], f["id"])
            for s in probes:
                self.assertEqual(s["probe"]["check"], corpus.RULES[f["rule"]]["check"], f["id"])

    def test_a_control_family_probes_every_instance_for_an_invented_rule(self):
        for f in self.corpus["families"]:
            if f["family"] != "control":
                continue
            probes = [s for s in f["steps"] if s["probe"]]
            self.assertEqual([s["instance"] for s in probes], [1, 2, 3], f["id"])
            for s in probes:
                self.assertEqual(s["probe"]["check"],
                                 {"kind": "not", "of": {"kind": "any_of", "of": corpus.FORBIDDEN}}, f["id"])

    def test_a_reversal_family_asks_for_the_new_rule_and_not_the_old(self):
        for f in self.corpus["families"]:
            if f["family"] != "reversal":
                continue
            third = [s for s in f["steps"] if s["instance"] == 3][0]
            self.assertTrue(third["correction"], f["id"])
            self.assertEqual(third["probe"]["check"], {"kind": "all_of", "of": [
                corpus.RULES[f["new_rule"]]["check"],
                {"kind": "not", "of": corpus.RULES[f["rule"]]["check"]},
            ]}, f["id"])

    def test_the_correction_has_left_the_window_by_the_next_instance(self):
        """The distance between the correction and the next instance is
        larger than the rolling window, so the window cannot carry it."""
        self.assertGreater(self.corpus["distance_messages"], self.corpus["window"])
        for f in self.corpus["families"]:
            if f["family"] == "control":
                continue
            kinds = [s["kind"] for s in f["steps"]]
            self.assertEqual(kinds, ["live", "live", "block", "live", "block", "live"], f["id"])
            # The correction precedes the first probed instance, and the
            # filler block that separates them is what carries it out of
            # the window.
            correction = [i for i, s in enumerate(f["steps"]) if s["correction"]][0]
            first_probe = [i for i, s in enumerate(f["steps"]) if s.get("probe")][0]
            self.assertLess(correction, first_probe, f["id"])
            self.assertEqual(f["steps"][first_probe - 1]["kind"], "block", f["id"])

    def test_two_families_never_ask_about_the_same_thing(self):
        domains = [f["domain"] for f in self.corpus["families"]]
        self.assertEqual(len(domains), len(set(domains)))
        subjects = [f["steps"][0]["content"] for f in self.corpus["families"]]
        self.assertEqual(len(subjects), len(set(subjects)))

    def test_every_rule_is_used_by_some_family(self):
        used = {f["rule"] for f in self.corpus["families"] if f["rule"]}
        self.assertEqual(used, set(corpus.RULES))

    def test_every_check_kind_is_one_the_scorer_knows(self):
        kinds = set()

        def walk(spec):
            kinds.add(spec["kind"])
            subs = spec.get("of")
            if isinstance(subs, list):
                for sub in subs:
                    walk(sub)
            elif isinstance(subs, dict):
                walk(subs)

        for f in self.corpus["families"]:
            for s in f["steps"]:
                if s.get("probe"):
                    walk(s["probe"]["check"])
        self.assertTrue(kinds)
        for kind in kinds:
            self.assertIn(kind, score.KINDS, kind)

    def test_the_written_corpus_matches_what_the_generator_builds(self):
        path = HERE / "corpus.json"
        if not path.exists():
            self.skipTest("corpus.json has not been written yet")
        with open(path) as f:
            self.assertEqual(json.load(f), self.corpus)


if __name__ == "__main__":
    unittest.main()
