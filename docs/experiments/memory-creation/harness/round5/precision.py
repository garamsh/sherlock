"""What the round-5 net column can separate, at round 5's own probe counts.

`net` is the mean of three family scores, each a mean over its own probes: 100
corrections, 40 controls and 64 coexist probes per seed. A second seed draws a
second set of all three, so a two-seed mean carries twice the probes and its
interval is the one-seed interval over sqrt(2).

Two approximations, both of which widen rather than narrow the true interval,
so the printed figures are a floor on the uncertainty: the coexist probes are
treated as Bernoulli though their per-probe values are fractions, and probes
are treated as independent though they share one store and one decider.

Reads the committed result files only; makes no API call.
"""
from __future__ import annotations
import glob
import json
import math
import os

HERE = os.path.dirname(os.path.abspath(__file__))
RESULTS = os.path.join(HERE, "..", "..", "results_round5")

# Probes behind each family score, per seed.
PROBES = {"latest_wins": 100, "control_survival": 40, "coexist_preserved": 64}
Z = 1.96


def _se(p, n):
    return math.sqrt(max(p * (1 - p), 0.0) / n)


def half_width(scores, seeds=1, coexist_probes=None):
    """95% half-width on `net` for one structure's family scores."""
    probes = dict(PROBES)
    if coexist_probes is not None:
        probes["coexist_preserved"] = coexist_probes
    return Z * math.sqrt(sum(_se(scores[m], probes[m] * seeds) ** 2
                             for m in probes)) / 3


def net(scores):
    return sum(scores.values()) / 3


def load():
    """{system: {seed: family scores}} from the committed round-5 results."""
    rows = {}
    for path in sorted(glob.glob(os.path.join(RESULTS, "*_seed*.json"))):
        with open(path) as f:
            data = json.load(f)
        row = data["row"]
        rows.setdefault(row["system"], {})[data["seed"]] = {
            m: row[m] for m in PROBES}
    return rows


def pooled(per_seed):
    """The family scores averaged over the seeds a structure ran on."""
    return {m: sum(s[m] for s in per_seed.values()) / len(per_seed)
            for m in PROBES}


def resolving_probes(gap, scores, seeds, limit=4000):
    """Coexist probes per seed at which two intervals of this width clear `gap`.

    Returns None when no coexist count reaches it: the corrected and control
    families put a floor under the width that only more of them can lower.
    """
    for n in range(PROBES["coexist_preserved"], limit + 1, 2):
        if 2 * half_width(scores, seeds, coexist_probes=n) <= gap:
            return n
    return None


def main():
    rows = load()
    if not rows:
        raise SystemExit(f"no result files in {RESULTS}")

    print("per seed")
    print(f"{'structure':32} {'seed':>5} {'net':>7} {'95% interval':>18}")
    for system, per_seed in sorted(rows.items()):
        for seed, scores in sorted(per_seed.items()):
            n, half = net(scores), half_width(scores)
            print(f"{system:32} {seed:>5} {n:>7.3f} "
                  f"  [{n - half:.3f}, {n + half:.3f}]")

    print("\npooled over the seeds each structure ran")
    print(f"{'structure':32} {'seeds':>5} {'net':>7} {'95% interval':>18} "
          f"{'half':>7}")
    ranked = []
    for system, per_seed in rows.items():
        scores = pooled(per_seed)
        half = half_width(scores, seeds=len(per_seed))
        ranked.append((net(scores), half, system, len(per_seed), scores))
    ranked.sort(key=lambda r: -r[0])
    for n, half, system, seeds, _ in ranked:
        print(f"{system:32} {seeds:>5} {n:>7.3f} "
              f"  [{n - half:.3f}, {n + half:.3f}] {half:>7.3f}")

    print("\nseparation, leader first")
    lead_net, lead_half, lead_name, lead_seeds, lead_scores = ranked[0]
    for n, half, system, seeds, scores in ranked[1:]:
        gap = lead_net - n
        separated = gap > lead_half + half
        need = None if separated else resolving_probes(
            gap, scores, min(seeds, lead_seeds))
        note = "separated" if separated else (
            f"unresolved; {need} coexist probes per seed would resolve it"
            if need else "unresolved; more corrected probes needed too")
        print(f"{lead_name} - {system}: {gap:.3f} against "
              f"{lead_half + half:.3f} — {note}")


if __name__ == "__main__":
    main()
