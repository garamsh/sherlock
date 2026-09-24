"""What a cheaper decider call schedule could buy, on the cached replies.

Every round-4 structure calls the decider once per turn, chatter included. This
replays B9 and B14 against the cache and records what each call did to the
store, cross-tabulated against the turn's label, then replays both with the
chatter turns dropped to bound what a perfect gate would save and what it would
cost in probes.

A call counts as an operation when it wrote a row or retired a live one. Effect
rather than reply shape, because that is what a skipped call forfeits, and
because B14 retires implicitly: an upsert onto an existing key kills the old row
with no retire op in the reply.

Zero-cost. The transport is disabled, and a replay whose prompts are not all in
the cache is reported as uncovered instead of scored.
"""
from __future__ import annotations
import json
import os
import random
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import api
import structures
from dataset import build, CATEGORIES, COEXIST, PERSONS
from scoring import classify, score
from structures import DeciderNeighborsWide, KeyUpsertPluralAware

# Results land beside the report, not beside the caller's shell.
RESULTS = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                       "..", "..", "results_round4")

STRUCTURES = (DeciderNeighborsWide, KeyUpsertPluralAware)
LABELS = ("chatter", "initial", "distractor", "correction", "coexist")


def _refuse(url, payload, timeout=90):
    raise RuntimeError(f"live request to {url}: this replay is cache-only")


class CachedDecider:
    """Serves decider replies from the cache and counts the prompts it lacks.

    An absent prompt returns an empty reply, so one gap does not stop the
    replay and the count covers the whole stream.
    """

    def __init__(self):
        self.misses = 0

    def __call__(self, system, user, model=api.DECIDER_MODEL):
        hit = api.CHAT.get(f"{model}\n{system}\n---\n{user}")
        if hit is None:
            self.misses += 1
            return "{}"
        return hit


def label_turns(turns, probes):
    """Label every turn chatter, correction, coexist, initial or distractor.

    Matching is on the exact turn text. A (person, category) pair falls in
    exactly one of the corrected, control and distractor sets, so a probed
    pair's initial templates cannot also spell a distractor turn.
    """
    corrections = {p.correction_text for p in probes if p.kind == "corrected"}
    coexist = {tmpl.format(p=person, v=value)
               for person in PERSONS[:4]
               for _, _, values, tmpl in COEXIST
               for value in values}
    templates = {question.format(p=person): inits
                 for person in PERSONS
                 for _, question, _, inits, _ in CATEGORIES}

    initials = set()
    for probe in probes:
        if probe.kind == "coexist":
            continue
        # A corrected probe's initial turn states the value its correction replaced.
        value = (probe.expect_absent[0] if probe.kind == "corrected"
                 else probe.expect_present[0])
        initials.update(t.format(p=probe.subject, v=value)
                        for t in templates[probe.question])

    out = []
    for text, is_fact in turns:
        if not is_fact:
            out.append("chatter")
        elif text in corrections:
            out.append("correction")
        elif text in coexist:
            out.append("coexist")
        elif text in initials:
            out.append("initial")
        else:
            out.append("distractor")
    return out


def _dead(inst):
    return sum(1 for r in inst.store.rows if r.dead)


def replay(cls, seed, turns):
    """Run one structure over the given turns, recording each call's effect.

    Returns (instance, per-turn (rows written, rows retired), cache misses).
    """
    guard = CachedDecider()
    # Every structure resolves `decider` from its own module namespace at call time.
    structures.decider = guard
    inst = cls(random.Random(seed))
    effects = []
    for text, is_fact in turns:
        rows, dead = len(inst.store.rows), _dead(inst)
        inst.turn(text, is_fact)
        effects.append((len(inst.store.rows) - rows, _dead(inst) - dead))
    return inst, effects, guard.misses


def census(labels, effects):
    """Cross-tabulate what the calls did against the turn labels."""
    out = {label: {"turns": 0, "stored": 0, "retired": 0, "no_op": 0,
                   "rows_written": 0, "rows_retired": 0} for label in LABELS}
    for label, (written, retired) in zip(labels, effects):
        row = out[label]
        row["turns"] += 1
        row["rows_written"] += written
        row["rows_retired"] += retired
        row["stored"] += bool(written)
        row["retired"] += bool(retired)
        row["no_op"] += not (written or retired)
    return out


def probe_outcomes(inst, probes):
    """Per expected value: recalled, never_written, retired or not_ranked."""
    out = {}
    for probe in probes:
        rows = inst.recall(probe.question)
        for value in probe.expect_present:
            out[f"{probe.kind}|{probe.subject}|{value}"] = classify(
                inst, probe.subject, value, rows)
    return out


def skipped_writes(inst, labels, probes, full):
    """Probe values lost by dropping only what the chatter turns wrote.

    Isolates the writes a gate forfeits from the downstream effect of running
    the rest of the stream against a different store, which is the half only a
    replay settles. Row.ts is the store clock, ticked once per turn, so it is
    the 1-based index of the turn that wrote the row.
    """
    chatter = {i + 1 for i, label in enumerate(labels) if label == "chatter"}
    removed = [r for r in inst.store.rows if r.ts in chatter and not r.dead]
    for row in removed:
        row.dead = True
    after = probe_outcomes(inst, probes)
    for row in removed:
        row.dead = False
    return {"rows_removed": [r.content for r in removed],
            "probes_changed": {k: [full[k], v] for k, v in after.items()
                               if full[k] != v}}


def gate(cls, seed, turns, labels, probes, full):
    """Replay the structure with every chatter turn dropped.

    The upper bound on a gate: no classifier separates the labels better than
    the labels themselves. Reports the calls saved, and the probes the skipped
    calls cost, whenever the cache covers the shortened stream.
    """
    kept = [turn for turn, label in zip(turns, labels) if label != "chatter"]
    inst, _, misses = replay(cls, seed, kept)
    out = {"calls": len(kept), "calls_saved": len(turns) - len(kept),
           "saved_fraction": round((len(turns) - len(kept)) / len(turns), 4),
           "cache_misses": misses, "covered": misses == 0}
    if misses:
        return out

    after = probe_outcomes(inst, probes)
    out["items"] = inst.size()
    out["score"] = score(inst, probes)
    out["probes_changed"] = {k: [full[k], v]
                             for k, v in after.items() if full[k] != v}
    return out


def measure(cls, seed, turns, labels, probes):
    """One structure's census, headroom, safety counts and gate bound."""
    inst, effects, misses = replay(cls, seed, turns)
    if misses:
        raise RuntimeError(f"{cls.NAME}: {misses} cached replies missing; "
                           "the full replay must be complete")
    by_label = census(labels, effects)
    no_op = sum(row["no_op"] for row in by_label.values())
    full = probe_outcomes(inst, probes)
    return {
        "calls": inst.calls,
        "items": inst.size(),
        "census": by_label,
        "no_op_calls": no_op,
        "no_op_fraction": round(no_op / len(turns), 4),
        "stores_on_chatter": by_label["chatter"]["rows_written"],
        "retires_on_chatter": by_label["chatter"]["rows_retired"],
        "score": score(inst, probes),
        "skipped_writes": skipped_writes(inst, labels, probes, full),
        "gate": gate(cls, seed, turns, labels, probes, full),
    }


def report(name, data):
    print(f"\n{name}: {data['calls']} calls, {data['items']} items")
    print("label       turns  stored  retired   no-op  rows written  rows retired")
    for label in LABELS:
        row = data["census"][label]
        print(f"{label:10} {row['turns']:6} {row['stored']:7} {row['retired']:8} "
              f"{row['no_op']:7} {row['rows_written']:13} {row['rows_retired']:13}")
    print(f"no operation at all: {data['no_op_calls']}/{data['calls']} "
          f"({data['no_op_fraction']:.1%})")
    print(f"landed on chatter: {data['stores_on_chatter']} stores, "
          f"{data['retires_on_chatter']} retires")

    skipped = data["skipped_writes"]
    print(f"dropping those writes alone costs "
          f"{len(skipped['probes_changed'])} probe values:")
    for content in skipped["rows_removed"]:
        print(f"    {content}")

    gated = data["gate"]
    print(f"chatter gate: {gated['calls_saved']} calls saved "
          f"({gated['saved_fraction']:.1%}), {gated['calls']} remain")
    if not gated["covered"]:
        print(f"  not scored: {gated['cache_misses']} of {gated['calls']} "
              "prompts absent from the cache")
        return
    print(f"  net {data['score']['net']} -> {gated['score']['net']}, "
          f"items {data['items']} -> {gated['items']}, "
          f"probe values changed: {len(gated['probes_changed'])}")
    for key, (before, after) in gated["probes_changed"].items():
        print(f"    {key}: {before} -> {after}")


def main():
    seed = int(os.environ.get("SEED", "7"))
    api._post = _refuse
    turns, probes = build(seed=seed)
    labels = label_turns(turns, probes)

    out = {"seed": seed, "turns": len(turns),
           "label_counts": {label: labels.count(label) for label in LABELS},
           "structures": {}}
    for cls in STRUCTURES:
        out["structures"][cls.NAME] = measure(cls, seed, turns, labels, probes)

    os.makedirs(RESULTS, exist_ok=True)
    with open(os.path.join(RESULTS, f"call_schedule_seed{seed}.json"), "w") as f:
        json.dump(out, f, indent=2)

    print(f"{out['turns']} turns: " + ", ".join(
        f"{label} {count}" for label, count in out["label_counts"].items()))
    for name, data in out["structures"].items():
        report(name, data)


if __name__ == "__main__":
    main()
