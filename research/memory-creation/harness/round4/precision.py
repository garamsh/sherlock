"""How much of the net column is sampling error.

`net` is the mean of three family scores, each a mean over its own probes:
100 corrections, 40 controls, 12 coexist probes. This prints the 95%
interval that follows from those counts, and which structures are therefore
indistinguishable from each other.

Two approximations, both of which widen rather than narrow the true interval,
so the printed figures are a floor on the uncertainty:

  - The coexist probes are treated as Bernoulli. Their per-probe values are
    fractions (a probe holding three values scores 2/3 when one is lost), so
    their real variance is smaller than p(1-p).
  - Probes are treated as independent. They share one store and one decider,
    so their errors are if anything positively correlated.

Reads the committed result files only; makes no API calls.
"""
from __future__ import annotations
import glob
import json
import math
import os

HERE = os.path.dirname(os.path.abspath(__file__))
RESULTS = os.path.join(HERE, "..", "..", "results_round4")

N_CORRECTED, N_CONTROL, N_COEXIST = 100, 40, 12
Z = 1.96


def _se(p, n):
    return math.sqrt(max(p * (1 - p), 0.0) / n)


def interval(row):
    """Return (net, half-width) for one scored structure."""
    half = Z * math.sqrt(
        _se(row["latest_wins"], N_CORRECTED) ** 2
        + _se(row["control_survival"], N_CONTROL) ** 2
        + _se(row["coexist_preserved"], N_COEXIST) ** 2
    ) / 3
    return row["net"], half


def load_rows():
    rows = {}
    for path in sorted(glob.glob(os.path.join(RESULTS, "B*_seed7.json"))):
        row = json.load(open(path))["row"]
        rows[row["system"]] = row
    # B1-B7 were scored by the sweep runner rather than one file each.
    sweep = json.load(open(os.path.join(RESULTS, "falsification_seed7.json")))
    for row in sweep.get("rows", []):
        if "net" in row:
            rows.setdefault(row["system"], row)
    return rows


def budget():
    """Half-width on net at other probe counts, holding the scores fixed.

    Answers which family is worth enlarging. The scores used are the middle of
    the observed range, so the comparison is between designs, not structures.
    """
    out = []
    for n_corrected, n_coexist in ((100, 12), (400, 12), (100, 60), (100, 200)):
        half = Z * math.sqrt(
            _se(0.80, n_corrected) ** 2
            + _se(0.95, N_CONTROL) ** 2
            + _se(0.80, n_coexist) ** 2
        ) / 3
        out.append((n_corrected, n_coexist, half))
    return out


def main():
    rows = load_rows()
    scored = sorted(((interval(r), name) for name, r in rows.items()),
                    key=lambda x: -x[0][0])

    print("structure                        net    95% interval")
    for (net, half), name in scored:
        print(f"{name:32} {net:.3f}  [{net - half:.3f}, {net + half:.3f}]")

    lead = dict(rows)["B9-decider-neighbors-wide"]
    lo, hi = interval(lead)[0] - interval(lead)[1], interval(lead)[0] + interval(lead)[1]
    overlapping = [name for (net, half), name in scored
                   if net + half >= lo and net - half <= hi]
    print(f"\noverlapping the adopted structure's interval "
          f"[{lo:.3f}, {hi:.3f}]: {len(overlapping)} of {len(scored)}")
    for name in overlapping:
        print(f"  {name}")

    print("\nwhere the width comes from")
    print("corrected  coexist  95% half-width on net")
    for n_corrected, n_coexist, half in budget():
        print(f"{n_corrected:9d}  {n_coexist:7d}  +/-{half:.3f}")


if __name__ == "__main__":
    main()
