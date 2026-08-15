"""Run every structure over the dataset and write the comparison table."""
from __future__ import annotations
import json
import os
import random
import sys
import time
from concurrent.futures import ThreadPoolExecutor

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import api
from api import embed_batch, flush
from dataset import build
import structures as S

# Results land beside the report, not beside the caller's shell.
RESULTS = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                       "..", "..", "results_round4")
from scoring import score

# B8 is slow enough to gate the run; claimgraph_runner.py drives it separately.
SYSTEMS = [
    S.StoreThreshold,
    S.DeciderWindow,
    S.DeciderNeighbors,
    S.KeyUpsert,
    S.KeyUpsertNeighbours,
    S.KeyAppend,
    S.KeyUpsertMainModel,
]


def run_one(cls, turns, probes, seed):
    rnd = random.Random(seed)
    inst = cls(rnd)
    t0 = time.perf_counter()
    for i, (text, is_fact) in enumerate(turns):
        inst.turn(text, is_fact)
        if i and i % 100 == 0:
            print(f"  {cls.NAME}: {i}/{len(turns)}", flush=True)
    elapsed = time.perf_counter() - t0
    row = {"system": cls.NAME, "blurb": cls.BLURB,
           "extra_calls": cls.EXTRA_CALLS, "decider_calls": inst.calls,
           "items": inst.size(), "elapsed_s": round(elapsed, 1)}
    row.update(score(inst, probes))
    print(f"  DONE {cls.NAME}: net={row['net']} latest={row['latest_wins']} "
          f"control={row['control_survival']} coexist={row['coexist_preserved']} "
          f"items={row['items']} | control miss: unwritten={row['control_never_written']} "
          f"retired={row['control_retired']} unranked={row['control_not_ranked']}", flush=True)
    return row


def main():
    seed = int(os.environ.get("SEED", "7"))
    turns, probes = build(seed=seed)
    kinds = {}
    for p in probes:
        kinds[p.kind] = kinds.get(p.kind, 0) + 1
    print(f"dataset seed={seed}: {len(turns)} turns, {len(probes)} probes {kinds}")

    print("pre-embedding corpus...", flush=True)
    embed_batch([t for t, _ in turns] + [p.question for p in probes])
    print(f"  embed batches: {api.API_CALLS['embed']}", flush=True)

    rows = []
    with ThreadPoolExecutor(max_workers=len(SYSTEMS)) as pool:
        futures = [pool.submit(run_one, cls, turns, probes, seed) for cls in SYSTEMS]
        for f in futures:
            rows.append(f.result())
    flush()

    rows.sort(key=lambda r: -r["net"])
    os.makedirs(RESULTS, exist_ok=True)
    with open(os.path.join(RESULTS, f"falsification_seed{seed}.json"), "w") as f:
        json.dump({"seed": seed, "turns": len(turns), "probes": len(probes),
                   "probe_kinds": kinds, "api_calls": api.API_CALLS,
                   "rows": rows}, f, indent=2)

    head = ("| system | latest-wins | control survival | coexist preserved | "
            "net | stale | control retired | coexist retired | items | "
            "extra calls/turn |")
    lines = [head, "|" + "---|" * 10]
    for r in rows:
        lines.append(
            f"| {r['system']} | {r['latest_wins']:.2f} | {r['control_survival']:.2f} "
            f"| {r['coexist_preserved']:.2f} | **{r['net']:.2f}** | "
            f"{r['stale_present']:.2f} | {r['control_retired']:.2f} | "
            f"{r['coexist_retired']:.2f} | {r['items']} | {r['extra_calls']:.1f} |")
    table = "\n".join(lines)
    with open(os.path.join(RESULTS, f"falsification_seed{seed}.md"), "w") as f:
        f.write(table + "\n")
    print("\n" + table)
    print(f"\nAPI: {api.API_CALLS}")


if __name__ == "__main__":
    main()
