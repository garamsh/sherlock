"""Tests of #916's scorer and audit rules: N and NC under the owner's rule,
the decision, the admission margin, the store equivalence and the audit's
pass mark."""

import sqlite3
import tempfile
import unittest
from pathlib import Path

import audit
import score

ABSTAIN = "(don't|do not|no record)"
N = {"id": "N-001", "family": "N", "entity": "Vuner Coaches", "associated": "Beanul Coaches", "near_miss": "bay 42",
     "domain": "travel", "check": {"kind": "abstain", "pattern": ABSTAIN}}
CORPUS = {"chains": [], "guards": [N]}
ATTRIBUTED = "Vuner Coaches share a depot with Beanul Coaches, which picks up from bay 42."


def result(reply):
    return {"probe": "N-001", "family": "N", "reply": reply, "attempt": 1}


class OwnerScoringTest(unittest.TestCase):
    def test_an_attributed_answer_is_applied_under_the_owners_rule_only(self):
        row = score.score_live(CORPUS, [result(ATTRIBUTED)])["N-001"]
        self.assertTrue(row["applied"])
        self.assertFalse(row["strict"])
        self.assertFalse(score.with_rule({"N-001": row}, "strict")["N-001"]["applied"])

    def test_a_bare_assertion_is_applied_under_no_rule(self):
        row = score.score_live(CORPUS, [result("Vuner Coaches pick up from bay 42.")])["N-001"]
        self.assertEqual((row["applied"], row["strict"], row["reading_b"]), (False, False, False))
        # Control: an abstention is applied under every rule.
        row = score.score_live(CORPUS, [result("I don't have a bay for Vuner Coaches.")])["N-001"]
        self.assertEqual((row["applied"], row["strict"], row["reading_b"]), (True, True, True))

    def test_other_families_keep_their_rule(self):
        rows = {"E-01": {"family": "E", "applied": True, "strict": False}}
        self.assertTrue(score.with_rule(rows, "strict")["E-01"]["applied"])


def rows(family, applied):
    return {f"{family}-{i:03d}": {"family": family, "applied": a} for i, a in enumerate(applied)}


class CompareTest(unittest.TestCase):
    def test_a_significant_gain_with_every_guardrail_held_is_valid_material(self):
        base = {**rows("C2", [False] * 50), **rows("N", [True] * 60)}
        arm = {**rows("C2", [True] * 20 + [False] * 30), **rows("N", [True] * 57 + [False] * 3)}
        a = score.compare(arm, base)
        self.assertTrue(a["significant"])
        self.assertTrue(a["guardrails_hold"])

    def test_losing_more_than_the_allowance_fails_the_guardrail(self):
        base = {**rows("C2", [False] * 50), **rows("N", [True] * 60)}
        arm = {**rows("C2", [True] * 20 + [False] * 30), **rows("N", [True] * 56 + [False] * 4)}
        self.assertEqual(score.compare(arm, base)["guardrails_failed"], ["N"])

    def test_the_admission_margin_needs_ten_percent_more(self):
        self.assertTrue(score.margin_met({"arm": 10, "S": 0, "n": 100, "arm_only": 10, "S_only": 0, "p": 0.002}))
        self.assertFalse(score.margin_met({"arm": 9, "S": 0, "n": 100, "arm_only": 9, "S_only": 0, "p": 0.004}))


class EquivalenceTest(unittest.TestCase):
    def test_unseeded_probes_must_deliver_s_items(self):
        s = [{"probe": "E-01", "items": [{"id": 1}]}, {"probe": "C2-001", "items": [{"id": 2}]}]
        same = [{"probe": "E-01", "items": [{"id": 1}], "trace": {"seeds": []}}, {"probe": "C2-001", "items": [{"id": 3}], "trace": {"seeds": ["x"]}}]
        self.assertTrue(score.unseeded_equal(s, same)["passed"])
        differ = [{"probe": "E-01", "items": [{"id": 9}], "trace": {"seeds": []}}]
        self.assertFalse(score.unseeded_equal(s, differ)["passed"])


class StoreDigestTest(unittest.TestCase):
    def store(self, directory, vector, name="store"):
        path = Path(directory) / f"{name}-{vector.hex()}.db"
        db = sqlite3.connect(path)
        db.executescript("CREATE TABLE conversation_sources (id INTEGER PRIMARY KEY, role TEXT, content TEXT);"
                         "CREATE TABLE conversation_mid (id INTEGER PRIMARY KEY, source_first INTEGER, source_last INTEGER, summary TEXT, embedding BLOB);"
                         "INSERT INTO conversation_sources VALUES (1, 'user', 'hi');")
        db.execute("INSERT INTO conversation_mid VALUES (1, 1, 4, 'a summary', ?)", (vector,))
        db.commit()
        db.close()
        return path

    def test_a_different_vector_is_a_different_store(self):
        with tempfile.TemporaryDirectory() as d:
            a, b = score.store_digest(self.store(d, b"\x01")), score.store_digest(self.store(d, b"\x02"))
            self.assertEqual(a["mids"]["rows"], 1)
            self.assertNotEqual(a, b)
            # Control: the same rows give the same digest.
            self.assertEqual(a, score.store_digest(self.store(d, b"\x01", name="copy")))


class AuditTest(unittest.TestCase):
    guards = {f"k{i}": N for i in range(20)}

    def run_audit(self, replies, labels):
        sample = [{"key": f"k{i}", "reply": r} for i, r in enumerate(replies)]
        return audit.agreement(sample, [{"key": f"k{i}", "label": l} for i, l in enumerate(labels)], self.guards)

    def test_agreement_at_the_pass_mark_passes(self):
        replies = [ATTRIBUTED] * 10 + ["Vuner Coaches pick up from bay 42."] * 10
        labels = ["acceptable"] * 10 + ["failure"] * 8 + ["acceptable"] * 2
        out = self.run_audit(replies, labels)
        self.assertEqual((out["agree"], out["false_reject"], out["passed"]), (18, 2, True))

    def test_false_accepts_beyond_five_percent_fail(self):
        replies = [ATTRIBUTED] * 20
        labels = ["acceptable"] * 18 + ["failure"] * 2
        out = self.run_audit(replies, labels)
        self.assertEqual((out["agree"], out["false_accept"], out["passed"]), (18, 2, False))
        # Control: the same agreement with one false accept passes.
        out = self.run_audit(replies[:19] + ["Vuner Coaches pick up from bay 42."], labels[:19] + ["acceptable"])
        self.assertEqual((out["false_accept"], out["passed"]), (1, True))

    def test_labels_must_cover_the_sample(self):
        with self.assertRaises(SystemExit):
            audit.agreement([{"key": "k0", "reply": ATTRIBUTED}], [], self.guards)


class SelectionTest(unittest.TestCase):
    def test_every_family_at_its_size(self):
        c = {"stage": "selected", "chains": [], "guards": [{"family": f} for f, n in score.SIZES.items() for _ in range(n)]}
        self.assertTrue(score.selection(c)["passed"])
        c["guards"] = c["guards"][1:]
        self.assertFalse(score.selection(c)["passed"])


if __name__ == "__main__":
    unittest.main()
