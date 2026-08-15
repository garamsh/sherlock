"""Check the round-5 dataset invariants on three seeds. Makes no API call."""
from __future__ import annotations
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from dataset import COEXIST, build
from round4 import dataset as r4

SEEDS = (7, 11, 23)
MIN_FAMILIES, MIN_PROBES, MAX_FAMILY_SHARE = 8, 60, 0.15

TEMPLATES = {family: tmpl for family, _, _, _, tmpl in COEXIST}


def coexist_texts(probes):
    """The statements the coexist block planted, rebuilt from the probes."""
    return {TEMPLATES[p.family].format(p=p.subject, v=v)
            for p in probes if p.kind == "coexist" for v in p.expect_present}


def round4_coexist_texts():
    return {tmpl.format(p=person, v=v) for person in r4.PERSONS[:4]
            for _, _, values, tmpl in r4.COEXIST for v in values}


def facts_outside(turns, texts):
    return sorted(t for t, is_fact in turns if is_fact and t not in texts)


def probe_key(p):
    return (p.kind, p.subject, p.question, tuple(p.expect_present),
            tuple(p.expect_absent))


def contaminated(probes, turns, texts):
    """Probe values a statement from another family also answers.

    Scoring matches a row by substring on the subject and the value, so a value
    that appears in a turn the probe was not planted for scores as a recall.
    """
    coexist_turns = sorted(texts)
    other_turns = facts_outside(turns, texts)
    hits = []
    for p in probes:
        pool = other_turns if p.kind == "coexist" else coexist_turns
        subject = p.subject.lower()
        for value in list(p.expect_present) + list(p.expect_absent):
            needle = value.lower()
            hits += [(p.subject, value, t) for t in pool
                     if subject in t.lower() and needle in t.lower()]
    return hits


def check_seed(seed, failures):
    turns, probes = build(seed=seed)
    texts = coexist_texts(probes)
    coexist = [p for p in probes if p.kind == "coexist"]
    families: dict[str, list] = {}
    for p in coexist:
        families.setdefault(p.family, []).append(p)

    kinds: dict[str, int] = {}
    for p in probes:
        kinds[p.kind] = kinds.get(p.kind, 0) + 1
    facts = sum(1 for _, is_fact in turns if is_fact)
    values = sum(len(p.expect_present) for p in coexist)
    share = max(len(ps) for ps in families.values()) / len(coexist)

    print(f"seed {seed}: {len(turns)} turns, {facts} fact turns, "
          f"{len(turns) - facts} chatter, probes {kinds}")
    print(f"  coexist: {len(families)} families, {len(coexist)} probes, "
          f"{values} values, largest family {share:.1%} of the probes")
    print(f"  subjects: {', '.join(sorted({p.subject for p in coexist}))}")
    for family, ps in sorted(families.items()):
        print(f"    {family}: {len(ps)} probes, "
              f"{sum(len(p.expect_present) for p in ps)} values")

    if len(families) < MIN_FAMILIES:
        failures.append(f"seed {seed}: {len(families)} coexist families")
    if len(coexist) < MIN_PROBES:
        failures.append(f"seed {seed}: {len(coexist)} coexist probes")
    if share > MAX_FAMILY_SHARE:
        failures.append(f"seed {seed}: one family holds {share:.1%} of the probes")

    bad = contaminated(probes, turns, texts)
    if bad:
        failures.append(f"seed {seed}: {len(bad)} probe values answered by "
                        f"another family, e.g. {bad[0]}")

    r4_turns, r4_probes = r4.build(seed=seed)
    if facts_outside(turns, texts) != facts_outside(r4_turns, round4_coexist_texts()):
        failures.append(f"seed {seed}: non-coexist fact turns differ from round 4")
    if (sorted(probe_key(p) for p in probes if p.kind != "coexist")
            != sorted(probe_key(p) for p in r4_probes if p.kind != "coexist")):
        failures.append(f"seed {seed}: non-coexist probes differ from round 4")

    return {p.subject for p in coexist}, texts


def main():
    failures: list[str] = []
    drawn = {seed: check_seed(seed, failures) for seed in SEEDS}

    for i, a in enumerate(SEEDS):
        for b in SEEDS[i + 1:]:
            if drawn[a][0] == drawn[b][0]:
                failures.append(f"seeds {a} and {b} draw the same coexist subjects")
            if drawn[a][1] == drawn[b][1]:
                failures.append(f"seeds {a} and {b} plant the same coexist turns")
            shared = len(drawn[a][1] & drawn[b][1])
            print(f"seeds {a} vs {b}: "
                  f"{len(drawn[a][0] & drawn[b][0])} subjects and "
                  f"{shared} coexist statements in common")

    if failures:
        print("\nFAIL")
        for line in failures:
            print(f"  {line}")
        raise SystemExit(1)
    print("\nOK")


if __name__ == "__main__":
    main()
