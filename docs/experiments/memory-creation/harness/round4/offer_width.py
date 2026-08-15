"""How wide the candidate offer has to be to reach the contradicted row.

Widening the offer from B3's 5 candidates to B9's 10 took the corrections
whose contradicted row was never offered from 11 of 100 to 2. This settles
where the rest of that tail sits, as a count rather than as a score.

B9 is replayed once and the full live ranking is kept for every turn, so each
correction turn carries the rank and cosine of the live row holding the value
it contradicts. The offered / not-offered split then follows for any K and any
floor without running a structure again, and the same rankings give the number
of candidates each K would have put in the prompt on every turn — the cost a
wider offer pays on all 488 turns to catch a tail that appears on a handful.

The store trajectory is B9's throughout. What a different K would have retired,
and how that would have moved later rankings, is not modelled: these counts say
what the decider would have been shown, not what it would have decided.

Retrieval is a re-rank over cached embeddings and every decider reply is
cached, so the replay makes no API call.
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
from structures import DeciderNeighborsWide

# Results land beside the report, not beside the caller's shell.
RESULTS = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                       "..", "..", "results_round4")

BASE_K = DeciderNeighborsWide.WIDE_K
BASE_FLOOR = DeciderNeighborsWide.WIDE_FLOOR
# None is the unbounded offer: every live row above the floor.
K_SWEEP = (5, BASE_K, 20, 40, None)
FLOOR_SWEEP = (0.0, 0.2, 0.3, 0.4, 0.5, 0.6)


class Traced(DeciderNeighborsWide):
    """B9, keeping the full live ranking behind each turn's offer."""

    def __init__(self, rnd, model=None):
        super().__init__(rnd, model=model)
        self.ranking = []

    def turn(self, text, is_fact):
        qv = embed(text)
        self.ranking = sorted(((cosine(qv, r.vec), r) for r in self.store.live()),
                              key=lambda x: -x[0])
        super().turn(text, is_fact)

    def context_rows(self, text):
        # The rows `Store.rank(text, WIDE_K, floor=WIDE_FLOOR)` returns: the
        # ranking is sliced to K first and floored second, in that order.
        return [r for s, r in self.ranking[:self.WIDE_K] if s >= self.WIDE_FLOOR]


def offer_size(scores, k, floor):
    """How many candidates an offer of this width would carry."""
    return sum(1 for s in (scores if k is None else scores[:k]) if s >= floor)


def reaches(record, k, floor):
    """Whether the contradicted row is inside an offer of this width."""
    return ((k is None or record["rank"] <= k) and record["cosine"] >= floor)


def split(records, k, floor):
    offered = [r for r in records if reaches(r, k, floor)]
    missed = [r for r in records if not reaches(r, k, floor)]
    return offered, missed


def cost(per_turn_scores, k, floor):
    per_turn = [offer_size(scores, k, floor) for scores in per_turn_scores]
    return {"candidates_mean": round(sum(per_turn) / len(per_turn), 2),
            "candidates_max": max(per_turn),
            "candidates_total": sum(per_turn)}


def main():
    seed = int(os.environ.get("SEED", "7"))
    turns, probes = build(seed=seed)
    embed_batch([t for t, _ in turns] + [p.question for p in probes])

    # Keyed on the exact correction turn; values repeat across subjects.
    corrections = {p.correction_text: (p.subject, p.expect_absent[0])
                   for p in probes if p.kind == "corrected"}

    inst = Traced(random.Random(seed))
    offered_scores = []
    records = []
    no_live_row = []

    for text, is_fact in turns:
        target = corrections.get(text)
        inst.turn(text, is_fact)
        offered_scores.append([s for s, _ in inst.ranking])

        if target is None:
            continue
        subject, old_val = target
        held = [(i + 1, s) for i, (s, r) in enumerate(inst.ranking)
                if old_val.lower() in r.content.lower()
                and subject.lower() in r.content.lower()]
        if not held:
            no_live_row.append({"turn": text, "subject": subject,
                                "old_value": old_val})
            continue
        # Rank order is cosine order, so the best-ranked row is also the one
        # that clears the highest floor: it decides the split on its own.
        rank, score = min(held)
        records.append({"turn": text, "subject": subject, "old_value": old_val,
                        "live_rows_holding_it": len(held),
                        "rank": rank, "cosine": round(score, 6)})

    flush()

    k_sweep = []
    for k in K_SWEEP:
        offered, missed = split(records, k, BASE_FLOOR)
        row = {"k": k, "floor": BASE_FLOOR,
               "offered": len(offered), "not_offered": len(missed),
               "missed": [{"turn": m["turn"], "old_value": m["old_value"],
                           "rank": m["rank"], "cosine": m["cosine"],
                           "reason": "below floor" if
                           (k is None or m["rank"] <= k) else "beyond k"}
                          for m in sorted(missed, key=lambda m: m["rank"])]}
        row.update(cost(offered_scores, k, BASE_FLOOR))
        k_sweep.append(row)

    floor_sweep = []
    for floor in FLOOR_SWEEP:
        offered, missed = split(records, BASE_K, floor)
        row = {"k": BASE_K, "floor": floor,
               "offered": len(offered), "not_offered": len(missed)}
        row.update(cost(offered_scores, BASE_K, floor))
        floor_sweep.append(row)

    _, missed_at_base = split(records, BASE_K, BASE_FLOOR)
    tail = Counter(m["rank"] for m in missed_at_base)

    summary = {
        "seed": seed, "structure": inst.NAME,
        "turns": len(turns), "correction_turns": len(corrections),
        "no_live_row_for_old": len(no_live_row),
        "corrections_with_a_live_row": len(records),
        "k_sweep": k_sweep, "floor_sweep": floor_sweep,
        "missed_at_base_rank_distribution": {str(r): n
                                             for r, n in sorted(tail.items())},
        "per_correction_turn": records,
        "no_live_row_detail": no_live_row,
    }

    os.makedirs(RESULTS, exist_ok=True)
    path = os.path.join(RESULTS, f"offer_width_seed{seed}.json")
    with open(path, "w") as f:
        json.dump(summary, f, indent=2)

    print(f"=== {inst.NAME} seed {seed}: {len(turns)} turns ===")
    print(f"correction turns: {len(corrections)}; "
          f"{len(records)} had a live row holding the old value, "
          f"{len(no_live_row)} had none")
    print(f"\noffer width, floor held at {BASE_FLOOR} "
          f"(of {len(records)} corrections)")
    print("|  K | reached | missed | candidates/turn mean | max | total |")
    print("|---|---|---|---|---|---|")
    for row in k_sweep:
        k = "all" if row["k"] is None else row["k"]
        print(f"| {k} | {row['offered']} | {row['not_offered']} | "
              f"{row['candidates_mean']} | {row['candidates_max']} | "
              f"{row['candidates_total']} |")
    print(f"\nfloor, K held at {BASE_K}")
    print("| floor | reached | missed | candidates/turn mean | max |")
    print("|---|---|---|---|---|")
    for row in floor_sweep:
        print(f"| {row['floor']} | {row['offered']} | {row['not_offered']} | "
              f"{row['candidates_mean']} | {row['candidates_max']} |")
    print(f"\nmissed at K={BASE_K}, floor {BASE_FLOOR}: {len(missed_at_base)}")
    for m in sorted(missed_at_base, key=lambda m: m["rank"]):
        print(f"  rank {m['rank']} cos {m['cosine']:.3f}  {m['turn']} "
              f"(old: {m['old_value']})")
    print("rank distribution of the missed:", dict(sorted(tail.items())))
    print(f"\nwrote {path}")


if __name__ == "__main__":
    main()
