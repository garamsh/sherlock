"""Probe scoring. Separates never-written, retired and not-ranked misses."""
from __future__ import annotations


def _rows_containing(inst, subject, value):
    subj, needle = subject.lower(), value.lower()
    return [r for r in inst.store.rows
            if needle in r.content.lower() and subj in r.content.lower()]


def _in_recall(rows, subject, value):
    """True when a recalled row carries both the subject and the value."""
    subj, needle = subject.lower(), value.lower()
    return any(needle in r.content.lower() and subj in r.content.lower()
               for r in rows)


def classify(inst, subject, value, recalled):
    if _in_recall(recalled, subject, value):
        return "recalled"
    rows = _rows_containing(inst, subject, value)
    if not rows:
        return "never_written"
    if all(r.dead for r in rows):
        return "retired"
    return "not_ranked"


def score(inst, probes):
    fam = {"corrected": [], "control": [], "coexist": []}
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
            fam["coexist"].append(len(hits) / len(p.expect_present))

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
        "outcomes": outcomes,
    }
