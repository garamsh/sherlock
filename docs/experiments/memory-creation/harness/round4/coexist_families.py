"""Coexist preservation per family, for the three key-addressed instructions.

B14, B17 and B18 are one structure under three narrowing examples: B14's names
a spoken language, B17's an allergy, B18's a family the turn stream never
mentions. The coexist column averages three families of unequal size, so a
single column value cannot say whether the decider learned the plural split or
recalled the family the prompt named. This scores each family separately,
classifying every planted value with the scorer the round's table uses.

Every decider reply is cached, so the replays make no API call.
"""
from __future__ import annotations
import json
import os
import random
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from api import embed_batch, flush
from dataset import build, COEXIST
from scoring import classify
from structures import KeyUpsertPluralAware
import structures_a as SA

# Results land beside the report, not beside the caller's shell.
RESULTS = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                       "..", "..", "results_round4")

STRUCTURES = {
    "B14": KeyUpsertPluralAware,
    "B17": SA.KeyUpsertQuestionKey,
    "B18": SA.KeyUpsertUnseenExample,
}

# The value set identifies the family a coexist probe belongs to.
FAMILY_OF = {tuple(values): name for name, _, values, _ in COEXIST}
FAMILIES = sorted(FAMILY_OF.values())


def run(cls, seed, turns, probes):
    inst = cls(random.Random(seed))
    for text, is_fact in turns:
        inst.turn(text, is_fact)

    preserved = dict.fromkeys(FAMILIES, 0)
    planted = dict.fromkeys(FAMILIES, 0)
    misses = []
    probe_scores = []
    for p in probes:
        if p.kind != "coexist":
            continue
        family = FAMILY_OF[tuple(p.expect_present)]
        recalled = inst.recall(p.question)
        hits = 0
        for value in p.expect_present:
            planted[family] += 1
            outcome = classify(inst, p.subject, value, recalled)
            if outcome == "recalled":
                preserved[family] += 1
                hits += 1
            else:
                rows = [r for r in inst.store.rows
                        if p.subject.lower() in r.content.lower()
                        and value.lower() in r.content.lower()]
                misses.append({"subject": p.subject, "value": value,
                               "family": family, "outcome": outcome,
                               "keys": sorted({r.key for r in rows if r.key})})
        probe_scores.append(hits / len(p.expect_present))

    return {"structure": cls.NAME, "items": inst.size(),
            "coexist_preserved": round(sum(probe_scores) / len(probe_scores), 4),
            "planted": planted, "preserved": preserved, "misses": misses}


def main():
    seed = int(os.environ.get("SEED", "7"))
    turns, probes = build(seed=seed)
    embed_batch([t for t, _ in turns] + [p.question for p in probes])

    results = {tag: run(cls, seed, turns, probes)
               for tag, cls in STRUCTURES.items()}
    flush()

    tags = list(STRUCTURES)
    families = [{"family": name,
                 "values": results[tags[0]]["planted"][name],
                 "preserved": {t: results[t]["preserved"][name] for t in tags}}
                for name in FAMILIES]

    os.makedirs(RESULTS, exist_ok=True)
    path = os.path.join(RESULTS, f"coexist_families_seed{seed}.json")
    with open(path, "w") as f:
        json.dump({"seed": seed, "families": families,
                   "structures": results}, f, indent=2)

    print(f"=== coexist by family, seed {seed} ===")
    print("| family | values | " + " | ".join(tags) + " |")
    print("|" + "---|" * (len(tags) + 2))
    for row in families:
        counts = " | ".join(str(row["preserved"][t]) for t in tags)
        print(f"| {row['family']} | {row['values']} | {counts} |")
    print()
    for tag in tags:
        r = results[tag]
        print(f"{tag} {r['structure']}: coexist {r['coexist_preserved']} "
              f"items {r['items']}")
        for m in r["misses"]:
            print(f"    {m['outcome']:<13} {m['subject']} / {m['value']} "
                  f"({m['family']}) keys={m['keys']}")
    print(f"\nwrote {path}")


if __name__ == "__main__":
    main()
