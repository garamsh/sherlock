"""Unit tests of the scorer, and the controls the preregistration names."""

import json
import tempfile
import unittest
from pathlib import Path

import score

HERE = Path(__file__).resolve().parent


def turn(reply="", sent=None, written=None, tools=None):
    return {"reply": reply, "sent": sent or [], "written": written or {}, "tools": tools or []}


class CheckTest(unittest.TestCase):
    def test_the_output_format_rules(self):
        self.assertTrue(score.check({"kind": "prefix", "value": "STATUS:"}, turn("STATUS: all good")))
        self.assertFalse(score.check({"kind": "prefix", "value": "STATUS:"}, turn("All good: STATUS: below")))
        self.assertTrue(score.check({"kind": "suffix", "value": "— Juniper"}, turn("Done.\n— Juniper")))
        self.assertFalse(score.check({"kind": "suffix", "value": "— Juniper"}, turn("— Juniper, done")))
        self.assertTrue(score.check({"kind": "all_lowercase"}, turn("all done here")))
        self.assertFalse(score.check({"kind": "all_lowercase"}, turn("All done here")))
        self.assertTrue(score.check({"kind": "all_uppercase"}, turn("ALL DONE HERE")))
        self.assertFalse(score.check({"kind": "all_uppercase"}, turn("All done here")))
        self.assertTrue(score.check({"kind": "roman_list"}, turn("I. water\nII. flour\nIII. salt")))
        self.assertFalse(score.check({"kind": "roman_list"}, turn("1. water\n2. flour")))
        self.assertTrue(score.check({"kind": "no_bullets"}, turn("Water first, then flour, then salt.")))
        self.assertFalse(score.check({"kind": "no_bullets"}, turn("- water\n- flour")))

    def test_the_code_indentation_rules(self):
        self.assertTrue(score.check({"kind": "tab_indent"}, turn("```python\ndef f():\n\treturn 1\n```")))
        self.assertFalse(score.check({"kind": "tab_indent"}, turn("```python\ndef f():\n    return 1\n```")))
        self.assertTrue(score.check({"kind": "space_indent"}, turn("```python\ndef f():\n    return 1\n```")))
        self.assertFalse(score.check({"kind": "space_indent"}, turn("```python\ndef f():\n\treturn 1\n```")))

    def test_the_naming_rules(self):
        self.assertTrue(score.check({"kind": "kebab_files"}, turn(written={"my-note.txt": "x"})))
        self.assertFalse(score.check({"kind": "kebab_files"}, turn(written={"my_note.txt": "x"})))
        self.assertTrue(score.check({"kind": "snake_files"}, turn(written={"my_note.txt": "x"})))
        self.assertFalse(score.check({"kind": "snake_files"}, turn(written={"my-note.txt": "x"})))

    def test_the_order_rule(self):
        self.assertTrue(score.check({"kind": "order_words", "first": "caveat", "second": "recommend"},
                                    turn("The caveat is cost. My recommendation is to wait.")))
        self.assertFalse(score.check({"kind": "order_words", "first": "caveat", "second": "recommend"},
                                     turn("My recommendation is to wait. The caveat is cost.")))
        # A reply that names neither word does not satisfy the rule.
        self.assertFalse(score.check({"kind": "order_words", "first": "caveat", "second": "recommend"},
                                     turn("Wait for it.")))

    def test_the_tool_rule(self):
        names = [{"name": "file_list"}, {"name": "file_read"}]
        self.assertTrue(score.check({"kind": "tool_before", "first": "file_list", "second": "file_read"},
                                     turn(tools=names)))
        self.assertFalse(score.check({"kind": "tool_before", "first": "file_read", "second": "file_list"},
                                     turn(tools=names)))
        # A turn that never listed the directory does not satisfy the rule.
        self.assertFalse(score.check({"kind": "tool_before", "first": "file_list", "second": "file_read"},
                                     turn(tools=[{"name": "file_read"}])))

    def test_the_composed_checks(self):
        both = {"kind": "all_of", "of": [
            {"kind": "tool_before", "first": "file_list", "second": "file_read"},
            {"kind": "contains", "value": "kiwi-4821"}]}
        tools = [{"name": "file_list"}, {"name": "file_read"}]
        self.assertTrue(score.check(both, turn("It says kiwi-4821", tools=tools)))
        self.assertFalse(score.check(both, turn("It says kiwi-4821", tools=[{"name": "file_read"}])))
        self.assertFalse(score.check(both, turn("I could not read it", tools=tools)))
        forbidden = {"kind": "not", "of": {"kind": "any_of", "of": [{"kind": "prefix", "value": "STATUS:"}]}}
        self.assertTrue(score.check(forbidden, turn("Here are three tips.")))
        self.assertFalse(score.check(forbidden, turn("STATUS: here are three tips.")))


class NearMissTest(unittest.TestCase):
    """The controls the preregistration names: a reply that mentions the
    rule without following it must not count as following it."""

    def test_a_mention_is_not_a_following(self):
        spec = {"kind": "prefix", "value": "STATUS:"}
        self.assertFalse(score.check(spec, turn("You asked for STATUS: lines; here is a plain answer.")))
        self.assertFalse(score.check(spec, turn("Sure — STATUS: is only a label, this is prose.")))

    def test_a_suffix_mention_is_not_a_signature(self):
        spec = {"kind": "suffix", "value": "— Juniper"}
        self.assertFalse(score.check(spec, turn("Sign it with — Juniper when you are done.")))

    def test_a_rule_mentioned_in_a_quote_is_not_an_applied_rule(self):
        spec = {"kind": "all_lowercase"}
        self.assertFalse(score.check(spec, turn('The rule was "all lowercase" but I will not obey it.')))

    def test_an_absent_rule_is_not_an_applied_rule(self):
        spec = {"kind": "prefix", "value": "STATUS:"}
        self.assertFalse(score.check(spec, turn("Here are three practical tips for baking.")))


class StatsTest(unittest.TestCase):
    def test_the_exact_paired_test(self):
        self.assertAlmostEqual(score.binom_two_sided(0, 5), 0.0625)
        self.assertAlmostEqual(score.binom_two_sided(3, 6), 1.0)
        b, c, p = score.mcnemar([1, 1, 1, 0, 1], [0, 0, 1, 1, 1])
        self.assertEqual((b, c), (2, 1))
        self.assertAlmostEqual(p, 1.0)

    def test_the_holm_correction(self):
        self.assertEqual(score.holm([0.01, 0.04]), [0.02, 0.04])
        self.assertEqual(score.holm([0.04, 0.01]), [0.04, 0.02])

    def test_the_wilson_interval(self):
        self.assertEqual(score.wilson(0, 0), [None, None])
        lo, hi = score.wilson(7, 10)
        self.assertTrue(0.39 < lo < 0.40 and 0.89 < hi < 0.90)


class DecisionTest(unittest.TestCase):
    def test_an_arm_that_does_not_beat_the_shipped_structure_stands_down(self):
        tests = {"N+": {"difference": 0.1, "p": 0.01, "p_holm": 0.03},
                 "P": {"difference": -0.1, "p": 0.5, "p_holm": 0.5},
                 "C": {"difference": 0.2, "p": 0.001, "p_holm": 0.003}}
        guard = {"N+": {"hold": True}, "P": {"hold": True}, "C": {"hold": False}}
        verdict = score.decide(tests, guard)
        self.assertEqual(verdict["valid"], ["N+"])
        self.assertEqual(verdict["winner"], "N+")

    def test_no_valid_arm_leaves_the_shipped_structure_standing(self):
        tests = {"N+": {"difference": 0.1, "p": 0.5, "p_holm": 0.5}}
        verdict = score.decide(tests, {"N+": {"hold": True}})
        self.assertEqual(verdict["outcome"], "shipped structure stands")
        self.assertEqual(verdict["valid"], [])

    def test_a_guardrail_failure_makes_the_arm_invalid(self):
        tests = {"N+": {"difference": 0.3, "p": 0.001, "p_holm": 0.003}}
        verdict = score.decide(tests, {"N+": {"hold": False}})
        self.assertEqual(verdict["valid"], [])


class ScoreRunTest(unittest.TestCase):
    """The scorer over a run directory of the shape the harness writes."""

    # Enough paired primary probes for the exact test to reach the
    # threshold: one discordant pair alone cannot.
    PRIMARY_FAMILIES = 20

    def build(self):
        families = []
        for i in range(self.PRIMARY_FAMILIES):
            families.append({
                "id": f"F{i + 1:02d}", "family": "primary", "task": "list", "domain": f"d{i}",
                "rule": "status_prefix", "new_rule": None, "files": {},
                "steps": [
                    {"kind": "live", "content": "a", "correction": False, "probe": None, "instance": 1},
                    {"kind": "live", "content": "b", "correction": True, "probe": None, "instance": 1},
                    {"kind": "block", "index": 0, "content": "", "correction": False, "probe": None, "instance": None},
                    {"kind": "live", "content": "c", "correction": False,
                     "probe": {"check": {"kind": "prefix", "value": "STATUS:"}}, "instance": 2},
                ]})
        families.append({
            "id": "C01", "family": "control", "task": "list", "domain": "control", "rule": None,
            "new_rule": None, "files": {},
            "steps": [
                {"kind": "live", "content": "d", "correction": False,
                 "probe": {"check": {"kind": "not", "of": {"kind": "any_of", "of": [{"kind": "prefix", "value": "STATUS:"}]}}},
                 "instance": i}
                for i in (1, 2, 3)
            ]})
        return {
            "window": 32, "distance_messages": 40, "seed": 1153, "blocks": [],
            "families": families,
        }

    def write(self, root, corpus_path, replies):
        """One arm's results: the primary probes and the control probes."""
        corpus_path.write_text(json.dumps(self.build()))
        for arm, (primary, control) in replies.items():
            (root / arm).mkdir()
            lines = []
            for i in range(self.PRIMARY_FAMILIES):
                if primary is None:
                    continue
                lines.append(json.dumps({"family": f"F{i + 1:02d}", "instance": 2, "reply": primary,
                                         "sent": [], "written": {}, "tools": []}))
            for i, reply in enumerate(control, start=1):
                lines.append(json.dumps({"family": "C01", "instance": i, "reply": reply,
                                         "sent": [], "written": {}, "tools": []}))
            (root / arm / "results.jsonl").write_text("\n".join(lines) + "\n")

    def test_a_candidate_that_beats_the_shipped_structure_is_valid(self):
        plain = ["plain answer"] * 3
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            corpus_path = root / "corpus.json"
            self.write(root, corpus_path, {
                "S": ("no prefix here", plain),
                "N+": ("STATUS: here", plain),
                "P": (None, plain),
                "C": (None, plain),
            })
            out = score.score(root, corpus_path)
        self.assertEqual(out["tests"]["N+"]["candidate_only"], self.PRIMARY_FAMILIES)
        self.assertEqual(out["tests"]["N+"]["shipped_only"], 0)
        self.assertLess(out["tests"]["N+"]["p_holm"], 0.05)
        self.assertEqual(out["verdict"]["valid"], ["N+"])
        self.assertEqual(out["verdict"]["winner"], "N+")

    def test_a_candidate_that_invents_a_rule_fails_the_control_guardrail(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            corpus_path = root / "corpus.json"
            self.write(root, corpus_path, {
                "S": ("no prefix here", ["plain answer"] * 3),
                "N+": ("STATUS: here", ["STATUS: invented", "STATUS: invented", "plain answer"]),
                "P": (None, ["plain answer"] * 3),
                "C": (None, ["plain answer"] * 3),
            })
            out = score.score(root, corpus_path)
        self.assertEqual(out["guardrails"]["N+"]["detail"]["control"]["difference"], -2)
        self.assertFalse(out["guardrails"]["N+"]["hold"])
        self.assertNotIn("N+", out["verdict"]["valid"])
        self.assertEqual(out["verdict"]["outcome"], "shipped structure stands")

    def test_one_lost_control_probe_is_within_the_stated_margin(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            corpus_path = root / "corpus.json"
            self.write(root, corpus_path, {
                "S": ("no prefix here", ["plain answer"] * 3),
                "N+": ("STATUS: here", ["STATUS: invented", "plain answer", "plain answer"]),
                "P": (None, ["plain answer"] * 3),
                "C": (None, ["plain answer"] * 3),
            })
            out = score.score(root, corpus_path)
        self.assertEqual(out["guardrails"]["N+"]["detail"]["control"]["difference"], -1)
        self.assertTrue(out["guardrails"]["N+"]["hold"])

    def test_an_arm_that_answered_no_primary_probe_is_not_valid(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            corpus_path = root / "corpus.json"
            self.write(root, corpus_path, {
                "S": ("no prefix here", ["plain answer"] * 3),
                "N+": (None, ["plain answer"] * 3),
                "P": (None, ["plain answer"] * 3),
                "C": (None, ["plain answer"] * 3),
            })
            out = score.score(root, corpus_path)
        self.assertIsNone(out["tests"]["P"]["difference"])
        self.assertEqual(out["verdict"]["valid"], [])


if __name__ == "__main__":
    unittest.main()
