"""Checks the round-5 figures generation does not reach.

§Round 5's four tables are written by `record.py` from `../../results_round5/`,
so a cell there cannot disagree with the run behind it and is not read back
here. What is left is what a generated table does not carry: the figures the
section states in its prose — the separations it claims, the leads it names,
what more probes would buy — read against `../../results_round5/` and the
arithmetic `precision.py` does over it.

Round 4's figures are not this file's business. `../round4/claim_sources.py`
covers them, and the two registers read disjoint sections of one document.

Reads committed files only; makes no API calls and needs no key. Exits non-zero
when a check fails or when an anchor no longer matches the README.
"""
from __future__ import annotations
import json
import math
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.join(HERE, "..", "..")
RESULTS = os.path.join(ROOT, "results_round5")

sys.path.insert(0, HERE)
sys.path.insert(0, os.path.join(HERE, ".."))

import precision
import record

README = open(os.path.join(ROOT, "README.md")).read()

# The result files, the pooling and what the record calls each structure are
# `record.py`'s, which writes the tables from them; reading them from there
# keeps one definition rather than two that can disagree.
TAGS = record.ROUND5_SYSTEMS
rows = record.rows
pooled_scores = record.pooled_scores


def section(heading):
    """The README text under one heading, up to the next heading of any level."""
    body = README.split(f"\n{heading}\n", 1)
    if len(body) != 2:
        raise SystemExit(f"heading not found: {heading}")
    return re.split(r"\n#{2,3} ", body[1], maxsplit=1)[0]


ROUND5 = section("## Round 5: the same question on a redrawn coexist set")


def half_up(value, places):
    """Round half away from zero, the direction a reader assumes.

    Settles the value first: a difference of two means carries binary noise that
    would otherwise decide the last digit.
    """
    settled = round(abs(value), places + 6)
    factor = 10 ** places
    return math.floor(settled * factor + 0.5) / factor * (1 if value >= 0 else -1)


class Anchor:
    """One regex over §Round 5 whose capture groups are the stated figures."""

    def __init__(self, pattern, text=None):
        matches = re.findall(pattern, ROUND5 if text is None else text)
        if len(matches) != 1:
            raise SystemExit(
                f"anchor matched {len(matches)} times, expected 1: {pattern}")
        self.groups = matches[0]

    def one(self, index=0):
        g = self.groups
        return g[index] if isinstance(g, tuple) else g


CHECKS = []


def check(part, claim, stated, source, found, places=None):
    """Record one figure read from the README against the committed value."""
    CHECKS.append({"part": part, "claim": claim, "stated": stated,
                   "source": source, "found": found, "places": places})


# --- The dataset ----------------------------------------------------------

def dataset():
    part = "what changed"
    import importlib.util
    spec = importlib.util.spec_from_file_location(
        "round5_dataset", os.path.join(HERE, "dataset.py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    turns, probes = module.build(seed=7)
    families = {}
    for p in probes:
        if p.kind == "coexist":
            families.setdefault(p.family, []).append(p)

    a = Anchor(r"eight families, (\d+) probes and (\d+) coexist statements per seed")
    check(part, "coexist probes", a.one(0), "round5/dataset.py build() coexist probes",
          sum(len(v) for v in families.values()))
    check(part, "coexist statements", a.one(1),
          "round5/dataset.py build() coexist values",
          sum(len(p.expect_present) for v in families.values() for p in v))

    a = Anchor(r"the stream grows from (\d+) turns to (\d+)")
    check(part, "round-5 turns", a.one(1), "round5/dataset.py build() turns", len(turns))

    a = Anchor(r"Round 4's coexist column is (\d+) probes over three families, "
               r"identical under every seed, and one family holds (\d+) of its (\d+) "
               r"values")
    check(part, "families", 8, "round5/dataset.py build() coexist families",
          len(families))


# --- What separates -------------------------------------------------------

def separation():
    part = "separation"
    scores = pooled_scores()
    coexist = "coexist_preserved"
    probes = precision.PROBES[coexist]

    def half(tag, metric=coexist):
        return precision.column_half_width(scores[tag][metric],
                                           precision.PROBES[metric], 2)

    a = Anchor(r"B14 leads B3 and B9 by (\d+\.?\d*) against a summed half-width of "
               r"(\d+\.?\d*), and B17 by (\d+\.?\d*) against (\d+\.?\d*)")
    check(part, "coexist gap to B3 and B9", a.one(0), "precision.py pooled coexist",
          scores["B14"][coexist] - scores["B3"][coexist], 3)
    check(part, "summed half-width against B3", a.one(1),
          "precision.py column_half_width coexist", half("B14") + half("B3"), 3)
    check(part, "coexist gap to B17", a.one(2), "precision.py pooled coexist",
          scores["B14"][coexist] - scores["B17"][coexist], 3)
    check(part, "summed half-width against B17", a.one(3),
          "precision.py column_half_width coexist", half("B14") + half("B17"), 3)

    a = Anchor(r"clear the (\d+\.?\d*) widest spread any structure showed between its "
               r"own seeds")
    data = rows()
    every = [max(data[t][7][m], data[t][11][m]) - min(data[t][7][m], data[t][11][m])
             for t in record.ROUND5_LABELS for m in precision.PROBES]
    check(part, "widest spread anywhere", a.one(), "results_round5 per-column spreads",
          max(every), 3)

    a = Anchor(r"B14's net leads of (\d+\.?\d*) to (\d+\.?\d*) sit inside summed half-widths "
               r"of (\d+\.?\d*) to (\d+\.?\d*)", README)
    net_gaps = sorted(precision.net(scores["B14"]) - precision.net(scores[t])
                      for t in ("B17", "B3", "B9"))
    net_sums = sorted(precision.half_width(scores["B14"], seeds=2)
                      + precision.half_width(scores[t], seeds=2)
                      for t in ("B17", "B3", "B9"))
    check(part, "smallest net lead", a.one(0), "precision.py net", net_gaps[0], 3)
    check(part, "largest net lead", a.one(1), "precision.py net", net_gaps[-1], 3)
    check(part, "smallest summed half-width", a.one(2), "precision.py half_width",
          net_sums[0], 3)
    check(part, "largest summed half-width", a.one(3), "precision.py half_width",
          net_sums[-1], 3)

    a = Anchor(r"the widest gap in either of the other two columns is (\d+\.?\d*) against "
               r"(\d+\.?\d*)")
    widest, widest_sum = 0.0, 0.0
    for metric in ("latest_wins", "control_survival"):
        ordered = sorted(record.ROUND5_LABELS, key=lambda t: -scores[t][metric])
        gap = scores[ordered[0]][metric] - scores[ordered[-1]][metric]
        if gap > widest:
            widest = gap
            widest_sum = half(ordered[0], metric) + half(ordered[-1], metric)
    check(part, "widest gap in the other columns", a.one(0),
          "precision.py pooled latest_wins and control_survival", widest, 3)
    check(part, "its summed half-width", a.one(1),
          "precision.py column_half_width", widest_sum, 3)

    a = Anchor(r"(\d+) coexist probes per seed would separate B14 from B3 on `net`, "
               r"and (\d+) would separate it from B9")
    for i, tag in enumerate(("B3", "B9")):
        gap = precision.net(scores["B14"]) - precision.net(scores[tag])
        check(part, f"coexist probes resolving B14 from {tag}", a.one(i),
              "precision.py resolving_probes",
              precision.resolving_probes(gap, scores[tag], 2))

    gap17 = precision.net(scores["B14"]) - precision.net(scores["B17"])
    check(part, "no coexist count resolves B14 from B17", "None",
          "precision.py resolving_probes",
          str(precision.resolving_probes(gap17, scores["B17"], 2)))


# --- Coexist by family ----------------------------------------------------

def by_family():
    part = "coexist by family"
    data = rows()
    families = sorted(data["B14"][7]["coexist_by_family"])

    def lead(family):
        per_family = {t: sum(data[t][s]["coexist_by_family"][family]
                             for s in (7, 11)) / 2 for t in record.ROUND5_LABELS}
        return per_family["B14"] - max(per_family[t] for t in ("B17", "B3", "B9"))

    a = Anchor(r"`instrument` \+(\d+\.?\d*) and `volunteer` \+(\d+\.?\d*) over the best other "
               r"structure, the remaining six ties or \+(\d+\.?\d*)", README)
    for i, family in enumerate(("instrument", "volunteer")):
        check(part, f"{family} lead restated", a.one(i),
              "results_round5 coexist_by_family", lead(family), 3)
    remaining = sorted(lead(f) for f in families)[:-2]
    check(part, "largest of the remaining six leads", a.one(2),
          "results_round5 coexist_by_family", max(remaining), 3)


# --- B3 across the two datasets -------------------------------------------

def anchor_structure():
    part = "B3 across the datasets"
    round4 = {}
    for seed in (7, 11):
        path = os.path.join(ROOT, "results_round4", f"falsification_seed{seed}.json")
        with open(path) as f:
            for r in json.load(f)["rows"]:
                if r["system"] == "B3-decider-neighbors":
                    round4[seed] = r
    round5 = rows()["B3"]

    a = Anchor(r"B3's round-4 coexist half-width at (\d+) probes per seed is "
               r"±(\d+\.?\d*), so (\d+\.?\d*) is inside it")
    four = [round4[7]["coexist_preserved"], round4[11]["coexist_preserved"]]
    check(part, "round-4 coexist probes per seed", a.one(0),
          "round4 dataset coexist probes", 12)
    check(part, "B3 round-4 coexist half-width", a.one(1),
          "precision.py column_half_width over falsification_seed*.json",
          precision.column_half_width(sum(four) / 2, 12, 2), 3)
    check(part, "B3 round-5 coexist restated", a.one(2),
          "results_round5 B3 coexist_preserved",
          sum(round5[s]["coexist_preserved"] for s in (7, 11)) / 2, 3)


# --- The announced-only corrections ---------------------------------------

def announced():
    part = "announced-only corrections"
    with open(os.path.join(RESULTS, "correction_categories.json")) as f:
        data = json.load(f)["structures"]
    a = Anchor(r"B3 −(\d+\.?\d*), B9 −(\d+\.?\d*), B14 −(\d+\.?\d*) and B17 −(\d+\.?\d*) on "
               r"latest-wins")
    for i, tag in enumerate(("B3", "B9", "B14", "B17")):
        per_seed = data[tag].values()
        two = sum(v["two_valued"]["latest_wins"] for v in per_seed) / len(data[tag])
        single = sum(v["single_valued"]["latest_wins"] for v in per_seed) / len(data[tag])
        check(part, f"{tag} cost of the two-valued categories", a.one(i),
              "correction_categories.json two_valued vs single_valued",
              single - two, 2)


def verdict(entry):
    """OK, ROUNDING when the record rounds the other way, MISMATCH otherwise."""
    found, stated = entry["found"], entry["stated"]
    if entry["places"] is None:
        return "OK" if str(found) == str(stated) else "MISMATCH"
    places = entry["places"]
    stated_value = float(stated)
    if half_up(float(found), places) == stated_value:
        return "OK"
    if abs(float(found) - stated_value) <= 0.5 * 10 ** -places + 1e-9:
        return "ROUNDING"
    return "MISMATCH"


def main():
    for part in (dataset, separation, by_family, anchor_structure, announced):
        part()

    failures = []
    by_part = {}
    for entry in CHECKS:
        v = verdict(entry)
        by_part.setdefault(entry["part"], []).append(v)
        if v != "OK":
            failures.append((v, entry))

    print(f"{len(CHECKS)} round-5 figures read from README.md and compared to "
          f"committed files\n")
    for part, verdicts in by_part.items():
        ok = sum(1 for v in verdicts if v == "OK")
        print(f"  {ok:>3}/{len(verdicts):<3} {part}")

    if failures:
        print(f"\n{len(failures)} figures do not match their source:\n")
        for v, entry in failures:
            print(f"  {v} [{entry['part']}] {entry['claim']}: README says "
                  f"{entry['stated']}, {entry['source']} holds {entry['found']}")
        return 1
    print("\nevery round-5 figure matches its source")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
