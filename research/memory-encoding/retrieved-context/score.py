"""Score #902's retrieved-context run with #890's matching (../validation/score.py).

    python3 research/memory-encoding/retrieved-context/score.py screen       # evidence/screen.json: what the search at write can link, no model call
    python3 research/memory-encoding/retrieved-context/score.py offline      # evidence/offline/scores.json: reach, controls, admission
    python3 research/memory-encoding/retrieved-context/score.py equivalence  # S re-recorded against #890's committed records
    python3 research/memory-encoding/retrieved-context/score.py live         # evidence/live/scores.json: applied, controls, decision
    python3 research/memory-encoding/retrieved-context/score.py tokens       # evidence/tokens.json

This file adds what preregistration.md fixes for #902: the arm names and
their order, carried reach beside #890's strict reach, the retrieval,
shown-text and well-formedness controls, stale copies after a correction,
summary length, the screen of what the search at write links, and the
decision.
"""

import importlib.util
import json
import sys
from collections import defaultdict
from pathlib import Path

HERE = Path(__file__).resolve().parent
VALIDATION = HERE.parent / "validation"


def _module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


V = _module("validation_score", VALIDATION / "score.py")
P = V.P
states, mcnemar, holm, load_jsonl, covers = P.states, P.mcnemar, P.holm, P.load_jsonl, V.covers

BASELINE = "S"
# Each record directory's arm and setting, as study.Main names them, and
# the arm it is in preregistration.md. #890's committed records are read in
# place: C always, and S where S was not re-recorded here.
NAMES = {"S/b4": "S", "RW/b4-rw-key": "RW-key", "RW/b4-rw": "RW"}
REFERENCE = {"C": VALIDATION / "evidence" / "offline" / "C" / "b4-w8", "S": VALIDATION / "evidence" / "offline" / "S" / "b4"}
# The arms compared with S, in the PM's order: RW-key is primary, RW the
# owner's form is secondary (preregistration.md §Decision).
ORDER = ("RW-key", "RW")
PRIMARY = ("C2", "C3")
CHAINS = ("C2", "C3", "IC2", "IC3")
SETTING = {"RW": "b4-rw", "RW-key": "b4-rw-key"}
# Net production lines before tests, as preregistration.md §Arms states them.
NET_LINES = {"RW": 75, "RW-key": 135}
ALLOWED_LOSS = V.ALLOWED_LOSS
MARGIN, ALPHA, ORACLE_SHARE = V.MARGIN, V.ALPHA, V.ORACLE_SHARE
WELL_FORMED = 0.90
BASE_LAST = 4800


def load(path):
    with open(path) as f:
        return json.load(f)


def corpus():
    return load(HERE / "corpus.json")


# ---- recordings --------------------------------------------------------------

def recording(setting):
    path = HERE / "arms" / "retrieved" / "evidence" / f"{setting}.jsonl"
    return {r["first"]: r for r in load_jsonl(path)} if path.exists() else {}


def s_summaries():
    """S's recorded summary of every stream batch, by its first source."""
    return {r["first"]: r["summary"].strip() for r in load_jsonl(HERE / "evidence" / "synthesis" / "stream-b4.jsonl")}


def c_summaries():
    """C's recorded summary and context line of every stream batch (#891)."""
    rows = load_jsonl(VALIDATION / "arms" / "context" / "evidence" / "context-b4-w8.jsonl")
    return {r["first"]: (r.get("summary") or "").strip() for r in rows if not r.get("failed")}, \
        {r["first"]: (r.get("context") or "").strip() for r in rows if not r.get("failed")}


def shown(arm, s, recorded):
    """The summary each stream batch's mid shows under an arm: RW's own
    where it wrote one, else S's."""
    if arm != "RW":
        return dict(s)
    out = dict(s)
    for first, r in recorded.items():
        if not r.get("failed"):
            out[first] = r["summary"].strip()
    return out


# ---- reach --------------------------------------------------------------------

OFFLINE = HERE / "evidence" / "offline"


def carried(items, hop):
    """Some delivered item states both of the hop's entities, whether or not
    it covers the hop's statement: a mid written later can carry it."""
    return any(states(it["text"], hop["key"]) for it in items)


def score_setting(c, probes, oracle):
    """#890's rows, with carried reach for every chain family; #890's strict
    reach is kept as reached_strict."""
    by_probe = {p["probe"]: p for p in probes}
    rows = V.score_setting(c, probes, oracle)
    for ch in c["chains"]:
        items = by_probe[ch["id"]]["items"]
        row = rows[ch["id"]]
        row["reached_strict"] = row["reached"]
        row["carried"] = [carried(items, h) for h in ch["hops"]]
        row["reached"] = all(row["carried"])
    for g in c["guards"]:
        if g["family"] == "KU":
            rows[g["id"]]["stale_delivered"] = stale_delivered(by_probe[g["id"]]["items"], g)
    return rows


def stale_delivered(items, guard):
    """A KU probe's stale-copy failure: a delivered mid written after the
    correction states the superseded value. Such an item is never reach:
    KU reach is #890's, the correction's own mid stating the new value."""
    change = guard["sources"][1]
    return any(it["first"] > change and states(it["text"], guard["obsolete"]) for it in items)


def records_of(d):
    return load_jsonl(d / "probes.jsonl") + load_jsonl(d / "oracle.jsonl")


def offline_records():
    found = {}
    for d in sorted(p for a in OFFLINE.iterdir() if a.is_dir() for p in a.iterdir() if p.is_dir()) if OFFLINE.exists() else []:
        if (d / "probes.jsonl").exists() and (d / "built.json").exists():
            key = f"{load(d / 'built.json')['arm']}/{d.name}"
            if key in NAMES:
                found[NAMES[key]] = d
    for name, d in REFERENCE.items():
        if name not in found and (d / "probes.jsonl").exists():
            found[name] = d
    return found


def compare(rows, base, families, field="reached"):
    ids = sorted(i for i, r in base.items() if r["family"] in families)
    b, cc, p = mcnemar([bool(rows[i][field]) for i in ids], [bool(base[i][field]) for i in ids])
    return {"n": len(ids), "arm": sum(bool(rows[i][field]) for i in ids), "S": sum(bool(base[i][field]) for i in ids),
            "arm_only": b, "S_only": cc, "p": p}


def margin_met(prim):
    """#890's admission margin: at least MARGIN of the family more than S,
    more discordant pairs for the arm, and exact McNemar p below ALPHA."""
    return prim["arm"] - prim["S"] >= MARGIN * prim["n"] and prim["arm_only"] > prim["S_only"] and prim["p"] < ALPHA


# ---- controls -----------------------------------------------------------------

def retrieval_control(recorded, stream_firsts):
    """Every recorded search at write read only what ends before its batch,
    within its bounds; and at least WELL_FORMED of the stream's batches hold
    an accepted answer."""
    late = sum(any(m >= r["first"] for m in r["messages"]) or any(m["last"] >= r["first"] for m in r["mids"]) for r in recorded.values())
    over = sum(len(r["messages"]) > 8 or len(r["mids"]) > 4 for r in recorded.values())
    failed = sum(bool(r.get("failed")) for r in recorded.values())
    missing = len(set(stream_firsts) - set(recorded))
    n = len(stream_firsts)
    return {"batches": n, "recorded": len(recorded), "missing": missing, "late": late, "over_bound": over, "failed": failed,
            "accepted_share": round((n - failed - missing) / max(n, 1), 4),
            "passed": bool(recorded) and not missing and not late and not over and (n - failed) >= WELL_FORMED * n}


def render(related):
    """relatedLine in arms/retrieved/synthesis.go."""
    parts = []
    if related.get("entities"):
        parts.append("Related to: " + ", ".join(related["entities"]))
    if related.get("mids"):
        parts.append("earlier mids " + ", ".join(str(m) for m in related["mids"]))
    return "; ".join(parts) + "." if parts else ""


def line_control(recorded):
    """RW-key: every stored line is the rendering of what the call named,
    names only retrieved mids, and holds at most 6 names."""
    bad = 0
    for r in recorded.values():
        if r.get("failed"):
            continue
        rel = r.get("related") or {}
        ids = {m["id"] for m in r["mids"]}
        bad += r.get("line", "") != render(rel) or not set(rel.get("mids") or []) <= ids or len(rel.get("entities") or []) > 6 or bool(r.get("summary"))
    return {"lines": sum(bool(r.get("line")) for r in recorded.values()), "bad": bad, "passed": bool(recorded) and bad == 0}


def shown_control(records, expected, lines=None):
    """Every delivered stream mid shows exactly the summary the arm stores
    for its batch, and no related line is ever shown."""
    items = [it for r in records for it in r["items"] if it["level"] == "mid" and it["first"] > BASE_LAST]
    equal = sum(it["text"].strip() == expected.get(it["first"]) for it in items)
    leaked = 0
    if lines:
        leaked = sum(any(ln and ln in it["text"] for ln in (lines.get(it["first"]), "Related to: ", "earlier mids ")) for it in items)
    return {"stream_items": len(items), "equal": equal, "line_shown": leaked, "passed": bool(items) and equal == len(items) and leaked == 0}


def oracle_control(rows):
    hops = [o for r in rows.values() for o in (r.get("oracle") or [])]
    return {"hops": len(hops), "reached": sum(bool(o) for o in hops),
            "passed": bool(hops) and sum(bool(o) for o in hops) >= ORACLE_SHARE * len(hops)}


# ---- measures -------------------------------------------------------------------

def stale_copies(c, texts):
    """For every KU item, the stream mids written after the correction whose
    text restates the superseded value, and of those the ones that do not
    also state the new value."""
    out = {}
    for g in c["guards"]:
        if g["family"] != "KU":
            continue
        change = g["sources"][1]
        later = [f for f, t in texts.items() if f > change and states(t, g["obsolete"])]
        out[g["id"]] = {"copies": len(later), "without_new": sum(not states(texts[f], g["key"]) for f in later)}
    return {"items_with_copies": sum(v["copies"] > 0 for v in out.values()), "copies": sum(v["copies"] for v in out.values()),
            "copies_without_new": sum(v["without_new"] for v in out.values()), "per_item": out}


def summary_length(texts):
    return round(sum(len(t) for t in texts.values()) / max(len(texts), 1), 1)


# ---- screen: what the search at write links ------------------------------------

def links(c, recorded):
    """Of every pair of neighbouring chain hops, whether the later-written
    hop's search found the earlier one: its statement among the messages,
    or a mid covering it. Likewise an SC item's introduction from its
    evidence, a KU item's first statement from its change, and an NC item's
    introduction from its second exchange (the risks)."""
    batch = lambda source: 4 * ((source - 1) // 4) + 1

    def found(later, earlier):
        r = recorded.get(batch(later))
        if r is None:
            return None
        by_message = earlier in r["messages"]
        by_mid = any(m["first"] <= earlier <= m["last"] for m in r["mids"])
        return by_message, by_mid

    out = {f: {"pairs": 0, "by_message": 0, "by_mid": 0, "either": 0} for f in CHAINS + ("SC", "KU", "NC")}

    def add(f, later, earlier):
        got = found(later, earlier)
        if got is None:
            return
        e = out[f]
        e["pairs"] += 1
        e["by_message"] += got[0]
        e["by_mid"] += got[1]
        e["either"] += got[0] or got[1]

    for ch in c["chains"]:
        if ch["family"] in CHAINS:
            hops = ch["hops"]
            for a, b in zip(hops, hops[1:]):
                later, earlier = max(a["source"], b["source"]), min(a["source"], b["source"])
                add(ch["family"], later, earlier)
        elif ch["family"] == "SC":
            add("SC", ch["hops"][0]["source"], ch["intro"]["source"])
    for g in c["guards"]:
        if g["family"] in ("KU", "NC"):
            add(g["family"], g["sources"][1], g["sources"][0])
    return out


def linked(r, earlier):
    """The search that wrote r found the source earlier, as a message or
    through a mid covering it."""
    return earlier in r["messages"] or any(m["first"] <= earlier <= m["last"] for m in r["mids"])


def chains_linked(c, recorded):
    """Per chain family, the chains whose every pair of neighbouring hops is
    linked: the most chains a mid written with retrieved context can carry,
    so the ceiling on carried reach through writing alone."""
    out = {f: {"n": 0, "every_pair": 0} for f in CHAINS}
    for ch in c["chains"]:
        if ch["family"] not in CHAINS:
            continue
        pairs = [(max(a["source"], b["source"]), min(a["source"], b["source"])) for a, b in zip(ch["hops"], ch["hops"][1:])]
        out[ch["family"]]["n"] += 1
        out[ch["family"]]["every_pair"] += all(linked(recorded[4 * ((later - 1) // 4) + 1], earlier) for later, earlier in pairs)
    return out


def screen():
    c = corpus()
    screened = {r["first"]: r for r in load_jsonl(HERE / "arms" / "retrieved" / "evidence" / "screen-b4.jsonl")}
    out = {"batches": len(screened), "links": links(c, screened), "chains": chains_linked(c, screened)}
    (HERE / "evidence" / "screen.json").write_text(json.dumps(out, indent=1) + "\n")
    print(json.dumps(out["links"]))
    print(json.dumps(out["chains"]))


# ---- offline ---------------------------------------------------------------------

def offline():
    c = corpus()
    s = s_summaries()
    c_text, c_context = c_summaries()
    stream_firsts = sorted(s)
    found = offline_records()
    results, per = {}, {}
    for name, d in found.items():
        rows = score_setting(c, load_jsonl(d / "probes.jsonl"), load_jsonl(d / "oracle.jsonl"))
        built = load(d / "built.json") if (d / "built.json").exists() else None
        r = V.summarize(rows, built) if built else {}
        per[name] = rows
        fam = defaultdict(list)
        for row in rows.values():
            fam[row["family"]].append(row)
        r["strict"] = {f: sum(bool(x["reached_strict"]) for x in rs) for f, rs in fam.items() if "reached_strict" in rs[0]}
        r["stale_delivered"] = sum(x["stale_delivered"] for x in fam.get("KU", []))
        r["oracle"] = oracle_control(rows)
        results[name] = r
    texts = {"S": s, "C": {f: (c_text[f] + " " + c_context.get(f, "")).strip() for f in c_text}}
    controls = {}
    for arm in ORDER:
        recorded = recording(SETTING[arm])
        if not recorded:
            continue
        shown_text = shown(arm, s, recorded)
        lines = {f: r.get("line", "") for f, r in recorded.items()} if arm == "RW-key" else None
        texts[arm] = shown_text if arm == "RW" else {f: (shown_text[f] + " " + (lines.get(f) or "")).strip() for f in shown_text}
        ctl = {"retrieval": retrieval_control(recorded, stream_firsts)}
        if arm == "RW-key":
            ctl["line"] = line_control(recorded)
        if arm in found:
            ctl["shown"] = shown_control(records_of(found[arm]), shown_text, lines)
            ctl["oracle"] = results[arm]["oracle"]
        ctl["passed"] = all(v["passed"] for v in ctl.values()) and "shown" in ctl
        controls[arm] = ctl
        results.setdefault(arm, {})["links"] = links(c, recorded)
    if "S" in found:
        # The shown-text comparison can fail: S's own records pass it only
        # against S's summaries.
        controls["S"] = {"shown": shown_control(records_of(found["S"]), s)}
    measures = {name: {"stale": stale_copies(c, t), "summary_chars": summary_length(t)} for name, t in texts.items()}
    base = per.get(BASELINE)
    if base:
        for name in per:
            if name == BASELINE:
                continue
            r = results[name]
            r["against_S"] = {f: compare(per[name], base, (f,)) for f in ("C2", "C3", "IC2", "IC3", "SC", "UP", "KU", "E", "K", "C1")}
            r["against_S_strict"] = {f: compare(per[name], base, (f,), "reached_strict") for f in CHAINS + ("SC", "UP")}
            if name in ORDER:
                prim = compare(per[name], base, PRIMARY)
                prim["margin_met"] = margin_met(prim)
                r["primary"] = {"families": list(PRIMARY), **prim}
                r["admitted"] = prim["margin_met"] and controls.get(name, {}).get("passed", False)
    out = {"baseline": BASELINE, "margin": MARGIN, "results": results, "controls": controls, "measures": measures, "per_probe": per}
    OFFLINE.mkdir(parents=True, exist_ok=True)
    (OFFLINE / "scores.json").write_text(json.dumps(out, indent=1) + "\n")
    for name, r in results.items():
        f = r.get("families", {})
        if not f:
            continue
        print(f"{name:7s} C2 {f['C2']['reached']:2d} C3 {f['C3']['reached']:2d} IC {f['IC2']['reached'] + f['IC3']['reached']:2d}"
              f" SC {f['SC']['reached']:2d} UP {f['UP']['reached']:2d} KU {f['KU']['reached']:2d} E {f['E']['reached']:2d} K {f['K']['reached']:2d}"
              f" NCmiss {f['NC']['near_miss_with_entity']:2d} KUstale {r['stale_delivered']:2d} strict {r['strict']} oracle {r['oracle']['reached']}/{r['oracle']['hops']}"
              f" admitted {r.get('admitted')}")
    print(json.dumps({n: {k: v for k, v in m.items() if k != "stale"} | {"stale": {k: v for k, v in m["stale"].items() if k != "per_item"}}
                      for n, m in measures.items()}))


# ---- equivalence -----------------------------------------------------------------

def strip(record):
    return {k: v for k, v in record.items() if k != "seconds"}


def equivalence():
    """S re-recorded here against #890's committed S records, field by
    field, timing excepted."""
    lines = []
    for f in ("probes.jsonl", "oracle.jsonl"):
        mine, theirs = OFFLINE / "S" / "b4" / f, VALIDATION / "evidence" / "offline" / "S" / "b4" / f
        a, b = [strip(r) for r in load_jsonl(mine)], [strip(r) for r in load_jsonl(theirs)]
        lines.append(f"S/b4 {f}: {len(a)} records, {sum(x != y for x, y in zip(a, b)) + abs(len(a) - len(b))} differ")
    (HERE / "evidence" / "equivalence.txt").write_text("\n".join(lines) + "\n")
    print("\n".join(lines))


# ---- live -----------------------------------------------------------------------

LIVE = HERE / "evidence" / "live"


def compare_live(per, controls):
    """Each live arm against S on C2 and C3: exact McNemar on applied, Holm
    across the arms compared; IC with its pairs; each guard's change."""
    base = per.get(BASELINE)
    if not base:
        return {}
    compared = [n for n in ORDER if n in per]
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
        out[n] = {"primary": {"families": list(PRIMARY), **pair(PRIMARY)}, "implicit": pair(("IC2", "IC3")), "guard_change": guard,
                  "guardrails_hold": all(guard[f] >= -ALLOWED_LOSS[f] for f in guard),
                  "controls_passed": controls.get(n, {}).get("passed", False)}
        ps.append(out[n]["primary"]["p"])
    for n, adjusted in zip(compared, holm(ps)):
        a = out[n]
        a["primary"]["holm_p"] = adjusted
        a["significant"] = a["primary"]["arm_only"] > a["primary"]["S_only"] and adjusted < ALPHA
        a["valid"] = a["significant"] and a["guardrails_hold"] and a["controls_passed"]
    return out


def decide(comparisons):
    """RW-key if it is valid, else RW if it is, else none."""
    return next((n for n in ORDER if comparisons.get(n, {}).get("valid")), None)


def live():
    c = corpus()
    per = {}
    for d in sorted(LIVE.glob("*/*/results.jsonl")):
        key = f"{d.parent.parent.name}/{d.parent.name}"
        if key in NAMES:
            per[NAMES[key]] = V.score_live(c, load_jsonl(d))
    offline_scores = load(OFFLINE / "scores.json") if (OFFLINE / "scores.json").exists() else {}
    comparisons = compare_live(per, offline_scores.get("controls", {}))
    out = {"comparisons": comparisons, "named": decide(comparisons), "per_probe": per}
    LIVE.mkdir(parents=True, exist_ok=True)
    (LIVE / "scores.json").write_text(json.dumps(out, indent=1) + "\n")
    for n, a in comparisons.items():
        print(n, json.dumps({k: v for k, v in a.items()}))
    print("named:", out["named"])


# ---- tokens ----------------------------------------------------------------------

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
    (HERE / "evidence" / "tokens.json").write_text(json.dumps(out, indent=1) + "\n")
    for k, v in out.items():
        print(k, v)


if __name__ == "__main__":
    {"screen": screen, "offline": offline, "equivalence": equivalence, "live": live, "tokens": tokens}[sys.argv[1]]()
