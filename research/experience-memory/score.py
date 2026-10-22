"""Score the #1153 runs: per-family probes, controls, guardrails and the verdict.

    python3 research/experience-memory/score.py <run dir> [corpus file]

A run directory holds one results.jsonl per arm under S/, N+/, P/ and C/,
each line one recorded turn. The rule the verdict applies is the
preregistration's.
"""

import json
import math
import re
import sys
from collections import defaultdict
from pathlib import Path

HERE = Path(__file__).resolve().parent
ARMS = ["S", "N+", "P", "C"]
CANDIDATES = ["N+", "P", "C"]
PRIMARY = "primary"
GUARD = ("control", "reversal")
ALPHA = 0.05

# The guardrail margin, in probes: an arm that does this many fewer
# control or reversal probes than the shipped structure fails the guardrail.
GUARD_MARGIN = 1

# The decision rule's production line counts, measured by the
# preregistration: the arms' diffs against the shipped structure.
PRODUCTION_LINES = {"N+": 0, "P": 0, "C": 0}

WORD = re.compile(r"[A-Za-z0-9_]+")

# Every check kind the scorer knows, so the corpus test can name them.
KINDS = {
    "prefix", "suffix", "all_lowercase", "all_uppercase", "roman_list",
    "no_bullets", "tab_indent", "space_indent", "kebab_files", "snake_files",
    "order_words", "tool_before", "contains", "all_of", "any_of", "not",
}


def normalize(text):
    return text.replace("—", "-").replace("’", "'").lower()


def words(text):
    return WORD.findall(normalize(text))


def code_lines(text):
    """The code a reply carries: the fenced blocks of its visible output."""
    out = []
    for block in re.findall(r"```[^\n]*\n(.*?)```", text, re.S):
        out += block.split("\n")
    return out


def indented(lines):
    return [ln for ln in lines if ln[:1] in (" ", "\t") and ln.strip()]


def tool_names(turn):
    return [t.get("name", "") for t in turn.get("tools") or []]


def visible(turn):
    """What the person reads: the reply, and what the agent sent."""
    parts = [turn.get("reply") or ""]
    parts += list(turn.get("sent") or [])
    return "\n".join(parts)


def word_index(found, stem):
    """The position of the first word that carries the stem, or -1.

    A rule names a word the reply carries in a longer form: "recommend"
    in "recommendation". The stem matches the word it starts.
    """
    for i, word in enumerate(found):
        if word.startswith(stem):
            return i
    return -1


def check(spec, turn):
    """Whether one probe's check holds for one recorded turn."""
    kind = spec["kind"]
    text = visible(turn)
    if kind == "prefix":
        return normalize(text).lstrip().startswith(normalize(spec["value"]))
    if kind == "suffix":
        return normalize(text).rstrip().endswith(normalize(spec["value"]))
    if kind == "all_lowercase":
        letters = [c for c in text if c.isalpha()]
        return bool(letters) and all(c.islower() for c in letters)
    if kind == "all_uppercase":
        letters = [c for c in text if c.isalpha()]
        return bool(letters) and all(c.isupper() for c in letters)
    if kind == "roman_list":
        return bool(re.search(r"(?m)^\s*I[.)]\s", text)) and bool(re.search(r"(?m)^\s*II[.)]\s", text))
    if kind == "no_bullets":
        return not re.search(r"(?m)^\s*[-*•]\s", text)
    if kind == "tab_indent":
        lines = indented(code_lines(text))
        return bool(lines) and all(ln.startswith("\t") for ln in lines)
    if kind == "space_indent":
        lines = indented(code_lines(text))
        return bool(lines) and all(ln.startswith(" ") for ln in lines)
    if kind == "kebab_files":
        names = [n for n in (turn.get("written") or {})]
        return bool(names) and all(re.fullmatch(r"[a-z0-9]+(-[a-z0-9]+)*\.[a-z0-9]+", n) for n in names)
    if kind == "snake_files":
        names = [n for n in (turn.get("written") or {})]
        return bool(names) and all(re.fullmatch(r"[a-z0-9]+(_[a-z0-9]+)*\.[a-z0-9]+", n) for n in names)
    if kind == "order_words":
        found = words(text)
        first, second = word_index(found, spec["first"]), word_index(found, spec["second"])
        return first >= 0 and second >= 0 and first < second
    if kind == "tool_before":
        names = tool_names(turn)
        if spec["first"] not in names:
            return False
        return spec["second"] not in names or names.index(spec["first"]) < names.index(spec["second"])
    if kind == "contains":
        return spec["value"] in text
    if kind == "all_of":
        return all(check(s, turn) for s in spec["of"])
    if kind == "any_of":
        return any(check(s, turn) for s in spec["of"])
    if kind == "not":
        return not check(spec["of"], turn)
    raise ValueError(f"unknown check {kind}")


# ---- statistics -------------------------------------------------------------

def wilson(k, n, z=1.959964):
    if n == 0:
        return [None, None]
    p = k / n
    d = 1 + z * z / n
    c = (p + z * z / (2 * n)) / d
    h = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / d
    return [round(c - h, 4), round(c + h, 4)]


def binom_two_sided(k, n):
    """Exact two-sided binomial test of k successes in n at p = 1/2."""
    if n == 0:
        return 1.0
    pmf = [math.comb(n, i) / 2 ** n for i in range(n + 1)]
    return min(1.0, sum(p for p in pmf if p <= pmf[k] * (1 + 1e-12)))


def mcnemar(candidate, shipped):
    """The paired discordant counts and their exact two-sided p."""
    b = sum(1 for c, s in zip(candidate, shipped) if c and not s)
    c = sum(1 for x, s in zip(candidate, shipped) if s and not x)
    return b, c, binom_two_sided(b, b + c)


def holm(pvalues):
    """Holm-adjusted p-values, in the order given."""
    order = sorted(range(len(pvalues)), key=lambda i: pvalues[i])
    adjusted, running = [0.0] * len(pvalues), 0.0
    for rank, i in enumerate(order):
        running = max(running, min(1.0, (len(pvalues) - rank) * pvalues[i]))
        adjusted[i] = running
    return adjusted


# ---- scoring ----------------------------------------------------------------

def load(run_dir, corpus_path):
    with open(corpus_path) as f:
        corpus = json.load(f)
    results = {}
    for arm in ARMS:
        path = Path(run_dir) / arm / "results.jsonl"
        turns = {}
        if path.exists():
            with open(path) as f:
                for line in f:
                    if not line.strip():
                        continue
                    record = json.loads(line)
                    turns.setdefault((record["family"], record["instance"]), record)
        results[arm] = turns
    return corpus, results


def score(run_dir, corpus_path):
    corpus, results = load(run_dir, corpus_path)
    families = {f["id"]: f for f in corpus["families"]}

    # Every probe the corpus declares, in family order.
    probes = []
    for f in corpus["families"]:
        for step in f["steps"]:
            if step.get("probe"):
                probes.append((f["id"], f["family"], step["instance"], step["probe"]["check"]))

    # Which probes each arm answered, and whether the check held.
    rows = {arm: {} for arm in ARMS}
    for arm in ARMS:
        for probe in probes:
            key = (probe[0], probe[2])
            record = results[arm].get(key)
            if record is None:
                continue
            held = bool(record.get("ok")) if "ok" in record else check(probe[3], record)
            rows[arm][key] = held

    per_family = {}
    for f in corpus["families"]:
        entry = {"kind": f["family"], "rule": f.get("rule"), "arms": {}}
        for arm in ARMS:
            keys = [(f["id"], s["instance"]) for s in f["steps"] if s.get("probe")]
            answered = [k for k in keys if k in rows[arm]]
            held = sum(1 for k in answered if rows[arm][k])
            entry["arms"][arm] = {"answered": len(answered), "held": held, "of": len(keys)}
        per_family[f["id"]] = entry

    # The primary endpoint: the probes of the primary families, paired by
    # family and instance, over the arms and the shipped structure.
    primary_keys = [(fid, inst) for fid, kind, inst, _ in probes if kind == PRIMARY]
    tests = {}
    for arm in CANDIDATES:
        pairs = [(fid, inst) for fid, inst in primary_keys
                 if (fid, inst) in rows[arm] and (fid, inst) in rows["S"]]
        cand = [rows[arm][k] for k in pairs]
        ship = [rows["S"][k] for k in pairs]
        b, c, p = mcnemar(cand, ship)
        tests[arm] = {"n": len(pairs), "candidate_only": b, "shipped_only": c,
                      "difference": round((b - c) / len(pairs), 4) if pairs else None,
                      "p": p}
    for arm, adjusted in zip(CANDIDATES, holm([tests[a]["p"] for a in CANDIDATES])):
        tests[arm]["p_holm"] = adjusted

    # The guardrails: the control and reversal families, each arm against
    # the shipped structure by a stated margin.
    guardrails = {}
    for arm in CANDIDATES:
        detail = {}
        for kind in GUARD:
            keys = [(fid, inst) for fid, k, inst, _ in probes if k == kind]
            shared = [k for k in keys if k in rows[arm] and k in rows["S"]]
            arm_held = sum(1 for k in shared if rows[arm][k])
            ship_held = sum(1 for k in shared if rows["S"][k])
            detail[kind] = {"n": len(shared), "arm": arm_held, "shipped": ship_held,
                             "difference": arm_held - ship_held}
        hold = all(d["difference"] >= -GUARD_MARGIN for d in detail.values())
        guardrails[arm] = {"detail": detail, "hold": hold}

    # The per-family applied counts, the way the report reads them.
    applied = {}
    for arm in ARMS:
        applied[arm] = {}
        for fid, entry in per_family.items():
            applied[arm][fid] = entry["arms"][arm]["held"]

    verdict = decide(tests, guardrails)
    out = {"families": per_family, "tests": tests, "guardrails": guardrails,
           "applied": applied, "verdict": verdict}
    return out


def decide(tests, guardrails):
    """The preregistration's decision rule.

    An arm with no paired probe has no difference and cannot be valid, and
    an arm whose guardrail did not hold cannot be valid either.
    """
    valid = [a for a in CANDIDATES
             if a in tests and tests[a].get("difference") is not None and tests[a]["difference"] > 0
             and tests[a].get("p_holm", 1.0) < ALPHA and guardrails.get(a, {}).get("hold", False)]
    if not valid:
        return {"outcome": "shipped structure stands", "valid": []}
    # The smallest production diff wins; a larger arm is taken only if it
    # beats the smaller one directly.
    valid.sort(key=lambda a: (PRODUCTION_LINES.get(a, 0), a))
    winner = valid[0]
    return {"outcome": f"take {winner}", "valid": valid, "winner": winner}


def main():
    run_dir = Path(sys.argv[1])
    corpus_path = Path(sys.argv[2]) if len(sys.argv) > 2 else HERE / "corpus.json"
    out = score(run_dir, corpus_path)
    (run_dir / "scores.json").write_text(json.dumps(out, indent=1, sort_keys=True) + "\n")
    for kind in (PRIMARY,) + GUARD:
        ids = [fid for fid, f in out["families"].items() if f["kind"] == kind]
        print(kind, {arm: f"{sum(out['applied'][arm][i] for i in ids)}/{len(ids)}" for arm in ARMS})
    print(json.dumps(out["tests"]))
    print(json.dumps(out["verdict"]))


if __name__ == "__main__":
    main()
