"""Draw W's stratified probe sample from #890's corpus (preregistration.md §W).

    python3 docs/research/memory-paradigms/validation/sample.py   # writes sample.json

Per family, the probe IDs in corpus order are sampled without replacement
by random.Random(SEED) and listed in corpus order; re-running rewrites
sample.json byte-identically.
"""

import json
import random
from pathlib import Path

HERE = Path(__file__).resolve().parent
CORPUS = HERE.parent.parent / "memory-encoding" / "validation" / "corpus.json"
SEED = 900
SIZES = {"C2": 25, "C3": 25, "N": 30, "K": 20, "NC": 10}


def draw(corpus, sizes=SIZES, seed=SEED):
    rng = random.Random(seed)
    probes = corpus["chains"] + corpus["guards"]
    out = {}
    for family, n in sizes.items():
        ids = [p["id"] for p in probes if p["family"] == family]
        picked = set(rng.sample(ids, n))
        out[family] = [i for i in ids if i in picked]
    return out


def main():
    sample = draw(json.loads(CORPUS.read_text()))
    (HERE / "sample.json").write_text(json.dumps({"seed": SEED, "sizes": SIZES, "probes": sample}, indent=1) + "\n")
    print({f: len(v) for f, v in sample.items()}, sum(len(v) for v in sample.values()))


if __name__ == "__main__":
    main()
