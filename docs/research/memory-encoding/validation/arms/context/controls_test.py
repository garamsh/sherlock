import unittest

import controls


def sc(i, subject, source):
    return {"id": f"SC-{i}", "family": "SC", "subject": subject, "hops": [{"source": source, "key": f"{subject} 4864"}]}


CORPUS = {
    "stream": [{}] * 16,
    "chains": [sc(1, "Oscar Goudre", 1), sc(2, "Aiko Veaholl", 5), {"id": "C1-1", "family": "C1", "hops": [{"source": 9, "key": "Rodend Gebind"}]}],
    "guards": [{"id": "NC-1", "family": "NC", "entity": "Wen Loth", "sources": [9, 13]}],
}


class ControlsTest(unittest.TestCase):
    def test_counts_contexts_naming_the_subject_and_not_other_names(self):
        recorded = [
            {"first": 1, "context": "A call with Oscar Goudre.", "summary": "The best line is extension 4864."},
            {"first": 5, "context": "A call with Aiko.", "summary": "Aiko Veaholl's line is 7508."},
            {"first": 9, "context": "", "summary": "s"},
            {"first": 13, "context": "Reaching Wen Loth.", "summary": "s"},
        ]
        out = controls.controls(CORPUS, recorded)
        self.assertEqual(out["sc_context_names_subject"], 1)
        self.assertEqual(out["sc_missing"], ["SC-2"], "a context naming part of the subject does not count")
        self.assertEqual(out["sc_summary_names_subject"], 1)
        self.assertEqual(out["with_context"], 3)
        self.assertEqual(out["context_share"], 0.75)
        self.assertEqual(out["nc_context_names_entity"], 1)
        self.assertFalse(out["passed"])

    def test_passes_when_both_shares_reach_their_thresholds(self):
        recorded = [
            {"first": 1, "context": "Oscar Goudre.", "summary": "s"},
            {"first": 5, "context": "Aiko Veaholl.", "summary": "s"},
            {"first": 9, "context": "c", "summary": "s"},
            {"first": 13, "context": "c", "summary": "s"},
        ]
        self.assertTrue(controls.controls(CORPUS, recorded)["passed"])

    def test_oracle_counts_hops_whose_statement_delivers_its_mid(self):
        records = [
            {"probe": "SC-1", "hop": 1, "items": [{"first": 1, "last": 4, "text": "x"}]},
            {"probe": "SC-2", "hop": 1, "items": [{"first": 9, "last": 12, "text": "x"}]},
        ]
        out = controls.oracle(CORPUS, records)
        self.assertEqual((out["hops"], out["reached"]), (2, 1))
        self.assertFalse(out["passed"])

    def test_tokens_take_the_last_successful_call_of_each_episode(self):
        ledger = [
            {"phase": "context-b4-w8", "episode": "b4-01201", "status": 200, "prompt_tokens": 500, "completion_tokens": 100},
            {"phase": "context-b4-w8", "episode": "b4-01201", "status": 200, "prompt_tokens": 520, "completion_tokens": 120},
            {"phase": "context-b4-w8", "episode": "b4-01202", "status": 429, "prompt_tokens": 0, "completion_tokens": 0},
            {"phase": "synth-b4", "episode": "b4-0001", "status": 200, "prompt_tokens": 300, "completion_tokens": 200},
        ]
        out = controls.tokens(ledger)
        self.assertEqual(out["context-b4-w8"]["batches"], 1)
        self.assertEqual(out["context-b4-w8"]["prompt_tokens"], 520)
        self.assertEqual(out["synth-b4"]["completion_tokens_mean"], 200)


if __name__ == "__main__":
    unittest.main()
