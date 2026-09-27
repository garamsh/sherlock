"""What each structure's decider returned against the 388 fact turns.

B16's largest single loss is `never_written`, against 0.00 for B9. The scorer
counts a fact as written only when some row carries both the subject and the
value as substrings, so two different things land in that bucket: a turn the
decider stored nothing for, and a turn it stored a paraphrase that dropped the
value word. They have opposite readings, so this replays both structures and
records, for every fact the scorer calls never-written, the turn that stated
it and what that turn stored.

The same replay answers two counting questions the record asks of the
key-addressed structures and of B16: how many fact turns came back with an
empty write list, and whether B16 returned a verdict for every candidate it
was shown.

Every decider reply is cached, so the replay makes no API call.
"""
from __future__ import annotations
import json
import os
import random
import sys
from collections import Counter

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import structures
import structures_a
import structures_b
from api import embed_batch, flush, parse_json
from dataset import build
from structures import DeciderNeighborsWide, KeyUpsertPluralAware
from structures_a import KeyUpsertNamespace, KeyUpsertQuestionKey
from structures_b import DeciderNeighborsVerdicts

# Results land beside the report, not beside the caller's shell.
RESULTS = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                       "..", "..", "results_round4")


def recorded(*modules):
    """Swap each module's decider for one appending every reply to a shared list.

    Each structure calls the decider once per turn, so the list runs parallel
    to the turn stream.
    """
    replies = []

    def wrap(inner):
        def recording(system, user, **kw):
            text = inner(system, user, **kw)
            replies.append(text)
            return text
        return recording

    for module in modules:
        module.decider = wrap(module.decider)
    return replies


class VerdictCensus(DeciderNeighborsVerdicts):
    """B16, recording the candidate ids it was shown on each turn."""

    def __init__(self, rnd, model=None):
        super().__init__(rnd, model=model)
        self.shown = []

    def context_rows(self, text):
        rows = super().context_rows(text)
        self.shown.append([r.id for r in rows])
        return rows


def traced(base):
    """The structure, logging the rows each turn added."""
    class T(base):
        def __init__(self, rnd, model=None):
            super().__init__(rnd, model=model)
            self.log = {}
            self.added = []

        def turn(self, text, is_fact):
            before = len(self.store.rows)
            super().turn(text, is_fact)
            self.log.setdefault(text, []).extend(
                r.content for r in self.store.rows[before:])
            self.added.append(len(self.store.rows) - before)
    return T


def run(base, seed, turns, replies):
    del replies[:]
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

    replies = recorded(structures, structures_a, structures_b)
    out = {}
    for label, base in (("B9", DeciderNeighborsWide), ("B14", KeyUpsertPluralAware),
                        ("B15", KeyUpsertNamespace), ("B16", VerdictCensus),
                        ("B17", KeyUpsertQuestionKey)):
        inst = run(base, seed, turns, replies)
        row = {"structure": base.NAME,
               "fact_turns": len(fact_turns),
               "rows_written": len(inst.store.rows),
               # A write the structure discarded as malformed leaves the store
               # untouched, so the turn counts as empty either way.
               "empty_write_turns": sum(1 for (_, is_fact), added
                                        in zip(turns, inst.added)
                                        if is_fact and added == 0)}

        if label == "B16":
            judged = skipped = 0
            for ids, reply in zip(inst.shown, replies):
                answered = {v.get("id") for v in parse_json(reply).get("verdicts")
                            or [] if isinstance(v, dict)}
                judged += sum(1 for i in ids if i in answered)
                skipped += sum(1 for i in ids if i not in answered)
            row["candidates_shown"] = sum(len(ids) for ids in inst.shown)
            row["candidates_judged"] = judged
            row["candidates_skipped"] = skipped

        if label in ("B9", "B16"):
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
            row.update({"facts_planted": len(wanted),
                        "never_written": len(misses),
                        "turn_stored_nothing": sum(1 for m in misses
                                                   if not m["stored"]),
                        "turn_stored_a_reworded_fact": sum(1 for m in misses
                                                           if m["stored"]),
                        "kinds": dict(kinds), "misses": misses})
        out[label] = row

    flush()
    os.makedirs(RESULTS, exist_ok=True)
    path = os.path.join(RESULTS, f"b16_writes_seed{seed}.json")
    with open(path, "w") as f:
        json.dump({"seed": seed, "structures": out}, f, indent=2)

    for label, d in out.items():
        print(f"=== {label} {d['structure']} (seed {seed}) ===")
        print(f"  rows ever written: {d['rows_written']} "
              f"on {d['fact_turns']} fact turns")
        print(f"  fact turns with an empty write list: {d['empty_write_turns']}")
        if "candidates_shown" in d:
            print(f"  candidates shown: {d['candidates_shown']}, "
                  f"judged: {d['candidates_judged']}, "
                  f"skipped: {d['candidates_skipped']}")
        if "never_written" in d:
            print(f"  facts the scorer calls never-written: {d['never_written']} "
                  f"of {d['facts_planted']}")
            print(f"    turn stored nothing:         {d['turn_stored_nothing']}")
            print(f"    turn stored a reworded fact: "
                  f"{d['turn_stored_a_reworded_fact']}")
        print()
    print(f"wrote {path}")


if __name__ == "__main__":
    main()
