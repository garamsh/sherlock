"""Writes the memory-creation separability pair set where the Go harness can read it.

The pairs come from `memory-creation/harness/round4/separability.py` unchanged —
`build_pairs(seed=7)`, 250 pairs per family — so the question this experiment
re-asks on the hashing embedder is the same question, on the same sentences,
that produced the committed OpenAI figures. Reproducing them in Go is not
possible: the draw is Python's own random module.

Reads committed files only; makes no API call and needs no key.

    python3 docs/experiments/memory-retrieval/harness/pairs_from_round4.py
"""
from __future__ import annotations
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROUND4 = os.path.join(HERE, "..", "..", "memory-creation", "harness", "round4")
RESULTS = os.path.join(HERE, "..", "results")

sys.path.insert(0, ROUND4)

import separability  # noqa: E402  (path must be set first)

SEED = 7


def main():
    families = separability.build_pairs(seed=SEED)
    out = {
        "seed": SEED,
        "source": "memory-creation/harness/round4/separability.py build_pairs",
        "must_supersede": {"correction": True, "coexist": False,
                           "cross-subject": False, "unrelated": False},
        "families": {name: [{"old": a, "new": b} for a, b in pairs]
                     for name, pairs in families.items()},
    }
    path = os.path.join(RESULTS, f"separability_pairs_seed{SEED}.json")
    with open(path, "w") as f:
        json.dump(out, f, indent=1)
        f.write("\n")
    counts = {k: len(v) for k, v in families.items()}
    print(f"wrote {path}: {counts}")


if __name__ == "__main__":
    main()
