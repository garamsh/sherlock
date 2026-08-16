"""Checks the figures generation does not reach.

The results table is written by `record.py` from `../results_raw.json`, so it
cannot disagree with the run and is not checked here. What is left is the three
kinds of figure a generated table does not carry: an input this record states
from `systems.py` or `system_runner.py`, a figure its prose restates outside
the table, and the headline figure `../../README.md` quotes — a document this
record does not own and no generator of its own writes.

Two registers:

  CHECKS      a stated figure and the committed value it must equal
  UNSOURCED   stated figures that no committed file carries

Reads committed files only; runs no system and makes no measurement. Exits
non-zero when a check fails or when an anchor no longer matches the document.
"""
from __future__ import annotations
import json
import math
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.join(HERE, "..")

sys.path.insert(0, HERE)

import system_runner
import systems

README = open(os.path.join(ROOT, "README.md")).read()
INDEX = open(os.path.join(ROOT, "..", "README.md")).read()
RAW = json.load(open(os.path.join(ROOT, "results_raw.json")))
BY_SYSTEM = {r["system"]: r for r in RAW}


def half_up(value, places):
    """Round half away from zero, the direction a reader assumes."""
    settled = round(abs(value), places + 6)
    factor = 10 ** places
    return math.floor(settled * factor + 0.5) / factor * (1 if value >= 0 else -1)


class Anchor:
    """One regex over a document whose capture groups are the stated figures."""

    def __init__(self, pattern, text=None):
        matches = re.findall(pattern, README if text is None else text)
        if len(matches) != 1:
            raise SystemExit(
                f"anchor matched {len(matches)} times, expected 1: {pattern}")
        self.groups = matches[0]

    def one(self, index=0):
        g = self.groups
        return g[index] if isinstance(g, tuple) else g


CHECKS = []
UNSOURCED = []


def check(part, claim, stated, source, found, places=None):
    """Record one figure read from a document against the committed value."""
    CHECKS.append({"part": part, "claim": claim, "stated": stated,
                   "source": source, "found": found, "places": places})


def unsourced(part, claim, needed):
    UNSOURCED.append({"part": part, "claim": claim, "needed": needed})


# --- The compliance table -------------------------------------------------

def compliance():
    part = "the method"
    model = systems.SimModel()
    rates = [
        ("Voluntary writeback block", model.voluntary_writeback_rate,
         "systems.py SimModel.voluntary_writeback_rate"),
        ("Structured output shape", model.structured_output_rate,
         "systems.py SimModel.structured_output_rate"),
        ("Platform extraction", model.extraction_rate,
         "systems.py SimModel.extraction_rate"),
        ("Forced narrow call", model.forced_summary_rate,
         "systems.py SimModel.forced_summary_rate"),
        ("On-demand search usage", model.ondemand_search_rate,
         "systems.py SimModel.ondemand_search_rate"),
    ]
    for label, rate, source in rates:
        a = Anchor(r"\| " + re.escape(label) + r" \| (\d+)% \|")
        check(part, label, a.one(), source, rate * 100, 0)

    a = Anchor(r"\| Structured output content \(write the facts\) \| (\d+)% / (\d+)% \|")
    check(part, "lazy write rate", a.one(0),
          "systems.py N2sStructuredLazy.optimistic_write_rate",
          systems.N2sStructuredLazy.optimistic_write_rate * 100, 0)
    check(part, "structured write rate", a.one(1),
          "systems.py N2StructuredWriteback.optimistic_write_rate",
          systems.N2StructuredWriteback.optimistic_write_rate * 100, 0)

    a = Anchor(r"Two willingness rates tested \((\d+)%, and a lazy (\d+)% variant N2s\)")
    check(part, "structured write rate restated", a.one(0),
          "systems.py N2StructuredWriteback.optimistic_write_rate",
          systems.N2StructuredWriteback.optimistic_write_rate * 100, 0)
    check(part, "lazy write rate restated", a.one(1),
          "systems.py N2sStructuredLazy.optimistic_write_rate",
          systems.N2sStructuredLazy.optimistic_write_rate * 100, 0)


# --- The workload and the store -------------------------------------------

def workload():
    part = "the method"
    a = Anchor(r"scripted (\d+)-turn conversation")
    check(part, "turns in the script", a.one(),
          "system_runner.py SCRIPT", len(system_runner.SCRIPT))

    a = Anchor(r"supersession at cosine ≥ (\d+\.\d+)")
    source = open(os.path.join(HERE, "systems.py")).read()
    check(part, "supersession threshold", a.one(),
          "systems.py Store.add supersession test",
          re.search(r"cosine_tokens\(new_toks, tokenize\(r\.content\)\) >= (\d+\.\d+)",
                    source).group(1), 1)

    a = Anchor(r"the top-(\d+) salience memories always injected")
    check(part, "core block size", a.one(),
          "systems.py N8StructuredCoreBlock.CORE_K",
          systems.N8StructuredCoreBlock.CORE_K)

    a = Anchor(r"the window holds the last (\d+) turns")
    check(part, "context window", a.one(),
          "systems.py N7FullContext.CONTEXT_TURNS",
          systems.N7FullContext.CONTEXT_TURNS)


# --- What the numbers establish -------------------------------------------

def findings():
    part = "what the numbers establish"
    a = Anchor(r"N1 scores (\d+\.\d+) not because its store")
    check(part, "N1 restated", a.one(), "results_raw.json N1 context_recall",
          BY_SYSTEM["N1-voluntary-writeback"]["context_recall"], 2)

    a = Anchor(r"N2 moves recall from (\d+\.\d+) to (\d+\.\d+) with zero extra calls")
    check(part, "N1 restated again", a.one(0), "results_raw.json N1 context_recall",
          BY_SYSTEM["N1-voluntary-writeback"]["context_recall"], 2)
    check(part, "N2 restated", a.one(1), "results_raw.json N2 context_recall",
          BY_SYSTEM["N2-structured-writeback"]["context_recall"], 2)

    a = Anchor(r"N2s \(lazy model, (\d+)% fill\) drops to (\d+\.\d+)")
    check(part, "lazy fill rate restated", a.one(0),
          "systems.py N2sStructuredLazy.optimistic_write_rate",
          systems.N2sStructuredLazy.optimistic_write_rate * 100, 0)
    check(part, "N2s restated", a.one(1), "results_raw.json N2s context_recall",
          BY_SYSTEM["N2s-structured-lazy"]["context_recall"], 2)

    a = Anchor(r"N4, N6, N8 all tie N2 at (\d+\.\d+)")
    tied = {BY_SYSTEM[s]["context_recall"] for s in
            ("N2-structured-writeback", "N4-rolling-narrative", "N6-forced-notes",
             "N8-structured-coreblock")}
    check(part, "the tie", a.one(), "results_raw.json context_recall for N2, N4, N6, N8",
          tied.pop() if len(tied) == 1 else sorted(tied), 2)

    a = Anchor(r"The (\d+\.\d+) ceiling is the write path's willingness")
    check(part, "the ceiling", a.one(), "results_raw.json N2 context_recall",
          BY_SYSTEM["N2-structured-writeback"]["context_recall"], 2)

    a = Anchor(r"(\d+\.\d+) at (\d+) turns, but it forgets everything older")
    check(part, "N7 restated", a.one(0), "results_raw.json N7 context_recall",
          BY_SYSTEM["N7-full-context"]["context_recall"], 2)
    check(part, "turns in the script restated", a.one(1),
          "system_runner.py SCRIPT", len(system_runner.SCRIPT))

    a = Anchor(r"Deterministic pattern-matching caught (\d+\.\d+)")
    check(part, "N3 restated", a.one(), "results_raw.json N3 context_recall",
          BY_SYSTEM["N3-platform-extraction"]["context_recall"], 2)


# --- What the experiments index quotes ------------------------------------

def index():
    part = "the experiments index"
    a = Anchor(r"the voluntary writeback is the root failure \((\d+\.\d+) context-recall\)",
               INDEX)
    check(part, "N1 quoted in the index", a.one(),
          "results_raw.json N1 context_recall",
          BY_SYSTEM["N1-voluntary-writeback"]["context_recall"], 2)


def external():
    part = "the method"
    unsourced(part, "the QA compliance measurements: gpt-4o-mini 0/7 (QA #5-#7), "
                    "gpt-4o 0/15 (QA #8), and the 15/15 restated in the opening",
              "they are behavioural QA passes against a live model, not runs of this "
              "harness; no file in this repository carries them, and the harness takes "
              "0% as its input rather than producing it")


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
    for part in (compliance, workload, findings, index, external):
        part()

    failures = []
    by_part = {}
    for entry in CHECKS:
        v = verdict(entry)
        by_part.setdefault(entry["part"], []).append(v)
        if v != "OK":
            failures.append((v, entry))

    print(f"{len(CHECKS)} figures read from the record and compared to committed "
          f"files\n")
    for part, verdicts in by_part.items():
        ok = sum(1 for v in verdicts if v == "OK")
        print(f"  {ok:>3}/{len(verdicts):<3} {part}")

    print(f"\n{len(UNSOURCED)} claims no committed file carries:\n")
    for u in UNSOURCED:
        print(f"  [{u['part']}] {u['claim']}")
        print(f"      needed: {u['needed']}")

    if failures:
        print(f"\n{len(failures)} figures do not match their source:\n")
        for v, entry in failures:
            print(f"  {v} [{entry['part']}] {entry['claim']}: the record says "
                  f"{entry['stated']}, {entry['source']} holds {entry['found']}")
        return 1
    print("\nevery figure with a committed source matches it")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
