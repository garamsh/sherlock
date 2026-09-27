"""Check the round-5 dataset and the match rule that reads it. Makes no API call."""
from __future__ import annotations
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from dataset import CATEGORIES, CHATTER, COEXIST, PERSONS, build
from round4 import dataset as r4, scoring as r4_scoring
from scoring import score, states

SEEDS = (7, 11, 23)
MIN_FAMILIES, MIN_PROBES, MAX_FAMILY_SHARE = 8, 60, 0.15
NEEDLE_KINDS = ("coexist value", "category value", "person")
# Round 5 copies round 4's aggregation. These are the metrics that copy produces;
# the outcome counts under them answer to the match rule, which is not shared.
METRICS = ("latest_wins", "control_survival", "coexist_preserved",
           "stale_present", "net")

TEMPLATES = {family: tmpl for family, _, _, _, tmpl in COEXIST}

# (row text, value, credited): the words round 4's substring match got wrong,
# and the multi-word and punctuated values that must still be credited.
MATCH_CASES = [
    ("Vera has since standardized on Python.", "Zed", False),
    ("Maria is allergic to shellfish.", "fish", False),
    ("Alma's team moved tracing to Datadog.", "tea", False),
    ("These days Yuki goes for coffee instead.", "Go", False),
    ("Bruno's web stack is Django.", "Go", False),
    ("Lisa writes code in VS Code.", "VS Code", True),
    ("Otto indents with four spaces.", "four spaces", True),
    ("Otto plays the cello.", "the cello", True),
    ("Sam writes end-to-end tests.", "end-to-end tests", True),
    ("Nina writes property-based tests.", "property-based tests", True),
    ("Kofi deploys on Fly.io.", "Fly.io", True),
    ("Maria's morning ritual is tea.", "Maria", True),
]


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


def _sentence(template):
    """A template without its placeholders: the words every row built from it carries."""
    return template.replace("{p}", " ").replace("{v}", " ")


def phrases():
    """Every phrase the scorer matches, and every phrase it matches against.

    A chatter line names no subject, so no probe can be credited from one
    whatever it says; a row built from any other kind carries a subject.
    """
    out = [(v, "coexist value") for _, _, pool, _, _ in COEXIST for v in pool]
    out += [(v, "category value") for _, _, values, _, _ in CATEGORIES for v in values]
    out += [(p, "person") for p in PERSONS]
    for _, question, _, inits, corrs in CATEGORIES:
        out += [(_sentence(t), "sentence") for t in [question] + inits + corrs]
    for _, question, _, _, tmpl in COEXIST:
        out += [(_sentence(t), "sentence") for t in (question, tmpl)]
    return out + [(t, "chatter") for t in CHATTER]


def collisions():
    """Values and names that sit inside another phrase of the vocabulary.

    A turn is a sentence, a subject and a value, so this covers every way one
    family's turn can carry another probe's value.
    """
    vocabulary = phrases()
    found = []
    for needle, kind in vocabulary:
        if kind not in NEEDLE_KINDS:
            continue
        found += [(needle, kind, text, text_kind, states(text, needle))
                  for text, text_kind in vocabulary
                  if text != needle and needle.lower() in text.lower()]
    return found


def check_matching(failures):
    for text, value, credited in MATCH_CASES:
        if states(text, value) is not credited:
            failures.append(f"{value!r} in {text!r}: credited "
                            f"{not credited}, expected {credited}")


def check_vocabulary(failures):
    found = collisions()
    for needle, kind, text, text_kind, word in found:
        if "coexist value" in (kind, text_kind):
            failures.append(f"round-5 vocabulary: {needle!r} sits inside {text!r}")
        elif word and text_kind != "chatter":
            failures.append(f"{needle!r} is a whole word inside {text!r}, which "
                            f"a row carries alongside a subject")

    grouped: dict[str, list] = {}
    for needle, _, text, _, word in found:
        grouped.setdefault(needle, []).append((text.strip(), word))
    print(f"vocabulary: {len(found)} substring collisions over {len(grouped)} "
          f"values, {sum(1 for c in found if c[4])} of them whole words")
    for needle, texts in sorted(grouped.items()):
        mark = " (whole word)" if any(word for _, word in texts) else ""
        print(f"  {needle!r} inside {len(texts)}, e.g. {texts[0][0]!r}{mark}")


class _Row:
    def __init__(self, content):
        self.content, self.dead = content, False


class _Store:
    def __init__(self, rows):
        self.rows = rows


class _EveryFact:
    """A store holding every fact turn, live, and recalling all of them.

    Both scorers read the same rows, so a metric that differs between them is
    the aggregation differing rather than the store or the ranking.
    """

    def __init__(self, turns):
        self.store = _Store([_Row(t) for t, is_fact in turns if is_fact])

    def recall(self, question):
        return self.store.rows


def check_aggregation(turns, probes, seed, failures):
    """Score one store through both rounds' scorers and compare the metrics.

    Only this direction runs: round 5's `score()` reads a coexist probe's
    family, which a round-4 probe does not carry.
    """
    inst = _EveryFact(turns)
    theirs, ours = r4_scoring.score(inst, probes), score(inst, probes)
    differing = [m for m in METRICS if ours.get(m) != theirs.get(m)]
    for m in differing:
        failures.append(f"seed {seed}: {m} is {ours.get(m)} against "
                        f"round 4's {theirs.get(m)}")
    print(f"  aggregation: {len(METRICS) - len(differing)} of {len(METRICS)} "
          f"metrics equal to round 4's scorer, net {ours['net']}")


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

    check_aggregation(turns, probes, seed, failures)

    r4_turns, r4_probes = r4.build(seed=seed)
    if facts_outside(turns, texts) != facts_outside(r4_turns, round4_coexist_texts()):
        failures.append(f"seed {seed}: non-coexist fact turns differ from round 4")
    if (sorted(probe_key(p) for p in probes if p.kind != "coexist")
            != sorted(probe_key(p) for p in r4_probes if p.kind != "coexist")):
        failures.append(f"seed {seed}: non-coexist probes differ from round 4")

    return {p.subject for p in coexist}, texts


def main():
    failures: list[str] = []
    check_matching(failures)
    check_vocabulary(failures)
    drawn = {seed: check_seed(seed, failures) for seed in SEEDS}

    for i, a in enumerate(SEEDS):
        for b in SEEDS[i + 1:]:
            if drawn[a][0] == drawn[b][0]:
                failures.append(f"seeds {a} and {b} draw the same coexist subjects")
            if drawn[a][1] == drawn[b][1]:
                failures.append(f"seeds {a} and {b} plant the same coexist turns")
            print(f"seeds {a} vs {b}: "
                  f"{len(drawn[a][0] & drawn[b][0])} subjects and "
                  f"{len(drawn[a][1] & drawn[b][1])} coexist statements in common")

    if failures:
        print("\nFAIL")
        for line in failures:
            print(f"  {line}")
        raise SystemExit(1)
    print("\nOK")


if __name__ == "__main__":
    main()
