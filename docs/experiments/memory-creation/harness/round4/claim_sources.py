"""Checks every numeric claim in the record against the committed file it came from.

`../../README.md` states figures; `../../results_round4/` and this directory hold
what produced them. This reads both and compares, so the register cannot drift
from either side: change a published figure or a result file and a check fails.

Three registers:

  CHECKS      a figure read out of the README and the committed value it must equal
  KNOWN       pairs that already disagree, recorded so a new disagreement still fails
  UNSOURCED   figures in the README that no committed file carries

Reads committed files only; makes no API calls and needs no key. Exits non-zero
when a check fails, when an anchor no longer matches the README, or when a KNOWN
entry no longer describes what the two sides say.
"""
from __future__ import annotations
import glob
import json
import math
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.join(HERE, "..", "..")
RESULTS = os.path.join(ROOT, "results_round4")

sys.path.insert(0, HERE)

import dataset
import precision

README = open(os.path.join(ROOT, "README.md")).read()


def load(name):
    return json.load(open(os.path.join(RESULTS, f"{name}.json")))


def row(name):
    return load(name)["row"]


def sweep_row(seed, system):
    for r in load(f"falsification_seed{seed}")["rows"]:
        if r["system"] == system:
            return r
    raise KeyError(system)


def half_up(value, places):
    """Round half away from zero, the direction a reader assumes."""
    factor = 10 ** places
    return math.floor(abs(value) * factor + 0.5) / factor * (1 if value >= 0 else -1)


class Anchor:
    """One regex over the README whose capture groups are the stated figures."""

    def __init__(self, pattern):
        matches = re.findall(pattern, README)
        if len(matches) != 1:
            raise SystemExit(
                f"anchor matched {len(matches)} times, expected 1: {pattern}")
        self.groups = matches[0]

    def one(self, index=0):
        g = self.groups
        return g[index] if isinstance(g, tuple) else g


def structures_table():
    """The §The structures rows, as {label: [cells]}."""
    out = {}
    for line in README.splitlines():
        if not re.match(r"\| \*{0,2}B\d", line):
            continue
        cells = [re.sub(r"\*+", "", c).strip() for c in line.strip("|").split("|")]
        out[cells[0].split()[0]] = cells[2:]
    return out


def category_table():
    """The §What the corrected column is measuring rows, as {label: [cells]}."""
    out = {}
    for line in README.splitlines():
        if not line.startswith("| ") or line.startswith("| B"):
            continue
        cells = [re.sub(r"\*+", "", c).strip() for c in line.strip("|").split("|")]
        if len(cells) == 6 and all(c.isdigit() for c in cells[1:]):
            out[cells[0]] = [int(c) for c in cells[1:]]
    return out


CHECKS = []
UNSOURCED = []


def check(section, claim, stated, source, found, places=None):
    """Record one figure read from the README against the committed value."""
    CHECKS.append({"section": section, "claim": claim, "stated": stated,
                   "source": source, "found": found, "places": places})


def unsourced(section, claim, needed):
    UNSOURCED.append({"section": section, "claim": claim, "needed": needed})


# --- Method ---------------------------------------------------------------

def method():
    s = "Method"
    a = Anchor(r"(\d+) turns, (\d+) probes, real embeddings")
    check(s, "turns", a.one(0), "falsification_seed7.json turns",
          load("falsification_seed7")["turns"])
    check(s, "probes", a.one(1), "falsification_seed7.json probes",
          load("falsification_seed7")["probes"])

    kinds = load("falsification_seed7")["probe_kinds"]
    for family in ("corrected", "control", "coexist"):
        a = Anchor(rf"\| {family} \| (\d+) \|")
        check(s, f"probe family {family}", a.one(),
              f"falsification_seed7.json probe_kinds.{family}", kinds[family])

    persons = re.search(r"^PERSONS = (\[.*?\])",
                        open(os.path.join(HERE, "dataset.py")).read(),
                        re.S | re.M).group(1)
    a = Anchor(r"Values repeat across the (\d+) subjects")
    check(s, "subjects", a.one(), "dataset.py PERSONS", len(eval(persons)))

    a = Anchor(r"(\w+) of the (\w+) structures ran on one seed")
    words = {"eight": 8, "seventeen": 17}
    seeds = scored_systems()
    check(s, "structures on one seed", words[a.one(0)],
          "results_round4/ scored rows carrying one seed",
          sum(1 for v in seeds.values() if len(v) == 1))
    check(s, "structures scored", words[a.one(1)],
          "results_round4/ scored rows", len(seeds))

    unsourced(s, "rounds 1-3: 'ten probes backed each number, and the store held "
                 "about fifteen items'",
              "no rounds 1-3 result file is committed; harness/creation_runner.py, "
              "real_runner.py and real_new_runner.py are present but write nothing "
              "into the tree")


def scored_systems():
    """{structure name: set of seeds it was scored on}, from the result files."""
    seeds = {}
    for path in glob.glob(os.path.join(RESULTS, "B*_seed*.json")) + \
            glob.glob(os.path.join(RESULTS, "claimgraph_seed*.json")):
        d = json.load(open(path))
        seeds.setdefault(d["row"]["system"], set()).add(d["seed"])
    for seed in (7, 11):
        d = load(f"falsification_seed{seed}")
        for r in d["rows"]:
            seeds.setdefault(r["system"], set()).add(d["seed"])
    return seeds


# --- No similarity threshold can decide supersession ----------------------

def separability():
    s = "No similarity threshold can decide supersession"
    check(s, "pairs per family", Anchor(r"(\d+) pairs per family").one(),
          "separability.json n_per_family", load("separability")["n_per_family"])

    table = {}
    for line in README.splitlines():
        m = re.match(r"\| (correction|coexist|cross-subject|unrelated) \| (?:yes|no) \| "
                     r"(\d+\.?\d*) \| (\d+\.?\d*) \| (\d+\.?\d*) \| (\d+\.?\d*) \|", line)
        if m:
            table[m.group(1)] = m.groups()[1:]
    for family, cells in table.items():
        f512 = load("separability")["families"][family]
        f1536 = load("separability_dims1536")["families"][family]
        check(s, f"{family} median 512", cells[0],
              f"separability.json families.{family}.median", f512["median"], 3)
        check(s, f"{family} IQR 512", cells[1],
              f"separability.json families.{family}.iqr", f512["iqr"], 3)
        check(s, f"{family} median 1536", cells[2],
              f"separability_dims1536.json families.{family}.median", f1536["median"], 3)
        check(s, f"{family} IQR 1536", cells[3],
              f"separability_dims1536.json families.{family}.iqr", f1536["iqr"], 3)

    a = Anchor(r"the family's minimum is (\d+\.?\d*) at 512 and (\d+\.?\d*) at 1536")
    check(s, "coexist minimum 512", a.one(0), "separability.json families.coexist.min",
          load("separability")["families"]["coexist"]["min"], 3)
    check(s, "coexist minimum 1536", a.one(1),
          "separability_dims1536.json families.coexist.min",
          load("separability_dims1536")["families"]["coexist"]["min"], 3)

    a = Anchor(r"share the range (\d+\.?\d*)–(\d+\.?\d*) at 512 and (\d+\.?\d*)–(\d+\.?\d*) at 1536")
    lo512, hi512 = load("separability")["correction_coexist_overlap"]["shared_range"]
    lo1536, hi1536 = load("separability_dims1536")["correction_coexist_overlap"]["shared_range"]
    for i, (label, value) in enumerate((("shared range low 512", lo512),
                                        ("shared range high 512", hi512),
                                        ("shared range low 1536", lo1536),
                                        ("shared range high 1536", hi1536))):
        check(s, label, a.one(i), "separability*.json correction_coexist_overlap."
                                  "shared_range", value, 3)

    a = Anchor(r"in (\d+\.?\d*)% of the ([\d,]+) cross pairs at 512, (\d+\.?\d*)% at 1536")
    check(s, "coexist at or above correction 512", a.one(0),
          "separability.json correction_coexist_overlap.coexist_at_or_above_correction",
          load("separability")["correction_coexist_overlap"]
          ["coexist_at_or_above_correction"] * 100, 1)
    check(s, "cross pairs", a.one(1).replace(",", ""),
          "separability.json n_per_family squared",
          load("separability")["n_per_family"] ** 2)
    check(s, "coexist at or above correction 1536", a.one(2),
          "separability_dims1536.json correction_coexist_overlap."
          "coexist_at_or_above_correction",
          load("separability_dims1536")["correction_coexist_overlap"]
          ["coexist_at_or_above_correction"] * 100, 1)

    for label, key in (("0.60 (shipped)", "harness_threshold_0.60"),
                       ("0.70 (ADR 0006)", "adr_0006_threshold_0.70"),
                       ("0.91 (highest accuracy)", "best_possible_threshold")):
        pattern = r"\| " + re.escape(label) + r" \| (\d+) \| (\d+) \| (\d+) \| (\d+) \|"
        a = Anchor(pattern)
        d512, d1536 = load("separability")[key], load("separability_dims1536")[key]
        check(s, f"{label} correct 512", a.one(0), f"separability.json {key}."
              "correct_retirements", d512["correct_retirements"])
        check(s, f"{label} wrong 512", a.one(1), f"separability.json {key}."
              "wrong_retirements", d512["wrong_retirements"])
        check(s, f"{label} correct 1536", a.one(2), f"separability_dims1536.json {key}."
              "correct_retirements", d1536["correct_retirements"])
        check(s, f"{label} wrong 1536", a.one(3), f"separability_dims1536.json {key}."
              "wrong_retirements", d1536["wrong_retirements"])

    a = Anchor(r"at the same (\d+\.?\d*) accuracy")
    check(s, "highest-accuracy setting accuracy", a.one(),
          "separability.json best_possible_threshold.accuracy",
          load("separability")["best_possible_threshold"]["accuracy"], 2)

    a = Anchor(r"correction median down (\d+\.?\d*) and the coexist median up (\d+\.?\d*), "
               r"so the inversion the conclusion rests on is (\d+\.?\d*) wider")
    c512 = load("separability")["families"]
    c1536 = load("separability_dims1536")["families"]
    check(s, "correction median shift", a.one(0), "separability*.json medians",
          c512["correction"]["median"] - c1536["correction"]["median"], 3)
    check(s, "coexist median shift", a.one(1), "separability*.json medians",
          c1536["coexist"]["median"] - c512["coexist"]["median"], 3)
    check(s, "inversion widening", a.one(2), "separability*.json medians",
          (c512["correction"]["median"] - c1536["correction"]["median"])
          + (c1536["coexist"]["median"] - c512["coexist"]["median"]), 3)

    a = Anchor(r"IQR narrows from (\d+\.?\d*) to (\d+\.?\d*)")
    check(s, "coexist IQR 512", a.one(0), "separability.json families.coexist.iqr",
          c512["coexist"]["iqr"], 3)
    check(s, "coexist IQR 1536", a.one(1),
          "separability_dims1536.json families.coexist.iqr", c1536["coexist"]["iqr"], 3)


# --- The structures -------------------------------------------------------

SOURCE_OF = {
    "B14": ("B14-key-upsert-plural", 7), "B17": ("B17-key-upsert-question-key", 7),
    "B18": ("B18-key-upsert-unseen-example", 7), "B15": ("B15-key-upsert-namespace", 7),
    "B9": ("B9-decider-neighbors-wide", 7), "B10": ("B10-neighbourhood-rewrite", 7),
    "B11": ("B11-rewrite-scoped", 7), "B12": ("B12-key-upsert-value-hints", 7),
    "B16": ("B16-decider-verdicts", 7),
}
SWEEP_OF = {"B1": "B1-store-threshold", "B2": "B2-decider-window",
            "B3": "B3-decider-neighbors", "B4": "B4-key-upsert",
            "B5": "B5-key-upsert-neighbours", "B6": "B6-key-append-readtime",
            "B7": "B7-key-mainmodel-field"}
COLUMNS = [("latest-wins", "latest_wins"), ("control", "control_survival"),
           ("coexist", "coexist_preserved"), ("net", "net"),
           ("net (s11)", None), ("control retired", "control_retired"),
           ("items", "items")]


def scored(tag, seed):
    """The scored row for one structure at one seed, wherever it was written."""
    if tag in SWEEP_OF:
        return sweep_row(seed, SWEEP_OF[tag]), f"falsification_seed{seed}.json rows[]"
    name = SOURCE_OF[tag][0]
    return row(f"{name}_seed{seed}"), f"{name}_seed{seed}.json row"


def structures():
    s = "The structures"
    for tag, cells in structures_table().items():
        base, where = scored(tag, 7)
        for cell, (label, key) in zip(cells, COLUMNS):
            if label == "net (s11)":
                if cell == "—":
                    continue
                other, where11 = scored(tag, 11)
                check(s, f"{tag} net (s11)", cell, where11, other["net"], 2)
                continue
            places = None if key == "items" else 2
            check(s, f"{tag} {label}", cell, f"{where}.{key}", base[key], places)

    a = Anchor(r"assumed (\d+)% compliance")
    src = open(os.path.join(HERE, "structures.py")).read()
    check(s, "main-response compliance", a.one(), "structures.py compliance default",
          float(re.search(r"def __init__\(self, rnd, compliance=(\d+\.?\d*)\)", src)
                .group(1)) * 100, 0)

    a = Anchor(r"moves corrections from (\d+\.?\d*) to (\d+\.?\d*) — a gap far outside")
    check(s, "B2 latest-wins", a.one(0), "falsification_seed7.json rows[] B2",
          sweep_row(7, "B2-decider-window")["latest_wins"], 2)
    check(s, "B3 latest-wins", a.one(1), "falsification_seed7.json rows[] B3",
          sweep_row(7, "B3-decider-neighbors")["latest_wins"], 2)

    a = Anchor(r"never reached the decider from (\d+) of (\d+) to (\d+)\.")
    check(s, "B3 contradicted rows not offered", a.one(0),
          "gap_diagnosis_seed7.json stats.not_offered",
          load("gap_diagnosis_seed7")["stats"]["not_offered"])
    check(s, "correction turns", a.one(1), "gap_diagnosis_seed7.json total",
          load("gap_diagnosis_seed7")["total"])
    check(s, "B9 contradicted rows not offered", a.one(2),
          "b9_diagnosis_seed7.json correction_gap.not_offered",
          load("b9_diagnosis_seed7")["correction_gap"]["not_offered"])

    a = Anchor(r"stays at (\d+\.?\d*) across B3 and B9")
    for tag, (r_, where) in (("B3", scored("B3", 7)), ("B9", scored("B9", 7))):
        check(s, f"{tag} control retired", a.one(), f"{where}.control_retired",
              r_["control_retired"], 2)

    a = Anchor(r"It retires half of the never-corrected facts and (\d+)% of the "
               r"coexisting ones")
    b1 = sweep_row(7, "B1-store-threshold")
    check(s, "B1 coexist retired", a.one(), "falsification_seed7.json rows[] B1."
          "coexist_retired", b1["coexist_retired"] * 100, 0)

    a = Anchor(r"corrections never land \((\d+\.?\d*)\)")
    check(s, "B4 latest-wins", a.one(), "falsification_seed7.json rows[] B4",
          sweep_row(7, "B4-key-upsert")["latest_wins"], 2)

    a = Anchor(r"destroys (\d+)% of the control facts")
    check(s, "B5 control retired", a.one(), "falsification_seed7.json rows[] B5."
          "control_retired", sweep_row(7, "B5-key-upsert-neighbours")["control_retired"]
          * 100, 0)

    a = Anchor(r"lifts net from (\d\.\d\d) to (\d\.\d\d)")
    check(s, "B5 net", a.one(0), "falsification_seed7.json rows[] B5.net",
          sweep_row(7, "B5-key-upsert-neighbours")["net"], 2)
    check(s, "B12 net", a.one(1), "B12-key-upsert-value-hints_seed7.json row.net",
          row("B12-key-upsert-value-hints_seed7")["net"], 2)

    a = Anchor(r"The residual was (\w+) keys holding several simultaneously-true facts")
    check(s, "B12 collided keys", {"five": 5}[a.one()],
          "key_diagnosis_seed7.json collided_keys",
          load("key_diagnosis_seed7")["collided_keys"])

    a = Anchor(r"still loses (\d+)% of control facts")
    b6 = sweep_row(7, "B6-key-append-readtime")
    check(s, "B6 control lost", a.one(), "falsification_seed7.json rows[] B6."
          "control_survival", (1 - b6["control_survival"]) * 100, 0)

    b16 = load("b16_writes_seed7")["structures"]["B16"]
    a = Anchor(r"stores (\d+) rows against (\d+) — (\d+) of its (\d+) never-written "
               r"facts come from a turn that stored nothing")
    check(s, "B16 rows written", a.one(0), "b16_writes_seed7.json B16.rows_written",
          b16["rows_written"])
    check(s, "fact turns", a.one(1), "b16_writes_seed7.json B16.fact_turns",
          b16["fact_turns"])
    check(s, "B16 turn stored nothing", a.one(2),
          "b16_writes_seed7.json B16.turn_stored_nothing", b16["turn_stored_nothing"])
    check(s, "B16 never written", a.one(3), "b16_writes_seed7.json B16.never_written",
          b16["never_written"])

    a = Anchor(r"All (\w+) of B9's coexist losses")
    check(s, "B9 coexist values lost", {"four": 4}[a.one()],
          "b9_diagnosis_seed7.json coexist_values_lost",
          load("b9_diagnosis_seed7")["coexist_values_lost"])

    fam = {f["family"]: f for f in load("coexist_families_seed7")["families"]}
    for family in ("allergy", "instrument", "languages"):
        a = Anchor(rf"\| {family} \| (\d+) \| (\d+) \| (\d+) \| (\d+) \|")
        check(s, f"{family} planted values", a.one(0),
              f"coexist_families_seed7.json families {family}.values",
              fam[family]["values"])
        for i, tag in enumerate(("B14", "B17", "B18")):
            check(s, f"{family} preserved under {tag}", a.one(i + 1),
                  f"coexist_families_seed7.json families {family}.preserved.{tag}",
                  fam[family]["preserved"][tag])

    a = Anchor(r"it costs instrument (\d+) of (\d+) — a family neither example names — "
               r"taking coexist from (\d+\.?\d*) to (\d+\.?\d*)")
    check(s, "B18 instrument preserved", a.one(0),
          "coexist_families_seed7.json families instrument.preserved.B18",
          fam["instrument"]["preserved"]["B18"])
    check(s, "instrument planted", a.one(1),
          "coexist_families_seed7.json families instrument.values",
          fam["instrument"]["values"])
    check(s, "B17 coexist", a.one(2), "B17-key-upsert-question-key_seed7.json row."
          "coexist_preserved", row("B17-key-upsert-question-key_seed7")
          ["coexist_preserved"], 2)
    check(s, "B18 coexist", a.one(3), "B18-key-upsert-unseen-example_seed7.json row."
          "coexist_preserved", row("B18-key-upsert-unseen-example_seed7")
          ["coexist_preserved"], 2)

    a = Anchor(r"`languages` is (\d+) of the (\d+) planted coexist values and (\d+) of "
               r"the (\d+) coexist probes")
    total_values = sum(f["values"] for f in fam.values())
    check(s, "languages values", a.one(0),
          "coexist_families_seed7.json families languages.values", fam["languages"]["values"])
    check(s, "coexist values planted", a.one(1),
          "coexist_families_seed7.json families values summed", total_values)
    check(s, "languages probes", a.one(2), "dataset.py COEXIST over PERSONS[:4]",
          COEXIST_PROBES["languages"])
    check(s, "coexist probes", a.one(3), "dataset.py COEXIST over PERSONS[:4]",
          sum(COEXIST_PROBES.values()))

    a = Anchor(r"latest-wins (\d+\.?\d*) against B14's (\d+\.?\d*), stale (\d+\.?\d*) against "
               r"(\d+\.?\d*)")
    b17, b14 = row("B17-key-upsert-question-key_seed7"), row("B14-key-upsert-plural_seed7")
    check(s, "B17 latest-wins", a.one(0), "B17-...seed7.json row.latest_wins",
          b17["latest_wins"], 2)
    check(s, "B14 latest-wins", a.one(1), "B14-...seed7.json row.latest_wins",
          b14["latest_wins"], 2)
    check(s, "B17 stale", a.one(2), "B17-...seed7.json row.stale_present",
          b17["stale_present"], 2)
    check(s, "B14 stale", a.one(3), "B14-...seed7.json row.stale_present",
          b14["stale_present"], 2)

    rw = load("rewrite_diagnosis_seed7")
    a = Anchor(r"Classifying all (\d+) of its removals shows what that costs — (\d+)% "
               r"are paraphrase churn")
    check(s, "B10 rows removed", a.one(0), "rewrite_diagnosis_seed7.json removed",
          rw["removed"])
    check(s, "B10 paraphrase share", a.one(1), "rewrite_diagnosis_seed7.json kinds."
          "replaced_by_a_paraphrase over removed",
          rw["kinds"]["replaced_by_a_paraphrase"] / rw["removed"] * 100, 0)

    a = Anchor(r"makes it worse, not better: (\d\.\d\d)")
    check(s, "B11 net", a.one(), "B11-rewrite-scoped_seed7.json row.net",
          row("B11-rewrite-scoped_seed7")["net"], 2)

    unsourced(s, "'answers 49 of 388 fact turns with an empty write list against "
                 "B14's 5' and 'empty writes are 8 of 388 here against B15's 49'",
              "a per-structure count of empty write lists over the 388 fact turns; "
              "b16_writes.py records this shape for B9 and B16 only. The figures are "
              "asserted in structures_a.py's comments, which are not a result file")
    unsourced(s, "'3,846 candidates judged, none skipped' for B16",
              "a count of candidates shown to and judged by B16; b16_writes_seed7.json "
              "records rows written and facts never written, not candidates judged")
    unsourced(s, "B17 'every value-named key is gone (no `alma.tea`, no "
                 "`kofi.pets.turtle`)' and 'Its remaining eight misses'",
              "B17's key assignments and its per-correction outcome classification; no "
              "committed file carries either. B17-...seed7.json row.outcomes gives "
              "corrected:not_ranked 7, which is not the same quantity")


# One probe per subject per family; `dataset.py` plants them over PERSONS[:4].
COEXIST_PROBES = {name: len(dataset.PERSONS[:4]) for name, _, _, _ in dataset.COEXIST}


# --- What the net column can and cannot separate --------------------------

def net_column():
    s = "What the net column can and cannot separate"
    src = "precision.py interval() over the committed rows"
    rows = precision.load_rows()
    halves = {name: precision.interval(r)[1] for name, r in rows.items()}

    a = Anchor(r"runs from (\d+\.?\d*) \(B14.*?\) to (\d+\.?\d*) \(B16\)")
    check(s, "narrowest half-width", a.one(0), src,
          halves["B14-key-upsert-plural"], 3)
    check(s, "widest half-width", a.one(1), src, halves["B16-decider-verdicts"], 3)

    a = Anchor(r"B9's interval of (\d+\.?\d*) to (\d+\.?\d*)")
    net, half = precision.interval(rows["B9-decider-neighbors-wide"])
    check(s, "B9 interval low", a.one(0), src, net - half, 3)
    check(s, "B9 interval high", a.one(1), src, net + half, 3)

    a = Anchor(r"The (\w+) structures from B14 down to B12 all overlap")
    overlapping = sum(1 for name, r in rows.items()
                      if precision.interval(r)[0] + precision.interval(r)[1] >= net - half
                      and precision.interval(r)[0] - precision.interval(r)[1] <= net + half)
    check(s, "structures overlapping B9", {"eight": 8}[a.one()], src, overlapping)
    a = Anchor(r"The (\w+) from B2 down to B1 do not")
    check(s, "structures not overlapping B9", {"eight": 8}[a.one()], src,
          len(rows) - overlapping)

    a = Anchor(r"\((\d+) unoffered corrections falling to (\d+)\)")
    check(s, "B3 unoffered", a.one(0), "gap_diagnosis_seed7.json stats.not_offered",
          load("gap_diagnosis_seed7")["stats"]["not_offered"])
    check(s, "B9 unoffered", a.one(1),
          "b9_diagnosis_seed7.json correction_gap.not_offered",
          load("b9_diagnosis_seed7")["correction_gap"]["not_offered"])

    budget = {(c, x): h for c, x, h in precision.budget()}
    for corrected, coexist in ((100, 12), (400, 12), (100, 60), (100, 200)):
        a = Anchor(rf"\| {corrected} \| {coexist} \| ±(\d+\.?\d*) \|")
        check(s, f"half-width at {corrected}/{coexist}", a.one(),
              "precision.py budget()", budget[(corrected, coexist)], 3)

    a = Anchor(r"corrected family buys (\d+\.?\d*)\. Taking coexist from 12 probes to 60 "
               r"buys (\d+\.?\d*)")
    check(s, "gain from quadrupling corrected", a.one(0), "precision.py budget()",
          budget[(100, 12)] - budget[(400, 12)], 3)
    check(s, "gain from five times coexist", a.one(1), "precision.py budget()",
          budget[(100, 12)] - budget[(100, 60)], 3)

    unsourced(s, "the family scores 0.80 / 0.95 / 0.80 behind the half-width table",
              "precision.py budget() holds them as literals; they are design "
              "arithmetic rather than any structure's measurement, and no result "
              "file carries them")


# --- Where the residual correction gap is ---------------------------------

def gap():
    s = "Where the residual correction gap is"
    g = load("gap_diagnosis_seed7")["stats"]
    for label, key in (("offered, retired", "offered_and_retired"),
                       ("offered, kept", "offered_not_retired"),
                       ("not offered", "not_offered"),
                       ("no live row held the old value", "no_row_for_old")):
        a = Anchor(r"\| " + re.escape(label) + r" \| (\d+) \|")
        check(s, f"B3 {label}", a.one(), f"gap_diagnosis_seed7.json stats.{key}", g[key])

    a = Anchor(r"reached the decider (\d+)% of the time and was retired in (\d+)% of those")
    total = load("gap_diagnosis_seed7")["total"]
    offered = g["offered_and_retired"] + g["offered_not_retired"]
    check(s, "B3 offered share", a.one(0), "gap_diagnosis_seed7.json stats summed",
          offered / total * 100, 0)
    check(s, "B3 retired share of offered", a.one(1),
          "gap_diagnosis_seed7.json stats summed",
          g["offered_and_retired"] / offered * 100, 0)

    b9 = load("b9_diagnosis_seed7")["correction_gap"]
    a = Anchor(r"to (\d+) offered and retired, (\d+) offered and kept, (\d+) not "
               r"offered, (\d+) with no live row")
    for i, key in enumerate(("offered_and_retired", "offered_and_kept", "not_offered",
                             "no_live_row_for_old")):
        check(s, f"B9 {key}", a.one(i),
              f"b9_diagnosis_seed7.json correction_gap.{key}", b9[key])

    a = Anchor(r"in (\d+) of the (\d+) the retire list came back empty")
    check(s, "B9 empty retire lists", a.one(0),
          "b9_diagnosis_seed7.json offered_and_kept_with_empty_retire_list",
          load("b9_diagnosis_seed7")["offered_and_kept_with_empty_retire_list"])
    check(s, "B9 offered and kept", a.one(1),
          "b9_diagnosis_seed7.json correction_gap.offered_and_kept", b9["offered_and_kept"])


# --- The offer is wide enough, and wider does not pay ---------------------

def offer_width():
    s = "The offer is wide enough, and wider does not pay"
    d = load("offer_width_seed7")
    a = Anchor(r"against the (\d+) corrections whose old value still had a live row")
    check(s, "corrections with a live row", a.one(),
          "offer_width_seed7.json corrections_with_a_live_row",
          d["corrections_with_a_live_row"])

    for i, label in enumerate(("5", "10", "20", "40", "unbounded")):
        a = Anchor(r"(?m)^\| " + label + r" \| \*{0,2}(\d+)\*{0,2} \| (\d+\.\d\d) \| "
                   r"([\d,]+) \|$")
        k = d["k_sweep"][i]
        check(s, f"K={label} reached", a.one(0), f"offer_width_seed7.json k_sweep[{i}]."
              "offered", k["offered"])
        check(s, f"K={label} candidates per turn", a.one(1),
              f"offer_width_seed7.json k_sweep[{i}].candidates_mean",
              k["candidates_mean"], 2)
        check(s, f"K={label} candidate rows", a.one(2).replace(",", ""),
              f"offer_width_seed7.json k_sweep[{i}].candidates_total",
              k["candidates_total"])

    ranks = [t["rank"] for t in d["per_correction_turn"]]
    a = Anchor(r"rank 1 for (\d+) of the (\d+) and within rank 5 for (\d+); rank (\d+) "
               r"is the deepest")
    check(s, "rank 1 count", a.one(0), "offer_width_seed7.json per_correction_turn[].rank",
          sum(1 for r in ranks if r == 1))
    check(s, "corrections ranked", a.one(1),
          "offer_width_seed7.json corrections_with_a_live_row",
          d["corrections_with_a_live_row"])
    check(s, "within rank 5", a.one(2), "offer_width_seed7.json per_correction_turn[].rank",
          sum(1 for r in ranks if r <= 5))
    check(s, "deepest rank", a.one(3),
          "offer_width_seed7.json per_correction_turn[].rank", max(ranks))

    a = Anchor(r"lowest contradicted-row cosine in the run is (\d+\.?\d*)")
    check(s, "lowest cosine", a.one(),
          "offer_width_seed7.json per_correction_turn[].cosine",
          min(t["cosine"] for t in d["per_correction_turn"]), 3)

    a = Anchor(r"(\d+\.?\d*) collapses coverage to (\d+)")
    floors = {f["floor"]: f for f in d["floor_sweep"]}
    check(s, "collapse floor", a.one(0), "offer_width_seed7.json floor_sweep[].floor",
          max(floors), 1)
    check(s, "coverage at the collapse floor", a.one(1),
          "offer_width_seed7.json floor_sweep[].offered", floors[max(floors)]["offered"])

    a = Anchor(r"Going 5 → 10 costs (\d+) extra candidate rows per correction "
               r"recovered; going 10 → 20 costs ([\d,]+), and buys (\d+) corrections "
               r"of (\d+) — (\d+\.?\d*) on latest-wins and (\d+\.?\d*) on `net`")
    k5, k10, k20 = d["k_sweep"][0], d["k_sweep"][1], d["k_sweep"][2]
    check(s, "cost of 5 to 10", a.one(0), "offer_width_seed7.json k_sweep[0..1]",
          (k10["candidates_total"] - k5["candidates_total"])
          / (k10["offered"] - k5["offered"]), 0)
    check(s, "cost of 10 to 20", a.one(1).replace(",", ""),
          "offer_width_seed7.json k_sweep[1..2]",
          (k20["candidates_total"] - k10["candidates_total"])
          / (k20["offered"] - k10["offered"]), 0)
    check(s, "corrections bought", a.one(2), "offer_width_seed7.json k_sweep[1..2]."
          "offered", k20["offered"] - k10["offered"])
    check(s, "corrected probes", a.one(3), "falsification_seed7.json probe_kinds."
          "corrected", load("falsification_seed7")["probe_kinds"]["corrected"])
    gain = (k20["offered"] - k10["offered"]) / \
        load("falsification_seed7")["probe_kinds"]["corrected"]
    check(s, "gain on latest-wins", a.one(4), "offer_width_seed7.json k_sweep[1..2]",
          gain, 2)
    check(s, "gain on net", a.one(5), "offer_width_seed7.json k_sweep[1..2] over three "
          "families", gain / 3, 3)

    a = Anchor(r"degradation on the (\d+) corrections already reaching the decider")
    check(s, "corrections reaching the decider at K=10", a.one(),
          "offer_width_seed7.json k_sweep[1].offered", k10["offered"])

    a = Anchor(r"retrieval half is closed to (\d+)%")
    check(s, "retrieval closure", a.one(), "offer_width_seed7.json k_sweep[1].offered "
          "over corrections_with_a_live_row",
          k10["offered"] / d["corrections_with_a_live_row"] * 100, 0)


# --- What the corrected column is measuring -------------------------------

ARMS = {"A": "the announced correction the dataset writes",
        "B": "the stored sentence carrying the new value",
        "C": "B with a temporal marker prepended",
        "D": "the category's other initial template"}


def marker():
    s = "What the corrected column is measuring"
    mf = load("marker_following_seed7")
    arms = mf["judgment"]["arms"]

    a = Anchor(r"on the (\d+) whose offer carried the contradicted row")
    check(s, "judgeable corrections", a.one(),
          "marker_following_seed7.json judgment.arms.A.n", arms["A"]["n"])

    for tag, phrasing in ARMS.items():
        a = Anchor(r"\| " + tag + r" \| (?:yes|no) \| " + re.escape(phrasing)
                   + r" \| (\d+) — (\d+\.?\d*) \| (\d+\.?\d*) \|")
        check(s, f"arm {tag} retired", a.one(0),
              f"marker_following_seed7.json judgment.arms.{tag}.retired",
              arms[tag]["retired"])
        check(s, f"arm {tag} rate", a.one(1),
              f"marker_following_seed7.json judgment.arms.{tag}.rate",
              arms[tag]["rate"], 2)
        check(s, f"arm {tag} mean cosine", a.one(2),
              f"marker_following_seed7.json judgment.arms.{tag}.mean_cosine",
              arms[tag]["mean_cosine"], 3)

    paired = {p["pair"]: p for p in mf["judgment"]["paired"]}
    a = Anchor(r"(\d+) discordant pairs, exact p = (\d+\.?\d*)")
    check(s, "C vs B discordant", a.one(0),
          "marker_following_seed7.json judgment.paired 'C vs B'.discordant",
          paired["C vs B"]["discordant"])
    check(s, "C vs B exact p", a.one(1),
          "marker_following_seed7.json judgment.paired 'C vs B'.exact_p",
          paired["C vs B"]["exact_p"], 2)

    a = Anchor(r"retiring eleven fewer \(p = (\d+\.?\d*)\)")
    check(s, "A vs D exact p", a.one(),
          "marker_following_seed7.json judgment.paired 'A vs D'.exact_p",
          paired["A vs D"]["exact_p"], 3)

    a = Anchor(r"Across all (\d+) outcomes the correlation with cosine is (\d+\.?\d*)")
    check(s, "gradient points", a.one(0),
          "marker_following_seed7.json gradient.points", mf["gradient"]["points"])
    check(s, "gradient correlation", a.one(1),
          "marker_following_seed7.json gradient.r_overall", mf["gradient"]["r_overall"], 2)

    bands = mf["gradient"]["bands"]
    a = Anchor(r"everything above (\d+\.?\d*) lands between (\d+\.?\d*) and (\d+\.?\d*)")
    check(s, "band floor cosine", a.one(0),
          "marker_following_seed7.json gradient.bands[1].cosine_from",
          bands[1]["cosine_from"], 2)
    check(s, "lowest band rate above the floor", a.one(1),
          "marker_following_seed7.json gradient.bands[1:].rate",
          min(b["rate"] for b in bands[1:]), 2)
    check(s, "highest band rate above the floor", a.one(2),
          "marker_following_seed7.json gradient.bands[1:].rate",
          max(b["rate"] for b in bands[1:]), 2)

    cats = mf["by_category"]
    table = category_table()
    for label, cells in table.items():
        if label == "all 20":
            group = list(cats)
        elif "," in label:
            group = [n.strip() for n in label.split(",")]
        elif label in cats:
            group = [label]
        else:
            continue
        for i, key in enumerate(("n", "A", "B", "C", "D")):
            check(s, f"category row '{label}' {key}", cells[i],
                  f"marker_following_seed7.json by_category summed over {len(group)}",
                  sum(cats[c][key] for c in group))

    check(s, "perfect categories",
          {"Eight": 8}[Anchor(r"(\w+) categories are perfect under every arm").one()],
          "marker_following_seed7.json by_category",
          sum(1 for c in cats.values() if all(c[k] == c["n"] for k in "ABCD")))

    a = Anchor(r"D trails C by (\d+) inside them")
    named = set(table)
    partial = [c for c, v in cats.items()
               if not all(v[k] == v["n"] for k in "ABCD") and c not in named]
    check(s, "D behind C in the unnamed block", a.one(),
          "marker_following_seed7.json by_category",
          sum(cats[c]["C"] - cats[c]["D"] for c in partial))

    a = Anchor(r"D is below n in (\d+) of the (\d+) categories")
    check(s, "categories where D is below n", a.one(0),
          "marker_following_seed7.json by_category",
          sum(1 for v in cats.values() if v["D"] < v["n"]))
    check(s, "categories", a.one(1), "marker_following_seed7.json by_category", len(cats))

    a = Anchor(r"In gym, pet and testing — (\d+) of the (\d+) —")
    check(s, "multi-valued corrections", a.one(0),
          "marker_following_seed7.json by_category gym+pet+testing.n",
          sum(cats[c]["n"] for c in ("gym", "pet", "testing")))
    check(s, "judgeable corrections restated", a.one(1),
          "marker_following_seed7.json judgment.arms.A.n", arms["A"]["n"])

    a = Anchor(r"A retires (\d+) of (\d+) where B retires (\d+)")
    for i, key in enumerate(("A", "n", "B")):
        check(s, f"manager {key}", a.one(i),
              f"marker_following_seed7.json by_category.manager.{key}",
              cats["manager"][key])

    a = Anchor(r"the decider on the (\d+) items whose topic is single-valued")
    check(s, "single-valued corrections", a.one(),
          "marker_following_seed7.json by_category", arms["A"]["n"]
          - sum(cats[c]["n"] for c in ("gym", "pet", "testing")))

    a = Anchor(r"The span across the four arms is (\d+\.?\d*), from D's (\d+\.?\d*) to C's "
               r"(\d+\.?\d*)")
    check(s, "arm span", a.one(0), "marker_following_seed7.json judgment.arms rates",
          arms["C"]["rate"] - arms["D"]["rate"], 2)
    check(s, "arm D rate restated", a.one(1),
          "marker_following_seed7.json judgment.arms.D.rate", arms["D"]["rate"], 2)
    check(s, "arm C rate restated", a.one(2),
          "marker_following_seed7.json judgment.arms.C.rate", arms["C"]["rate"], 2)

    a = Anchor(r"both rows unreachable at K=10 are reached at rank (\d+) under arm B")
    missed = {m["turn"] for m in load("offer_width_seed7")["k_sweep"][1]["missed"]}
    ranks = {t["retrieval"]["B"]["rank"] for t in mf["trials"]
             if t["texts"]["A"] in missed}
    check(s, "arm B rank for the two unreachable rows", a.one(),
          "marker_following_seed7.json trials[].retrieval.B.rank for the turns in "
          "offer_width_seed7.json k_sweep[1].missed",
          ranks.pop() if len(ranks) == 1 else sorted(ranks))


# --- One decider call per turn --------------------------------------------

def call_schedule():
    s = "One decider call per turn"
    cs = load("call_schedule_seed7")
    b9 = cs["structures"]["B9-decider-neighbors-wide"]
    b14 = cs["structures"]["B14-key-upsert-plural"]

    a = Anchor(r"once per turn, on all (\d+) turns")
    check(s, "turns", a.one(), "call_schedule_seed7.json turns", cs["turns"])

    a = Anchor(r"(\d+) of (\d+) calls \((\d+\.?\d*)%\) returned no operation at all "
               r"under B9, (\d+) under B14")
    check(s, "B9 idle calls", a.one(0),
          "call_schedule_seed7.json B9.no_op_calls", b9["no_op_calls"])
    check(s, "calls", a.one(1), "call_schedule_seed7.json B9.calls", b9["calls"])
    check(s, "B9 idle share", a.one(2),
          "call_schedule_seed7.json B9.no_op_fraction", b9["no_op_fraction"] * 100, 1)
    check(s, "B14 idle calls", a.one(3),
          "call_schedule_seed7.json B14.no_op_calls", b14["no_op_calls"])

    a = Anchor(r"only (\d+) of B9's and (\d+) of B14's fall on a fact turn")
    for index, (label, structure) in enumerate((("B9", b9), ("B14", b14))):
        fact = sum(v["no_op"] for k, v in structure["census"].items() if k != "chatter")
        check(s, f"{label} idle calls on a fact turn", a.one(index),
              f"call_schedule_seed7.json {label}.census[non-chatter].no_op", fact)

    a = Anchor(r"(\d+) across (\d+) chatter calls in both structures, against (\d+) "
               r"wrong retirements")
    check(s, "retires on chatter", a.one(0),
          "call_schedule_seed7.json B9/B14.retires_on_chatter",
          b9["retires_on_chatter"] + b14["retires_on_chatter"])
    check(s, "chatter calls across both", a.one(1),
          "call_schedule_seed7.json label_counts.chatter doubled",
          cs["label_counts"]["chatter"] * 2)
    check(s, "wrong retirements at 0.60", a.one(2),
          "separability.json harness_threshold_0.60.wrong_retirements",
          load("separability")["harness_threshold_0.60"]["wrong_retirements"])

    a = Anchor(r"saves (\d+\.?\d*)% of calls and changes nothing measurable: B14's replay "
               r"returns `net` (\d+\.?\d*) unchanged, (\d+) items down to (\d+)")
    check(s, "calls saved", a.one(0),
          "call_schedule_seed7.json B14.gate.saved_fraction",
          b14["gate"]["saved_fraction"] * 100, 1)
    check(s, "B14 net under the gate", a.one(1),
          "call_schedule_seed7.json B14.gate.score.net", b14["gate"]["score"]["net"], 4)
    check(s, "B14 items", a.one(2), "call_schedule_seed7.json B14.items", b14["items"])
    check(s, "B14 items under the gate", a.one(3),
          "call_schedule_seed7.json B14.gate.items", b14["gate"]["items"])

    a = Anchor(r"the (\d+) fact turns carry the whole of `net` between them at "
               r"(\d+\.?\d*) each")
    check(s, "fact turns", a.one(0), "call_schedule_seed7.json B14.gate.calls",
          b14["gate"]["calls"])
    check(s, "share of net per fact turn", a.one(1),
          "call_schedule_seed7.json B14.gate.calls inverted",
          1 / b14["gate"]["calls"], 4)

    a = Anchor(r"a gate needs (\d+\.?\d*)% recall on fact turns")
    check(s, "gate recall", a.one(), "precision.py interval() over B14's row",
          (1 - precision.interval(row("B14-key-upsert-plural_seed7"))[1]) * 100, 1)

    unsourced(s, "'at 95% it costs more than the distance from the adopted structure "
                 "to the round's best-scoring one', and 'build one when that is "
                 "measured above 50%'",
              "the 95% and the 50% are settings the author chose, not measurements; "
              "no committed file carries either")


# --- Adopted direction ----------------------------------------------------

def adopted():
    s = "Adopted direction"
    a = Anchor(r"\(B9, net (\d+\.?\d*)\)")
    check(s, "B9 net", a.one(), "B9-decider-neighbors-wide_seed7.json row.net",
          row("B9-decider-neighbors-wide_seed7")["net"], 2)

    a = Anchor(r"scores coexist (\d+\.?\d*) and net (\d+\.?\d*)\. B14's (\d+\.?\d*) was measured")
    b18 = row("B18-key-upsert-unseen-example_seed7")
    check(s, "B18 coexist", a.one(0), "B18-...seed7.json row.coexist_preserved",
          b18["coexist_preserved"], 2)
    check(s, "B18 net", a.one(1), "B18-...seed7.json row.net", b18["net"], 2)
    check(s, "B14 net", a.one(2), "B14-key-upsert-plural_seed7.json row.net",
          row("B14-key-upsert-plural_seed7")["net"], 2)

    a = Anchor(r"B17 at (\d+\.?\d*) and B18 at (\d+\.?\d*), against B9's (\d+\.?\d*) — though "
               r"B14 itself is not among them at (\d+\.?\d*)")
    for i, (label, name) in enumerate((
            ("B17", "B17-key-upsert-question-key_seed7"),
            ("B18", "B18-key-upsert-unseen-example_seed7"),
            ("B9", "B9-decider-neighbors-wide_seed7"),
            ("B14", "B14-key-upsert-plural_seed7"))):
        check(s, f"{label} latest-wins", a.one(i), f"{name}.json row.latest_wins",
              row(name)["latest_wins"], 2)

    a = Anchor(r"latest-wins (\d+\.?\d*) and (\d+\.?\d*) across two unrelated examples")
    check(s, "B17 latest-wins restated", a.one(0),
          "B17-...seed7.json row.latest_wins",
          row("B17-key-upsert-question-key_seed7")["latest_wins"], 2)
    check(s, "B18 latest-wins restated", a.one(1),
          "B18-...seed7.json row.latest_wins",
          row("B18-key-upsert-unseen-example_seed7")["latest_wins"], 2)


# --- Limits ---------------------------------------------------------------

def limits():
    s = "Limits"
    a = Anchor(r"The (\d+) judgment misses in (\d+) corrections under B9")
    check(s, "B9 judgment misses", a.one(0),
          "b9_diagnosis_seed7.json correction_gap.offered_and_kept",
          load("b9_diagnosis_seed7")["correction_gap"]["offered_and_kept"])
    check(s, "correction turns restated", a.one(1),
          "offer_width_seed7.json correction_turns",
          load("offer_width_seed7")["correction_turns"])

    a = Anchor(r"(\d+) of the (\d+) judgeable corrections are in categories")
    cats = load("marker_following_seed7")["by_category"]
    check(s, "multi-valued corrections restated", a.one(0),
          "marker_following_seed7.json by_category gym+pet+testing.n",
          sum(cats[c]["n"] for c in ("gym", "pet", "testing")))
    check(s, "judgeable corrections in Limits", a.one(1),
          "marker_following_seed7.json judgment.arms.A.n",
          load("marker_following_seed7")["judgment"]["arms"]["A"]["n"])

    a = Anchor(r"truncated to (\d+) dimensions")
    check(s, "embedding dimensions", a.one(), "separability.json dims",
          load("separability")["dims"])
    a = Anchor(r"re-measured at the native (\d+)")
    check(s, "native dimensions", a.one(), "separability_dims1536.json dims",
          load("separability_dims1536")["dims"])

    a = Anchor(r"at ±(\d+\.?\d*) on `net` it moves nothing")
    budget = {(c, x): h for c, x, h in precision.budget()}
    check(s, "half-width restated", a.one(), "precision.py budget() first row",
          budget[(100, 12)], 3)

    a = Anchor(r"the (\d+) coexist probes and the (\d+) fact turns behind them")
    check(s, "coexist probes", a.one(0),
          "falsification_seed7.json probe_kinds.coexist",
          load("falsification_seed7")["probe_kinds"]["coexist"])
    check(s, "coexist fact turns", a.one(1),
          "call_schedule_seed7.json label_counts.coexist",
          load("call_schedule_seed7")["label_counts"]["coexist"])

    fam = {f["family"]: f for f in load("coexist_families_seed7")["families"]}
    a = Anchor(r"coexist probe set is (\d+) values in three families, and one family "
               r"is (\d+) of them, carried by (\d+) of the (\d+) coexist probes, so "
               r"losing that one family moves the column by (\d+\.?\d*)")
    check(s, "coexist values", a.one(0),
          "coexist_families_seed7.json families values summed",
          sum(f["values"] for f in fam.values()))
    check(s, "languages values restated", a.one(1),
          "coexist_families_seed7.json families languages.values",
          fam["languages"]["values"])
    check(s, "languages probes restated", a.one(2), "dataset.py COEXIST over PERSONS[:4]",
          COEXIST_PROBES["languages"])
    check(s, "coexist probes restated", a.one(3),
          "falsification_seed7.json probe_kinds.coexist",
          load("falsification_seed7")["probe_kinds"]["coexist"])
    check(s, "column moved by one family", a.one(4),
          "scoring.py means the 12 coexist probes",
          COEXIST_PROBES["languages"]
          / load("falsification_seed7")["probe_kinds"]["coexist"], 2)

    a = Anchor(r"One probe is (\d+\.?\d*)% of the column, against (\d+)% for a corrected "
               r"probe")
    check(s, "share of the coexist column per probe", a.one(0),
          "falsification_seed7.json probe_kinds.coexist inverted",
          100 / load("falsification_seed7")["probe_kinds"]["coexist"], 1)
    check(s, "share of the corrected column per probe", a.one(1),
          "falsification_seed7.json probe_kinds.corrected inverted",
          100 / load("falsification_seed7")["probe_kinds"]["corrected"], 0)

    b12s7 = row("B12-key-upsert-value-hints_seed7")
    b12s11 = row("B12-key-upsert-value-hints_seed11")
    b14s7 = row("B14-key-upsert-plural_seed7")
    b14s11 = row("B14-key-upsert-plural_seed11")
    a = Anchor(r"B12 scores coexist (\d+\.?\d*) on seed 7 and (\d+\.?\d*) on seed 11 against "
               r"the identical coexist set, so (\d+\.?\d*) of that column")
    check(s, "B12 coexist seed 7", a.one(0),
          "B12-...seed7.json row.coexist_preserved", b12s7["coexist_preserved"], 2)
    check(s, "B12 coexist seed 11", a.one(1),
          "B12-...seed11.json row.coexist_preserved", b12s11["coexist_preserved"], 2)
    check(s, "B12 coexist movement", a.one(2), "B12-...seed7/seed11 coexist_preserved",
          b12s7["coexist_preserved"] - b12s11["coexist_preserved"], 3)

    a = Anchor(r"B12 moves (\d+\.?\d*) on net between its two seeds and B14 moves (\d+\.?\d*)")
    check(s, "B12 net movement", a.one(0), "B12-...seed7/seed11 row.net",
          b12s7["net"] - b12s11["net"], 3)
    check(s, "B14 net movement", a.one(1), "B14-...seed7/seed11 row.net",
          b14s11["net"] - b14s7["net"], 3)

    a = Anchor(r"its decider stored (\d+) of roughly (\d+) facts")
    check(s, "claim graph items", a.one(0), "claimgraph_seed7.json row.items",
          load("claimgraph_seed7")["row"]["items"])

    unsourced(s, "'replayed on seed 7: B9 2, B14 3, B17 3' substring-only credits",
              "a script that scores each structure twice, once on the substring rule "
              "and once on a whole-word rule, and counts the difference. "
              "round5/dataset_check.py checks the round-5 vocabulary, not these counts")


# --- Pairs that already disagree ------------------------------------------
#
# Each entry states what the record says, what the committed file says, and a
# probe that recomputes the file side. The probe keeps the entry honest: fix
# either side and it stops matching, so the disagreement cannot go quiet.

def b9_kept_ranks():
    detail = load("b9_diagnosis_seed7")["offered_and_kept_detail"]
    empty = [d for d in detail if d["retire_list_empty"]]
    ranks = sorted(r["rank"] for d in empty for r in d["survivors"])
    return {"empty_retire_lists": len(empty), "ranks": ranks,
            "at_rank_1_or_2": sum(1 for r in ranks if r <= 2)}


def b12_collided_order():
    return {k: [c for c, _ in rows]
            for k, rows in load("key_diagnosis_seed7")["detail"].items()}


def b15_offer_sizes():
    n = [t["n_offered"] for t in load("upsert_diagnosis_B15_seed7")["per_turn"]]
    return {"turns": len(n), "min": min(n), "max": max(n)}


def offer_width_units():
    d = load("offer_width_seed7")
    k10, k20 = d["k_sweep"][1], d["k_sweep"][2]
    corrected = load("falsification_seed7")["probe_kinds"]["corrected"]
    gain_latest = (k20["offered"] - k10["offered"]) / corrected
    loss_latest = 0.03 * k10["offered"] / corrected
    return {"gain_on_latest_wins": round(gain_latest, 4),
            "loss_on_latest_wins": round(loss_latest, 4),
            "ratio_in_latest_wins": round(loss_latest / gain_latest, 2),
            "ratio_in_net": round((loss_latest / 3) / (gain_latest / 3), 2),
            "ratio_mixing_units": round(loss_latest / (gain_latest / 3), 2)}


def fact_counts():
    labels = load("call_schedule_seed7")["label_counts"]
    return {"initial_fact_statements":
            labels["initial"] + labels["distractor"] + labels["coexist"],
            "distinct_facts_planted":
            load("b16_writes_seed7")["structures"]["B9"]["facts_planted"],
            "fact_turns": sum(v for k, v in labels.items() if k != "chatter")}


def control_column_rounding():
    out = {}
    for tag in ("B14", "B9", "B3"):
        r, _ = scored(tag, 7)
        out[tag] = r["control_survival"]
    return out


KNOWN = [
    {"section": "Where the residual correction gap is",
     "record": "in 14 of the 16 the retire list came back empty with the contradicted "
               "row at rank 1 or 2",
     "file": "b9_diagnosis_seed7.json offered_and_kept_detail: 14 empty retire lists, "
             "of which 5 sit at rank 1 or 2; the ranks run 1 to 10",
     "probe": b9_kept_ranks,
     "expect": {"empty_retire_lists": 14, "at_rank_1_or_2": 5,
                "ranks": [1, 1, 1, 2, 2, 3, 4, 5, 5, 7, 8, 9, 9, 10]}},
    {"section": "The structures",
     "record": "`otto.language` taking Korean, then English, then German",
     "file": "key_diagnosis_seed7.json detail: otto.language takes German, Korean, "
             "English; Korean, English, German is maria.language's order",
     "probe": lambda: b12_collided_order()["otto.language"],
     "expect": ["Otto speaks German.", "Otto speaks Korean.", "Otto speaks English."]},
    {"section": "The structures",
     "record": "B15 must match against 32-99 offered keys",
     "file": "upsert_diagnosis_B15_seed7.json per_turn.n_offered spans 11 to 88 over "
             "the 100 correction turns; no committed file records the range over all "
             "388 fact turns, and that file's own producing script is absent",
     "probe": b15_offer_sizes,
     "expect": {"turns": 100, "min": 11, "max": 88}},
    {"section": "The offer is wide enough, and wider does not pay",
     "record": "a 3% degradation on the 93 corrections already reaching the decider "
               "would be four times the gain in the opposite direction",
     "file": "offer_width_seed7.json: the loss and the gain are four times apart only "
             "when a latest-wins loss is divided by a net gain; in either column alone "
             "the ratio is 1.4",
     "probe": offer_width_units,
     "expect": {"gain_on_latest_wins": 0.02, "loss_on_latest_wins": 0.0279,
                "ratio_in_latest_wins": 1.4, "ratio_in_net": 1.4,
                "ratio_mixing_units": 4.18}},
    {"section": "Limits",
     "record": "its decider stored 85 of roughly 290 facts",
     "file": "no committed file carries 290. call_schedule_seed7.json label_counts "
             "gives 288 initial fact statements and 388 fact turns; "
             "b16_writes_seed7.json gives 268 distinct facts planted",
     "probe": fact_counts,
     "expect": {"initial_fact_statements": 288, "distinct_facts_planted": 268,
                "fact_turns": 388}},
    {"section": "The structures",
     "record": "the control column prints B3 as 0.97 and B14 and B9 as 0.98",
     "file": "all three rows hold control_survival 0.975; the same value is rounded "
             "in two directions",
     "probe": control_column_rounding,
     "expect": {"B14": 0.975, "B9": 0.975, "B3": 0.975},
     "accounts_for": [("The structures", "B3 control")]},
]


# --- Report ---------------------------------------------------------------

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
    for section in (method, separability, structures, net_column, gap, offer_width,
                    marker, call_schedule, adopted, limits):
        section()

    accounted = {pair for k in KNOWN for pair in k.get("accounts_for", ())}
    failures = []
    by_section = {}
    for entry in CHECKS:
        v = verdict(entry)
        by_section.setdefault(entry["section"], []).append(v)
        if v != "OK" and (entry["section"], entry["claim"]) not in accounted:
            failures.append((v, entry))

    print(f"{len(CHECKS)} figures read from README.md and compared to committed files\n")
    for section, verdicts in by_section.items():
        ok = sum(1 for v in verdicts if v == "OK")
        print(f"  {ok:>3}/{len(verdicts):<3} {section}")

    print(f"\n{len(UNSOURCED)} claims no committed file carries:\n")
    for u in UNSOURCED:
        print(f"  [{u['section']}] {u['claim']}")
        print(f"      needed: {u['needed']}")

    print(f"\n{len(KNOWN)} recorded disagreements between the record and its sources:\n")
    stale = []
    for k in KNOWN:
        got = k["probe"]()
        agrees = got == k["expect"]
        print(f"  [{k['section']}] {'' if agrees else 'CHANGED '}{k['record']}")
        print(f"      file: {k['file']}")
        if not agrees:
            print(f"      probe now returns {got!r}, recorded {k['expect']!r}")
            stale.append(k)

    if failures:
        print(f"\n{len(failures)} figures do not match their source:\n")
        for v, entry in failures:
            print(f"  {v} [{entry['section']}] {entry['claim']}: README says "
                  f"{entry['stated']}, {entry['source']} holds {entry['found']}")

    if failures or stale:
        return 1
    print("\nevery figure with a committed source matches it")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
