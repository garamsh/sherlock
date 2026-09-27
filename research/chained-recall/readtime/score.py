"""Score the #888 read-time variants run with #885's scorer
(../confirm/score.py), which calls #874's (../score.py).

    python3 docs/research/chained-recall/readtime/score.py calibrate   # evidence/calibration/gate.json, from #885's offline records
    python3 docs/research/chained-recall/readtime/score.py power       # evidence/power.json, from #885's live records
    python3 docs/research/chained-recall/readtime/score.py offline     # evidence/offline/scores.json
    python3 docs/research/chained-recall/readtime/score.py live        # evidence/live/scores.json
    python3 docs/research/chained-recall/readtime/score.py tokens      # evidence/tokens.json

Hops, chains, answers, abstentions and the McNemar test are #885's and
#874's. This file adds what preregistration.md fixes for #888: four arms
against S with Holm across them, L+V's inspect-before-stating check, L-T's
call control, L+G's gate control and threshold, and the ranking that names
the best read-time variant when none is valid.
"""

import importlib.util
import json
import re
import sys
from collections import defaultdict
from pathlib import Path

HERE = Path(__file__).resolve().parent


def _confirm_score():
    spec = importlib.util.spec_from_file_location("chained_recall_confirm_score", HERE.parent / "confirm" / "score.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


C = _confirm_score()
P = C.P
states, load, load_jsonl, holm, mcnemar, wilson = P.states, C.load, P.load_jsonl, P.holm, P.mcnemar, P.wilson

PRIMARY, BASELINE, ALLOWED_LOSS, ALPHA = C.PRIMARY, C.BASELINE, C.ALLOWED_LOSS, C.ALPHA
L, G, T, V = "L/b4-model", "L/b4-model-g", "L/b4-model-t", "L/b4-model-v"
ARMS = (L, G, T, V)
NAMES = {BASELINE: "S", L: "L", G: "L+G", T: "L-T", V: "L+V"}
# Net production lines of each arm, before tests (preregistration.md).
NET_LINES = {L: 270, G: 284, T: 265, V: 272}
# L-T's call must be made in at least this share of primary probes.
CALL_SHARE = 0.20
ASSOCIATE = "memory_associate"

_seen = P.seen


def seen(result):
    """What the turn had in view from memory, as ../score.py reads it, with
    L-T's memory_associate results added."""
    return _seen(result) + [t["result"] for t in result.get("tools") or [] if t["name"] == ASSOCIATE]


P.seen = seen

# #885's scorer reads and writes beside this file.
C.HERE, C.OFFLINE, C.LIVE = HERE, HERE / "evidence" / "offline", HERE / "evidence" / "live"
C.ARMS, C.NET_LINES = ARMS, NET_LINES


# ---- L+G's threshold ---------------------------------------------------------

def gate_candidates(probes, corpus):
    """For every probe of #885's L+G records, the walk mids ungated L
    delivers that name none of the query's entities, labelled by what they
    are: a later hop of a primary chain ("hop"), the associated episode of
    an absent query ("near_miss"), or anything else ("other")."""
    chains = {c["id"]: c for c in corpus["chains"]}
    absent = {g["id"]: g for g in corpus["guards"] if g["family"] == "N"}
    out = []
    for p in probes:
        for w in p["trace"].get("gate", []):
            if w["names_seed"]:
                continue
            first, last = w["first"], w["last"]
            kind = "other"
            if p["probe"] in chains and chains[p["probe"]]["family"] in PRIMARY:
                if any(first <= h["source"] <= last for h in chains[p["probe"]]["hops"][1:]):
                    kind = "hop"
            elif p["probe"] in absent and first <= absent[p["probe"]]["sources"][1] <= last:
                kind = "near_miss"
            out.append({"probe": p["probe"], "id": w["id"], "score": w["score"], "kind": kind})
    return out


def choose_threshold(candidates):
    """The threshold that keeps the most later hops of primary chains while
    keeping the associated episodes of at most as many absent queries as
    the N guardrail allows to be lost; among those, the fewest associated
    episodes, then the smallest threshold, so the gate drops no more than it
    must. The thresholds tried are the candidates' own scores and infinity,
    which is a gate on the query's entities alone."""
    allowed = ALLOWED_LOSS["N"]
    best = None
    for t in sorted({c["score"] for c in candidates}) + [float("inf")]:
        hops = sum(c["kind"] == "hop" and c["score"] >= t for c in candidates)
        near = sum(c["kind"] == "near_miss" and c["score"] >= t for c in candidates)
        if near <= allowed and (best is None or (hops, -near) > (best[1], -best[2])):
            best = (t, hops, near)
    return best[0], best[1]


def calibrate():
    """L+G's threshold from #885's offline L+G records
    (../confirm/evidence/offline/L/b4-model-g/probes.jsonl) and corpus."""
    corpus = load(HERE.parent / "confirm" / "corpus.json")
    probes = load_jsonl(HERE / "evidence" / "calibration" / "probes.jsonl")
    cands = gate_candidates(probes, corpus)
    threshold, value = choose_threshold(cands)
    by_kind = defaultdict(list)
    for c in cands:
        by_kind[c["kind"]].append(c["score"])
    out = {"threshold": threshold if threshold != float("inf") else "inf", "hops_kept": value,
           "kept_at_threshold": {k: sum(s >= threshold for s in v) for k, v in by_kind.items()},
           "candidates": {k: len(v) for k, v in by_kind.items()},
           "score_range": {k: [min(v), max(v)] for k, v in by_kind.items()}}
    (HERE / "evidence" / "calibration" / "gate.json").write_text(json.dumps(out, indent=1) + "\n")
    print(json.dumps(out, indent=1))


# ---- power --------------------------------------------------------------------

def power():
    """The primary set's power at the first of four Holm steps, from #885's
    paired live records of L against S."""
    per = load(HERE.parent / "confirm" / "evidence" / "live" / "scores.json")["per_probe"]
    s, l = per["S/b4"], per["L/b4-model"]
    ids = [i for i, r in s.items() if r["family"] in PRIMARY]
    l_only = sum(l[i]["applied"] and not s[i]["applied"] for i in ids) / len(ids)
    s_only = sum(s[i]["applied"] and not l[i]["applied"] for i in ids) / len(ids)
    alpha = ALPHA / len(ARMS)
    out = {"primary_n": len(ids), "alpha_first_holm_step": alpha, "from_885": {"arm_only": l_only, "s_only": s_only}, "primary": {}}
    for name, share in (("885 L against S", 1), ("half that gain", 0.5), ("a quarter of that gain", 0.25)):
        a = s_only + (l_only - s_only) * share
        out["primary"][name] = {"arm_only": round(a, 4), "s_only": s_only, "power": C.exact_power(len(ids), a, s_only, alpha)}
    (HERE / "evidence" / "power.json").write_text(json.dumps(out, indent=1) + "\n")
    print(json.dumps(out, indent=1))


# ---- offline ------------------------------------------------------------------

def offline():
    C.offline()
    scores = load(C.OFFLINE / "scores.json")
    corpus = C.corpus()
    probes = load_jsonl(C.OFFLINE / "L" / "b4-model-g" / "probes.jsonl") if (C.OFFLINE / "L" / "b4-model-g" / "probes.jsonl").exists() else []
    families = {c["id"]: c["family"] for c in corpus["chains"]} | {g["id"]: g["family"] for g in corpus["guards"]}
    gate = defaultdict(lambda: {"probes": 0, "passed": 0, "dropped": 0, "probes_with_both": 0})
    for p in probes:
        walked = p["trace"].get("gate", [])
        g = gate[families[p["probe"]]]
        g["probes"] += 1
        passed, dropped = sum(not w["dropped"] for w in walked), sum(w["dropped"] for w in walked)
        g["passed"] += passed
        g["dropped"] += dropped
        g["probes_with_both"] += bool(passed and dropped)
    if probes:
        scores["gate"] = dict(sorted(gate.items()))
        scores["gate_control"] = gate["N"]["passed"] > 0 and gate["N"]["dropped"] > 0
    (C.OFFLINE / "scores.json").write_text(json.dumps(scores, indent=1) + "\n")
    print("gate:", json.dumps(scores.get("gate")), "control:", scores.get("gate_control"))


# ---- live ---------------------------------------------------------------------

ITEM = re.compile(r"^- \[(\d+)\] \((\w+)\): .*\[mid:\d+ sources:(\d+)-(\d+);", re.M)


def recall_items(recall):
    """(kind, first, last) of every mid line of a recall block."""
    return [(m.group(2), int(m.group(3)), int(m.group(4))) for m in ITEM.finditer(recall)]


def associated_fact(corpus_item, family):
    """The fact a probe's output may take from a walk mid, and the source that
    states it: a chain's answer at its last hop, or an absent query's
    near miss at the associated episode. None for E and K."""
    if family == "N":
        return corpus_item["near_miss"], corpus_item["sources"][1]
    if "hops" in corpus_item:
        return corpus_item["answer"], corpus_item["hops"][-1]["source"]
    return None


def inspected_before_stating(result, fact, first, last):
    """Whether a memory_inspect of a source in [first, last] comes before the
    first message or file stating the fact; the reply comes last, so any
    such inspect in the turn precedes a fact stated only there."""
    for t in result.get("tools") or []:
        if t["name"] == "memory_inspect":
            try:
                source = json.loads(t["input"]).get("id") if isinstance(t["input"], str) else t["input"].get("id")
            except (ValueError, AttributeError):
                source = None
            if isinstance(source, int) and first <= source <= last and not t.get("failed"):
                return True
        if t["name"] in ("message_send", "file_write") and states(json.dumps(t["input"]), fact):
            return False
    return False


def verify(corpus_item, family, result):
    """L+V's check: whether the probe states a fact that only a walk mid of
    its recall block held, and if so whether it inspected that mid's source
    before stating it. Returns (stated from a walk mid, inspected first)."""
    fact = associated_fact(corpus_item, family)
    if fact is None:
        return False, None
    value, source = fact
    items = recall_items(result.get("recall") or "")
    walk = [(f, l) for k, f, l in items if k == "linked" and f <= source <= l]
    other = [(f, l) for k, f, l in items if k != "linked" and f <= source <= l]
    if not walk or other or not states(P.artifact(result), value):
        return False, None
    return True, inspected_before_stating(result, value, *walk[0])


def associate_calls(result):
    return sum(t["name"] == ASSOCIATE for t in result.get("tools") or [])


def score_arm(name, corpus, results):
    rows = C.score_live(corpus, results)
    by_probe = {r["probe"]: r for r in results}
    items = {c["id"]: c for c in corpus["chains"]} | {g["id"]: g for g in corpus["guards"]}
    for i, row in rows.items():
        row["associate_calls"] = associate_calls(by_probe[i])
        if name == V:
            enforce(row, items[i], by_probe[i])
    return rows


def enforce(row, item, result):
    """L+V's check in scoring: a fact stated from a walk mid without an
    inspect of its source first is not applied, and for N not abstained."""
    stated, inspected = verify(item, row["family"], result)
    row["associated_fact_stated"], row["inspected_first"] = stated, inspected
    row["applied_unenforced"] = row["applied"]
    if stated and not inspected:
        row["applied"] = False


def best_variant(comparisons):
    """preregistration.md §Decision: the smallest valid arm by net lines;
    with none valid, the read-time variant with the fewest failed
    guardrails, then the fewest probes lost past the allowances, then the
    largest net gain on the primary set, then the fewest net lines."""
    valid = C.smallest_valid(comparisons)
    if valid:
        return valid, "valid"

    def rank(n):
        a = comparisons[n]
        failed = sum(a["guard_change"][f] < -ALLOWED_LOSS[f] for f in ALLOWED_LOSS)
        excess = sum(max(0, -a["guard_change"][f] - ALLOWED_LOSS[f]) for f in ALLOWED_LOSS)
        return (failed, excess, -(a["arm_only"] - a["S_only"]), NET_LINES[n])

    variants = [n for n in (G, T, V) if n in comparisons]
    return (min(variants, key=rank), "none valid; ranked by the tradeoff rule") if variants else (None, "no variant scored")


def live_controls(summary):
    """preregistration.md §Controls that the live records decide."""
    controls = {}
    for n in (L, V):
        if n in summary:
            controls[f"{NAMES[n]} walk items in half the primary recall blocks"] = summary[n]["primary_with_walk_items_in_recall"] >= summary[n]["primary"]["n"] / 2
    if T in summary:
        controls["L-T call in 20% of primary probes"] = summary[T]["primary_with_associate_call"] >= CALL_SHARE * summary[T]["primary"]["n"]
    if V in summary:
        controls["L+V's check met a stated associated fact"] = summary[V]["verify"]["associated_facts_stated"] > 0
    return controls


def live():
    corpus = C.corpus()
    per = {}
    for d in sorted(C.LIVE.glob("*/*/results.jsonl")):
        name = f"{d.parent.parent.name}/{d.parent.name}"
        per[name] = score_arm(name, corpus, load_jsonl(d))
    summary = {}
    for name, rows in per.items():
        fam = defaultdict(list)
        for row in rows.values():
            fam[row["family"]].append(row)
        prim = [r for f in PRIMARY for r in fam[f]]
        absent = fam["N"]
        s = {
            "families": {f: {"n": len(rs), "applied": sum(r["applied"] for r in rs)} for f, rs in sorted(fam.items())},
            "primary": {"n": len(prim), "applied": sum(r["applied"] for r in prim), "wilson": wilson(sum(r["applied"] for r in prim), len(prim)),
                        "all_seen": sum(r["all_seen"] for r in prim)},
            "absent": {"n": len(absent), "abstained": sum(r["applied"] for r in absent),
                       "near_miss_stated_without_abstaining": sum(r["near_miss_stated"] and not r["applied"] for r in absent)},
            "searches_per_probe": round(sum(r["searches"] for r in rows.values()) / max(len(rows), 1), 2),
            "primary_with_walk_items_in_recall": sum(r["arm_items"] for r in prim),
            "primary_with_associate_call": sum(r["associate_calls"] > 0 for r in prim),
            "associate_calls_per_probe": round(sum(r["associate_calls"] for r in rows.values()) / max(len(rows), 1), 2),
            "rerun": sum(r["attempt"] > 1 for r in rows.values()),
            "errors": sum(bool(r["error"]) for r in rows.values()),
        }
        if name == V:
            stated = [r for r in rows.values() if r["associated_fact_stated"]]
            s["verify"] = {"associated_facts_stated": len(stated), "inspected_first": sum(bool(r["inspected_first"]) for r in stated),
                           "primary_applied_unenforced": sum(r["applied_unenforced"] for r in prim),
                           "absent_abstained_unenforced": sum(r["applied_unenforced"] for r in absent)}
        summary[name] = s
    comparisons = C.compare(per)
    for n, a in comparisons.items():
        summary[n]["against_S_b4"] = a
    controls = live_controls(summary)
    best, basis = best_variant(comparisons)
    out = {"summary": summary, "controls": controls, "smallest_valid_arm": C.smallest_valid(comparisons),
           "best_read_time_variant": best, "basis": basis, "per_probe": per}
    (C.LIVE / "scores.json").write_text(json.dumps(out, indent=1) + "\n")
    for n, s in summary.items():
        print(NAMES.get(n, n), json.dumps({k: v for k, v in s.items() if k != "families"}))
    print("controls:", json.dumps(controls))
    print("smallest valid arm:", out["smallest_valid_arm"], "; best read-time variant:", best, f"({basis})")


# ---- tokens -------------------------------------------------------------------

def phase_of(call, windows):
    """A call's arm and phase. The ledger routes every L setting under arm L,
    so live calls are told apart by the setting windows of
    evidence/live/run.json."""
    if call["arm"] == "L" and call["phase"] == "live":
        for w in windows:
            if w["start"] <= call["time"] <= w["end"]:
                return f"{w['name']}/live"
        return "L/live, outside every window"
    return f"{call['arm']}/{call['phase']}"


def tokens():
    windows = load(HERE / "evidence" / "live" / "run.json")["windows"] if (HERE / "evidence" / "live" / "run.json").exists() else []
    agg = defaultdict(lambda: {"calls": 0, "status_429": 0, "prompt_tokens": 0, "completion_tokens": 0})
    for c in load_jsonl(HERE / "evidence" / "ledger" / "calls.jsonl"):
        a = agg[phase_of(c, windows)]
        a["calls"] += 1
        a["status_429"] += c.get("status") == 429
        a["prompt_tokens"] += c.get("prompt_tokens", 0)
        a["completion_tokens"] += c.get("completion_tokens", 0)
    out = dict(sorted(agg.items()))
    out["total"] = {k: sum(v[k] for v in agg.values()) for k in ("calls", "status_429", "prompt_tokens", "completion_tokens")}
    (HERE / "evidence" / "tokens.json").write_text(json.dumps(out, indent=1) + "\n")
    for k, v in out.items():
        print(k, v)


if __name__ == "__main__":
    {"calibrate": calibrate, "power": power, "offline": offline, "live": live, "tokens": tokens}[sys.argv[1]]()
