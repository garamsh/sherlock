"""Score the #900 validation run with #890's matching (../../memory-encoding/validation/score.py).

    python3 research/memory-paradigms/validation/score.py offline    # evidence/offline/scores.json: D's admission
    python3 research/memory-paradigms/validation/score.py controls   # evidence/controls.json
    python3 research/memory-paradigms/validation/score.py live       # evidence/live/scores.json: the verdict
    python3 research/memory-paradigms/validation/score.py tokens     # evidence/tokens.json

This file adds what preregistration.md fixes for #900: the arms' primary
families, D's offline admission, R's and D's controls, W's reading beside
S on its sample, and the decision rule over R and D.
"""

import importlib.util
import json
import re
import sys
from collections import defaultdict
from pathlib import Path

HERE = Path(__file__).resolve().parent
RESEARCH = HERE.parent.parent


def _module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


V = _module("validation_score", RESEARCH / "memory-encoding" / "validation" / "score.py")
P = V.P
states, mcnemar, holm, load_jsonl, load = V.states, V.mcnemar, V.holm, V.load_jsonl, V.load

BASELINE = "S/b4"
# Each candidate's primary family (../README.md §Test plan). W is a ceiling
# and has none: it is reported, never selected.
PRIMARY = {"R": ("C2", "C3"), "D": ("UP",)}
SECONDARY = ("IC2", "IC3")
ALLOWED_LOSS = V.ALLOWED_LOSS
MARGIN, ALPHA = V.MARGIN, V.ALPHA
# Net production lines before tests, the upper ends of ../README.md §R and §D.
NET_LINES = {"R/b4-agent": 190, "D/b4-doc16k": 215}
# Control thresholds (preregistration.md §Controls).
R_LOOP_SHARE = 0.95
D_UP_SHARE = 0.50
W_PROMPT = (180_000, 196_608)
W_WINDOW_FIRST = 2039


def corpus():
    return V.corpus()


def primary_of(name):
    return PRIMARY.get(name.split("/")[0], ())


def sources_by_id():
    c = corpus()
    base = load(RESEARCH / "mid-memory-rag" / "evidence" / "replication" / "corpus.json")
    return {m["id"]: m["content"] for m in base + c["stream"]}


# ---- offline ----------------------------------------------------------------

OFFLINE = HERE / "evidence" / "offline"


def offline():
    """D against S offline, with #890's reach; D is admitted to the live
    stage when its controls pass and it meets #890's margin on UP."""
    c = corpus()
    per, results = {}, {}
    for name in ("S/b4", "D/b4-doc16k"):
        d = OFFLINE / name
        if not (d / "probes.jsonl").exists():
            continue
        rows = V.score_setting(c, load_jsonl(d / "probes.jsonl"), load_jsonl(d / "oracle.jsonl"))
        per[name], results[name] = rows, V.summarize(rows, load(d / "built.json"))
    out = {"results": results}
    if "S/b4" in per:
        theirs = RESEARCH / "memory-encoding" / "validation" / "evidence" / "offline" / "S" / "b4" / "probes.jsonl"
        strip = lambda ps: [{k: v for k, v in p.items() if k != "seconds"} for p in ps]
        out["S_equals_890"] = strip(load_jsonl(OFFLINE / "S" / "b4" / "probes.jsonl")) == strip(load_jsonl(theirs))
    if len(per) == 2:
        prim = V.compare(per["D/b4-doc16k"], per["S/b4"], PRIMARY["D"])
        prim["margin_met"] = prim["arm"] - prim["S"] >= MARGIN * prim["n"] and prim["arm_only"] > prim["S_only"] and prim["p"] < ALPHA
        out["D_primary"] = prim
        out["D_against_S"] = {f: V.compare(per["D/b4-doc16k"], per["S/b4"], (f,)) for f in ("C2", "C3", "IC2", "IC3", "SC", "UP", "KU", "E", "K", "C1")}
    out["per_probe"] = per
    OFFLINE.mkdir(parents=True, exist_ok=True)
    (OFFLINE / "scores.json").write_text(json.dumps(out, indent=1) + "\n")
    print(json.dumps({k: v for k, v in out.items() if k != "per_probe" and k != "results"}, indent=1))
    for n, r in results.items():
        print(n, {f: r["families"][f].get("reached", r["families"][f].get("near_miss_in_view")) for f in r["families"]}, r["context_chars_per_turn"], r["stored_chars_per_source"])


# ---- controls ---------------------------------------------------------------

LIVE = HERE / "evidence" / "live"


def supported(fact, sources):
    """A kept fact is supported when its cited sources state every value
    token of its statement but the first word (digits and capitalized
    words), as #874 matches values."""
    words = fact["statement"].split(maxsplit=1)
    rest = words[1] if len(words) > 1 else ""
    text = " ".join(sources.get(s, "") for s in fact["sources"])
    return states(text, rest)


def d_controls(c):
    path = HERE / "arms" / "document" / "evidence" / "document-b4-doc16k.json"
    passes = HERE / "arms" / "document" / "evidence" / "passes-b4-doc16k.jsonl"
    if not path.exists() or not passes.exists():
        return None
    doc, records = load(path), load_jsonl(passes)
    rendered = "".join(f"{i + 1}. {l['text']} (mids {', '.join(map(str, l['mids']))})\n" for i, l in enumerate(doc["lines"]))
    cited = {m for r in records for m in r["mids"]}
    ops = sum(r["applied"]["added"] + r["applied"]["superseded"] + r["applied"]["dropped"] for r in records)
    ups = [ch for ch in c["chains"] if ch["family"] == "UP"]
    covered = [ch["id"] for ch in ups if any(states(l["text"], ch["hops"][0]["key"]) for l in doc["lines"])]
    out = {
        "document_chars": len(rendered), "bound_held": len(rendered) <= 16000,
        "lines": len(doc["lines"]), "lines_citing_a_passed_mid": sum(bool(l["mids"]) and set(l["mids"]) <= cited for l in doc["lines"]),
        "versions": len(doc["history"]), "operations_applied": ops, "history_complete": len(doc["history"]) == ops,
        "up_covered": len(covered), "up_n": len(ups), "passes": len(records),
        "partial_passes": sum(not r["strict"] for r in records), "refused_attempts": sum(len(r.get("refusals") or []) for r in records),
        "rejected_operations": sum(r["applied"]["rejected"] for r in records),
    }
    out["passed"] = out["bound_held"] and out["lines_citing_a_passed_mid"] == out["lines"] and out["history_complete"] and out["up_covered"] >= D_UP_SHARE * out["up_n"]
    return out


def r_controls():
    path = LIVE / "R" / "b4-agent" / "results.jsonl"
    if not path.exists():
        return None
    results = load_jsonl(path)
    sources = sources_by_id()
    traces = [r.get("trace") or {} for r in results]
    kept = [f for t in traces for f in t.get("kept") or []]
    dropped = [f for t in traces for f in t.get("dropped") or []]
    ran = sum(t.get("loop_calls", 0) >= 1 for t in traces)
    out = {
        "probes": len(results), "loop_ran": ran, "loop_share": round(ran / max(len(results), 1), 4),
        "loop_calls_per_probe": round(sum(t.get("loop_calls", 0) for t in traces) / max(len(results), 1), 2),
        "ended_with_result": sum(t.get("ended") == "result" for t in traces),
        "facts_returned": sum(t.get("returned", 0) for t in traces), "facts_kept": len(kept), "facts_dropped": len(dropped),
        "kept_supported": sum(supported(f, sources) for f in kept),
    }
    out["support_share"] = round(out["kept_supported"] / max(len(kept), 1), 4)
    out["passed"] = out["loop_share"] >= R_LOOP_SHARE and out["facts_dropped"] >= 1
    return out


def w_controls():
    path = LIVE / "W" / "w180k" / "results.jsonl"
    ledger = HERE / "evidence" / "ledger" / "calls.jsonl"
    if not path.exists():
        return None
    results = load_jsonl(path)
    calls = [x for x in load_jsonl(ledger) if x.get("arm") == "W" and x.get("phase") == "live" and x.get("status") == 200] if ledger.exists() else []
    within = sum(W_PROMPT[0] <= x["prompt_tokens"] <= W_PROMPT[1] for x in calls)
    out = {"probes": len(results), "window_start_held": sum(r.get("window_first") == W_WINDOW_FIRST for r in results),
           "act_calls": len(calls), "prompt_within_bounds": within,
           "latency_ms_p50": sorted(x["latency_ms"] for x in calls)[len(calls) // 2] if calls else None,
           "latency_ms_p90": sorted(x["latency_ms"] for x in calls)[int(len(calls) * 0.9)] if calls else None}
    out["passed"] = bool(calls) and within == len(calls) and out["window_start_held"] == len(results)
    return out


def controls():
    out = {"D": d_controls(corpus()), "R": r_controls(), "W": w_controls()}
    (HERE / "evidence").mkdir(exist_ok=True)
    (HERE / "evidence" / "controls.json").write_text(json.dumps(out, indent=1) + "\n")
    print(json.dumps(out, indent=1))
    return out


# ---- live -------------------------------------------------------------------

def sample():
    return load(HERE / "sample.json")["probes"]


def pair(per, n, ids):
    b, cc, p = mcnemar([per[n][i]["applied"] for i in ids], [per[BASELINE][i]["applied"] for i in ids])
    return {"n": len(ids), "arm": sum(per[n][i]["applied"] for i in ids), "S": sum(per[BASELINE][i]["applied"] for i in ids),
            "arm_only": b, "S_only": cc, "p": p}


def compare_live(per):
    """R and D against S on their primary families, Holm across those that
    went live, with #890's guardrails; IC reported."""
    base = per.get(BASELINE)
    if not base:
        return {}
    compared = [n for n in per if n != BASELINE and primary_of(n)]
    out, ps = {}, []
    for n in compared:
        def fam(fams):
            return sorted(i for i, r in base.items() if r["family"] in fams and i in per[n])
        guard = {f: sum(per[n][i]["applied"] for i in fam((f,))) - sum(base[i]["applied"] for i in fam((f,))) for f in ALLOWED_LOSS}
        out[n] = {"primary": {"families": list(primary_of(n)), **pair(per, n, fam(primary_of(n)))}, "implicit": pair(per, n, fam(SECONDARY)),
                  "guard_change": guard, "guardrails_hold": all(guard[f] >= -ALLOWED_LOSS[f] for f in guard)}
        ps.append(out[n]["primary"]["p"])
    for n, adjusted in zip(compared, holm(ps)):
        a = out[n]
        a["primary"]["holm_p"] = adjusted
        a["significant"] = a["primary"]["arm_only"] > a["primary"]["S_only"] and adjusted < ALPHA
        a["valid"] = a["significant"] and a["guardrails_hold"]
    return out


def w_reading(per, c):
    """W beside S on each family of its sample: pairs and p, outside Holm.
    K is split by whether its evidence lies in W's window."""
    if "W/w180k" not in per or BASELINE not in per:
        return None
    guards = {g["id"]: g for g in c["guards"]}
    out = {}
    for f, ids in sample().items():
        ids = [i for i in ids if i in per["W/w180k"] and i in per[BASELINE]]
        out[f] = pair(per, "W/w180k", ids)
        if f == "K":
            inside = [i for i in ids if min(guards[i]["evidence"]) >= W_WINDOW_FIRST]
            out["K, evidence in window"] = pair(per, "W/w180k", inside)
            out["K, evidence outside"] = pair(per, "W/w180k", [i for i in ids if i not in inside])
        if f in ("N", "NC"):
            out[f]["near_miss_stated"] = {a: sum(per[a][i].get("near_miss_stated", False) for i in ids) for a in ("W/w180k", BASELINE)}
    return out


def live():
    c = corpus()
    per = {}
    for d in sorted(LIVE.glob("*/*/results.jsonl")):
        per[f"{d.parent.parent.name}/{d.parent.name}"] = V.score_live(c, load_jsonl(d))
    summary = {}
    for name, rows in per.items():
        fam = defaultdict(list)
        for row in rows.values():
            fam[row["family"]].append(row)
        summary[name] = {
            "families": {f: {"n": len(rs), "applied": sum(r["applied"] for r in rs),
                             "near_miss_stated": sum(r.get("near_miss_stated", False) for r in rs) if f in ("N", "NC") else None,
                             "old_stated": sum(r.get("old_stated", False) for r in rs) if f == "KU" else None}
                         for f, rs in sorted(fam.items())},
            "searches_per_probe": round(sum(r["searches"] for r in rows.values()) / max(len(rows), 1), 2),
            "rerun": sum(r["attempt"] > 1 for r in rows.values()),
            "errors": sum(bool(r["error"]) for r in rows.values()),
        }
    comparisons = compare_live(per)
    for n, a in comparisons.items():
        summary[n]["against_S_b4"] = a
    valid = [n for n, a in comparisons.items() if a["valid"]]
    decision = min(valid, key=lambda n: NET_LINES[n]) if valid else None
    out = {"summary": summary, "smallest_valid_arm": decision, "W_beside_S": w_reading(per, c), "per_probe": per}
    LIVE.mkdir(parents=True, exist_ok=True)
    (LIVE / "scores.json").write_text(json.dumps(out, indent=1) + "\n")
    for n, s in summary.items():
        print(n, json.dumps({k: v for k, v in s.items() if k != "families"}))
    print("W beside S:", json.dumps(out["W_beside_S"]))
    print("smallest valid arm:", decision)


# ---- tokens -----------------------------------------------------------------

def tokens():
    agg = defaultdict(lambda: {"calls": 0, "status_429": 0, "prompt_tokens": 0, "completion_tokens": 0})
    for x in load_jsonl(HERE / "evidence" / "ledger" / "calls.jsonl"):
        a = agg[f"{x['arm']}/{x['phase']}"]
        a["calls"] += 1
        a["status_429"] += x.get("status") == 429
        a["prompt_tokens"] += x.get("prompt_tokens", 0)
        a["completion_tokens"] += x.get("completion_tokens", 0)
    out = dict(sorted(agg.items()))
    (HERE / "evidence" / "tokens.json").write_text(json.dumps(out, indent=1) + "\n")
    for k, v in out.items():
        print(k, v)


if __name__ == "__main__":
    {"offline": offline, "controls": controls, "live": live, "tokens": tokens}[sys.argv[1]]()
