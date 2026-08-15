"""Why the neighbourhood rewrite drops memories it was shown.

Replays B10 and records, for every row it removed, whether the reply carried a
near-identical replacement. Paraphrase churn and genuine omission look the same
in the score and need different fixes.
"""
from __future__ import annotations
import json
import os
import random
import sys
from collections import Counter

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from api import cosine, embed, embed_batch, flush
from dataset import build
from structures import NeighbourhoodRewrite

# Results land beside the report, not beside the caller's shell.
RESULTS = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                       "..", "..", "results_round4")


class Traced(NeighbourhoodRewrite):
    def __init__(self, rnd, model=None):
        super().__init__(rnd, model=model)
        self.drops = []

    def turn(self, text, is_fact):
        shown_before = [r.content for r in
                        self.store.rank(text, self.K, floor=self.FLOOR)]
        live_before = {id(r): r.content for r in self.store.live()}
        super().turn(text, is_fact)
        live_after = {id(r) for r in self.store.live()}
        dropped = [c for rid, c in live_before.items() if rid not in live_after]
        if not dropped:
            return
        added = [r.content for r in self.store.rows if r.ts == self.store.now]
        for d in dropped:
            self.drops.append({"turn": text, "dropped": d, "added": added,
                               "was_shown": d in shown_before})


def main():
    seed = int(os.environ.get("SEED", "7"))
    turns, probes = build(seed=seed)
    embed_batch([t for t, _ in turns] + [p.question for p in probes])
    inst = Traced(random.Random(seed))
    for text, is_fact in turns:
        inst.turn(text, is_fact)
    flush()

    corrections = {p.correction_text for p in probes if p.kind == "corrected"}
    kinds = Counter()
    examples = {}
    for d in inst.drops:
        best = 0.0
        match = None
        dv = embed(d["dropped"])
        for a in d["added"]:
            s = cosine(dv, embed(a))
            if s > best:
                best, match = s, a
        if d["turn"] in corrections and best < 0.9:
            kind = "dropped_on_a_correction_turn"
        elif best >= 0.98:
            kind = "replaced_verbatim_churn"
        elif best >= 0.85:
            kind = "replaced_by_a_paraphrase"
        elif best >= 0.6:
            kind = "replaced_by_a_related_line"
        else:
            kind = "dropped_with_no_replacement"
        kinds[kind] += 1
        examples.setdefault(kind, {"turn": d["turn"], "dropped": d["dropped"],
                                   "closest_added": match,
                                   "similarity": round(best, 3)})

    total = sum(kinds.values())
    print(f"rows removed: {total}")
    for k, n in kinds.most_common():
        print(f"  {k}: {n} ({n / total:.0%})")
    print()
    for k, ex in examples.items():
        print(f"[{k}] turn: {ex['turn']}")
        print(f"    dropped: {ex['dropped']}")
        print(f"    closest added: {ex['closest_added']} (cos {ex['similarity']})")

    os.makedirs(RESULTS, exist_ok=True)
    with open(os.path.join(RESULTS, f"rewrite_diagnosis_seed{seed}.json"), "w") as f:
        json.dump({"seed": seed, "removed": total, "kinds": dict(kinds),
                   "examples": examples, "drops": inst.drops[:200]}, f, indent=2)


if __name__ == "__main__":
    main()
