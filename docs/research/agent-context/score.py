"""Score the #862 runs: checks, intervals, paired tests, controls and verdict.

    python3 docs/research/agent-context/score.py <run dir> [episodes file]

A run directory holds ledger/calls.jsonl and one results.jsonl per arm
under S/, A/ and B/. The scores are written to <run dir>/scores.json; the
rule they apply is preregistration.md's.
"""

import json
import math
import re
import sys
from collections import defaultdict
from pathlib import Path

HERE = Path(__file__).resolve().parent
ARMS = ["S", "A", "B"]
CANDIDATES = ["A", "B"]
PRIMARY = ["F", "G", "I", "K"]
GUARD = ["E", "N", "R"]
ALPHA = 0.05
GUARD_PRIMARY_LOSS = 7
GUARD_CONTROL_LOSS = 3

MONTHS = "january february march april may june july august september october november december".split()
STOP = {"with", "the", "a", "an", "of", "and"}


# ---- visible output ---------------------------------------------------------

def probe(result):
    """The probe turn: the episode's last live turn."""
    return result["turns"][-1] if result.get("turns") else {}


def primary_text(turn):
    """What the person reads: the messages sent, or the reply when none was."""
    sent = [s for s in turn.get("sent") or [] if s.strip()]
    return "\n".join(sent) if sent else (turn.get("reply") or "")


def all_text(turn):
    parts = [turn.get("reply") or ""] + list(turn.get("sent") or []) + list((turn.get("written") or {}).values())
    return "\n".join(parts)


# ---- value matching ---------------------------------------------------------

def normalize(text):
    text = re.sub(r"(?<=\d),(?=\d{3})", "", text.lower())
    return text.replace("€", " ").replace("$", " ")


def value_tokens(value):
    """The tokens an answer must hold to state a value: every token with a
    digit, every capitalized word, and, for a value with neither, every word
    but connectives."""
    raw = re.findall(r"[A-Za-z0-9]+(?::[0-9]+)?", re.sub(r"(?<=\d),(?=\d{3})", "", value))
    marked = [t for t in raw if re.search(r"\d", t) or t[0].isupper() or t.lower() in MONTHS]
    tokens = marked or [t for t in raw if t.lower() not in STOP]
    return [t.lower() for t in tokens]


def states(text, value):
    norm = normalize(text)
    return all(re.search(r"(?<![a-z0-9])" + re.escape(t) + r"(?![a-z0-9])", norm) for t in value_tokens(value))


# ---- checks -----------------------------------------------------------------

def code_lines(turn):
    lines = []
    sources = [turn.get("reply") or ""] + list(turn.get("sent") or [])
    for text in sources:
        for block in re.findall(r"```[^\n]*\n(.*?)```", text, re.S):
            lines += block.split("\n")
    for content in (turn.get("written") or {}).values():
        lines += content.split("\n")
    return lines


def check(spec, turn):
    """Whether the probe turn applied the evidence, by the item's rule."""
    kind = spec["kind"]
    text = primary_text(turn)
    if kind == "contains":
        return states(all_text(turn), spec["value"])
    if kind == "correction":
        full = all_text(turn)
        return states(full, spec["new"]) and not states(full, spec["old"])
    if kind == "roman_list":
        return bool(re.search(r"(?m)^\W*I[.)]\s", text)) and bool(re.search(r"(?m)^\W*II[.)]\s", text))
    if kind == "celsius":
        return bool(re.search(r"°\s?C\b|celsius|℃", text, re.I)) and not re.search(r"°\s?F\b|fahrenheit|℉", text, re.I)
    if kind == "iso_date":
        return bool(re.search(r"\b\d{4}-\d{2}-\d{2}\b", text)) and not re.search(spec["forbid"], text, re.I)
    if kind == "lowercase":
        return bool(re.search(r"[a-z]", text)) and not re.search(r"[A-Z]", text)
    if kind == "tab_indent":
        indented = [ln for ln in code_lines(turn) if ln[:1] in (" ", "\t") and ln.strip()]
        return bool(indented) and all(ln.startswith("\t") for ln in indented)
    if kind == "starts_with":
        return text.strip().lower().startswith(spec["text"])
    if kind == "ends_with":
        return bool(re.search(re.escape(spec["text"]) + r"\W*$", text.strip().lower()))
    if kind == "euro":
        return bool(re.search(r"€\s?\d", text)) and "$" not in text
    if kind == "clock24":
        return bool(re.search(r"\b\d{1,2}:\d{2}\b", text)) and not re.search(r"\b(am|pm|a\.m\.|p\.m\.)(?![a-z])", text, re.I)
    if kind == "abstain":
        return bool(re.search(spec["pattern"], text, re.I))
    if kind == "rsvp":
        return rsvp(spec, turn)
    raise ValueError(f"unknown check {kind}")


def rsvp(spec, turn):
    written = {k.lower(): v for k, v in (turn.get("written") or {}).items()}
    names = spec["names"]

    def lists_all(content):
        return all(re.search(r"\b" + re.escape(n) + r"\b", content) for n in names)

    first, second = spec["file"].lower(), spec["changed_file"].lower()
    if spec["case"] == "continue":
        return first in written and lists_all(written[first])
    if spec["case"] == "changed":
        return second in written and lists_all(written[second]) and first not in written
    # cancelled: nothing listing the names is written
    return not any(sum(bool(re.search(r"\b" + re.escape(n) + r"\b", c)) for n in names) >= 2 for c in written.values())


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


def power(n, psi, delta, alpha):
    """Power of the exact two-sided McNemar test at level alpha to detect a
    paired gain delta with discordance psi over n pairs."""
    if psi <= 0 or delta <= 0 or delta >= psi:
        return 0.0
    q = (psi + delta) / 2 / psi
    total = 0.0
    for d in range(n + 1):
        pd = math.comb(n, d) * psi ** d * (1 - psi) ** (n - d)
        if pd < 1e-12:
            continue
        for b in range(d + 1):
            if b * 2 > d and binom_two_sided(b, d) < alpha:
                total += pd * math.comb(d, b) * q ** b * (1 - q) ** (d - b)
    return total


# ---- scoring ----------------------------------------------------------------

def load(run_dir, episodes_path):
    corpus = json.load(open(episodes_path))
    episodes = {e["id"]: e for e in corpus["episodes"]}
    results = {}
    for arm in ARMS:
        path = Path(run_dir) / arm / "results.jsonl"
        results[arm] = {}
        if path.exists():
            for line in open(path):
                r = json.loads(line)
                results[arm][r["episode"]] = r
    calls = []
    ledger = Path(run_dir) / "ledger" / "calls.jsonl"
    if ledger.exists():
        calls = [json.loads(line) for line in open(ledger)]
    return episodes, results, calls


def score_episode(e, r):
    t = probe(r)
    failed = bool(r.get("error")) or bool(t.get("error"))
    applied = False if failed else check(e["check"], t)
    tools = [u["name"] for u in t.get("tools") or []]
    row = {"applied": applied, "failed": failed, "reached": r.get("reached", False), "inspected": r.get("inspected", False),
           "oracle": r.get("oracle"), "searched": "memory_search" in tools}
    if e["family"] == "K":
        before = r["turns"][-2].get("note_after") if len(r.get("turns", [])) >= 2 else None
        if before is not None:
            row["note_obsolete"] = states(before, e["check"]["old"]) and not states(before, e["check"]["new"])
            row["note_current"] = states(before, e["check"]["new"])
    if e["family"] == "I" and r.get("turns"):
        rule_note = r["turns"][0].get("note_after")
        if rule_note is not None:
            row["note_after_rule"] = bool(rule_note.strip())
    return row


def score(run_dir, episodes_path):
    episodes, results, calls = load(run_dir, episodes_path)
    ids = [i for i in episodes if all(i in results[a] for a in ARMS)]
    rows = {a: {i: score_episode(episodes[i], results[a][i]) for i in ids} for a in ARMS}
    fam = lambda f: [i for i in ids if episodes[i]["family"] == f]

    families = {}
    for f in PRIMARY + GUARD:
        ep = fam(f)
        families[f] = {}
        for a in ARMS:
            k = sum(rows[a][i]["applied"] for i in ep)
            entry = {"n": len(ep), "applied": k, "applied_ci": wilson(k, len(ep)),
                     "reached": sum(rows[a][i]["reached"] for i in ep), "inspected": sum(rows[a][i]["inspected"] for i in ep),
                     "failed": sum(rows[a][i]["failed"] for i in ep)}
            if f == "N":
                entry["abstained"] = k
            if f == "R":
                for case in ("continue", "changed", "canceled"):
                    sub = [i for i in ep if episodes[i]["case"] == case]
                    entry[case] = sum(rows[a][i]["applied"] for i in sub)
            if f == "K":
                entry["note_obsolete"] = sum(rows[a][i].get("note_obsolete", False) for i in ep)
                entry["note_current"] = sum(rows[a][i].get("note_current", False) for i in ep)
            families[f][a] = entry

    primary_ids = [i for f in PRIMARY for i in fam(f)]
    tests = {}
    for c in CANDIDATES:
        cand = [rows[c][i]["applied"] for i in primary_ids]
        ship = [rows["S"][i]["applied"] for i in primary_ids]
        b, d, p = mcnemar(cand, ship)
        n = len(primary_ids)
        tests[c] = {"n": n, "candidate_only": b, "shipped_only": d, "difference": round((b - d) / n, 4) if n else None,
                    "discordance": round((b + d) / n, 4) if n else None, "p": p}
    for c, adj in zip(CANDIDATES, holm([tests[c]["p"] for c in CANDIDATES])):
        tests[c]["p_holm"] = adj

    guardrails = {}
    for c in CANDIDATES:
        losses = {f: families[f]["S"]["applied"] - families[f][c]["applied"] for f in PRIMARY}
        control = {f: families[f]["S"]["applied"] - families[f][c]["applied"] for f in ("E", "N")}
        canceled = [i for i in fam("R") if episodes[i]["case"] == "canceled"]
        resumed = [i for i in canceled if rows["S"][i]["applied"] and not rows[c][i]["applied"]]
        guardrails[c] = {"primary_losses": losses, "control_losses": control, "canceled_resumed_where_S_did_not": resumed,
                         "hold": all(v <= GUARD_PRIMARY_LOSS for v in losses.values()) and all(v <= GUARD_CONTROL_LOSS for v in control.values()) and not resumed}

    controls = compute_controls(episodes, results, rows, ids, fam)
    costs = cost_summary(calls, episodes, ids)
    b, d, p = mcnemar([rows["B"][i]["applied"] for i in primary_ids], [rows["A"][i]["applied"] for i in primary_ids])
    tests["B_over_A"] = {"B_only": b, "A_only": d, "p": p}
    verdict = decide(tests, guardrails, controls)
    return {"episodes": len(ids), "families": families, "tests": tests, "guardrails": guardrails,
            "controls": controls, "cost": costs, "verdict": verdict,
            "per_episode": {a: rows[a] for a in ARMS}}


def compute_controls(episodes, results, rows, ids, fam):
    primary_ids = [i for f in PRIMARY for i in fam(f)]
    out = {}
    for a in ARMS:
        reach = sum(bool(rows[a][i]["oracle"]) for i in primary_ids)
        out[f"oracle_reach_{a}"] = {"value": reach, "of": len(primary_ids), "required": math.ceil(0.95 * len(primary_ids)), "pass": reach >= math.ceil(0.95 * len(primary_ids))}
    g = fam("G")
    searched = sum(rows["A"][i]["searched"] for i in g)
    out["search_rate_A_in_G"] = {"value": searched, "of": len(g), "required": math.ceil(0.20 * len(g)), "pass": searched >= math.ceil(0.20 * len(g))}
    rule_eps = fam("I")
    rules = defaultdict(list)
    for i in rule_eps:
        rules[episodes[i].get("rule", i)].append(i)
    for a in CANDIDATES:
        nonempty = sum(rows[a][i].get("note_after_rule", False) for i in rule_eps)
        out[f"note_after_rule_{a}"] = {"value": nonempty, "of": len(rule_eps), "required": math.ceil(0.5 * len(rule_eps)), "pass": nonempty >= math.ceil(0.5 * len(rule_eps))}
    understood = called = 0
    for i in ids:
        for t in results["B"][i].get("turns") or []:
            if t.get("understood") is not None:
                understood += 1
                called += bool(t["understood"].get("called"))
    out["understand_call_rate_B"] = {"value": called, "of": understood, "required": math.ceil(0.9 * understood), "pass": understood > 0 and called >= math.ceil(0.9 * understood)}
    return out


def cost_summary(calls, episodes, ids):
    by_arm = defaultdict(lambda: {"calls": 0, "cached": 0, "prompt_tokens": 0, "completion_tokens": 0, "usd": 0.0, "live_turns": 0})
    for c in calls:
        a = by_arm[c["arm"]]
        a["calls"] += 1
        a["cached"] += bool(c.get("cached"))
        a["prompt_tokens"] += c.get("prompt_tokens", 0)
        a["completion_tokens"] += c.get("completion_tokens", 0)
        a["usd"] += c.get("usd", 0.0)
    live = sum(1 for i in ids for s in episodes[i]["steps"] if s["kind"] == "live")
    for arm, a in by_arm.items():
        a["usd"] = round(a["usd"], 4)
        a["live_turns"] = live
        a["usd_per_live_turn"] = round(a["usd"] / live, 5) if live else None
    per_family = defaultdict(lambda: defaultdict(float))
    for c in calls:
        e = episodes.get(c["episode"])
        if e:
            per_family[e["family"]][c["arm"]] += c.get("usd", 0.0)
    return {"by_arm": dict(by_arm), "total_usd": round(sum(c.get("usd", 0.0) for c in calls), 4),
            "per_family_usd": {f: {a: round(v, 4) for a, v in d.items()} for f, d in per_family.items()}}


def decide(tests, guardrails, controls):
    failed = [k for k, v in controls.items() if not v["pass"]]
    if failed:
        return {"outcome": "undetermined", "failed_controls": failed}
    valid = [c for c in CANDIDATES if tests[c]["difference"] and tests[c]["difference"] > 0 and tests[c]["p_holm"] < ALPHA and guardrails[c]["hold"]]
    if not valid:
        return {"outcome": "shipped structure stands", "valid": []}
    # A has the smaller production diff; B is taken over it only by beating it.
    winner = valid[0]
    if valid == ["A", "B"]:
        ba = tests["B_over_A"]
        winner = "B" if ba["B_only"] > ba["A_only"] and ba["p"] < ALPHA else "A"
    return {"outcome": f"valid: {', '.join(valid)}; take {winner}", "valid": valid, "winner": winner}


def main():
    run_dir = Path(sys.argv[1])
    episodes = Path(sys.argv[2]) if len(sys.argv) > 2 else HERE / "episodes.json"
    out = score(run_dir, episodes)
    (run_dir / "scores.json").write_text(json.dumps(out, indent=1, sort_keys=True) + "\n")
    for f, arms in out["families"].items():
        print(f, {a: f"{v['applied']}/{v['n']}" for a, v in arms.items()})
    print(json.dumps(out["tests"]), json.dumps(out["verdict"]))


if __name__ == "__main__":
    main()
