"""Where does B3's remaining correction gap live: retrieval or judgment?

B3 recalls every control fact and destroys nothing, but only half of its
corrections retire the old value. Two very different causes produce that same
number, and they have opposite fixes:

  retrieval  the contradicted row was never offered to the decider, so the
             decider could not have retired it -> widen what is offered
  judgment   the row was offered and the decider kept it anyway
             -> the structure is right and the decider is the ceiling

This replays the correction turns and records which one happened. It reuses
the cached decider replies, so it costs nothing to run after the main round.
"""
from __future__ import annotations
import json
import os
import random
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from api import flush, embed_batch
from dataset import build, CATEGORIES, PERSONS
from structures import DeciderNeighbors, CANDIDATE_FLOOR, RECALL_K

# Results land beside the report, not beside the caller's shell.
RESULTS = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                       "..", "..", "results_round4")


def main():
    seed = int(os.environ.get("SEED", "7"))
    turns, probes = build(seed=seed)
    embed_batch([t for t, _ in turns] + [p.question for p in probes])

    # Keyed on the exact correction turn, not on the value string: values
    # repeat across subjects, so matching by value would flag every unrelated
    # statement that happens to mention the same editor or database.
    corrections = {p.correction_text: (p.subject, p.expect_absent[0])
                   for p in probes if p.kind == "corrected"}

    inst = DeciderNeighbors(random.Random(seed))
    stats = {"offered_and_retired": 0, "offered_not_retired": 0,
             "not_offered": 0, "no_row_for_old": 0}
    per_turn = []

    for text, is_fact in turns:
        target = corrections.get(text)

        if target is None:
            inst.turn(text, is_fact)
            continue

        # This turn is a correction. Find the live rows holding the old value.
        subject, old_val = target
        old_rows = [r for r in inst.store.rows
                    if not r.dead and old_val.lower() in r.content.lower()
                    and subject.lower() in r.content.lower()]
        offered = {r.id for r in inst.store.rank(text, RECALL_K,
                                                 floor=CANDIDATE_FLOOR)}
        inst.turn(text, is_fact)

        if not old_rows:
            stats["no_row_for_old"] += 1
            outcome = "no_row_for_old"
        elif not any(r.id in offered for r in old_rows):
            stats["not_offered"] += 1
            outcome = "not_offered"
        elif all(r.dead for r in old_rows):
            stats["offered_and_retired"] += 1
            outcome = "offered_and_retired"
        else:
            stats["offered_not_retired"] += 1
            outcome = "offered_not_retired"
        per_turn.append({"text": text, "old": old_val, "outcome": outcome})

    flush()
    total = sum(stats.values())
    os.makedirs(RESULTS, exist_ok=True)
    with open(os.path.join(RESULTS, f"gap_diagnosis_seed{seed}.json"), "w") as f:
        json.dump({"seed": seed, "stats": stats, "total": total,
                   "per_turn": per_turn}, f, indent=2)
    print(json.dumps(stats, indent=2))
    print(f"total correction turns: {total}")
    if total:
        offered = stats["offered_and_retired"] + stats["offered_not_retired"]
        print(f"contradicted row was offered: {offered}/{total} "
              f"({offered / total:.0%})")
        if offered:
            print(f"  of those, retired: {stats['offered_and_retired']}/{offered} "
                  f"({stats['offered_and_retired'] / offered:.0%})")


if __name__ == "__main__":
    main()
