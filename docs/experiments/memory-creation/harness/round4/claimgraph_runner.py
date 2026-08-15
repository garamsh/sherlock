"""B8 (claim graph) on its own.

The claim graph emits a variable number of claims per turn, so its calls are
long and it runs roughly seven times slower than the other structures. Running
it separately keeps the main comparison from being gated on it.
"""
from __future__ import annotations
import json
import os
import random
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import api
from api import embed_batch, flush
from dataset import build
from scoring import score
import structures as S

# Results land beside the report, not beside the caller's shell.
RESULTS = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                       "..", "..", "results_round4")


def main():
    seed = int(os.environ.get("SEED", "7"))
    turns, probes = build(seed=seed)
    embed_batch([t for t, _ in turns] + [p.question for p in probes])
    inst = S.ClaimGraph(random.Random(seed))
    t0 = time.perf_counter()
    for i, (text, is_fact) in enumerate(turns):
        inst.turn(text, is_fact)
        if i and i % 50 == 0:
            print(f"  {i}/{len(turns)} items={inst.store.size()}", flush=True)
    row = {"system": S.ClaimGraph.NAME, "blurb": S.ClaimGraph.BLURB,
           "extra_calls": S.ClaimGraph.EXTRA_CALLS, "decider_calls": inst.calls,
           "items": inst.size(), "elapsed_s": round(time.perf_counter() - t0, 1)}
    row.update(score(inst, probes))
    flush()
    with open(os.path.join(RESULTS, f"claimgraph_seed{seed}.json"), "w") as f:
        json.dump({"seed": seed, "row": row, "api_calls": api.API_CALLS}, f, indent=2)
    print(f"DONE {row['system']}: net={row['net']} latest={row['latest_wins']} "
          f"control={row['control_survival']} coexist={row['coexist_preserved']} "
          f"items={row['items']} retired(control)={row['control_retired']} "
          f"retired(coexist)={row['coexist_retired']}")


if __name__ == "__main__":
    main()
