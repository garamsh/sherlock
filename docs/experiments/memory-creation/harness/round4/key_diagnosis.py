"""Which keys B12 assigns to facts that must coexist.

Value hints removed B12's cross-topic collision, but coexisting facts still
collapse. Either the decider gives two simultaneously-true facts one key, or it
gives them distinct keys and they are lost some other way. The fix differs.
"""
from __future__ import annotations
import json
import os
import random
import sys
from collections import defaultdict

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from api import embed_batch, flush
from dataset import build, COEXIST
from structures import KeyUpsertValueHints

RESULTS = os.path.join(os.path.dirname(os.path.abspath(__file__)), "results")


def main():
    seed = int(os.environ.get("SEED", "7"))
    turns, probes = build(seed=seed)
    embed_batch([t for t, _ in turns] + [p.question for p in probes])

    coexist_values = {v for _, _, values, _ in COEXIST for v in values}
    inst = KeyUpsertValueHints(random.Random(seed))
    for text, is_fact in turns:
        inst.turn(text, is_fact)
    flush()

    # Group every row that carries a coexist value by the key it was given.
    by_key = defaultdict(list)
    for r in inst.store.rows:
        for v in coexist_values:
            if v.lower() in r.content.lower():
                by_key[r.key].append((r.content, r.dead))
                break

    collided = {k: v for k, v in by_key.items() if len(v) > 1}
    survived = sum(1 for rows in by_key.values() for _, dead in rows if not dead)
    total = sum(len(rows) for rows in by_key.values())

    print(f"coexist rows written: {total}, still live: {survived}")
    print(f"keys holding more than one coexist fact: {len(collided)} "
          f"of {len(by_key)}")
    for k, rows in list(collided.items())[:6]:
        print(f"  {k}")
        for content, dead in rows:
            print(f"    {'dropped' if dead else 'live   '}  {content}")

    os.makedirs(RESULTS, exist_ok=True)
    with open(os.path.join(RESULTS, f"key_diagnosis_seed{seed}.json"), "w") as f:
        json.dump({"seed": seed, "rows": total, "live": survived,
                   "keys": len(by_key), "collided_keys": len(collided),
                   "detail": {k: v for k, v in list(collided.items())[:40]}},
                  f, indent=2)


if __name__ == "__main__":
    main()
