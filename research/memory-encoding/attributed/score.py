"""Score #916's run with #896's and #890's matching (../structural/score.py).

    python3 research/memory-encoding/attributed/score.py check               # evidence/check-summary.json: #890's corpus checks
    python3 research/memory-encoding/attributed/score.py offline             # evidence/offline/scores.json: reach, controls, admission
    python3 research/memory-encoding/attributed/score.py equivalence <work>  # evidence/equivalence.json: S and L-attr share the store
    python3 research/memory-encoding/attributed/score.py live                # evidence/live/scores.json: applied, controls, decision
    python3 research/memory-encoding/attributed/score.py tokens              # evidence/tokens.json

This file adds what preregistration.md fixes for #916: N and NC scored by
the owner's rule through matcher.py, the strict rule and reading B beside
it, the store equivalence, and the decision between L-attr and S.
"""

import hashlib
import importlib.util
import json
import sqlite3
import sys
from collections import defaultdict
from pathlib import Path

import audit
import matcher

HERE = Path(__file__).resolve().parent


def _module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


St = _module("structural_score", HERE.parent / "structural" / "score.py")
V, P = St.V, St.P
mcnemar, load_jsonl = P.mcnemar, P.load_jsonl

ARMS = {"S/b4": "S", "L/b4-model-attr": "L-attr"}
BASELINE, ARM = "S", "L-attr"
PRIMARY = ("C2", "C3")
ABSENT = ("N", "NC")
ALLOWED_LOSS = V.ALLOWED_LOSS
MARGIN, ALPHA, ORACLE_SHARE = V.MARGIN, V.ALPHA, V.ORACLE_SHARE
ENTITY_SHARE = 0.90
MATCHER_CONTROLS = ("audit_1", "audit_2")
SIZES = {"C1": 30, "C2": 50, "C3": 50, "IC2": 25, "IC3": 25, "SC": 50, "UP": 50, "N": 60, "NC": 30, "KU": 30, "E": 30, "K": 40}
OFFLINE = HERE / "evidence" / "offline"
LIVE = HERE / "evidence" / "live"


def load(path):
    with open(path) as f:
        return json.load(f)


def corpus():
    return load(HERE / "corpus.json")


def entities():
    return St.Entities(load_jsonl(HERE / "arms" / "links" / "evidence" / "entities-b4.jsonl"))


def stream_summaries():
    return {r["first"]: r["summary"].strip() for r in load_jsonl(HERE / "evidence" / "synthesis" / "stream-b4.jsonl")}


# ---- corpus checks -----------------------------------------------------------

def selection(c):
    """The selected corpus keeps every family at its size."""
    have = defaultdict(int)
    for x in c["chains"] + c["guards"]:
        have[x["family"]] += 1
    return {"stage": c.get("stage"), "sizes": dict(sorted(have.items())), "passed": c.get("stage") == "selected" and dict(have) == SIZES}


def check():
    c = corpus()
    out = {"selection": selection(c), "check": V.check_rows(load(HERE / "evidence" / "check-b4.json"), c)}
    out["passed"] = out["selection"]["passed"] and out["check"]["passed"]
    (HERE / "evidence" / "check-summary.json").write_text(json.dumps(out, indent=1) + "\n")
    print(json.dumps(out))


# ---- offline -----------------------------------------------------------------

def offline_records():
    found = {}
    for key, name in ARMS.items():
        d = OFFLINE / key
        if (d / "probes.jsonl").exists() and (d / "built.json").exists():
            found[name] = d
    return found


def margin_met(prim):
    return prim["arm"] - prim["S"] >= MARGIN * prim["n"] and prim["arm_only"] > prim["S_only"] and prim["p"] < ALPHA


def offline_controls(name, c, probes, summary, built, ents):
    oracle = summary["oracle"]
    out = {"oracle": {**oracle, "passed": oracle["reached"] >= ORACLE_SHARE * oracle["hops"]}}
    if name == ARM:
        share = built["built"]["notes"]["share_with_entities"]
        out["entities_stored"] = {"share": share, "passed": share >= ENTITY_SHARE}
        out["attribution"] = St.attribution_control(c, probes, ents)
    out["passed"] = all(v["passed"] for v in out.values())
    return out


def offline():
    c, ents = corpus(), entities()
    per, results = {}, {}
    for name, d in offline_records().items():
        probes, oracle, built = load_jsonl(d / "probes.jsonl"), load_jsonl(d / "oracle.jsonl"), load(d / "built.json")
        rows = St.score_setting(c, probes, oracle)
        per[name] = rows
        summary = V.summarize(rows, built)
        results[name] = {**summary, "controls": offline_controls(name, c, probes, summary, built, ents)}
    if BASELINE in per and ARM in per:
        r = results[ARM]
        r["against_S"] = {f: V.compare(per[ARM], per[BASELINE], (f,)) for f in ("C2", "C3", "IC2", "IC3", "SC", "UP", "C1", "E", "K", "KU")}
        prim = V.compare(per[ARM], per[BASELINE], PRIMARY)
        prim["margin_met"] = margin_met(prim)
        r["primary"] = {"families": list(PRIMARY), **prim}
        r["admitted"] = prim["margin_met"] and r["controls"]["passed"]
    out = {"baseline": BASELINE, "margin": MARGIN, "results": results, "per_probe": per}
    OFFLINE.mkdir(parents=True, exist_ok=True)
    (OFFLINE / "scores.json").write_text(json.dumps(out, indent=1) + "\n")
    for name, r in results.items():
        f = r["families"]
        print(f"{name:7s} C2 {f['C2']['reached']:2d} C3 {f['C3']['reached']:2d} C1 {f['C1']['reached']:2d} E {f['E']['reached']:2d}"
              f" K {f['K']['reached']:2d} KU {f['KU']['reached']:2d} N in view {f['N']['near_miss_in_view']:2d}"
              f" NC in view {f['NC']['near_miss_in_view']:2d} oracle {r['oracle']['reached']}/{r['oracle']['hops']}"
              f" controls {'pass' if r['controls']['passed'] else 'FAIL'} admitted {r.get('admitted')}")


# ---- equivalence -------------------------------------------------------------

def store_digest(path):
    """Row count and SHA-256 of every source and every mid (ID, sources,
    summary, vector) of a build's store."""
    db = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
    try:
        out = {}
        for table, query in (("sources", "SELECT id, role, content FROM conversation_sources ORDER BY id"),
                             ("mids", "SELECT id, source_first, source_last, summary, embedding FROM conversation_mid ORDER BY id")):
            h, n = hashlib.sha256(), 0
            for row in db.execute(query):
                h.update(repr(row).encode())
                n += 1
            out[table] = {"rows": n, "sha256": h.hexdigest()}
        return out
    finally:
        db.close()


def unseeded_equal(s_probes, l_probes):
    """Probes whose L-attr trace has no seed deliver the same items in both arms."""
    s = {p["probe"]: p["items"] for p in s_probes}
    unseeded = [p for p in l_probes if not (p.get("trace") or {}).get("seeds")]
    equal = sum(p["items"] == s[p["probe"]] for p in unseeded)
    return {"unseeded": len(unseeded), "equal": equal, "passed": bool(unseeded) and equal == len(unseeded)}


def equivalence(work):
    stores = {name: store_digest(Path(work) / "builds" / key.split("/")[0] / key.split("/")[1] / "memory.db") for key, name in ARMS.items()}
    records = offline_records()
    probes = unseeded_equal(load_jsonl(records[BASELINE] / "probes.jsonl"), load_jsonl(records[ARM] / "probes.jsonl"))
    same = stores[BASELINE] == stores[ARM] and stores[BASELINE]["mids"]["rows"] > 0
    out = {"stores": stores, "stores_equal": same, "unseeded_probes": probes, "passed": same and probes["passed"]}
    (HERE / "evidence" / "equivalence.json").write_text(json.dumps(out, indent=1) + "\n")
    print(json.dumps(out))


# ---- live --------------------------------------------------------------------

def score_live(c, results):
    """#890's live rows, with N and NC applied under the owner's rule
    (reading A), and the strict rule and reading B beside it."""
    rows = V.score_live(c, results)
    guards = {g["id"]: g for g in c["guards"]}
    for r in results:
        row = rows[r["probe"]]
        row["inspects"] = sum(t["name"] == "memory_inspect" for t in r.get("tools") or [])
        if row["family"] in ABSENT:
            j = matcher.judge(audit.visible(r), guards[r["probe"]])
            row.update({"strict": j["strict"], "reading_b": j["reading_b"], "attributed": j["attributed"], "asserted": j["asserted"],
                        "applied": j["owner"]})
    return rows


def with_rule(rows, rule):
    """The rows with N and NC applied under another rule."""
    return {i: ({**r, "applied": r[rule]} if r["family"] in ABSENT else r) for i, r in rows.items()}


def compare(arm, base):
    def pair(fams):
        ids = sorted(i for i, r in base.items() if r["family"] in fams and i in arm)
        b, cc, p = mcnemar([arm[i]["applied"] for i in ids], [base[i]["applied"] for i in ids])
        return {"n": len(ids), "arm": sum(arm[i]["applied"] for i in ids), "S": sum(base[i]["applied"] for i in ids),
                "arm_only": b, "S_only": cc, "p": p}
    guard = {}
    for f in ALLOWED_LOSS:
        fid = [i for i, r in base.items() if r["family"] == f and i in arm]
        guard[f] = sum(arm[i]["applied"] for i in fid) - sum(base[i]["applied"] for i in fid)
    failed = [f for f in guard if guard[f] < -ALLOWED_LOSS[f]]
    prim = pair(PRIMARY)
    return {"primary": {"families": list(PRIMARY), **prim}, "implicit": pair(("IC2", "IC3")), "guard_change": guard,
            "guardrails_failed": failed, "guardrails_hold": not failed,
            "significant": prim["arm_only"] > prim["S_only"] and prim["p"] < ALPHA}


def family_summary(rows):
    fam = defaultdict(list)
    for row in rows.values():
        fam[row["family"]].append(row)
    out = {}
    for f, rs in sorted(fam.items()):
        e = {"n": len(rs), "applied": sum(r["applied"] for r in rs)}
        if "all_seen" in rs[0]:
            e["all_seen"] = sum(r["all_seen"] for r in rs)
        if f in ABSENT:
            for k in ("strict", "reading_b", "attributed", "asserted", "near_miss_stated"):
                e[k] = sum(r[k] for r in rs)
        if f == "KU":
            e["old_stated"] = sum(r["old_stated"] for r in rs)
        out[f] = e
    return out


def gate(path, key="passed"):
    """A control recorded in another evidence file, or failed when absent."""
    return bool(path.exists() and load(path).get(key))


def study_controls(offline_scores):
    audit1 = HERE / "evidence" / "audit" / "audit-1.json"
    audit1b = HERE / "evidence" / "audit" / "audit-1b.json"
    return {"corpus_checks": gate(HERE / "evidence" / "check-summary.json"),
            "fresh": gate(HERE / "evidence" / "fresh.json"),
            "equivalence": gate(HERE / "evidence" / "equivalence.json"),
            "offline_controls": all(r["controls"]["passed"] for r in offline_scores["results"].values()),
            "audit_1": gate(audit1) or gate(audit1b),
            "audit_2": gate(HERE / "evidence" / "audit" / "audit-2.json")}


def template_reuse(per):
    """UP and KU applied with and without the probes whose query the
    preregistered fresh check found in an earlier corpus (Amendment 2)."""
    reused = {s["probe"] for s in load(HERE / "evidence" / "fresh.json")["as_written"]["seen_generated"]}
    out = {}
    for f in ("UP", "KU"):
        for label, keep in (("all", lambda i: True), ("without_reused", lambda i: i not in reused)):
            ids = [i for i, r in per[BASELINE].items() if r["family"] == f and keep(i) and i in per[ARM]]
            out[f"{f}/{label}"] = {"n": len(ids), "S": sum(per[BASELINE][i]["applied"] for i in ids),
                                   "L-attr": sum(per[ARM][i]["applied"] for i in ids)}
    return out


def live():
    c, ents, summaries = corpus(), entities(), stream_summaries()
    ledger = load_jsonl(HERE / "evidence" / "ledger" / "calls.jsonl") if (HERE / "evidence" / "ledger" / "calls.jsonl").exists() else []
    offline_scores = load(OFFLINE / "scores.json")
    per, summary = {}, {}
    for key, name in ARMS.items():
        path = LIVE / key / "results.jsonl"
        if not path.exists():
            continue
        results = load_jsonl(path)
        rows = score_live(c, results)
        per[name] = rows
        ctl = St.live_controls(name, c, results, summaries, ents, [], ledger)
        summary[name] = {"families": family_summary(rows),
                         "searches_per_probe": round(sum(r["searches"] for r in rows.values()) / max(len(rows), 1), 2),
                         "inspects_per_probe": round(sum(r["inspects"] for r in rows.values()) / max(len(rows), 1), 2),
                         "rerun": sum(r["attempt"] > 1 for r in rows.values()), "errors": sum(bool(r["error"]) for r in rows.values()),
                         "controls": ctl}
    decision = {}
    if BASELINE in per and ARM in per:
        controls = {**study_controls(offline_scores), "live_controls": all(s["controls"]["passed"] for s in summary.values())}
        common = all(v for k, v in controls.items() if k not in MATCHER_CONTROLS)
        audited = all(controls[k] for k in MATCHER_CONTROLS)
        for rule in ("owner", "strict", "reading_b"):
            arm = with_rule(per[ARM], rule) if rule != "owner" else per[ARM]
            base = with_rule(per[BASELINE], rule) if rule != "owner" else per[BASELINE]
            a = compare(arm, base)
            # The strict rule does not rest on the matcher, so the audits do not gate it.
            passed = common and (audited or rule == "strict")
            a["controls_passed"] = passed
            a["valid"] = a["significant"] and a["guardrails_hold"] and passed
            decision[rule] = a
        decision["controls"] = controls
        decision["decided_by"] = "owner"
    if BASELINE in per and ARM in per:
        decision["template_reuse"] = template_reuse(per)
    out = {"summary": summary, "decision": decision, "per_probe": per}
    LIVE.mkdir(parents=True, exist_ok=True)
    (LIVE / "scores.json").write_text(json.dumps(out, indent=1) + "\n")
    for n, s in summary.items():
        print(n, {k: v["applied"] for k, v in s["families"].items()}, "controls", "pass" if s["controls"]["passed"] else "FAIL")
    for rule in ("owner", "strict", "reading_b"):
        if rule in decision:
            a = decision[rule]
            print(rule, a["primary"], a["guard_change"], "valid", a["valid"])


# ---- tokens ------------------------------------------------------------------

def tokens():
    agg = defaultdict(lambda: {"calls": 0, "cached": 0, "status_429": 0, "prompt_tokens": 0, "completion_tokens": 0})
    for call in load_jsonl(HERE / "evidence" / "ledger" / "calls.jsonl"):
        a = agg[f"{call['arm']}/{call['phase']}"]
        a["calls"] += 1
        a["cached"] += bool(call.get("cached"))
        a["status_429"] += call.get("status") == 429
        a["prompt_tokens"] += call.get("prompt_tokens", 0)
        a["completion_tokens"] += call.get("completion_tokens", 0)
    out = dict(sorted(agg.items()))
    out["total"] = {k: sum(v[k] for v in agg.values()) for k in ("calls", "cached", "status_429", "prompt_tokens", "completion_tokens")}
    (HERE / "evidence" / "tokens.json").write_text(json.dumps(out, indent=1) + "\n")
    for k, v in out.items():
        print(k, v)


if __name__ == "__main__":
    command = sys.argv[1]
    if command == "equivalence":
        equivalence(sys.argv[2])
    else:
        {"check": check, "offline": offline, "live": live, "tokens": tokens}[command]()
