"""Did B16 stop writing, or reword the value out of the fact it wrote?

B16's largest single loss is `never_written`, against 0.00 for B9. The scorer
counts a fact as written only when some row carries both the subject and the
value as substrings, so two different things land in that bucket: a turn the
decider stored nothing for, and a turn it stored a paraphrase that dropped the
value word. They have opposite readings, so this replays both structures and
records, for every fact the scorer calls never-written, the turn that stated
it and what that turn stored.

Every decider reply is cached, so the replay makes no API call.
"""
from __future__ import annotations
import json
import os
import random
import sys
from collections import Counter

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from api import embed_batch, flush
from dataset import build
from structures import DeciderNeighborsWide
from structures_b import DeciderNeighborsVerdicts

# Results land beside the report, not beside the caller's shell.
RESULTS = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                       "..", "..", "results_round4")


def traced(base):
    """The structure, logging the rows each turn added."""
    class T(base):
        def __init__(self, rnd, model=None):
            super().__init__(rnd, model=model)
            self.log = {}

        def turn(self, text, is_fact):
            before = len(self.store.rows)
            super().turn(text, is_fact)
            self.log.setdefault(text, []).extend(
                r.content for r in self.store.rows[before:])
    return T


def run(base, seed, turns):
    inst = traced(base)(random.Random(seed))
    for text, is_fact in turns:
        inst.turn(text, is_fact)
    return inst


def main():
    seed = int(os.environ.get("SEED", "7"))
    turns, probes = build(seed=seed)
    embed_batch([t for t, _ in turns] + [p.question for p in probes])

    # Every planted fact, with the turn that stated it. An initial statement is
    # not carried on the probe, so it is recovered by matching subject and
    # value against the fact turns.
    fact_turns = [t for t, f in turns if f]
    wanted = []
    for p in probes:
        if p.kind == "corrected":
            wanted.append((p.subject, p.expect_absent[0], "corrected-old"))
            wanted.append((p.subject, p.expect_present[0], "corrected-new"))
        else:
            for v in p.expect_present:
                wanted.append((p.subject, v, p.kind))

    out = {}
    for label, base in (("B9", DeciderNeighborsWide),
                        ("B16", DeciderNeighborsVerdicts)):
        inst = run(base, seed, turns)
        kinds = Counter()
        misses = []
        for subject, value, kind in wanted:
            written = any(subject.lower() in r.content.lower()
                          and value.lower() in r.content.lower()
                          for r in inst.store.rows)
            if written:
                kinds[f"{kind}:written"] += 1
                continue
            kinds[f"{kind}:never_written"] += 1
            src = [t for t in fact_turns
                   if subject.lower() in t.lower() and value.lower() in t.lower()]
            misses.append({"subject": subject, "value": value, "kind": kind,
                           "turns": src,
                           "stored": [s for t in src for s in inst.log.get(t, [])]})
        out[label] = {"structure": base.NAME,
                      "fact_turns": len(fact_turns),
                      "rows_written": len(inst.store.rows),
                      "facts_planted": len(wanted),
                      "never_written": len(misses),
                      "turn_stored_nothing": sum(1 for m in misses
                                                 if not m["stored"]),
                      "turn_stored_a_reworded_fact": sum(1 for m in misses
                                                         if m["stored"]),
                      "kinds": dict(kinds), "misses": misses}

    flush()
    os.makedirs(RESULTS, exist_ok=True)
    path = os.path.join(RESULTS, f"b16_writes_seed{seed}.json")
    with open(path, "w") as f:
        json.dump({"seed": seed, "structures": out}, f, indent=2)

    for label in ("B9", "B16"):
        d = out[label]
        print(f"=== {label} {d['structure']} (seed {seed}) ===")
        print(f"  rows ever written: {d['rows_written']} "
              f"on {d['fact_turns']} fact turns")
        print(f"  facts the scorer calls never-written: {d['never_written']} "
              f"of {d['facts_planted']}")
        print(f"    turn stored nothing:         {d['turn_stored_nothing']}")
        print(f"    turn stored a reworded fact: "
              f"{d['turn_stored_a_reworded_fact']}")
        for m in d["misses"][:8]:
            print(f"    [{m['kind']}] {m['subject']} / {m['value']}")
            for t in m["turns"]:
                print(f"       turn:   {t}")
            print(f"       stored: {m['stored']}")
        print()
    print(f"wrote {path}")


if __name__ == "__main__":
    main()
