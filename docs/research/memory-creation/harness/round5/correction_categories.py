"""Whether a structure's corrected column leans on categories that hold two values.

Three of the twenty categories — `pet`, `gym` and `testing` — carry corrections
that read as a second true fact rather than a replacement, which is what
`harness/round4/marker_following.py` measured when an unannounced restatement
of them stopped being retired. Every structure is scored on the same probes, so
the aggregate is not distorted; an advantage that sits only in those categories
is a narrower claim than one that holds across the column, and this says which
it is.

Replays the committed runs and refuses live calls, so it makes no API call and
requires the runs to have been made first.
"""
from __future__ import annotations
import json
import os
import random
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "round4"))
sys.path.insert(0, HERE)

import api
from dataset import CATEGORIES, build
from scoring import classify
import structures as S
import structures_a as SA

RESULTS = os.path.join(HERE, "..", "..", "results_round5")

# The categories whose two values can be simultaneously true of one person.
TWO_VALUED = ("pet", "gym", "testing")

SEEDS = (7, 11)
STRUCTURES = {
    "B3": S.DeciderNeighbors,
    "B9": S.DeciderNeighborsWide,
    "B14": S.KeyUpsertPluralAware,
    "B17": SA.KeyUpsertQuestionKey,
}


def _refuse(*_args, **_kwargs):
    raise RuntimeError("live call refused: this replay runs from the cache")


def category_of(probe):
    for name, question, *_ in CATEGORIES:
        if probe.question == question.format(p=probe.subject):
            return name
    raise KeyError(probe.question)


def replay(cls, seed, turns, probes):
    """Latest-wins per correction, keyed by the probe's category."""
    inst = cls(random.Random(seed))
    for text, is_fact in turns:
        inst.turn(text, is_fact)

    scored = {}
    for p in probes:
        if p.kind != "corrected":
            continue
        recalled = inst.recall(p.question)
        hit = any(classify(inst, p.subject, v, recalled) == "recalled"
                  for v in p.expect_present)
        clean = not any(classify(inst, p.subject, v, recalled) == "recalled"
                        for v in p.expect_absent)
        scored.setdefault(category_of(p), []).append(1.0 if (hit and clean) else 0.0)
    return scored


def split(scored):
    two = [x for name, xs in scored.items() if name in TWO_VALUED for x in xs]
    rest = [x for name, xs in scored.items() if name not in TWO_VALUED for x in xs]
    return two, rest


def rate(xs):
    return round(sum(xs) / len(xs), 4) if xs else None


def main():
    api._post = _refuse

    out = {}
    for tag, cls in STRUCTURES.items():
        for seed in SEEDS:
            turns, probes = build(seed=seed)
            scored = replay(cls, seed, turns, probes)
            two, rest = split(scored)
            out.setdefault(tag, {})[seed] = {
                "structure": cls.NAME,
                "two_valued": {"n": len(two), "latest_wins": rate(two)},
                "single_valued": {"n": len(rest), "latest_wins": rate(rest)},
                "by_category": {name: {"n": len(xs), "latest_wins": rate(xs)}
                                for name, xs in sorted(scored.items())},
            }
            print(f"{tag} seed {seed}: {TWO_VALUED} {rate(two)} on {len(two)}, "
                  f"rest {rate(rest)} on {len(rest)}", flush=True)

    os.makedirs(RESULTS, exist_ok=True)
    path = os.path.join(RESULTS, "correction_categories.json")
    with open(path, "w") as f:
        json.dump({"two_valued_categories": list(TWO_VALUED), "seeds": list(SEEDS),
                   "structures": out}, f, indent=2)

    print(f"\n| structure | {'/'.join(TWO_VALUED)} | the other 17 | difference |")
    print("|---|---|---|---|")
    for tag, per_seed in out.items():
        two = [per_seed[s]["two_valued"] for s in SEEDS]
        rest = [per_seed[s]["single_valued"] for s in SEEDS]
        a = sum(r["latest_wins"] for r in two) / len(two)
        b = sum(r["latest_wins"] for r in rest) / len(rest)
        print(f"| {tag} | {a:.2f} | {b:.2f} | {a - b:+.2f} |")
    print(f"\nwrote {path}")


if __name__ == "__main__":
    main()
