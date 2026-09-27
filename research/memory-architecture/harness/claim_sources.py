"""Checks every figure in the record against the committed file it came from.

`../README.md` states figures; `../results/` holds the runs behind them and this
directory holds the harness behind those. This reads both and compares, so the
register cannot drift from either side: change a published figure or a result
file and a check fails.

Three registers:

  CHECKS      a stated figure and the committed value it must equal
  KNOWN       pairs that already disagree, recorded so a new disagreement still fails
  UNSOURCED   stated figures that no committed file carries

The research index's architecture count is checked against the harness.

Reads committed files only; runs no architecture and makes no measurement. Exits
non-zero when a check fails, when an anchor no longer matches the document it reads,
or when a KNOWN entry no longer describes what the two sides say.
"""
from __future__ import annotations
import glob
import json
import math
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.join(HERE, "..")
RESULTS = os.path.join(ROOT, "results")

sys.path.insert(0, HERE)

import architectures
import metrics
import record
import runner
import scale_runner
from workload_spec import SESSIONS, LONG_HORIZON_SESSIONS
import diverse_workload

README = open(os.path.join(ROOT, "README.md")).read()


# --- Reading the two sides -------------------------------------------------

def _cells(line):
    return [re.sub(r"\*+", "", c).strip() for c in line.strip().strip("|").split("|")]


def _is_rule(line):
    return set(line.strip()) <= set("|-: ")


def result_rows(name):
    """Rows of the one table in `results/<name>.md`, as a list of dicts."""
    lines = [ln for ln in open(os.path.join(RESULTS, f"{name}.md"))
             if ln.lstrip().startswith("|")]
    header, rows = None, []
    for line in lines:
        if _is_rule(line):
            continue
        cells = _cells(line)
        if header is None:
            header = cells
        elif cells != header:
            rows.append(dict(zip(header, cells)))
    return rows


def by_arch(name, key="arch"):
    """`results/<name>.md` keyed by its architecture column."""
    return {r[key]: r for r in result_rows(name)}


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


def num(text):
    """A README or results cell as a float, ignoring thousands separators."""
    return float(str(text).replace(",", ""))


def places(text):
    """Decimal places the README chose to print, which sets the tolerance."""
    body = str(text).replace(",", "")
    return len(body.split(".")[1]) if "." in body else 0


def half_up(value, digits):
    """Round half away from zero, the direction a reader assumes."""
    factor = 10 ** digits
    return math.floor(abs(value) * factor + 0.5) / factor * (1 if value >= 0 else -1)


# --- Registers -------------------------------------------------------------

CHECKS = []
UNSOURCED = []


def check(section, claim, stated, source, found, rounded=True):
    CHECKS.append({"section": section, "claim": claim, "stated": str(stated),
                   "source": source, "found": found, "rounded": rounded})


def unsourced(section, claim, needed):
    UNSOURCED.append({"section": section, "claim": claim, "needed": needed})


# --- The method ------------------------------------------------------------

def architecture_ids():
    """Every architecture id any runner can select, from the runners themselves."""
    ids = set()
    for name in ("runner.py", "scale_runner.py", "long_horizon_runner.py",
                 "diverse_runner.py", "hybrid_runner.py", "large_runner.py"):
        source = open(os.path.join(HERE, name)).read()
        ids |= {i for i, _ in re.findall(r'"([A-Z]\d)":\s*"(\w+)"', source)}
    return ids


def architecture_classes():
    """{id: class} over every architecture a runner can select."""
    out = {}
    for name in ("runner.py", "long_horizon_runner.py", "diverse_runner.py",
                 "hybrid_runner.py", "large_runner.py"):
        source = open(os.path.join(HERE, name)).read()
        for i, cls in re.findall(r'"([A-Z]\d)":\s*"(\w+)"', source):
            out[i] = getattr(architectures, cls)
    return out


def large_defaults():
    """(real items per session, noise items) from make_large_workload's signature."""
    a = Anchor(r"def make_large_workload\(n_sessions=\d+, n_real_per_session=(\d+), "
               r"n_noise=(\d+)\)", open(os.path.join(HERE, "large_runner.py")).read())
    return int(a.one(0)), int(a.one(1))


def large_real_items(constant="REAL_SESSIONS_FULL"):
    """Real items the large-scale run stores, for the full or the reduced arm."""
    source = open(os.path.join(HERE, "large_runner.py")).read()
    return int(Anchor(rf"{constant} = (\d+)", source).one()) * large_defaults()[0]


def method():
    s = "The method"
    words = {"Thirty-eight": 38}
    a = Anchor(r"([\w-]+) candidate architectures were implemented")
    check(s, "architecture count", words[a.one()],
          "the six runners' class-name maps", len(architecture_ids()))

    a = Anchor(r"`harness/architectures/`, one file per series letter, "
               r"(\d+) to (\d+) lines each")
    loc = [metrics.lines_of_code(c) for c in architecture_classes().values()]
    check(s, "shortest architecture", a.one(0), "metrics.lines_of_code", min(loc))
    check(s, "longest architecture", a.one(1), "metrics.lines_of_code", max(loc))

    a = Anchor(r"\| small \| ([\d,]+) sessions \+ ([\d,]+) noise turns \|")
    check(s, "small sessions", a.one(0), "workload_spec.SESSIONS", len(SESSIONS))
    check(s, "small noise turns", a.one(1), "runner.py noise loop",
          int(Anchor(r"for n in range\((\d+)\):", open(
              os.path.join(HERE, "runner.py")).read()).one()))

    a = Anchor(r"\| long-horizon \| (\d+) sessions across a simulated (\d+)-year "
               r"life \+ ([\d,]+) noise \|")
    check(s, "long-horizon sessions", a.one(0), "workload_spec.LONG_HORIZON_SESSIONS",
          len(LONG_HORIZON_SESSIONS))
    check(s, "long-horizon noise", a.one(2), "long_horizon_runner.py target_noise",
          int(Anchor(r"target_noise: int = (\d+)", open(
              os.path.join(HERE, "long_horizon_runner.py")).read()).one()))

    a = Anchor(r"\| diverse \| (\d+) sessions across (\d+) memory categories "
               r"\+ ([\d,]+) noise \|")
    check(s, "diverse sessions", a.one(0), "diverse_workload.SESSIONS",
          len(diverse_workload.SESSIONS))
    check(s, "diverse categories", a.one(1), "diverse_workload.SESSIONS categories",
          len({sess.category for sess in diverse_workload.SESSIONS}))
    check(s, "diverse noise", a.one(2), "diverse_runner.py noise_filler default",
          int(Anchor(r"def noise_filler\(n: int = (\d+)\)", open(
              os.path.join(HERE, "diverse_runner.py")).read()).one()))

    a = Anchor(r"\| large-scale \| ([\d,]+) real items \+ ([\d,]+) noise \|")
    check(s, "large real items", a.one(0),
          "large_runner REAL_SESSIONS_FULL × n_real_per_session", large_real_items())
    check(s, "large noise", a.one(1), "large_runner.make_large_workload default",
          large_defaults()[1])

    a = Anchor(r"padded toward ([\d,]+) and ([\d,]+) stored items")
    for i, scale in enumerate(scale_runner.SCALES):
        check(s, f"scale target {i + 1}", a.one(i), "scale_runner.SCALES", scale)


# --- Small workload --------------------------------------------------------

def density_from_store():
    """Rows whose R/KB is the row's own recall over the KB that row stored."""
    agree = 0
    for row in json.load(open(os.path.join(RESULTS, "raw.json"))):
        if "error" in row:
            continue
        kb = row["storage_bytes"] / 1024.0
        expected = row["R@5"] / kb if kb else 0.0
        agree += abs(row["R_per_KB"] - expected) < 1e-9
    return agree


def small():
    s = "Small workload"
    table = by_arch("table")
    check(s, "heading item count",
          Anchor(r"### Small workload \(([\d,]+) items\)").one(),
          "results/table.md A1 items", num(table["A1"]["items"]))

    shown = ("R@5", "P@5", "anti-poll", "items", "KB")
    tied = [c for c in shown if table["E3"][c] == table["E4"][c]]
    check(s, "E4 ties E3 on every column this table shows", len(shown),
          "results/table.md E3 and E4 rows", len(tied), rounded=False)

    # The run is complete and was produced by the committed harness: no
    # architecture failed, every name the runner selects has a measured row, and
    # the file's column count is the one the committed renderer writes.
    detail = open(os.path.join(RESULTS, "per_architecture.md")).read()
    check(s, "architectures that failed to run", 0,
          "results/per_architecture.md ERROR blocks",
          len(re.findall(r"^ERROR: ", detail, re.M)), rounded=False)
    check(s, "architectures the runner selects that the table measures",
          len(runner.ALL_NAMES), "results/table.md rows carrying an item count",
          sum(1 for a in runner.ALL_NAMES if table[a]["items"] != "—"),
          rounded=False)
    committed = [ln for ln in open(os.path.join(RESULTS, "table.md"))
                 if ln.lstrip().startswith("|")][0]
    check(s, "columns the committed renderer writes",
          len(_cells(runner.render_table([]).splitlines()[0])),
          "results/table.md header", len(_cells(committed)), rounded=False)

    # Every column that reads the store reads the one the run filled. A column
    # measured against a fresh store is zero for every architecture, and an R/KB
    # that is not the row's own recall over its own stored KB came from elsewhere.
    measured = [r for r in result_rows("table") if re.fullmatch(r"[A-Z]\d", r["arch"])]
    zeroed = [c for c in ("R/stored-KB", "supers", "stale", "rep-keep")
              if all(num(r[c]) == 0 for r in measured)]
    check(s, "store-reading columns that are zero for every architecture", 0,
          "results/table.md R/stored-KB, supers, stale and rep-keep", len(zeroed),
          rounded=False)
    check(s, "rows whose density is their own recall over their own stored KB",
          len(measured), "results/raw.json R@5 and storage_bytes",
          density_from_store(), rounded=False)

    # The two densities the record names apart, and the store they share.
    a = Anchor(r"F5 holds (\d+) items in both runs and reads (\d\.\d+) stored "
               r"against (\d\.\d+) modelled")
    at_1052 = [r for r in result_rows("scale")
               if r["arch"] == "F5" and num(r["scale"]) == 1052][0]
    check(s, "F5 items here", a.one(0), "results/table.md F5 items",
          num(table["F5"]["items"]))
    check(s, "F5 items on the scale run", a.one(0), "results/scale.md F5 items at 1052",
          num(at_1052["items"]))
    check(s, "F5 stored density", a.one(1), "results/table.md F5 R/stored-KB",
          num(table["F5"]["R/stored-KB"]))
    check(s, "F5 modelled density", a.one(2),
          "results/scale.md F5 R/modelled-KB at 1052",
          num(at_1052["R/modelled-KB"]))
    check(s, "the storage each item is modelled at",
          Anchor(r"at ([\d.]+) KB a stored item").one(),
          "metrics.KB_PER_ITEM", metrics.KB_PER_ITEM)


# --- Scale run -------------------------------------------------------------

def scale():
    s = "Scale run"
    at = {r["arch"]: r for r in result_rows("scale") if num(r["scale"]) == 1052}
    padded = {r["arch"]: r for r in result_rows("scale") if num(r["scale"]) == 5000}
    check(s, "padding target named in the prose",
          Anchor(r"padded toward ([\d,]+) items").one(),
          "scale_runner.SCALES", scale_runner.SCALES[0])

    # The runner prints this from the module, so the file cannot drift from it.
    check(s, "sessions the result file states",
          Anchor(r"Same (\d+) sessions' queries",
                 open(os.path.join(RESULTS, "scale.md")).read()).one(),
          "workload_spec.SESSIONS", len(SESSIONS))
    held = [a for a in at
            if at[a]["R@5"] == padded[a]["R@5"] and at[a]["P@5"] == padded[a]["P@5"]]
    stated = [arch for arch, _, _ in record.TABLES["scale"]["rows"]]
    check(s, "rows holding R@5 and P@5 at the larger padding", len(stated),
          "results/scale.md rows equal at both scales",
          sum(1 for a in stated if a in held), rounded=False)

    a = Anchor(r"B3 from ([\d,.]+) μs to ([\d,.]+) μs")
    check(s, "B3 query at 1,052", a.one(0), "results/scale.md B3 qry μs",
          num(at["B3"]["qry μs"]))
    check(s, "B3 query at 5,000", a.one(1), "results/scale.md B3 qry μs",
          num(padded["B3"]["qry μs"]))


# --- The remaining workloads -----------------------------------------------

def long_horizon():
    s = "Long-horizon workload"

    # The runner prints these from the module, so the file cannot drift from it.
    a = Anchor(r"(\d+) sessions across 10 simulated years \+ (\d+) noise turns",
               open(os.path.join(RESULTS, "long_horizon.md")).read())
    check(s, "sessions the result file states", a.one(0),
          "workload_spec.LONG_HORIZON_SESSIONS", len(LONG_HORIZON_SESSIONS))
    check(s, "noise the result file states", a.one(1),
          "long_horizon_runner.py noise target",
          int(Anchor(r"noise = len\(LONG_HORIZON_SESSIONS\), (\d+)",
                     open(os.path.join(HERE, "long_horizon_runner.py")).read()).one()))

    a = Anchor(r"F5 falls to (\d\.\d+) from the (\d\.\d+) it scores on the scale run")
    check(s, "F5 here", a.one(0), "results/long_horizon.md F5 R@5",
          num(by_arch("long_horizon")["F5"]["R@5"]))
    check(s, "F5 on the scale run", a.one(1), "results/scale.md F5 R@5 at 1052",
          num([r for r in result_rows("scale")
               if r["arch"] == "F5" and num(r["scale"]) == 1052][0]["R@5"]))
    check(s, "G6 here", Anchor(r"G6 reaches (\d\.\d+)").one(),
          "results/long_horizon.md G6 R@5",
          num(by_arch("long_horizon")["G6"]["R@5"]))


def diverse():
    s = "Diverse workload"
    a = Anchor(r"(\d+) sessions across (\d+) cognitive categories",
               open(os.path.join(RESULTS, "diverse.md")).read())
    check(s, "sessions the result file states", a.one(0),
          "diverse_workload.SESSIONS", len(diverse_workload.SESSIONS))
    check(s, "categories the result file states", a.one(1),
          "diverse_workload.SESSIONS categories",
          len({sess.category for sess in diverse_workload.SESSIONS}))


def large_scale():
    s = "Large-scale workload"
    a = Anchor(r"H1 and H6 ran a smaller workload than the rest of this table, "
               r"([\d,]+) real items rather than ([\d,]+)")
    note = Anchor(r"H1 and H6 ran on a smaller workload \((\d+) sessions × (\d+) "
                  r"turns = ([\d,]+) real items\)",
                  open(os.path.join(RESULTS, "large_scale.md")).read())
    check(s, "H1 and H6 workload", a.one(0),
          "results/large_scale.md note", num(note.one(2)))
    check(s, "H1 and H6 workload against the runner", a.one(0),
          "large_runner REAL_SESSIONS_SMALL × n_real_per_session",
          large_real_items("REAL_SESSIONS_SMALL"))
    check(s, "the rest of the table's workload", a.one(1),
          "large_runner REAL_SESSIONS_FULL × n_real_per_session", large_real_items())


# --- What the numbers establish --------------------------------------------

def establishes():
    s = "What the numbers establish"
    a = Anchor(r"A1 and A2 score (\d\.\d+) recall on the small, scale and "
               r"long-horizon runs; on the diverse workload A1 reaches (\d\.\d+) "
               r"and A2 stays at (\d\.\d+)")
    for label, arch, source in (("A1 small", "A1", "table"), ("A2 small", "A2", "table"),
                                ("A1 long-horizon", "A1", "long_horizon"),
                                ("A2 long-horizon", "A2", "long_horizon")):
        check(s, f"{label} recall", a.one(0), f"results/{source}.md {arch} R@5",
              num(by_arch(source)[arch]["R@5"]))
    for arch in ("A1", "A2"):
        for row in result_rows("scale"):
            if row["arch"] == arch:
                check(s, f"{arch} scale recall at {row['scale']}", a.one(0),
                      "results/scale.md R@5", num(row["R@5"]))
    check(s, "A1 diverse recall", a.one(1), "results/diverse.md A1 R@5",
          num(by_arch("diverse")["A1"]["R@5"]))
    check(s, "A2 diverse recall", a.one(2), "results/diverse.md A2 R@5",
          num(by_arch("diverse")["A2"]["R@5"]))

    a = Anchor(r"their R@5 stays between (\d\.\d+) and (\d\.\d+) while their store "
               r"is (\d+) to (\d+) times smaller")
    recalls, ratios = [], []
    keeps_everything = {"table": "A1", "scale": "A1", "long_horizon": "A1",
                        "diverse": "A1", "large_scale": "M1"}
    for source in ("long_horizon", "diverse", "large_scale"):
        rows = by_arch(source)
        big = num(rows[keeps_everything[source]]["Items"])
        for arch in ("F5", "G6"):
            recalls.append(num(rows[arch]["R@5"]))
            ratios.append(big / num(rows[arch]["Items"]))
    for row in result_rows("scale"):
        if row["arch"] == "F5" and num(row["scale"]) == 1052:
            recalls.append(num(row["R@5"]))
            big = [r for r in result_rows("scale")
                   if r["arch"] == "A1" and num(r["scale"]) == 1052][0]
            ratios.append(num(big["items"]) / num(row["items"]))
    check(s, "lowest recall", a.one(0), "F5 and G6 rows across the runs", min(recalls))
    check(s, "highest recall", a.one(1), "F5 and G6 rows across the runs", max(recalls))
    check(s, "smallest store ratio", a.one(2),
          "items of F5/G6 against the architectures that keep everything",
          math.floor(min(ratios)), rounded=False)
    check(s, "largest store ratio", a.one(3),
          "items of F5/G6 against the architectures that keep everything",
          math.ceil(max(ratios)), rounded=False)

    unsourced(s, "the implementation is #322; QA pass #7 and #326",
              "issue and pull-request numbers; nothing under results/ or harness/ "
              "records them")


# --- Pairs that already disagree ------------------------------------------
#
# Each entry states what one committed file says, what another says, and a probe
# that recomputes both. Fix either side and the probe stops matching, so the
# disagreement cannot go quiet.

KNOWN = []


# --- Report ----------------------------------------------------------------

def verdict(entry):
    """OK, ROUNDING when the record rounds the other way, MISMATCH otherwise."""
    stated, found = entry["stated"], entry["found"]
    if not entry["rounded"]:
        return "OK" if stated == str(found) else "MISMATCH"
    digits = places(stated)
    if half_up(float(found), digits) == num(stated):
        return "OK"
    if abs(float(found) - num(stated)) <= 0.5 * 10 ** -digits + 1e-9:
        return "ROUNDING"
    return "MISMATCH"


def citing_documents():
    """What the research index says about this record."""
    index = open(os.path.join(ROOT, "..", "README.md")).read()
    return {
        "index_architectures": int(
            Anchor(r"memory-architecture/README\.md\)[^\n]*?(\d+) candidate",
                   index).one()),
        "harness_architectures": len(architecture_ids()),
    }


def main():
    for section in (method, small, scale, long_horizon, diverse, large_scale,
                    establishes):
        section()

    citing = citing_documents()
    failures = []
    by_section = {}
    for entry in CHECKS:
        v = verdict(entry)
        by_section.setdefault(entry["section"], []).append(v)
        if v != "OK":
            failures.append((v, entry))

    print(f"{len(CHECKS)} figures read from README.md and compared "
          f"to committed files\n")
    for section, verdicts in by_section.items():
        ok = sum(1 for v in verdicts if v == "OK")
        print(f"  {ok:>3}/{len(verdicts):<3} {section}")

    print(f"\n{len(UNSOURCED)} claims no committed file carries:\n")
    for u in UNSOURCED:
        print(f"  [{u['section']}] {u['claim']}")
        print(f"      needed: {u['needed']}")

    print(f"\n{len(KNOWN)} recorded disagreements between committed files:\n")
    stale = []
    for k in KNOWN:
        got = k["probe"]()
        agrees = got == k["expect"]
        print(f"  [{k['section']}] {'' if agrees else 'CHANGED '}{k['record']}")
        print(f"      file: {k['file']}")
        if not agrees:
            print(f"      probe now returns {got!r}, recorded {k['expect']!r}")
            stale.append(k)

    print("\nresearch index:\n")
    print(f"  docs/research/README.md counts "
          f"{citing['index_architectures']} architectures")
    if citing["index_architectures"] != citing["harness_architectures"]:
        print(f"  the index disagrees with the harness's "
              f"{citing['harness_architectures']}")
        stale.append("index count")

    if failures:
        print(f"\n{len(failures)} figures do not match their source:\n")
        for v, entry in failures:
            print(f"  {v} [{entry['section']}] {entry['claim']}: stated "
                  f"{entry['stated']}, {entry['source']} holds {entry['found']}")

    if failures or stale:
        return 1
    print("\nevery figure with a committed source matches it")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
