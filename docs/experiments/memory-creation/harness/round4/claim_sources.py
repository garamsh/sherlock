"""Checks the round-4 figures generation does not reach.

Round 4's ten result tables are written by `record.py` from
`../../results_round4/`, so a cell there cannot disagree with the run behind it
and is not read back here. What is left is what a generated table does not
carry: an input the record states from this directory, and — most of what
follows — the figures its prose states outside a table, which is every reading
it draws from those tables and every count it quotes from a diagnosis file.

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
import record

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

    Anchor(r"planted (\w+) topics, corrected every one of them, and backed "
           r"each number with (\w+) probes")
    unsourced(s, "the rounds 1–3 dataset shape: ten topics, ten probes each",
              "the harness that carried it was deleted in #483 as superseded, so "
              "the record now states the shape on its own authority; the anchor "
              "above still fails if the sentence is reworded")


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

# Which result file holds each structure's scored row is `record.py`'s, which
# writes the table from them; reading it from there keeps one definition.
SYSTEM_OF = {tag: system for tag, _, _, system, _, _ in record.STRUCTURES}


def structures():
    s = "The structures"
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
    for tag in ("B3", "B9"):
        r_ = record.scored(SYSTEM_OF[tag], 7)
        check(s, f"{tag} control retired", a.one(),
              f"the scored row of {SYSTEM_OF[tag]} at seed 7 .control_retired",
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

    writes = load("b16_writes_seed7")["structures"]
    a = Anchor(r"answers (\d+) of (\d+) fact turns with an empty write list against "
               r"B14's (\d+)")
    check(s, "B15 empty write turns", a.one(0),
          "b16_writes_seed7.json B15.empty_write_turns",
          writes["B15"]["empty_write_turns"])
    check(s, "fact turns restated", a.one(1),
          "b16_writes_seed7.json B15.fact_turns", writes["B15"]["fact_turns"])
    check(s, "B14 empty write turns", a.one(2),
          "b16_writes_seed7.json B14.empty_write_turns",
          writes["B14"]["empty_write_turns"])

    a = Anchor(r"empty writes are (\d+) of (\d+) here against B15's (\d+)")
    check(s, "B17 empty write turns", a.one(0),
          "b16_writes_seed7.json B17.empty_write_turns",
          writes["B17"]["empty_write_turns"])
    check(s, "fact turns behind B17", a.one(1),
          "b16_writes_seed7.json B17.fact_turns", writes["B17"]["fact_turns"])
    check(s, "B15 empty write turns restated", a.one(2),
          "b16_writes_seed7.json B15.empty_write_turns",
          writes["B15"]["empty_write_turns"])

    a = Anchor(r"\(([\d,]+) candidates judged, none skipped\)")
    check(s, "B16 candidates judged", a.one().replace(",", ""),
          "b16_writes_seed7.json B16.candidates_judged",
          writes["B16"]["candidates_judged"])
    check(s, "B16 candidates skipped", 0,
          "b16_writes_seed7.json B16.candidates_skipped",
          writes["B16"]["candidates_skipped"])

    upsert = {tag: load(f"upsert_diagnosis_{tag}_seed7")
              for tag in ("B14", "B15", "B17")}
    a = Anchor(r"(\d+) of B14's live keys name one, against (\d+) of B17's, and both "
               r"`(\S+?)` and `(\S+?)` are gone")
    check(s, "B14 value-named keys", a.one(0),
          "upsert_diagnosis_B14_seed7.json value_named_keys",
          len(upsert["B14"]["value_named_keys"]))
    check(s, "B17 value-named keys", a.one(1),
          "upsert_diagnosis_B17_seed7.json value_named_keys",
          len(upsert["B17"]["value_named_keys"]))
    for i in (2, 3):
        key = a.one(i)
        check(s, f"{key} under B14 and not B17", "B14",
              "upsert_diagnosis_*_seed7.json value_named_keys",
              ("B14" if key in upsert["B14"]["value_named_keys"] else "")
              + ("B17" if key in upsert["B17"]["value_named_keys"] else ""))

    a = Anchor(r"Its remaining (\w+) misses are genuine topic ambiguity")
    coined = sum(v for k, v in upsert["B17"]["stats"].items() if k.startswith("coined"))
    check(s, "B17 coined-key misses", {"eight": 8}[a.one()],
          "upsert_diagnosis_B17_seed7.json stats coined_key_*", coined)

    a = Anchor(r"`(\S+?)` was on the table and the decider wrote `(\S+?)`")
    fish = [e for e in upsert["B17"]["per_turn"] if e["text"] == "Maria moved to fish."]
    check(s, "the key Maria's shell correction should have reused", a.one(0),
          "upsert_diagnosis_B17_seed7.json per_turn old_keys", fish[0]["old_keys"][0])
    check(s, "the key it wrote instead", a.one(1),
          "upsert_diagnosis_B17_seed7.json per_turn written_keys",
          fish[0]["written_keys"][0])

    a = Anchor(r"B15 must match against (\d+)-(\d+) offered keys on the correction turns")
    widths = [e["n_offered"] for e in upsert["B15"]["per_turn"]]
    check(s, "B15 narrowest offer", a.one(0),
          "upsert_diagnosis_B15_seed7.json per_turn.n_offered", min(widths))
    check(s, "B15 widest offer", a.one(1),
          "upsert_diagnosis_B15_seed7.json per_turn.n_offered", max(widths))

    a = Anchor(r"`otto.language` taking (\w+), then (\w+), then (\w+)")
    order = [c for c, _ in load("key_diagnosis_seed7")["detail"]["otto.language"]]
    for i in range(3):
        check(s, f"otto.language write {i + 1}", a.one(i),
              "key_diagnosis_seed7.json detail['otto.language']",
              order[i].split()[-1].rstrip("."))


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
    a = Anchor(r"corrected family buys (\d+\.?\d*)\. Taking coexist from 12 probes to 60 "
               r"buys (\d+\.?\d*)")
    check(s, "gain from quadrupling corrected", a.one(0), "precision.py budget()",
          budget[(100, 12)] - budget[(400, 12)], 3)
    check(s, "gain from five times coexist", a.one(1), "precision.py budget()",
          budget[(100, 12)] - budget[(100, 60)], 3)

    a = Anchor(r"hold the three family scores fixed at (\d\.\d+), (\d\.\d+) and "
               r"(\d\.\d+)")
    literals = re.findall(r"_se\((\d\.\d+), ",
                          open(os.path.join(HERE, "precision.py")).read())
    for i, label in enumerate(("corrected", "control", "coexist")):
        check(s, f"half-width table {label} score", a.one(i),
              "precision.py budget() literals", literals[i], 2)


# --- Where the residual correction gap is ---------------------------------

def gap():
    s = "Where the residual correction gap is"
    g = load("gap_diagnosis_seed7")["stats"]
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

    a = Anchor(r"all (\d+) were offered, and in (\d+) of them the retire list came "
               r"back empty")
    check(s, "B9 offered and kept", a.one(0),
          "b9_diagnosis_seed7.json correction_gap.offered_and_kept", b9["offered_and_kept"])
    check(s, "B9 empty retire lists", a.one(1),
          "b9_diagnosis_seed7.json offered_and_kept_with_empty_retire_list",
          load("b9_diagnosis_seed7")["offered_and_kept_with_empty_retire_list"])

    a = Anchor(r"Those 14 sit at ranks ([\d, and]+?), so what the decider")
    stated_ranks = [int(n) for n in re.findall(r"\d+", a.one())]
    detail = load("b9_diagnosis_seed7")["offered_and_kept_detail"]
    found_ranks = sorted(r["rank"] for d in detail if d["retire_list_empty"]
                         for r in d["survivors"])
    check(s, "ranks of the rows passed over", str(stated_ranks),
          "b9_diagnosis_seed7.json offered_and_kept_detail[].survivors[].rank",
          str(found_ranks))


# --- The offer is wide enough, and wider does not pay ---------------------

def offer_width():
    s = "The offer is wide enough, and wider does not pay"
    d = load("offer_width_seed7")
    a = Anchor(r"against the (\d+) corrections whose old value still had a live row")
    check(s, "corrections with a live row", a.one(),
          "offer_width_seed7.json corrections_with_a_live_row",
          d["corrections_with_a_live_row"])

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

def marker():
    s = "What the corrected column is measuring"
    mf = load("marker_following_seed7")
    arms = mf["judgment"]["arms"]

    a = Anchor(r"on the (\d+) whose offer carried the contradicted row")
    check(s, "judgeable corrections", a.one(),
          "marker_following_seed7.json judgment.arms.A.n", arms["A"]["n"])

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
    check(s, "perfect categories",
          {"Eight": 8}[Anchor(r"(\w+) categories are perfect under every arm").one()],
          "marker_following_seed7.json by_category",
          sum(1 for c in cats.values() if all(c[k] == c["n"] for k in "ABCD")))

    a = Anchor(r"D trails C by (\d+) inside them")
    # The categories the table gives a row of their own; the rest are the block
    # this claim is about.
    named = {c for group, _ in record.CATEGORY_ROWS if group and len(group) == 1
             for c in group}
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

    a = Anchor(r"Take (\d+)% as an illustration rather than a measurement: at that "
               r"recall the gate loses (\d\.\d+) of `net`, which is more than the "
               r"(\d\.\d+) between")
    missed = (100 - int(a.one(0))) / 100
    check(s, "net lost at the illustrated recall", a.one(1),
          "call_schedule_seed7.json B14.gate.calls, each fact turn carrying 1/388",
          missed * b14["gate"]["calls"] / b14["gate"]["calls"], 3)
    check(s, "distance from the adopted structure to the best-scoring one", a.one(2),
          "B14-...seed7.json and B9-...seed7.json row.net",
          row("B14-key-upsert-plural_seed7")["net"]
          - row("B9-decider-neighbors-wide_seed7")["net"], 3)


# --- Adopted direction ----------------------------------------------------

def adopted():
    s = "Adopted direction"
    a = Anchor(r"\(B9, net (\d+\.?\d*)\)")
    check(s, "B9 net", a.one(), "B9-decider-neighbors-wide_seed7.json row.net",
          row("B9-decider-neighbors-wide_seed7")["net"], 2)

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

    a = Anchor(r"its decider stored (\d+) items against the (\d+) facts the stream "
               r"states before any correction")
    check(s, "claim graph items", a.one(0), "claimgraph_seed7.json row.items",
          load("claimgraph_seed7")["row"]["items"])
    labels = load("call_schedule_seed7")["label_counts"]
    check(s, "facts stated before correction", a.one(1),
          "call_schedule_seed7.json label_counts initial+distractor+coexist",
          labels["initial"] + labels["distractor"] + labels["coexist"])

    credits = load("substring_credits_seed7")["structures"]
    a = Anchor(r"replayed on seed 7: B9 (\d+), B14 (\d+), B17 (\d+)")
    for i, tag in enumerate(("B9", "B14", "B17")):
        check(s, f"{tag} substring-only credits", a.one(i),
              f"substring_credits_seed7.json {tag}.substring_only_credits",
              credits[tag]["substring_only_credits"])
    check(s, "substring credits all corrected", "corrected",
          "substring_credits_seed7.json credits[].family",
          "".join(sorted({c["family"] for d in credits.values()
                          for c in d["credits"]})))




# --- Pairs that already disagree ------------------------------------------
#
# Empty: #396 resolved the six this register reported. An entry belongs here
# only while the record and its source say different things, with a probe that
# recomputes the file side so the disagreement cannot go quiet.

KNOWN = []


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
