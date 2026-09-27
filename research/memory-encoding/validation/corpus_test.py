"""Static checks of the #890 corpus: the rules every arm's measurement rests
on, over candidates.json and, once selected, corpus.json."""

import json
import unittest
from pathlib import Path

import corpus as validation

HERE = Path(__file__).resolve().parent
gen, confirm = validation.gen, validation.confirm


def load(path):
    with open(path) as f:
        return json.load(f)


def exchanges(corpus):
    """(item, family, exchange number, statement, source) of every exchange."""
    for c in corpus["chains"]:
        if c["family"] == "SC":
            yield c["id"], "SC", 0, c["intro"]["statement"], c["intro"]["source"]
        for i, h in enumerate(c["hops"]):
            yield c["id"], c["family"], i + 1, h["statement"], h["source"]
    for g in corpus["guards"]:
        for i, x in enumerate(g.get("exchanges", [])):
            yield g["id"], g["family"], i + 1, x["statement"], x["source"]


class CandidatesTest(unittest.TestCase):
    def setUp(self):
        self.corpus = load(HERE / "candidates.json")
        self.stream = {m["id"]: m for m in self.corpus["stream"]}

    def test_every_exchange_fills_one_batch_of_four_alone(self):
        n = 0
        for item, _, part, statement, source in exchanges(self.corpus):
            self.assertEqual((source - 1) % 4, 0, f"{item} exchange {part} does not open a batch")
            batch = [self.stream[source + j] for j in range(4)]
            self.assertEqual([m["chain"] for m in batch], [item] * 4)
            self.assertEqual([m["hop"] for m in batch], [part] * 4)
            self.assertEqual(batch[0]["content"], statement)
            n += 1
        # Control: every stream message outside the tail belongs to an exchange.
        self.assertEqual(4 * n, sum(m["chain"] != "tail" for m in self.corpus["stream"]))

    def test_the_stream_closes_on_whole_batches_and_a_tail(self):
        stream = self.corpus["stream"]
        self.assertEqual(stream[0]["id"], gen.BASE_MESSAGES + 1)
        self.assertEqual([m["id"] for m in stream], list(range(gen.BASE_MESSAGES + 1, gen.BASE_MESSAGES + 1 + len(stream))))
        self.assertEqual((gen.BASE_MESSAGES + len(stream)) % gen.ALIGN, 0)
        self.assertGreaterEqual(sum(m["chain"] == "tail" for m in stream[-gen.TAIL_MIN:]), gen.TAIL_MIN)

    def test_exchanges_of_one_item_keep_their_distance_and_order(self):
        by_item = {}
        for item, family, part, _, source in exchanges(self.corpus):
            by_item.setdefault(item, (family, []))[1].append((part, source))
        adjacent = 0
        for item, (family, parts) in by_item.items():
            sources = sorted(s for _, s in parts)
            if family in ("SC", "NC"):
                # The introduction is the exchange right before the second.
                first = min(parts)[1]
                self.assertEqual(sorted(parts)[1][1], first + 4, item)
                adjacent += 1
                continue
            for a, b in zip(sources, sources[1:]):
                self.assertGreaterEqual((b - a) // 4, gen.MIN_GAP, item)
            if family == "KU":
                self.assertLess(dict(parts)[1], dict(parts)[2], f"{item}: the change comes before the first statement")
        # Control: the adjacency rule ran on every SC and NC item.
        self.assertEqual(adjacent, sum(1 for c in self.corpus["chains"] if c["family"] == "SC")
                         + sum(1 for g in self.corpus["guards"] if g["family"] == "NC"))

    def test_chains_keep_the_lexical_rule(self):
        chains = [c for c in self.corpus["chains"] if c["family"] in ("C1", "C2", "C3", "IC2", "IC3")]
        for c in chains:
            self.assertTrue(gen.lexical_ok(c), c["id"])
        c = next(c for c in chains if c["k"] == 3)
        leaked = dict(c, query=c["query"] + " " + c["hops"][1]["ends"][1])
        self.assertFalse(gen.lexical_ok(leaked))

    def situated(self):
        for c in self.corpus["chains"]:
            if c["family"] == "SC":
                yield {"subject": c["subject"], "value": c["answer"], "query": c["query"],
                       "exchanges": [c["intro"], c["hops"][0]]}
        for g in self.corpus["guards"]:
            if g["family"] == "NC":
                yield {"subject": g["entity"], "value": g["near_miss"], "query": g["query"], "exchanges": g["exchanges"]}

    def test_situated_items_name_the_subject_only_in_the_introduction(self):
        items = list(self.situated())
        self.assertEqual(len(items), validation.POOL["SC"] + validation.EXACT["NC"])
        for s in items:
            self.assertTrue(validation.situated_ok(s), s)
        # Controls: the rule fails when the evidence names the subject, and
        # when the query carries the value.
        s = items[0]
        named = dict(s, exchanges=[s["exchanges"][0], dict(s["exchanges"][1], statement=s["exchanges"][1]["statement"] + " " + s["subject"])])
        self.assertFalse(validation.situated_ok(named))
        self.assertFalse(validation.situated_ok(dict(s, query=s["query"] + " " + s["value"])))

    def test_standing_and_changed_names_are_stated_only_where_they_belong(self):
        statements = [st for _, _, _, st, _ in exchanges(self.corpus)]
        up = [c for c in self.corpus["chains"] if c["family"] == "UP"]
        self.assertEqual(len({c["hops"][0]["statement"] for c in up}), len(up))
        for c in up:
            w = gen.marked(c["answer"])
            self.assertFalse(w & gen.marked(c["query"]), c["id"])
            self.assertEqual(sum(bool(w & gen.marked(st)) for st in statements), 1, c["id"])
        for g in (g for g in self.corpus["guards"] if g["family"] == "KU"):
            new, old = gen.marked(g["key"]) - gen.marked(g["obsolete"]), gen.marked(g["obsolete"]) - gen.marked(g["key"])
            first, change = (gen.marked(x["statement"]) for x in g["exchanges"])
            self.assertTrue(new <= change and not new & first and old <= first and old <= change, g["id"])
            self.assertFalse((new | old) & gen.marked(g["query"]), g["id"])
        # Control: a UP task that names its fact's word is caught.
        c = up[0]
        self.assertTrue(gen.marked(c["answer"]) & gen.marked(c["query"] + " " + c["answer"]))

    def test_absent_items_keep_885s_rule(self):
        absent = [g for g in self.corpus["guards"] if g["family"] == "N"]
        self.assertEqual(len(absent), validation.EXACT["N"])
        for g in absent:
            self.assertTrue(confirm.absent_ok(g), g["id"])

    def test_values_are_unique_and_absent_from_the_base(self):
        base = gen.base_text()
        values = [c["answer"] for c in self.corpus["chains"]] + [g.get("near_miss") or g.get("key") for g in self.corpus["guards"] if g["family"] in ("N", "NC", "KU")]
        self.assertEqual(len(values), len(set(values)))
        fresh = validation.Used(base)
        for v in values:
            self.assertNotIn(v, fresh, v)
        # Control: a word of the base counts as drawn.
        self.assertIn("the", fresh)


class SelectionTest(unittest.TestCase):
    def test_a_candidate_passes_only_with_every_needed_source_beyond_the_top(self):
        top = validation.CHECK_TOP
        self.assertTrue(validation.passes({"raw": [1, top + 1]}, "C2"))
        self.assertFalse(validation.passes({"raw": [1, top]}, "C2"))
        # SC and UP need their one hop beyond the top, where a chain's
        # first hop is free.
        self.assertFalse(validation.passes({"raw": [1]}, "SC"))
        self.assertTrue(validation.passes({"raw": [top + 1]}, "UP"))

    def test_the_selected_corpus_holds_each_family_at_its_size(self):
        corpus = load(HERE / "corpus.json")
        if corpus["stage"] != "selected":
            self.skipTest("not selected yet")
        fam = {}
        for c in corpus["chains"]:
            fam[c["family"]] = fam.get(c["family"], 0) + 1
        self.assertEqual(fam, validation.SIZES)
        guards = {}
        for g in corpus["guards"]:
            guards[g["family"]] = guards.get(g["family"], 0) + 1
        self.assertEqual(guards, {**validation.EXACT, **validation.REPLICATION})
        check = load(HERE / "evidence" / "selection" / "check-b4.json")
        rows = {r["id"]: r for r in check["chains"]}
        for c in corpus["chains"]:
            self.assertTrue(validation.passes(rows[c["id"]], c["family"]), c["id"])


if __name__ == "__main__":
    unittest.main()
