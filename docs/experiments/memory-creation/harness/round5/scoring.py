"""Round 4's probe scoring, plus the coexist breakdown per family."""
from __future__ import annotations

from round4 import scoring as r4


def _coexist_by_family(inst, probes):
    """Preserved fraction per coexist family.

    A second pass over the coexist probes only. Recall ranks live rows against a
    cached embedding and changes nothing, so this costs no API call.
    """
    families: dict[str, list[float]] = {}
    for p in probes:
        if p.kind != "coexist":
            continue
        rows = inst.recall(p.question)
        hits = sum(1 for v in p.expect_present
                   if r4.classify(inst, p.subject, v, rows) == "recalled")
        families.setdefault(p.family, []).append(hits / len(p.expect_present))
    return {name: round(sum(xs) / len(xs), 4)
            for name, xs in sorted(families.items())}


def score(inst, probes):
    """Round 4's metrics, with `coexist_by_family` beside `coexist_preserved`."""
    result = r4.score(inst, probes)
    result["coexist_by_family"] = _coexist_by_family(inst, probes)
    return result
