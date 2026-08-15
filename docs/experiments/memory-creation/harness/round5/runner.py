"""Run named structures over the round-5 dataset."""
from __future__ import annotations
import json
import os
import random
import sys
import time
from concurrent.futures import ThreadPoolExecutor

HERE = os.path.dirname(os.path.abspath(__file__))
# Round 4 second: its `dataset` and `scoring` are the ones round 5 replaces.
sys.path.insert(0, os.path.join(HERE, "..", "round4"))
sys.path.insert(0, HERE)

import api
from api import embed_batch, flush
from dataset import build
from scoring import score
import structures as S
import structures_a as SA
import structures_b as SB

# Results land beside the report, not beside the caller's shell.
RESULTS = os.path.join(HERE, "..", "..", "results_round5")


def resolve(name):
    for module in (S, SA, SB):
        cls = getattr(module, name, None)
        if cls is not None:
            return cls
    raise SystemExit(f"unknown structure: {name}")


def run_one(cls, turns, probes, seed):
    inst = cls(random.Random(seed))
    t0 = time.perf_counter()
    for i, (text, is_fact) in enumerate(turns):
        inst.turn(text, is_fact)
        if i and i % 100 == 0:
            print(f"  {cls.NAME}: {i}/{len(turns)} items={inst.size()}", flush=True)
    row = {"system": cls.NAME, "blurb": cls.BLURB, "extra_calls": cls.EXTRA_CALLS,
           "decider_calls": inst.calls, "items": inst.size(),
           "elapsed_s": round(time.perf_counter() - t0, 1)}
    row.update(score(inst, probes))
    print(f"DONE {row['system']}: net={row['net']} latest={row['latest_wins']} "
          f"control={row['control_survival']} coexist={row['coexist_preserved']} "
          f"items={row['items']} by family={row['coexist_by_family']}", flush=True)
    return row


def main():
    names = sys.argv[1:]
    if not names:
        raise SystemExit("usage: runner.py <Structure> [<Structure> ...]")
    classes = [resolve(n) for n in names]

    seed = int(os.environ.get("SEED", "7"))
    turns, probes = build(seed=seed)
    kinds = {}
    for p in probes:
        kinds[p.kind] = kinds.get(p.kind, 0) + 1
    print(f"dataset seed={seed}: {len(turns)} turns, {len(probes)} probes {kinds}")

    print("pre-embedding corpus...", flush=True)
    embed_batch([t for t, _ in turns] + [p.question for p in probes])

    rows = []
    with ThreadPoolExecutor(max_workers=len(classes)) as pool:
        futures = [pool.submit(run_one, cls, turns, probes, seed) for cls in classes]
        for f in futures:
            rows.append(f.result())
    flush()

    os.makedirs(RESULTS, exist_ok=True)
    for row in rows:
        path = os.path.join(RESULTS, f"{row['system']}_seed{seed}.json")
        with open(path, "w") as f:
            json.dump({"seed": seed, "turns": len(turns), "probes": len(probes),
                       "probe_kinds": kinds, "row": row,
                       "api_calls": api.API_CALLS}, f, indent=2)
    print(f"\nAPI: {api.API_CALLS}")


if __name__ == "__main__":
    main()
