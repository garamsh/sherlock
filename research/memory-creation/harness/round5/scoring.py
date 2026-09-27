"""Probe scoring on a word-boundary match, plus the coexist breakdown per family.

Round 4 credits a value wherever it appears in a row, so "standardized on
Python" credits `Zed` and "allergic to shellfish" credits `fish`. The metrics
below are round 4's and are computed the same way; what changed is the match.
Round 4's `scoring.py` keeps its own rule, because its committed results were
measured under it.
"""
from __future__ import annotations
import re
from functools import lru_cache


@lru_cache(maxsize=None)
def _pattern(phrase):
    """Match `phrase` where no letter or digit runs into it.

    Each edge is guarded only where the phrase itself begins or ends
    alphanumeric, so a value carrying punctuation there — `Fly.io`, `Next.js` —
    still matches, which a plain `\\b` on both edges would refuse.
    """
    left = r"(?<![0-9a-z])" if phrase[:1].isalnum() else ""
    right = r"(?![0-9a-z])" if phrase[-1:].isalnum() else ""
    return re.compile(left + re.escape(phrase) + right)


def states(text, phrase):
    """True when `text` states `phrase` as a whole word, ignoring case."""
    return _pattern(phrase.lower()).search(text.lower()) is not None


def _rows_containing(inst, subject, value):
    return [r for r in inst.store.rows
            if states(r.content, value) and states(r.content, subject)]


def _in_recall(rows, subject, value):
    """True when a recalled row states both the subject and the value."""
    return any(states(r.content, value) and states(r.content, subject)
               for r in rows)


def classify(inst, subject, value, recalled):
    """Why a value is missing from a recall: never written, retired, not ranked."""
    if _in_recall(recalled, subject, value):
        return "recalled"
    rows = _rows_containing(inst, subject, value)
    if not rows:
        return "never_written"
    if all(r.dead for r in rows):
        return "retired"
    return "not_ranked"


def score(inst, probes):
    """One structure's family scores, miss reasons and coexist breakdown."""
    fam = {"corrected": [], "control": [], "coexist": []}
    by_family: dict[str, list[float]] = {}
    stale, outcomes = [], {}
    for p in probes:
        rows = inst.recall(p.question)
        hits = [v for v in p.expect_present if _in_recall(rows, p.subject, v)]
        clean = not any(_in_recall(rows, p.subject, v) for v in p.expect_absent)

        for v in p.expect_present:
            key = f"{p.kind}:{classify(inst, p.subject, v, rows)}"
            outcomes[key] = outcomes.get(key, 0) + 1

        if p.kind == "corrected":
            fam["corrected"].append(1.0 if (hits and clean) else 0.0)
            stale.append(0.0 if clean else 1.0)
        elif p.kind == "control":
            fam["control"].append(1.0 if hits else 0.0)
        else:
            preserved = len(hits) / len(p.expect_present)
            fam["coexist"].append(preserved)
            by_family.setdefault(p.family, []).append(preserved)

    def mean(xs):
        return round(sum(xs) / len(xs), 4) if xs else 0.0

    latest, control, coexist = mean(fam["corrected"]), mean(fam["control"]), mean(fam["coexist"])

    def rate(kind, outcome, total):
        return round(outcomes.get(f"{kind}:{outcome}", 0) / total, 4) if total else 0.0

    n_control = sum(1 for p in probes if p.kind == "control")
    n_coexist = sum(len(p.expect_present) for p in probes if p.kind == "coexist")

    return {
        "latest_wins": latest,
        "control_survival": control,
        "coexist_preserved": coexist,
        "stale_present": mean(stale),
        "net": round((latest + control + coexist) / 3, 4),
        # Why the misses happened, per probe family.
        "control_never_written": rate("control", "never_written", n_control),
        "control_retired": rate("control", "retired", n_control),
        "control_not_ranked": rate("control", "not_ranked", n_control),
        "coexist_never_written": rate("coexist", "never_written", n_coexist),
        "coexist_retired": rate("coexist", "retired", n_coexist),
        "coexist_not_ranked": rate("coexist", "not_ranked", n_coexist),
        "coexist_by_family": {name: mean(xs) for name, xs in sorted(by_family.items())},
        "outcomes": outcomes,
    }
