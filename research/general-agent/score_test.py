"""Unit tests of the scorer, and the instrument controls the preregistration
names. Every check is shown to accept the work it names and to reject a
near miss of it.

    PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s research/general-agent -p '*_test.py'
"""

import json
import tempfile
import unittest
from pathlib import Path

import corpus
import score


def write(path, content):
    return {"name": "file_write", "input": {"path": path, "content": content}}


def read(path):
    return {"name": "file_read", "input": {"path": path}}


def listing(path="."):
    return {"name": "file_list", "input": {"path": path}}


def turn(reply="", sent=None, tools=None):
    return {"reply": reply, "sent": sent or [], "tools": tools or []}


def holds(spec, t, task=None, before=None):
    return score.check(spec, t, task or {}, before or {})


# A reference solution of every coding task, and a near miss that gets one
# case wrong.
REFERENCE = {
    "slugify": (
        "import re\ndef slugify(text):\n    return re.sub(r'[^a-z0-9]+', '-', text.lower()).strip('-')\n",
        "import re\ndef slugify(text):\n    return re.sub(r'[^a-z0-9]+', '-', text.lower())\n"),
    "parse_duration": (
        "import re\ndef parse_duration(text):\n    if not re.fullmatch(r'(\\d+[hms])+', text):\n        raise ValueError(text)\n"
        "    return sum(int(n) * {'h': 3600, 'm': 60, 's': 1}[u] for n, u in re.findall(r'(\\d+)([hms])', text))\n",
        "import re\ndef parse_duration(text):\n"
        "    return sum(int(n) * {'h': 3600, 'm': 60, 's': 1}[u] for n, u in re.findall(r'(\\d+)([hms])', text))\n"),
    "roman_to_int": (
        "def roman_to_int(numeral):\n    v = {'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100, 'D': 500, 'M': 1000}\n"
        "    total = 0\n    for i, c in enumerate(numeral):\n"
        "        total += -v[c] if i + 1 < len(numeral) and v[c] < v[numeral[i + 1]] else v[c]\n    return total\n",
        "def roman_to_int(numeral):\n    v = {'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100, 'D': 500, 'M': 1000}\n"
        "    return sum(v[c] for c in numeral)\n"),
    "word_counts": (
        "import re\ndef word_counts(text):\n    out = {}\n    for w in re.findall('[A-Za-z]+', text):\n"
        "        out[w.lower()] = out.get(w.lower(), 0) + 1\n    return out\n",
        "def word_counts(text):\n    out = {}\n    for w in text.lower().split():\n"
        "        out[w] = out.get(w, 0) + 1\n    return out\n"),
    "chunk": (
        "def chunk(items, size):\n    if size < 1:\n        raise ValueError(size)\n"
        "    return [items[i:i + size] for i in range(0, len(items), size)]\n",
        "def chunk(items, size):\n    return [items[i:i + size] for i in range(0, len(items), max(size, 1))]\n"),
    "dedupe": (
        "def dedupe(items):\n    seen, out = set(), []\n    for x in items:\n        if x not in seen:\n"
        "            seen.add(x)\n            out.append(x)\n    return out\n",
        "def dedupe(items):\n    return sorted(set(items))\n"),
    "median": (
        "def median(values):\n    if not values:\n        raise ValueError('empty')\n    s = sorted(values)\n"
        "    n = len(s)\n    return float(s[n // 2]) if n % 2 else (s[n // 2 - 1] + s[n // 2]) / 2\n",
        "def median(values):\n    if not values:\n        raise ValueError('empty')\n    s = sorted(values)\n"
        "    return float(s[len(s) // 2])\n"),
    "run_length": (
        "import itertools\ndef run_length(text):\n"
        "    return ''.join(c + str(len(list(g))) for c, g in itertools.groupby(text))\n",
        "import itertools\ndef run_length(text):\n"
        "    return ''.join(c + str(min(len(list(g)), 9)) for c, g in itertools.groupby(text))\n"),
    "merge_intervals": (
        "def merge_intervals(intervals):\n    out = []\n    for s, e in sorted(intervals):\n"
        "        if out and s <= out[-1][1]:\n            out[-1][1] = max(out[-1][1], e)\n"
        "        else:\n            out.append([s, e])\n    return out\n",
        "def merge_intervals(intervals):\n    out = []\n    for s, e in sorted(intervals):\n"
        "        if out and s < out[-1][1]:\n            out[-1][1] = max(out[-1][1], e)\n"
        "        else:\n            out.append([s, e])\n    return out\n"),
    "initials": (
        "def initials(name):\n    return ''.join(w[0].upper() + '.' for w in name.split())\n",
        "def initials(name):\n    return '.'.join(w[0].upper() for w in name.split())\n"),
    "flatten": (
        "def flatten(nested):\n    out = []\n    for x in nested:\n"
        "        out.extend(flatten(x) if isinstance(x, list) else [x])\n    return out\n",
        "def flatten(nested):\n    out = []\n    for x in nested:\n"
        "        out.extend(x if isinstance(x, list) else [x])\n    return out\n"),
    "to_snake": (
        "def to_snake(name):\n    return ''.join('_' + c.lower() if c.isupper() and i else c.lower()"
        " for i, c in enumerate(name))\n",
        "import re\ndef to_snake(name):\n    return re.sub(r'(?<!^)(?=[A-Z][a-z])', '_', name).lower()\n"),
}


class CodeTaskTest(unittest.TestCase):
    """Control: every reference solution passes its cases, and its near miss,
    which gets one case wrong, does not."""

    def test_every_function_has_a_reference_and_a_near_miss(self):
        self.assertEqual(sorted(REFERENCE), sorted(f["function"] for f in corpus.FUNCTIONS))

    def test_the_reference_passes_and_the_near_miss_fails(self):
        for fn in corpus.FUNCTIONS:
            good, near = REFERENCE[fn["function"]]
            with self.subTest(fn["function"]):
                self.assertTrue(score.run_cases(good, fn["function"], fn["cases"]))
                self.assertFalse(score.run_cases(near, fn["function"], fn["cases"]))

    def test_a_module_that_never_returns_fails_within_the_timeout(self):
        fn = corpus.FUNCTIONS[0]
        hang = REFERENCE[fn["function"]][0] + "while True:\n    pass\n"
        self.assertFalse(score.run_cases(hang, fn["function"], fn["cases"]))
        # Control: the same module without the loop passes.
        self.assertTrue(score.run_cases(REFERENCE[fn["function"]][0], fn["function"], fn["cases"]))

    def test_the_sandbox_has_no_network_and_no_home(self):
        probe = ("import os, socket\ndef slugify(text):\n"
                 "    if os.path.exists(os.path.expanduser('~sauce')) and os.listdir('/home'):\n        return 'home'\n"
                 "    try:\n        socket.create_connection(('1.1.1.1', 53), timeout=2)\n        return 'net'\n"
                 "    except OSError:\n        return 'none'\n")
        self.assertTrue(score.run_cases(probe, "slugify", [[["x"], "none"]]))

    def test_the_task_reads_the_module_this_turn_wrote(self):
        fn = corpus.FUNCTIONS[2]
        task = {"kind": "module_tests", "module": fn["module"], "function": fn["function"], "cases": fn["cases"]}
        good = REFERENCE[fn["function"]][0]
        self.assertTrue(holds(task, turn(tools=[write("src/roman.py", good)]), task))
        # Near miss: the right code under another module name.
        self.assertFalse(holds(task, turn(tools=[write("src/numerals.py", good)]), task))
        # Near miss: a write that failed.
        failed = dict(write("roman.py", good), failed=True)
        self.assertFalse(holds(task, turn(tools=[failed]), task))


class CodeRuleTest(unittest.TestCase):
    task = {"module": "roman", "function": "roman_to_int"}
    body = REFERENCE["roman_to_int"][0]

    def rule(self, rule_id):
        return corpus.RULES[rule_id]["check"]

    def test_the_license_line_must_open_the_module(self):
        good = "# SPDX-License-Identifier: MIT\n" + self.body
        self.assertTrue(holds(self.rule("spdx_mit"), turn(tools=[write("roman.py", good)]), self.task))
        near = "#!/usr/bin/env python3\n# SPDX-License-Identifier: MIT\n" + self.body
        self.assertFalse(holds(self.rule("spdx_mit"), turn(tools=[write("roman.py", near)]), self.task))
        self.assertFalse(holds(self.rule("spdx_apache"), turn(tools=[write("roman.py", good)]), self.task))

    def test_the_module_directory(self):
        self.assertTrue(holds(self.rule("src_dir"), turn(tools=[write("src/roman.py", self.body)]), self.task))
        self.assertFalse(holds(self.rule("src_dir"), turn(tools=[write("source/roman.py", self.body)]), self.task))
        self.assertFalse(holds(self.rule("src_dir"), turn(tools=[write("roman.py", self.body),
                                                                  write("src/README.md", "x")]), self.task))
        self.assertTrue(holds(self.rule("lib_dir"), turn(tools=[write("lib/roman.py", self.body)]), self.task))

    def test_the_test_file(self):
        test = "from roman import roman_to_int\n\ndef test_three():\n    assert roman_to_int('III') == 3\n"
        good = turn(tools=[write("roman.py", self.body), write("test_roman.py", test)])
        self.assertTrue(holds(self.rule("test_file"), good, self.task))
        # Near miss: a test file with no test function.
        empty = turn(tools=[write("roman.py", self.body), write("test_roman.py", "import roman\n")])
        self.assertFalse(holds(self.rule("test_file"), empty, self.task))
        # Near miss: a test file for another module.
        other = turn(tools=[write("roman.py", self.body), write("test_numerals.py", test)])
        self.assertFalse(holds(self.rule("test_file"), other, self.task))
        self.assertFalse(holds(self.rule("no_test_file"), good, self.task))
        self.assertTrue(holds(self.rule("no_test_file"), turn(tools=[write("roman.py", self.body)]), self.task))

    def test_type_hints_cover_every_parameter_and_the_return(self):
        good = "def roman_to_int(numeral: str) -> int:\n    return 1\n"
        near = "def roman_to_int(numeral: str):\n    return 1\n"
        self.assertTrue(holds(self.rule("type_hints"), turn(tools=[write("roman.py", good)]), self.task))
        self.assertFalse(holds(self.rule("type_hints"), turn(tools=[write("roman.py", near)]), self.task))
        helper = good + "def _value(c) -> int:\n    return 1\n"
        self.assertFalse(holds(self.rule("type_hints"), turn(tools=[write("roman.py", helper)]), self.task))

    def test_docstrings_on_every_function(self):
        good = 'def roman_to_int(numeral):\n    """Convert."""\n    return 1\n'
        near = '"""Module docstring."""\ndef roman_to_int(numeral):\n    # Convert.\n    return 1\n'
        self.assertTrue(holds(self.rule("docstrings"), turn(tools=[write("roman.py", good)]), self.task))
        self.assertFalse(holds(self.rule("docstrings"), turn(tools=[write("roman.py", near)]), self.task))


class WritingTest(unittest.TestCase):
    three = "- Keep the chain clean\n- Check tyre pressure\n- Oil the bicycle chain"

    def test_the_task_is_exactly_three_bullets_about_the_subject(self):
        task = {"kind": "all_of", "of": [{"kind": "bullet_count", "value": 3}, {"kind": "mentions", "value": "bicycle"}]}
        self.assertTrue(holds(task, turn(self.three)))
        # Near misses: a numbered list, four bullets, or the wrong subject.
        self.assertFalse(holds(task, turn("1. Keep it clean\n2. Check tyres\n3. Oil the bicycle chain")))
        self.assertFalse(holds(task, turn(self.three + "\n- Wash it")))
        self.assertFalse(holds(task, turn("- Knead\n- Proof\n- Bake")))

    def test_what_the_person_reads_is_the_sent_message_when_there_is_one(self):
        rule = corpus.RULES["status_prefix"]["check"]
        self.assertTrue(holds(rule, turn("Sent.", sent=["STATUS: done\n" + self.three])))
        # Near miss: the reply carries the prefix, the message the person
        # reads does not.
        self.assertFalse(holds(rule, turn("STATUS: sent", sent=[self.three])))

    def test_the_reply_rules(self):
        r = corpus.RULES
        self.assertTrue(holds(r["status_prefix"]["check"], turn("STATUS: ok\n" + self.three)))
        self.assertFalse(holds(r["status_prefix"]["check"], turn("Here it is. STATUS: ok\n" + self.three)))
        self.assertTrue(holds(r["tldr_prefix"]["check"], turn("TL;DR: three tips\n" + self.three)))
        self.assertFalse(holds(r["tldr_prefix"]["check"], turn(self.three + "\nTL;DR: three tips")))
        self.assertTrue(holds(r["juniper_suffix"]["check"], turn(self.three + "\n— Juniper")))
        self.assertTrue(holds(r["juniper_suffix"]["check"], turn(self.three + "\n- Juniper\n")))
        self.assertFalse(holds(r["juniper_suffix"]["check"], turn("— Juniper\n" + self.three)))
        self.assertTrue(holds(r["mara_suffix"]["check"], turn(self.three + "\n— Mara")))
        self.assertFalse(holds(r["mara_suffix"]["check"], turn(self.three + "\n— Juniper")))
        self.assertTrue(holds(r["lowercase"]["check"], turn(self.three.lower())))
        self.assertFalse(holds(r["lowercase"]["check"], turn(self.three)))
        self.assertTrue(holds(r["uppercase"]["check"], turn(self.three.upper())))
        self.assertFalse(holds(r["uppercase"]["check"], turn(self.three)))

    def test_the_bullet_ending_rules(self):
        r = corpus.RULES
        periods = "- One.\n- Two.\n- Three."
        mixed = "- One\n- Two.\n- Three"
        self.assertTrue(holds(r["bullet_no_period"]["check"], turn(self.three)))
        self.assertFalse(holds(r["bullet_no_period"]["check"], turn(mixed)))
        self.assertTrue(holds(r["bullet_period"]["check"], turn(periods)))
        self.assertFalse(holds(r["bullet_period"]["check"], turn(mixed)))
        # A reply with no bullets keeps neither rule.
        self.assertFalse(holds(r["bullet_no_period"]["check"], turn("Plain prose")))


class ToolsTest(unittest.TestCase):
    def test_every_end_state_accepts_the_right_output_and_rejects_a_near_miss(self):
        cases = [
            ({"kind": "output_number", "name": "total.txt", "value": 210}, "210\n", "209\n"),
            ({"kind": "output_number", "name": "total.txt", "value": 210}, "210.0", "Total: 210"),
            ({"kind": "output_names_file", "name": "found.txt", "value": "bravo.txt"}, "job1/notes/bravo.txt", "brav.txt"),
            ({"kind": "output_lines", "name": "open.txt", "value": ["TODO a #1", "TODO b #3"]},
             "TODO a #1\nTODO b #3\n", "TODO b #3\nTODO a #1\n"),
            ({"kind": "output_counts", "name": "counts.txt", "value": {"api.log": 4, "db.log": 2}},
             "api.log: 4\ndb.log: 2\n", "api.log: 4\ndb.log: 3\n"),
            ({"kind": "output_word", "name": "best.txt", "value": "Hana"}, "Hana\n", "Hana 97\n"),
        ]
        for spec, good, near in cases:
            with self.subTest(spec["kind"]):
                self.assertTrue(holds(spec, turn(tools=[write("out/" + spec["name"], good)]), spec))
                self.assertFalse(holds(spec, turn(tools=[write(spec["name"], near)]), spec))
                # Near miss: the right content under another name.
                self.assertFalse(holds(spec, turn(tools=[write("result.txt", good)]), spec))

    def test_the_order_rules(self):
        r = corpus.RULES
        good = turn(tools=[listing("job1"), read("job1/a.txt")])
        bad = turn(tools=[read("job1/a.txt"), listing("job1")])
        self.assertTrue(holds(r["list_first"]["check"], good))
        self.assertFalse(holds(r["list_first"]["check"], bad))
        self.assertFalse(holds(r["no_list"]["check"], good))
        self.assertTrue(holds(r["no_list"]["check"], turn(tools=[read("job1/a.txt")])))
        # A failed listing is not a listing.
        failed = turn(tools=[dict(listing("job1"), failed=True), read("job1/a.txt")])
        self.assertFalse(holds(r["list_first"]["check"], failed))

    def test_the_output_directory(self):
        task = {"name": "total.txt"}
        r = corpus.RULES
        self.assertTrue(holds(r["out_dir"]["check"], turn(tools=[write("out/total.txt", "1")]), task))
        self.assertFalse(holds(r["out_dir"]["check"], turn(tools=[write("output/total.txt", "1")]), task))
        self.assertFalse(holds(r["out_dir"]["check"], turn(tools=[write("total.txt", "1"), write("out/log.txt", "x")]), task))
        self.assertTrue(holds(r["results_dir"]["check"], turn(tools=[write("results/total.txt", "1")]), task))

    def test_the_worklog(self):
        r = corpus.RULES
        self.assertTrue(holds(r["worklog"]["check"], turn(tools=[write("worklog.md", "- summed the ledger")])))
        self.assertFalse(holds(r["worklog"]["check"], turn(tools=[write("work-log.md", "- summed")])))
        self.assertFalse(holds(r["worklog"]["check"], turn(tools=[read("worklog.md")])))
        self.assertTrue(holds(r["no_worklog"]["check"], turn(tools=[write("total.txt", "1")])))

    def test_the_readback(self):
        task = {"name": "total.txt"}
        rule = corpus.RULES["readback"]["check"]
        self.assertTrue(holds(rule, turn(tools=[write("out/total.txt", "1"), read("out/total.txt")]), task))
        self.assertTrue(holds(rule, turn(tools=[write("./out/total.txt", "1"), read("/out/total.txt")]), task))
        # Near misses: reading before writing, or reading another file.
        self.assertFalse(holds(rule, turn(tools=[read("out/total.txt"), write("out/total.txt", "1")]), task))
        self.assertFalse(holds(rule, turn(tools=[write("out/total.txt", "1"), read("job1/ledger/a.csv")]), task))


class ContinuationTest(unittest.TestCase):
    parts = ["Kestrel", "Osprey", "Heron", "Wren"]
    task = {"kind": "next_part", "path": "guide-x.md", "parts": parts}
    first = "# Guide\n\n## Kestrel\nStart here.\n"
    second = first + "\n## Osprey\nThen this.\n"

    def test_the_next_part_is_the_first_one_the_guide_lacks(self):
        before = {"guide-x.md": self.first}
        self.assertTrue(holds(self.task, turn(tools=[write("guide-x.md", self.second)]), self.task, before))
        # Near misses: skipping a part, rewriting the first part only,
        # dropping the earlier part, or a heading with no text under it.
        skip = self.first + "\n## Heron\nThen this.\n"
        self.assertFalse(holds(self.task, turn(tools=[write("guide-x.md", skip)]), self.task, before))
        self.assertFalse(holds(self.task, turn(tools=[write("guide-x.md", self.first)]), self.task, before))
        self.assertFalse(holds(self.task, turn(tools=[write("guide-x.md", "## Osprey\nThen this.\n")]), self.task, before))
        self.assertFalse(holds(self.task, turn(tools=[write("guide-x.md", self.first + "\n## Osprey\n")]), self.task, before))

    def test_the_first_part_starts_an_empty_guide(self):
        self.assertTrue(holds(self.task, turn(tools=[write("guide-x.md", self.first)]), self.task, {}))
        self.assertFalse(holds(self.task, turn(tools=[write("guide-x.md", self.second)]), self.task, {}))

    def test_a_correction_turn_rewrites_the_part_it_names(self):
        task = dict(self.task, part=0)
        before = {"guide-x.md": self.first}
        self.assertTrue(holds(task, turn(tools=[write("guide-x.md", self.first)]), task, before))
        self.assertFalse(holds(task, turn(tools=[write("guide-x.md", self.second)]), task, before))

    def test_the_part_rules_read_the_new_part(self):
        r = corpus.RULES
        before = {"guide-x.md": self.first}
        good = self.first + "\n## Part 2: Osprey\n**Status: draft**\nThen this.\nIn short: do it.\n"
        # The rule holds on the first part only: a near miss.
        near = "# Guide\n\n## Part 1: Kestrel\nStatus: draft\nStart here.\nIn short: go.\n\n## Osprey\nThen this.\n"
        t_good, t_near = turn(tools=[write("guide-x.md", good)]), turn(tools=[write("guide-x.md", near)])
        for rule in ("status_draft", "numbered", "in_short"):
            with self.subTest(rule):
                self.assertTrue(holds(r[rule]["check"], t_good, self.task, before))
                self.assertFalse(holds(r[rule]["check"], t_near, self.task, before))
        self.assertTrue(holds(r["unnumbered"]["check"], t_near, self.task, before))
        self.assertFalse(holds(r["unnumbered"]["check"], t_good, self.task, before))
        self.assertFalse(holds(r["status_review"]["check"], t_good, self.task, before))
        self.assertFalse(holds(r["key_point"]["check"], t_good, self.task, before))

    def test_the_footer_is_the_last_line(self):
        rule = corpus.RULES["footer"]["check"]
        good = self.second + "\n_Draft by Sherlock_\n"
        near = "_Draft by Sherlock_\n" + self.second
        self.assertTrue(holds(rule, turn(tools=[write("guide-x.md", good)]), self.task))
        self.assertFalse(holds(rule, turn(tools=[write("guide-x.md", near)]), self.task))


class ScoreTest(unittest.TestCase):
    """The scorer end to end on a two-family stand-in run, and the decision
    rule's guards."""

    def stand_in(self, h_holds, s_holds):
        fams = [
            {"id": "W01", "kind": "writing", "group": "primary", "files": {}, "steps": [
                {"kind": "live", "content": "a", "instance": 1, "task": {"kind": "bullet_count", "value": 3}, "probe": None},
                {"kind": "block", "index": 0},
                {"kind": "live", "content": "b", "instance": 2, "task": {"kind": "bullet_count", "value": 3},
                 "probe": {"rule": {"kind": "prefix", "value": "STATUS:"}}}]},
        ]
        good = "STATUS: ok\n- a\n- b\n- c"
        bad = "- a\n- b\n- c"
        tmp = tempfile.mkdtemp()
        for arm, ok in (("H", h_holds), ("S", s_holds)):
            Path(tmp, arm).mkdir()
            rec = {"episode": "W01", "turns": [{"step": 0, "reply": bad}, {"step": 2, "reply": good if ok else bad}]}
            Path(tmp, arm, "results.jsonl").write_text(json.dumps(rec) + "\n")
        Path(tmp, "ledger").mkdir()
        Path(tmp, "ledger", "calls.jsonl").write_text(
            json.dumps({"arm": "S", "usd": 0.01}) + "\n" + json.dumps({"arm": "H", "usd": 0.0, "cached_usd": 0.01}) + "\n")
        cpath = Path(tmp, "corpus.json")
        cpath.write_text(json.dumps({"families": fams}))
        return score.score(tmp, cpath)

    def test_a_probe_holds_only_when_task_and_rule_both_hold(self):
        out = self.stand_in(True, False)
        self.assertEqual(out["paired"]["primary"]["H_only"], 1)
        self.assertEqual(out["tasks"]["H"]["completed"], 2)
        # A cached synthesis is charged to the arm it served.
        self.assertEqual(out["cost"]["by_arm_usd"], {"S": 0.01, "H": 0.01})
        self.assertEqual(out["cost"]["spent_usd"], 0.01)

    def test_one_discordant_pair_is_not_significant(self):
        out = self.stand_in(True, False)
        self.assertEqual(out["verdict"]["outcome"], "shipped structure stands")
        self.assertFalse(out["verdict"]["beats"])

    def test_a_missing_family_makes_the_run_undetermined(self):
        out = self.stand_in(True, True)
        out["missing"]["H"] = ["W02"]
        self.assertTrue(score.decide(out)["outcome"].startswith("undetermined"))

    def test_the_decision_rule(self):
        base = {"missing": {"S": [], "H": []},
                "paired": {"primary": {"difference": 0.2, "p": 0.001}},
                "guardrails": {"control": {"hold": True}, "reversal": {"hold": True}, "cost": {"hold": True}}}
        self.assertEqual(score.decide(base)["outcome"], "H valid")
        # Near misses: a guardrail that fails, a p at alpha, a loss.
        for path, value in ((("guardrails", "cost"), {"hold": False}),
                            (("paired", "primary"), {"difference": 0.2, "p": 0.05}),
                            (("paired", "primary"), {"difference": -0.2, "p": 0.001})):
            out = json.loads(json.dumps(base))
            out[path[0]][path[1]] = value
            self.assertEqual(score.decide(out)["outcome"], "shipped structure stands", path)

    def test_mcnemar_exact(self):
        self.assertEqual(score.mcnemar([True] * 6, [False] * 6)[:2], (6, 0))
        self.assertAlmostEqual(score.binom_two_sided(6, 6), 0.03125)
        self.assertAlmostEqual(score.binom_two_sided(5, 6), 0.21875)


if __name__ == "__main__":
    unittest.main()
