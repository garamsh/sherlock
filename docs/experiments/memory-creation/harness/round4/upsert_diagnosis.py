"""Where a key-addressed correction lands, and what the keys are named.

A key structure carries a correction only when the incoming fact reaches the
key the old value already lives under. Every miss is one of three things: the
old key was never offered, it was offered and the decider coined a new one
anyway, or no live row held the old value at all. The split says whether to
widen the offer or to change how a key is defined.

It also counts keys that name their own value. `alma.tea` cannot survive a
switch to coffee, so a key naming a value is a correction the structure has
already lost; a key naming the question it answers is not.

Replays B14, B15 and B17 on the committed cache, one result file each.
Every decider reply is cached, so the replay makes no API call.
"""
from __future__ import annotations
import ast
import json
import os
import random
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import structures
import structures_a
from api import embed_batch, flush
from dataset import CATEGORIES, COEXIST, build
from structures import KeyUpsertPluralAware
from structures_a import KeyUpsertNamespace, KeyUpsertQuestionKey

# Results land beside the report, not beside the caller's shell.
RESULTS = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                       "..", "..", "results_round4")

STRUCTURES = {"B14": KeyUpsertPluralAware, "B15": KeyUpsertNamespace,
              "B17": KeyUpsertQuestionKey}


def recorded(*modules):
    """Swap each module's decider for one appending every offer to a list."""
    offers = []

    def wrap(inner):
        def recording(system, user, **kw):
            offers.append(user)
            return inner(system, user, **kw)
        return recording

    for module in modules:
        module.decider = wrap(module.decider)
    return offers


def offered_keys(user):
    """The keys the decider was shown, read back off the prompt it received."""
    marker = "Existing keys: "
    if marker not in user:
        return []
    return list(ast.literal_eval(user.split(marker, 1)[1]))


def _bare(value):
    """The value as a key segment: no article, lowercased."""
    for article in ("the ", "a ", "an "):
        if value.lower().startswith(article):
            return value[len(article):].lower()
    return value.lower()


def vocabularies():
    """Single-valued answers and multi-answer ones, kept apart.

    A key naming a single-valued answer is a correction already lost. A key
    naming one of a coexist family's answers is the plural split B14 and B17
    are instructed to make, so the two cannot be counted together.
    """
    single = {_bare(v) for _, _, vals, _, _ in CATEGORIES for v in vals}
    multi = {_bare(v) for _, _, vals, _ in COEXIST for v in vals}
    return single - multi, multi


def value_named(key, vocabulary):
    """A key whose own segments spell out the answer rather than the question."""
    return [seg for seg in key.split(".")[1:] if seg.lower() in vocabulary]


def holders(inst, subject, value):
    """Live rows carrying both the subject and the value."""
    return [r for r in inst.store.rows
            if not r.dead and subject.lower() in r.content.lower()
            and value.lower() in r.content.lower()]


def run(cls, seed, turns, probes, offers):
    corrections = {p.correction_text: p for p in probes
                   if p.kind == "corrected" and p.correction_text}
    del offers[:]
    inst = cls(random.Random(seed))
    per_turn = []
    for text, is_fact in turns:
        probe = corrections.get(text)
        if probe is None:
            inst.turn(text, is_fact)
            continue
        old = probe.expect_absent[0]
        old_keys = sorted({r.key for r in holders(inst, probe.subject, old) if r.key})
        before = len(inst.store.rows)
        inst.turn(text, is_fact)
        written = list(dict.fromkeys(r.key for r in inst.store.rows[before:] if r.key))
        offer = offered_keys(offers[-1]) if offers else []
        if not old_keys:
            outcome = "no_live_old_row"
        elif set(written) & set(old_keys):
            outcome = "reused_key"
        elif set(offer) & set(old_keys):
            outcome = "coined_key_offered"
        else:
            outcome = "coined_key_not_offered"
        per_turn.append({"text": text, "old": old, "outcome": outcome,
                         "old_keys": old_keys, "written_keys": written,
                         "n_offered": len(offer)})

    single, multi = vocabularies()
    live = {r.key for r in inst.store.rows if not r.dead and r.key}
    stats = {}
    for entry in per_turn:
        stats[entry["outcome"]] = stats.get(entry["outcome"], 0) + 1
    return {"stats": stats, "per_turn": per_turn, "live_keys": len(live),
            "value_named_keys": sorted(k for k in live if value_named(k, single)),
            "split_keys": sorted(k for k in live if value_named(k, multi))}


def main():
    seed = int(os.environ.get("SEED", "7"))
    turns, probes = build(seed=seed)
    embed_batch([t for t, _ in turns] + [p.question for p in probes])
    offers = recorded(structures, structures_a)

    os.makedirs(RESULTS, exist_ok=True)
    for tag, cls in STRUCTURES.items():
        out = run(cls, seed, turns, probes, offers)
        path = os.path.join(RESULTS, f"upsert_diagnosis_{tag}_seed{seed}.json")
        with open(path, "w") as f:
            json.dump({"seed": seed, **out}, f, indent=2)

        widths = [e["n_offered"] for e in out["per_turn"]]
        coined = sum(v for k, v in out["stats"].items() if k.startswith("coined"))
        print(f"=== {tag} {cls.NAME} (seed {seed}) ===")
        print(f"  {out['stats']}")
        print(f"  coined a new key: {coined}")
        print(f"  keys offered per correction turn: {min(widths)}-{max(widths)}")
        print(f"  live keys: {out['live_keys']}, naming a single-valued answer: "
              f"{len(out['value_named_keys'])} {out['value_named_keys'][:6]}")
        print(f"  keys splitting a coexist family: {len(out['split_keys'])}")
        print(f"  wrote {path}")
    flush()


if __name__ == "__main__":
    main()
