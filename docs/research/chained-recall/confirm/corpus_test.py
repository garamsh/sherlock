"""Static checks of the #885 corpus: the rules every arm's measurement rests
on, over candidates.json and, once selected, corpus.json."""

import json
import unittest
from pathlib import Path

import corpus as confirm

HERE = Path(__file__).resolve().parent
gen = confirm.gen


def load(path):
    with open(path) as f:
        return json.load(f)


class CandidatesTest(unittest.TestCase):
    def setUp(self):
        self.corpus = load(HERE / "candidates.json")
        self.stream = {m["id"]: m for m in self.corpus["stream"]}
        self.absent = [g for g in self.corpus["guards"] if g["family"] == "N"]

    def parts(self):
        """(item id, part number, statement, source) of every chain hop and
        absent exchange."""
        for c in self.corpus["chains"]:
            for i, h in enumerate(c["hops"]):
                yield c["id"], i + 1, h["statement"], h["source"]
        for a in self.absent:
            for i, x in enumerate(a["exchanges"]):
                yield a["id"], i + 1, x["statement"], x["source"]

    def test_every_part_fills_one_batch_of_four_alone(self):
        n = 0
        for item, part, statement, source in self.parts():
            self.assertEqual((source - 1) % 4, 0, f"{item} part {part} does not open a batch")
            batch = [self.stream[source + j] for j in range(4)]
            self.assertEqual([m["chain"] for m in batch], [item] * 4)
            self.assertEqual([m["hop"] for m in batch], [part] * 4)
            self.assertEqual(batch[0]["content"], statement)
            n += 1
        # Control: every stream message outside the tail belongs to a part.
        self.assertEqual(4 * n, sum(m["chain"] != "tail" for m in self.corpus["stream"]))

    def test_parts_of_one_item_share_no_batch_8_mid_or_group(self):
        spans = {}
        for item, _, _, source in self.parts():
            spans.setdefault(item, []).append((source - 1) // 32)
        for item, blocks in spans.items():
            self.assertEqual(len(set(blocks)), len(blocks), item)
        # Control: two parts 16 exchanges apart open blocks 64 messages apart.
        self.assertNotEqual((1 - 1) // 32, (1 + 64 - 1) // 32)

    def test_later_hop_words_appear_only_in_the_hop_before(self):
        for c in self.corpus["chains"]:
            self.assertTrue(gen.lexical_ok(c), c["id"])
        c = next(c for c in self.corpus["chains"] if c["k"] == 3)
        leaked = dict(c, query=c["query"] + " " + c["hops"][1]["ends"][1])
        self.assertFalse(gen.lexical_ok(leaked), "the control: a leaked name breaks the rule")

    def test_absent_items_join_e_to_f_and_state_only_fs_value(self):
        for a in self.absent:
            self.assertTrue(confirm.absent_ok(a), a["id"])
            self.assertEqual(a["check"]["near_miss"], a["near_miss"])
        a = self.absent[0]
        stated = dict(a, exchanges=[a["exchanges"][0], dict(a["exchanges"][1], statement=a["exchanges"][1]["statement"] + " " + a["entity"])])
        self.assertFalse(confirm.absent_ok(stated), "the control: E named beside F's value breaks the rule")
        asked = dict(a, query=a["query"] + " " + a["near_miss"])
        self.assertFalse(confirm.absent_ok(asked), "F's value in the query breaks the rule")

    def test_absent_entity_occurs_only_in_its_joining_exchange(self):
        # E's distinctive word is in the stream only in the joining exchange
        # (statement and acknowledgement), so no episode states E's value.
        text = {m["id"]: m["content"].lower() for m in self.corpus["stream"]}
        for a in self.absent:
            invented = gen.marked(a["entity"]) - gen.marked(a["associated"]) - {f.lower() for f in gen.FIRST}
            self.assertEqual(len(invented), 1, a["id"])
            word = invented.pop()
            holding = {(i - 1) // 4 for i, t in text.items() if word in gen.marked(t) or word in t.split()}
            self.assertEqual(holding, {(a["sources"][0] - 1) // 4}, a["id"])
            self.assertNotIn(a["near_miss"].lower(), " ".join(text[a["sources"][0] + j] for j in range(4)))

    def test_every_value_is_unique_across_the_corpus(self):
        values = [c["answer"] for c in self.corpus["chains"]] + [a["near_miss"] for a in self.absent]
        self.assertEqual(len(values), len(set(values)))

    def test_stream_closes_batch_8_and_groups_of_8(self):
        n = self.corpus["base_messages"] + len(self.corpus["stream"])
        self.assertEqual(n % 32, 0)
        ids = [m["id"] for m in self.corpus["stream"]]
        self.assertEqual(ids, list(range(gen.BASE_MESSAGES + 1, gen.BASE_MESSAGES + 1 + len(ids))))

    def test_candidate_counts(self):
        counts = {}
        for c in self.corpus["chains"]:
            counts[c["family"]] = counts.get(c["family"], 0) + 1
            self.assertEqual(len(c["hops"]), c["k"])
        self.assertEqual(counts, confirm.POOL)
        guards = {}
        for g in self.corpus["guards"]:
            guards[g["family"]] = guards.get(g["family"], 0) + 1
        self.assertEqual(guards, {"E": confirm.GUARDS["E"], "K": confirm.GUARDS["K"], "N": confirm.N_POOL})

    def test_the_abstention_pattern_counts_a_clarifying_question(self):
        import re
        self.assertTrue(re.search(confirm.ABSTAIN, "Could you tell me which office you mean?", re.I))
        self.assertTrue(re.search(confirm.ABSTAIN, "I don't have that number.", re.I))
        self.assertFalse(re.search(confirm.ABSTAIN, "Dial extension 7256.", re.I), "the control: an answer")


class SelectedTest(unittest.TestCase):
    """corpus.json once `select` has run: the chain check passes on it with
    no leak, and every absent query has its associated episode delivered."""

    def setUp(self):
        self.corpus = load(HERE / "corpus.json")
        if self.corpus["stage"] != "selected":
            self.skipTest("corpus.json holds the candidates; select has not run")
        self.candidates = load(HERE / "candidates.json")

    def test_the_stream_is_the_candidates_stream(self):
        self.assertEqual(self.corpus["stream"], self.candidates["stream"])

    def test_families_have_their_sizes(self):
        counts = {}
        for c in self.corpus["chains"]:
            counts[c["family"]] = counts.get(c["family"], 0) + 1
        self.assertEqual(counts, confirm.SIZES)
        guards = {}
        for g in self.corpus["guards"]:
            guards[g["family"]] = guards.get(g["family"], 0) + 1
        self.assertEqual(guards, confirm.GUARDS)

    def test_no_selected_chain_leaks_in_the_check(self):
        check = load(HERE / "evidence" / "selection" / "check-b4.json")
        leaks = confirm.leaking(check)
        self.assertTrue(leaks, "the control: the check found leaking candidates to leave out")
        for c in self.corpus["chains"]:
            self.assertNotIn(c["id"], leaks)

    def test_every_absent_query_has_its_associated_episode_delivered(self):
        probes = {p["probe"]: p for p in gen.load_jsonl(HERE / "evidence" / "selection" / "L-b4-model-probes.jsonl")}
        for g in self.corpus["guards"]:
            if g["family"] == "N":
                self.assertTrue(confirm.delivers_associated(probes[g["id"]], g), g["id"])


if __name__ == "__main__":
    unittest.main()
