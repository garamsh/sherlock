"""What round 4's substring match credits that a word-boundary match would not.

`scoring.py` credits a value wherever it appears inside a row, so "standardized
on Python" credits `Zed` and "allergic to shellfish" credits `fish`. This
replays the scored structures and counts the credits that hold only as a
substring, against `harness/round5/scoring.py`, which states the same values on
a word boundary. Both rules are committed, so the count is the distance between
them rather than a third rule invented here.

A credit lands in one of two places: on a value the probe expects present, where
it hands out a recall the row does not state, or on a value the probe expects
absent, where it holds a superseded value present and costs the probe its
`clean`.

Every decider reply is cached, so the replay makes no API call.
"""
from __future__ import annotations
import importlib.util
import json
import os
import random
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import scoring as round4
from api import embed_batch, flush
from dataset import build
from structures import DeciderNeighborsWide, KeyUpsertPluralAware
from structures_a import KeyUpsertQuestionKey


def _load(name, path):
    """Import a module by path; both rounds name their scorer `scoring`."""
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


round5 = _load("round5_scoring",
               os.path.join(HERE, "..", "round5", "scoring.py"))

# Results land beside the report, not beside the caller's shell.
RESULTS = os.path.join(HERE, "..", "..", "results_round4")

STRUCTURES = {"B9": DeciderNeighborsWide, "B14": KeyUpsertPluralAware,
              "B17": KeyUpsertQuestionKey}


def run(cls, seed, turns, probes):
    inst = cls(random.Random(seed))
    for text, is_fact in turns:
        inst.turn(text, is_fact)

    credits = []
    for p in probes:
        rows = inst.recall(p.question)
        for value, expected in ([(v, "present") for v in p.expect_present]
                                + [(v, "absent") for v in p.expect_absent]):
            if not round4._in_recall(rows, p.subject, value):
                continue
            if round5._in_recall(rows, p.subject, value):
                continue
            credits.append({
                "family": p.kind, "subject": p.subject, "value": value,
                "expected": expected,
                "rows": [r.content for r in rows
                         if round4._in_recall([r], p.subject, value)]})
    return {"structure": cls.NAME, "substring_only_credits": len(credits),
            "credits": credits}


def main():
    seed = int(os.environ.get("SEED", "7"))
    turns, probes = build(seed=seed)
    embed_batch([t for t, _ in turns] + [p.question for p in probes])

    out = {tag: run(cls, seed, turns, probes) for tag, cls in STRUCTURES.items()}
    flush()

    os.makedirs(RESULTS, exist_ok=True)
    path = os.path.join(RESULTS, f"substring_credits_seed{seed}.json")
    with open(path, "w") as f:
        json.dump({"seed": seed, "structures": out}, f, indent=2)

    for tag, d in out.items():
        print(f"=== {tag} {d['structure']} (seed {seed}) ===")
        print(f"  credits holding only as a substring: "
              f"{d['substring_only_credits']}")
        for c in d["credits"]:
            print(f"    [{c['family']}/{c['expected']}] {c['subject']} / "
                  f"{c['value']} <- {c['rows']}")
    print(f"wrote {path}")


if __name__ == "__main__":
    main()
