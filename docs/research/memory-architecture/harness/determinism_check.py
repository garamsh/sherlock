"""Checks that the large-scale runner builds and scores the same run every time.

Its workload draws each session's topics from a pseudo-random stream. That stream
was the global one, seeded from the OS at import, so every process built a
different workload and the committed figures recorded one arbitrary draw. The draw
runs off `large_runner.TOPIC_SEED` now, and this asserts it: the workload and one
architecture's retrievals are rebuilt in subprocesses under different
`PYTHONHASHSEED` values and must come out identical.

Runs one architecture over a reduced workload, so it costs seconds rather than the
minutes a full run costs. Exits non-zero when a digest differs between seeds.
"""
from __future__ import annotations
import os
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
SEEDS = ("0", "1", "12345")

PROBE = """
import hashlib, sys
sys.path.insert(0, "__HERE__")
import large_runner as L, architectures

cur, noise, queries = L.make_large_workload(10, 100, 100)
run = L.eval_one("M6", architectures.LsmMemory, n_sessions=10)
retrieved = "".join(r.content for hits in run["results_by_query"].values()
                    for r in hits[:5])
for label, value in (("workload", cur), ("noise", noise), ("queries", queries),
                     ("retrieved", retrieved), ("items", run["item_count"])):
    print(f"{label}={hashlib.sha256(repr(value).encode()).hexdigest()[:16]}")
"""


def digests(hash_seed):
    """The probe's digests, from a subprocess started at this hash seed."""
    env = dict(os.environ, PYTHONHASHSEED=hash_seed)
    out = subprocess.run([sys.executable, "-c", PROBE.replace("__HERE__", HERE)],
                         capture_output=True, text=True, env=env, check=True)
    return dict(line.split("=", 1) for line in out.stdout.strip().splitlines())


def main():
    runs = {seed: digests(seed) for seed in SEEDS}
    first = runs[SEEDS[0]]
    failed = []
    print(f"large_runner over {len(SEEDS)} hash seeds: {', '.join(SEEDS)}\n")
    for label in first:
        values = {seed: runs[seed][label] for seed in SEEDS}
        agree = len(set(values.values())) == 1
        print(f"  {'ok ' if agree else 'DIFFERS'} {label:10} {first[label]}")
        if not agree:
            for seed, value in values.items():
                print(f"      PYTHONHASHSEED={seed}: {value}")
            failed.append(label)

    if failed:
        print(f"\n{len(failed)} stage(s) differ between hash seeds: "
              f"{', '.join(failed)}")
        return 1
    print("\nthe run is the same at every seed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
