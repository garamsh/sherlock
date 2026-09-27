"""Score the #890 validation run with #874's matching (../../chained-recall/score.py).

    python3 docs/research/memory-encoding/validation/score.py check     # the check and oracle on evidence/check-b4.json
    python3 docs/research/memory-encoding/validation/score.py offline   # evidence/offline/scores.json
    python3 docs/research/memory-encoding/validation/score.py live      # evidence/live/scores.json
    python3 docs/research/memory-encoding/validation/score.py tokens    # evidence/tokens.json

Values are matched as #874 matches them (states(): every token with a
digit and every capitalized word). This file adds what preregistration.md
fixes for #890: the families SC, UP, KU and NC, each arm's primary family,
the offline margin that admits an arm to the live stage, the guardrails and
the decision rule.
"""

import importlib.util
import json
import sys
from collections import defaultdict
from pathlib import Path

HERE = Path(__file__).resolve().parent


def _module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


P = _module("chained_recall_score", HERE.parent.parent / "chained-recall" / "score.py")
states, wilson, mcnemar, holm, load_jsonl = P.states, P.wilson, P.mcnemar, P.holm, P.load_jsonl

CHAINS = ("C1", "C2", "C3", "IC2", "IC3")
BASELINE = "S/b4"
# Each arm's primary family (../README.md §Test plan). L is the offline
# reference and never goes live.
PRIMARY = {"C": ("SC",), "P": ("C2", "C3"), "F": ("UP",), "L": ("C2", "C3")}
SECONDARY = ("IC2", "IC3")
# Lost probes each guard family may show against S, live.
ALLOWED_LOSS = {"N": 3, "K": 2, "C1": 3, "E": 3, "NC": 2, "KU": 2}
# The offline margin that admits an arm to the live stage: on its primary
# family it reaches at least this share of the family more than S does,
# and the exact McNemar test against S gives p below ALPHA.
MARGIN = 0.10
ALPHA = 0.05
CHECK_TOP = 20
ORACLE_SHARE = 0.95
# Net production lines of each arm and setting, before tests, as its README
# states them before the live stage (preregistration.md §Decision).
NET_LINES = {}


def load(path):
    with open(path) as f:
        return json.load(f)


def corpus():
    return load(HERE / "corpus.json")


def primary_of(name):
    return PRIMARY.get(name.split("/")[0], ())


# ---- check ------------------------------------------------------------------

def check_rows(check, c):
    """The check's verdict per selected item: the family's needed sources
    beyond the raw top, and each hop's oracle rank within 5."""
    fam = {x["id"]: x["family"] for x in c["chains"]}
    rows = {r["id"]: r for r in check["chains"] if r["id"] in fam}
    leaks, hops, oracle = defaultdict(int), 0, 0
    for i, r in rows.items():
        needed = r["raw"][1:] if fam[i] in CHAINS else r["raw"]
        leaks[fam[i]] += sum(x <= CHECK_TOP for x in needed)
        hops += len(r["oracle"])
        oracle += sum(o <= 5 for o in r["oracle"])
    return {"leaks": {f: n for f, n in leaks.items() if n}, "hops": hops, "oracle_within_5": oracle, "oracle_share": round(oracle / max(hops, 1), 4),
            "passed": not sum(leaks.values()) and oracle >= ORACLE_SHARE * hops}


def check():
    c = corpus()
    out = check_rows(load(HERE / "evidence" / "check-b4.json"), c)
    (HERE / "evidence" / "check-summary.json").write_text(json.dumps(out, indent=1) + "\n")
    print(json.dumps(out))


# ---- offline ----------------------------------------------------------------

OFFLINE = HERE / "evidence" / "offline"


def covers(item, source):
    return item["first"] <= source <= item["last"]


def stated_in(items, source, value):
    """Some item holding the source states the value."""
    return any(covers(it, source) and states(it["text"], value) for it in items)


def score_setting(c, probes, oracle):
    by_probe = {p["probe"]: p for p in probes}
    rows = {}
    for ch in c["chains"]:
        items = by_probe[ch["id"]]["items"]
        hops = [P.hop_reached(items, h) for h in ch["hops"]]
        row = {"family": ch["family"], "k": ch["k"], "interval": [h[0] for h in hops], "stated": [h[1] for h in hops],
               "reached": all(h[1] for h in hops), "context_chars": by_probe[ch["id"]]["context_chars"]}
        if ch["family"] == "SC":
            # The subject's own introduction delivered, which S can reach by name.
            row["intro_delivered"] = any(covers(it, ch["intro"]["source"]) for it in items)
        rows[ch["id"]] = row
    for o in oracle:
        ch = next(x for x in c["chains"] if x["id"] == o["probe"])
        interval, _ = P.hop_reached(o["items"], ch["hops"][o["hop"] - 1])
        rows[o["probe"]].setdefault("oracle", [None] * ch["k"])[o["hop"] - 1] = interval
    for g in c["guards"]:
        items = by_probe[g["id"]]["items"]
        row = {"family": g["family"], "context_chars": by_probe[g["id"]]["context_chars"]}
        if g["family"] in ("E", "K"):
            row["reached"] = P.guard_reached(items, g)
        elif g["family"] == "KU":
            row["reached"] = stated_in(items, g["evidence"][0], g["key"])
            row["old_only"] = stated_in(items, g["sources"][0], g["obsolete"]) and not row["reached"]
        else:
            # N and NC: whether a value of the asked kind that is not the
            # answer is in view, and, the risk context at synthesis adds,
            # in an item that also names the query's entity.
            row["near_miss_in_view"] = any(states(it["text"], g["near_miss"]) for it in items)
            row["near_miss_with_entity"] = any(states(it["text"], g["near_miss"]) and states(it["text"], g["entity"]) for it in items)
        rows[g["id"]] = row
    return rows


def summarize(rows, built):
    fam = defaultdict(list)
    for r in rows.values():
        fam[r["family"]].append(r)
    out = {"families": {}}
    for f, rs in sorted(fam.items()):
        e = {"n": len(rs)}
        if f in ("N", "NC"):
            e["near_miss_in_view"] = sum(r["near_miss_in_view"] for r in rs)
            e["near_miss_with_entity"] = sum(r["near_miss_with_entity"] for r in rs)
        else:
            e["reached"] = sum(bool(r["reached"]) for r in rs)
            e["wilson"] = wilson(e["reached"], len(rs))
        if "stated" in rs[0]:
            e["hop_stated"] = [sum(r["stated"][h] for r in rs) for h in range(len(rs[0]["stated"]))]
            e["hop_oracle"] = [sum(bool((r.get("oracle") or [None] * len(r["stated"]))[h]) for r in rs) for h in range(len(rs[0]["stated"]))]
        if f == "SC":
            e["intro_delivered"] = sum(r["intro_delivered"] for r in rs)
        if f == "KU":
            e["old_only"] = sum(r["old_only"] for r in rs)
        out["families"][f] = e
    everything = list(rows.values())
    out["context_chars_per_turn"] = round(sum(r["context_chars"] for r in everything) / len(everything), 1)
    out["stored_chars_per_source"] = round(built["stored_chars_per_source"], 4)
    out["stored_chars"] = built["built"]["stored_chars"]
    out["vectors"] = built["built"]["vectors"]
    out["units"] = built["built"]["units"]
    hops = [o for r in everything for o in (r.get("oracle") or [])]
    out["oracle"] = {"hops": len(hops), "reached": sum(bool(o) for o in hops)}
    return out


def offline_records():
    """(arm/setting, directory) of every arm's offline records under
    evidence/offline/<arm>/<setting>/, named by built.json's arm."""
    found = {}
    for d in sorted(p for a in OFFLINE.iterdir() if a.is_dir() for p in a.iterdir() if p.is_dir()):
        if (d / "probes.jsonl").exists() and (d / "built.json").exists():
            found[f"{load(d / 'built.json')['arm']}/{d.name}"] = d
    return sorted(found.items())


def compare(rows, base, families):
    ids = sorted(i for i, r in base.items() if r["family"] in families)
    b, cc, p = mcnemar([rows[i]["reached"] for i in ids], [base[i]["reached"] for i in ids])
    return {"n": len(ids), "arm": sum(rows[i]["reached"] for i in ids), "S": sum(base[i]["reached"] for i in ids),
            "arm_only": b, "S_only": cc, "p": p}


def offline():
    c = corpus()
    results, per = {}, {}
    for name, d in offline_records():
        rows = score_setting(c, load_jsonl(d / "probes.jsonl"), load_jsonl(d / "oracle.jsonl"))
        results[name] = summarize(rows, load(d / "built.json"))
        per[name] = rows
    base = per.get(BASELINE)
    if base:
        for name, r in results.items():
            if name == BASELINE:
                continue
            r["against_S_b4"] = {f: compare(per[name], base, (f,)) for f in ("C2", "C3", "IC2", "IC3", "SC", "UP", "KU", "E", "K", "C1")}
            fams = primary_of(name)
            if fams:
                prim = compare(per[name], base, fams)
                prim["margin_met"] = prim["arm"] - prim["S"] >= MARGIN * prim["n"] and prim["arm_only"] > prim["S_only"] and prim["p"] < ALPHA
                r["primary"] = {"families": list(fams), **prim}
    out = {"baseline": BASELINE, "margin": MARGIN, "results": results, "per_probe": per}
    OFFLINE.mkdir(parents=True, exist_ok=True)
    (OFFLINE / "scores.json").write_text(json.dumps(out, indent=1) + "\n")
    for name, r in results.items():
        f = r["families"]
        print(f"{name:14s} C2 {f['C2']['reached']:2d} C3 {f['C3']['reached']:2d} IC {f['IC2']['reached'] + f['IC3']['reached']:2d}"
              f" SC {f['SC']['reached']:2d} UP {f['UP']['reached']:2d} KU {f['KU']['reached']:2d} E {f['E']['reached']:2d} K {f['K']['reached']:2d}"
              f" NCmiss {f['NC']['near_miss_with_entity']:2d}  ctx {r['context_chars_per_turn']:6.0f} stored/src {r['stored_chars_per_source']:.3f}"
              f" oracle {r['oracle']['reached']}/{r['oracle']['hops']}  {r.get('primary', '')}")


# ---- live -------------------------------------------------------------------

LIVE = HERE / "evidence" / "live"


def score_live(c, results):
    by_probe = {r["probe"]: r for r in results}
    out = {}
    for ch in c["chains"]:
        r = by_probe.get(ch["id"])
        if r is None:
            continue
        views = P.seen(r)
        hops = [any(states(v, h["key"]) for v in views) for h in ch["hops"]]
        out[ch["id"]] = {"family": ch["family"], "applied": P.applied(ch["check"], r), "hops_seen": hops, "all_seen": all(hops),
                         "searches": sum(t["name"] == "memory_search" for t in r.get("tools") or []),
                         "error": r.get("error", ""), "attempt": r.get("attempt", 1)}
    for g in c["guards"]:
        r = by_probe.get(g["id"])
        if r is None:
            continue
        row = {"family": g["family"], "applied": P.applied(g["check"], r), "error": r.get("error", ""), "attempt": r.get("attempt", 1),
               "searches": sum(t["name"] == "memory_search" for t in r.get("tools") or [])}
        if g["family"] in ("N", "NC"):
            row["near_miss_stated"] = states(P.artifact(r), g["near_miss"])
        if g["family"] == "KU":
            row["old_stated"] = states(P.artifact(r), g["obsolete"])
        out[g["id"]] = row
    return out


def compare_live(per):
    """Each live arm against S on its primary family: exact McNemar on
    applied, Holm across the arms compared; IC with its pairs and p; and
    each guard family's change in probes."""
    base = per.get(BASELINE)
    if not base:
        return {}
    compared = [n for n in per if n != BASELINE and primary_of(n)]
    out, ps = {}, []
    for n in compared:
        def pair(fams):
            ids = sorted(i for i, r in base.items() if r["family"] in fams and i in per[n])
            b, cc, p = mcnemar([per[n][i]["applied"] for i in ids], [base[i]["applied"] for i in ids])
            return {"n": len(ids), "arm": sum(per[n][i]["applied"] for i in ids), "S": sum(base[i]["applied"] for i in ids),
                    "arm_only": b, "S_only": cc, "p": p}
        guard = {}
        for f in ALLOWED_LOSS:
            fid = [i for i, r in base.items() if r["family"] == f and i in per[n]]
            guard[f] = sum(per[n][i]["applied"] for i in fid) - sum(base[i]["applied"] for i in fid)
        out[n] = {"primary": {"families": list(primary_of(n)), **pair(primary_of(n))}, "implicit": pair(SECONDARY), "guard_change": guard,
                  "guardrails_hold": all(guard[f] >= -ALLOWED_LOSS[f] for f in guard)}
        ps.append(out[n]["primary"]["p"])
    for n, adjusted in zip(compared, holm(ps)):
        a = out[n]
        a["primary"]["holm_p"] = adjusted
        a["significant"] = a["primary"]["arm_only"] > a["primary"]["S_only"] and adjusted < ALPHA
        a["valid"] = a["significant"] and a["guardrails_hold"]
    return out


def smallest_valid(comparisons):
    valid = [n for n, a in comparisons.items() if a["valid"]]
    if not valid:
        return None
    missing = [n for n in valid if n not in NET_LINES]
    if missing:
        raise SystemExit(f"no net production lines recorded for {missing}")
    return min(valid, key=lambda n: NET_LINES[n])


def live():
    c = corpus()
    per = {}
    for d in sorted(LIVE.glob("*/*/results.jsonl")):
        per[f"{d.parent.parent.name}/{d.parent.name}"] = score_live(c, load_jsonl(d))
    summary = {}
    for name, rows in per.items():
        fam = defaultdict(list)
        for row in rows.values():
            fam[row["family"]].append(row)
        summary[name] = {
            "families": {f: {"n": len(rs), "applied": sum(r["applied"] for r in rs),
                             "all_seen": sum(r.get("all_seen", False) for r in rs) if "all_seen" in rs[0] else None,
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
    decision = smallest_valid(comparisons)
    out = {"summary": summary, "smallest_valid_arm": decision, "per_probe": per}
    LIVE.mkdir(parents=True, exist_ok=True)
    (LIVE / "scores.json").write_text(json.dumps(out, indent=1) + "\n")
    for n, s in summary.items():
        print(n, json.dumps({k: v for k, v in s.items() if k != "families"}))
    print("smallest valid arm:", decision)


# ---- tokens -----------------------------------------------------------------

def tokens():
    """Tokens per arm and phase, from this run's ledger."""
    agg = defaultdict(lambda: {"calls": 0, "cached": 0, "status_429": 0, "prompt_tokens": 0, "completion_tokens": 0})
    for c in load_jsonl(HERE / "evidence" / "ledger" / "calls.jsonl"):
        a = agg[f"{c['arm']}/{c['phase']}"]
        a["calls"] += 1
        a["cached"] += bool(c.get("cached"))
        a["status_429"] += c.get("status") == 429
        a["prompt_tokens"] += c.get("prompt_tokens", 0)
        a["completion_tokens"] += c.get("completion_tokens", 0)
    out = dict(sorted(agg.items()))
    (HERE / "evidence" / "tokens.json").write_text(json.dumps(out, indent=1) + "\n")
    for k, v in out.items():
        print(k, v)


if __name__ == "__main__":
    {"check": check, "offline": offline, "live": live, "tokens": tokens}[sys.argv[1]]()
