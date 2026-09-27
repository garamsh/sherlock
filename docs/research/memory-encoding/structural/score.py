"""Score #896's structural safeguards run with #890's matching (../validation/score.py).

    python3 docs/research/memory-encoding/structural/score.py offline      # evidence/offline/scores.json: reach, controls, admission
    python3 docs/research/memory-encoding/structural/score.py equivalence  # S and L re-recorded against #890's committed records
    python3 docs/research/memory-encoding/structural/score.py live         # evidence/live/scores.json: applied, controls, decision
    python3 docs/research/memory-encoding/structural/score.py tokens       # evidence/tokens.json

This file adds what preregistration.md fixes for #896: the arm names, each
arm's primary family, pointer reach and SC value reach, every arm's
mechanism controls, and the decision among the arms that went live.
"""

import importlib.util
import json
import re
import sys
from collections import defaultdict
from pathlib import Path

HERE = Path(__file__).resolve().parent


def _module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


V = _module("validation_score", HERE.parent / "validation" / "score.py")
P = V.P
states, mcnemar, holm, load_jsonl, covers = P.states, P.mcnemar, P.holm, P.load_jsonl, V.covers

BASELINE = "S"
# Each record directory's arm and setting, as study.Main names them, and
# the arm it is in preregistration.md.
NAMES = {"S/b4": "S", "L/b4-model": "L", "L/b4-model-ptr": "L-ptr", "L/b4-model-attr": "L-attr", "L/b4-model-chk": "L+chk",
         "C/b4-w8-key": "C-key", "L/b4-model-key-ptr": "C-key+L-ptr"}
PRIMARY = {"L": ("C2", "C3"), "L-ptr": ("C2", "C3"), "L-attr": ("C2", "C3"), "L+chk": ("C2", "C3"),
           "C-key": ("SC",), "C-key+L-ptr": ("C2", "C3", "SC")}
# L is the reference and is never compared in the decision.
DECIDED = ("L-ptr", "L-attr", "L+chk", "C-key", "C-key+L-ptr")
POINTERS = ("L-ptr", "C-key+L-ptr")
# Net production lines before tests, as preregistration.md §Arms states them.
NET_LINES = {"L": 270, "L-ptr": 275, "L-attr": 285, "L+chk": 345, "C-key": 55, "C-key+L-ptr": 330}
ALLOWED_LOSS = V.ALLOWED_LOSS
MARGIN, ALPHA, ORACLE_SHARE = V.MARGIN, V.ALPHA, V.ORACLE_SHARE
ATTRIBUTION_CORRECT = 0.90
RANKING_DIFFERS = 0.10
BASE_LAST = 4800

POINTER = re.compile(r'^Linked through "(.+)"; content not shown, read its sources with memory_inspect\.$')
NOTICE = "No stored memory states this fact."
LINE = re.compile(r'^- \[(\d+)\] \((\w+)\): (.*) \[mid:\d+ sources:(\d+)-(\d+); summary, not verified source text\]$')


def load(path):
    with open(path) as f:
        return json.load(f)


def corpus():
    return load(HERE / "corpus.json")


# ---- stored entities, as ../../chained-recall/arms/links/entities.go keys them ----

WORD = re.compile(r"[^\W_]+(?:[-'’][^\W_]+)*")
STOPWORDS = set("""a an the this that these those every each all any some both no not
our their his her its my your we they he she it i you me us them
user assistant what when where which who whom whose why how
and or but if then so also as at by for from in into of on onto to under with without
is are was were be been has have had do does did will would can could should may might must
please thanks thank yes ok okay got noted understood kept sure great good hi hello here there""".split())


def words(text):
    out = []
    for w in WORD.findall(text):
        w = w.lower()
        for suffix in ("'s", "’s"):
            if w.endswith(suffix) and len(w) > len(suffix):
                w = w[: -len(suffix)]
                break
        out.append(w)
    return out


def normalize(name):
    ws = words(name)
    while ws and ws[0] in STOPWORDS:
        ws = ws[1:]
    return " ".join(ws)


def names(raw):
    out, seen = [], set()
    for n in raw:
        n = normalize(n)
        if n and n not in seen:
            seen.add(n)
            out.append(n)
    return out


class Entities:
    """Every batch's stored entity names, by the batch's first source, and
    the walk's seeds for a text: the stored names whose words occur,
    contiguously, among the text's words."""

    def __init__(self, rows):
        self.by_first = {r["first"]: names(r.get("entities") or []) for r in rows}
        self.all = {n for ns in self.by_first.values() for n in ns}

    def subject(self, first):
        ns = self.by_first.get(first) or []
        return ns[0] if ns else ""

    def seeds(self, text):
        joined = " " + " ".join(words(text)) + " "
        return sorted(n for n in self.all if " " + n + " " in joined)


def entities():
    return Entities(load_jsonl(HERE / "arms" / "links" / "evidence" / "entities-b4.jsonl"))


def attribution(subject, seeds):
    return f'About "{subject}"; the question names ' + " and ".join(f'"{s}"' for s in seeds) + ". "


def stream_summaries():
    """S's recorded summary of every stream batch, by its first source."""
    return {r["first"]: r["summary"].strip() for r in load_jsonl(HERE / "evidence" / "synthesis" / "stream-b4.jsonl")}


# ---- offline -----------------------------------------------------------------

OFFLINE = HERE / "evidence" / "offline"


def pointed(item):
    """The linking entity of a pointer item, or None."""
    m = POINTER.match(item["text"])
    return m.group(1) if m else None


def hop_reached(items, hop):
    """(interval, stated): some item holds the statement; one of them states
    both of the hop's entities or is a pointer through one of them."""
    ends = {normalize(e) for e in hop["ends"]}
    holding = [it for it in items if covers(it, hop["source"])]
    return bool(holding), any(states(it["text"], hop["key"]) or pointed(it) in ends for it in holding)


def score_setting(c, probes, oracle):
    """#890's rows, with pointer reach for chain hops and value reach for
    SC; #890's strict SC reach is kept as reached_strict."""
    by_probe = {p["probe"]: p for p in probes}
    rows = V.score_setting(c, probes, oracle)
    for ch in c["chains"]:
        items = by_probe[ch["id"]]["items"]
        row = rows[ch["id"]]
        if ch["family"] == "SC":
            row["reached_strict"] = row["reached"]
            row["reached"] = V.stated_in(items, ch["hops"][0]["source"], ch["answer"])
            continue
        hops = [hop_reached(items, h) for h in ch["hops"]]
        row["stated"] = [h[1] for h in hops]
        row["reached"] = all(row["stated"])
    for o in oracle:
        ch = next(x for x in c["chains"] if x["id"] == o["probe"])
        rows[o["probe"]]["oracle"][o["hop"] - 1] = hop_reached(o["items"], ch["hops"][o["hop"] - 1])[0]
    return rows


def offline_records():
    found = {}
    for d in sorted(p for a in OFFLINE.iterdir() if a.is_dir() for p in a.iterdir() if p.is_dir()):
        if (d / "probes.jsonl").exists() and (d / "built.json").exists():
            key = f"{load(d / 'built.json')['arm']}/{d.name}"
            if key in NAMES:
                found[NAMES[key]] = d
    return found


def pointer_control(records):
    """Every walk item of every recall and oracle search is exactly a pointer."""
    walk = [it for r in records for it in r["items"] if it["kind"] == "linked"]
    bad = [it["id"] for it in walk if pointed(it) is None]
    return {"walk_items": len(walk), "not_pointer": len(bad), "passed": bool(walk) and not bad}


def attribution_control(c, probes, ents):
    """L-attr on N and NC: every walk item opens with its attribution exactly
    when its recorded subject is none of the seeds; where the walk delivers
    an N item's associated-value episode, the attribution names the
    associated entity and the queried one; on NC the subject is never the
    queried entity. The recorded subjects are compared with this file's own
    reading of the stored entities, so the check reads what the store holds."""
    guards = {g["id"]: g for g in c["guards"] if g["family"] in ("N", "NC")}
    present = absent_ok = walk = agree = 0
    n_items = n_correct = nc_items = nc_ok = 0
    nc_attributed = nc_attributed_ok = nc_queried = nc_queried_plain = 0
    for p in probes:
        g = guards.get(p["probe"])
        if not g:
            continue
        trace = p.get("trace") or {}
        seeds = trace.get("seeds") or []
        subjects = trace.get("subjects") or {}
        for it in p["items"]:
            if it["kind"] != "linked":
                continue
            walk += 1
            subject = subjects.get(str(it["id"]), "")
            agree += subject == ents.subject(it["first"])
            if subject and subject not in seeds:
                present += it["text"].startswith(attribution(subject, seeds))
            else:
                absent_ok += not it["text"].startswith('About "')
            if g["family"] == "N" and covers(it, g["sources"][1]):
                n_items += 1
                n_correct += subject == normalize(g["associated"]) and it["text"].startswith(attribution(subject, seeds)) and normalize(g["entity"]) in seeds
            if g["family"] == "NC" and subject:
                nc_items += 1
                nc_ok += subject != normalize(g["entity"])
                shows = it["text"].startswith('About "')
                if shows:
                    nc_attributed += 1
                    nc_attributed_ok += subject != normalize(g["entity"])
                if subject == normalize(g["entity"]):
                    nc_queried += 1
                    nc_queried_plain += not shows
    base = walk > 0 and present + absent_ok == walk and agree == walk and n_items > 0 and n_correct >= ATTRIBUTION_CORRECT * n_items
    ok = base and nc_attributed_ok == nc_attributed and nc_queried_plain == nc_queried
    return {"walk_items": walk, "attributed": present, "plain_where_subject_is_a_seed": absent_ok, "subjects_agree_with_store": agree,
            "n_associated_items": n_items, "n_correct": n_correct,
            "nc_attributed": nc_attributed, "nc_attributed_subject_not_queried": nc_attributed_ok,
            "nc_subject_queried": nc_queried, "nc_subject_queried_shown_plain": nc_queried_plain,
            "as_written": {"nc_items": nc_items, "nc_subject_not_queried": nc_ok, "passed": base and nc_ok == nc_items}, "passed": ok}


def key_text_control(probes, summaries, skip_linked=False):
    """Every delivered stream mid shows S's recorded summary of its batch."""
    checked = equal = 0
    for p in probes:
        for it in p["items"]:
            if it.get("level") != "mid" or it["first"] <= BASE_LAST or (skip_linked and it["kind"] == "linked"):
                continue
            checked += 1
            equal += it["text"].strip() == summaries.get(it["first"])
    return {"stream_items": checked, "equal_to_S_summary": equal, "passed": checked > 0 and equal == checked}


def ranking_control(probes, base):
    """How many probes deliver a different mid list from S's."""
    s = {p["probe"]: [it["id"] for it in p["items"]] for p in base}
    differ = sum([it["id"] for it in p["items"]] != s[p["probe"]] for p in probes)
    return {"probes": len(probes), "differ_from_S": differ, "passed": differ >= RANKING_DIFFERS * len(probes)}


def check_control(c, probes, oracle):
    """L+chk offline: every record that held a walk mid ran the check; and
    per family how often it ran, named a set, connected it, kept nothing."""
    fam = {x["id"]: x["family"] for x in c["chains"] + c["guards"]}
    per = defaultdict(lambda: {"probes": 0, "ran": 0, "named": 0, "connected": 0, "disconnected": 0, "notice": 0, "malformed": 0})
    unchecked = 0
    for records, count in ((probes, True), (oracle, False)):
        for r in records:
            rec = (r.get("trace") or {}).get("check") or {}
            if rec.get("walk") and not rec.get("ran"):
                unchecked += 1
            if not count:
                continue
            e = per[fam[r["probe"]]]
            e["probes"] += 1
            if rec.get("ran"):
                e["ran"] += 1
                e["named"] += bool(rec["named"])
                e["connected"] += rec["connected"]
                e["disconnected"] += bool(rec["named"]) and not rec["connected"]
                e["notice"] += not rec["connected"]
                e["malformed"] += bool(rec.get("malformed"))
    checks = sum(bool(((r.get("trace") or {}).get("check") or {}).get("ran")) for r in probes + oracle)
    return {"checks": checks, "walk_without_check": unchecked, "per_family": dict(sorted(per.items())), "passed": checks > 0 and unchecked == 0}


def controls(name, c, d, per_records, summaries, ents, summary):
    probes, oracle = per_records[name]
    out = {}
    oracle_of = summary["L"]["oracle"] if name == "L+chk" and "L" in summary else summary[name]["oracle"]
    out["oracle"] = {**oracle_of, "from": "L" if name == "L+chk" else name,
                     "passed": oracle_of["reached"] >= ORACLE_SHARE * oracle_of["hops"]}
    if name in POINTERS:
        out["pointer"] = pointer_control(probes + oracle)
    if name == "L-attr":
        out["attribution"] = attribution_control(c, probes, ents)
    if name in ("C-key", "C-key+L-ptr"):
        out["shown_text"] = key_text_control(probes, summaries, skip_linked=name == "C-key+L-ptr")
    if name == "C-key":
        out["ranking"] = ranking_control(probes, per_records[BASELINE][0])
    if name == "L+chk":
        out["check"] = check_control(c, probes, oracle)
    if name == BASELINE:
        # The comparison C-key's shown-text control makes, on S's own
        # records: it can fail, since S's stream items must pass it.
        out["shown_text"] = key_text_control(probes, summaries)
    out["passed"] = all(v["passed"] for v in out.values())
    return out


def compare(rows, base, families):
    return V.compare(rows, base, families)


def margin_met(prim):
    """#890's offline margin: at least MARGIN of the family more than S,
    more pairs won than lost, exact McNemar p below ALPHA."""
    return prim["arm"] - prim["S"] >= MARGIN * prim["n"] and prim["arm_only"] > prim["S_only"] and prim["p"] < ALPHA


def offline():
    c = corpus()
    summaries, ents = stream_summaries(), entities()
    records = offline_records()
    per, summary, per_records = {}, {}, {}
    for name, d in records.items():
        probes, oracle = load_jsonl(d / "probes.jsonl"), load_jsonl(d / "oracle.jsonl")
        per_records[name] = (probes, oracle)
        rows = score_setting(c, probes, oracle)
        per[name] = rows
        summary[name] = V.summarize(rows, load(d / "built.json"))
        summary[name]["families"]["SC"]["reached_strict"] = sum(bool(r["reached_strict"]) for r in rows.values() if r["family"] == "SC")
    results = {}
    for name in summary:
        r = {**summary[name], "controls": controls(name, c, records[name], per_records, summaries, ents, summary)}
        if name != BASELINE and BASELINE in per:
            r["against_S"] = {f: compare(per[name], per[BASELINE], (f,)) for f in ("C2", "C3", "SC", "IC2", "IC3", "UP", "C1", "E", "K", "KU")}
            if name in PRIMARY:
                prim = compare(per[name], per[BASELINE], PRIMARY[name])
                prim["margin_met"] = margin_met(prim)
                r["primary"] = {"families": list(PRIMARY[name]), **prim}
                r["admitted"] = name in DECIDED and prim["margin_met"] and r["controls"]["passed"]
        results[name] = r
    out = {"baseline": BASELINE, "margin": MARGIN, "results": results, "per_probe": per}
    (OFFLINE / "scores.json").write_text(json.dumps(out, indent=1) + "\n")
    for name, r in results.items():
        f = r["families"]
        print(f"{name:12s} C2 {f['C2']['reached']:2d} C3 {f['C3']['reached']:2d} SC {f['SC']['reached']:2d} (strict {f['SC']['reached_strict']:2d})"
              f" C1 {f['C1']['reached']:2d} E {f['E']['reached']:2d} K {f['K']['reached']:2d} KU {f['KU']['reached']:2d}"
              f" N in view {f['N']['near_miss_in_view']:2d} NC in view {f['NC']['near_miss_in_view']:2d} beside {f['NC']['near_miss_with_entity']:2d}"
              f" ctx {r['context_chars_per_turn']:6.0f} oracle {r['oracle']['reached']}/{r['oracle']['hops']}"
              f" controls {'pass' if r['controls']['passed'] else 'FAIL'} admitted {r.get('admitted')}")


def equivalence():
    """S and L re-recorded here against #890's committed records, every
    field but timing."""
    lines = []
    for arm, setting in (("S", "b4"), ("L", "b4-model")):
        for f in ("built.json", "probes.jsonl", "oracle.jsonl"):
            ours, theirs = OFFLINE / arm / setting / f, HERE.parent / "validation" / "evidence" / "offline" / arm / setting / f
            if f == "built.json":
                a, b = load(ours), load(theirs)
                a.pop("build_seconds", None), b.pop("build_seconds", None)
                same, n = a == b, 1
            else:
                a, b = load_jsonl(ours), load_jsonl(theirs)
                for x in a + b:
                    x.pop("seconds", None)
                same, n = a == b, len(a)
            lines.append(f"{arm}/{setting}/{f}: {n} records, {'equal' if same else 'DIFFERENT'}")
    text = "\n".join(lines) + "\n"
    (HERE / "evidence" / "equivalence.txt").write_text(text)
    print(text, end="")


# ---- live --------------------------------------------------------------------

LIVE = HERE / "evidence" / "live"


def recall_lines(recall):
    """(id, kind, text, first, last) of every memory line of a recall block."""
    out = []
    for line in recall.splitlines():
        m = LINE.match(line)
        if m:
            out.append((int(m.group(1)), m.group(2), m.group(3), int(m.group(4)), int(m.group(5))))
    return out


def task_of(result, c):
    """The task the live session recalled for: the inbound message line."""
    query = {x["id"]: x["query"] for x in c["chains"] + c["guards"]}[result["probe"]]
    return f"[msg-{result['probe']}] owner: {query}"


def live_controls(name, c, results, summaries, ents, checks, ledger):
    out = {}
    fam = {x["id"]: x["family"] for x in c["chains"] + c["guards"]}
    if name in POINTERS:
        lines = [l for r in results for l in recall_lines(r.get("recall") or "") if l[1] == "linked"]
        bad = sum(POINTER.match(l[2]) is None for l in lines)
        primary = [r for r in results if fam[r["probe"]] in ("C2", "C3")]
        with_walk = sum("] (linked):" in (r.get("recall") or "") for r in primary)
        out["pointer"] = {"linked_lines": len(lines), "not_pointer": bad, "primary_probes": len(primary), "with_walk": with_walk,
                          "passed": bool(lines) and bad == 0 and with_walk >= len(primary) / 2}
    if name == "L-attr":
        walk = present = n_items = n_correct = nc_items = nc_ok = 0
        nc_attributed = nc_attributed_ok = nc_queried = nc_queried_plain = 0
        guards = {g["id"]: g for g in c["guards"]}
        for r in results:
            g = guards.get(r["probe"])
            if not g or g["family"] not in ("N", "NC"):
                continue
            seeds = ents.seeds(task_of(r, c))
            for _, kind, text, first, last in recall_lines(r.get("recall") or ""):
                if kind != "linked":
                    continue
                walk += 1
                subject = ents.subject(first)
                if subject and subject not in seeds:
                    present += text.startswith(attribution(subject, seeds))
                else:
                    present += not text.startswith('About "')
                if g["family"] == "N" and first <= g["sources"][1] <= last:
                    n_items += 1
                    n_correct += subject == normalize(g["associated"]) and text.startswith(attribution(subject, seeds)) and normalize(g["entity"]) in seeds
                if g["family"] == "NC" and subject:
                    nc_items += 1
                    nc_ok += subject != normalize(g["entity"])
                    shows = text.startswith('About "')
                    if shows:
                        nc_attributed += 1
                        nc_attributed_ok += subject != normalize(g["entity"])
                    if subject == normalize(g["entity"]):
                        nc_queried += 1
                        nc_queried_plain += not shows
        base = walk > 0 and present == walk and n_items > 0 and n_correct >= ATTRIBUTION_CORRECT * n_items
        out["attribution"] = {"walk_lines": walk, "as_expected": present, "n_associated_items": n_items, "n_correct": n_correct,
                              "nc_attributed": nc_attributed, "nc_attributed_subject_not_queried": nc_attributed_ok,
                              "nc_subject_queried": nc_queried, "nc_subject_queried_shown_plain": nc_queried_plain,
                              "as_written": {"nc_items": nc_items, "nc_subject_not_queried": nc_ok, "passed": base and nc_ok == nc_items},
                              "passed": base and nc_attributed_ok == nc_attributed and nc_queried_plain == nc_queried}
    if name in ("C-key", "C-key+L-ptr", BASELINE):
        checked = equal = 0
        for r in results:
            for _, kind, text, first, _ in recall_lines(r.get("recall") or ""):
                if first <= BASE_LAST or (name == "C-key+L-ptr" and kind == "linked"):
                    continue
                checked += 1
                equal += text.strip() == summaries.get(first)
        out["shown_text"] = {"stream_lines": checked, "equal_to_S_summary": equal, "passed": checked > 0 and equal == checked}
    if name == "L+chk":
        last = {}
        for rec in checks:
            last[rec["probe"]] = max(last.get(rec["probe"], ""), rec["opened"])
        final = [rec for rec in checks if rec["opened"] == last[rec["probe"]]]
        calls = defaultdict(int)
        for call in ledger:
            if call["arm"] == "L" and call["phase"] == "check" and call["episode"] in last:
                calls[call["episode"]] += 1
        ran = defaultdict(int)
        for rec in final:
            ran[rec["probe"]] += rec["ran"]
        unchecked = sum(bool(rec["walk"]) and not rec["ran"] for rec in final)
        short = sorted(p for p, n in ran.items() if calls[p] < n)
        per = defaultdict(lambda: {"probes": 0, "recall_checked": 0, "named": 0, "connected": 0, "disconnected": 0, "notice": 0, "malformed": 0, "search_checks": 0})
        for r in results:
            per[fam[r["probe"]]]["probes"] += 1
        for rec in final:
            e = per[fam[rec["probe"]]]
            if not rec["ran"]:
                continue
            if rec["call"] == "search":
                e["search_checks"] += 1
                continue
            e["recall_checked"] += 1
            e["named"] += bool(rec["named"])
            e["connected"] += rec["connected"]
            e["disconnected"] += bool(rec["named"]) and not rec["connected"]
            e["notice"] += not rec["connected"]
            e["malformed"] += bool(rec.get("malformed"))
        probes = {r["probe"] for r in results}
        out["check"] = {"records": len(final), "checks": sum(ran.values()), "walk_without_check": unchecked, "probes_short_in_ledger": short,
                        "probes_without_record": sorted(probes - set(last)), "per_family": dict(sorted(per.items())),
                        "passed": sum(ran.values()) > 0 and unchecked == 0 and not short}
    out["passed"] = all(v["passed"] for v in out.values())
    return out


def compare_live(per, admitted):
    base = per.get(BASELINE)
    if not base:
        return {}
    compared = [n for n in DECIDED if n in per and n in admitted]
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
        failed = [f for f in guard if guard[f] < -ALLOWED_LOSS[f]]
        out[n] = {"primary": {"families": list(PRIMARY[n]), **pair(PRIMARY[n])}, "implicit": pair(("IC2", "IC3")), "guard_change": guard,
                  "guardrails_failed": failed, "lost_beyond": sum(-guard[f] - ALLOWED_LOSS[f] for f in failed), "guardrails_hold": not failed}
        ps.append(out[n]["primary"]["p"])
    for n, adjusted in zip(compared, holm(ps)):
        a = out[n]
        a["primary"]["holm_p"] = adjusted
        a["significant"] = a["primary"]["arm_only"] > a["primary"]["S_only"] and adjusted < ALPHA
    return out


def decide(comparisons, live_ok):
    """The valid arm with the fewest net lines; else the #888 ranking, as a
    description."""
    for n, a in comparisons.items():
        a["controls_passed"] = live_ok.get(n, False)
        a["valid"] = a["significant"] and a["guardrails_hold"] and a["controls_passed"]
    valid = [n for n, a in comparisons.items() if a["valid"]]
    ranking = sorted(comparisons, key=lambda n: (len(comparisons[n]["guardrails_failed"]), comparisons[n]["lost_beyond"],
                                                 -(comparisons[n]["primary"]["arm_only"] - comparisons[n]["primary"]["S_only"]), NET_LINES[n]))
    return {"best_valid": min(valid, key=lambda n: NET_LINES[n]) if valid else None, "ranking_if_none_valid": ranking}


def live():
    c = corpus()
    summaries, ents = stream_summaries(), entities()
    ledger = load_jsonl(HERE / "evidence" / "ledger" / "calls.jsonl") if (HERE / "evidence" / "ledger" / "calls.jsonl").exists() else []
    admitted = {n for n, r in load(OFFLINE / "scores.json")["results"].items() if r.get("admitted")}
    per, summary, live_ok = {}, {}, {}
    for d in sorted(LIVE.glob("*/*/results.jsonl")):
        key = f"{d.parent.parent.name}/{d.parent.name}"
        if key not in NAMES:
            continue
        name = NAMES[key]
        results = load_jsonl(d)
        rows = V.score_live(c, results)
        for r in results:
            rows[r["probe"]]["inspects"] = sum(t["name"] == "memory_inspect" for t in r.get("tools") or [])
        per[name] = rows
        checks = load_jsonl(d.parent / "checks.jsonl") if (d.parent / "checks.jsonl").exists() else []
        ctl = live_controls(name, c, results, summaries, ents, checks, ledger)
        live_ok[name] = ctl["passed"]
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
            "inspects_per_probe": round(sum(r["inspects"] for r in rows.values()) / max(len(rows), 1), 2),
            "rerun": sum(r["attempt"] > 1 for r in rows.values()),
            "errors": sum(bool(r["error"]) for r in rows.values()),
            "controls": ctl,
        }
    comparisons = compare_live(per, admitted)
    decision = decide(comparisons, live_ok)
    for n, a in comparisons.items():
        summary[n]["against_S"] = a
    out = {"summary": summary, "decision": decision, "per_probe": per}
    (LIVE / "scores.json").write_text(json.dumps(out, indent=1) + "\n")
    for n, s in summary.items():
        f = s["families"]
        print(n, {k: f[k]["applied"] for k in f}, "controls", "pass" if s["controls"]["passed"] else "FAIL")
    print("decision:", json.dumps(decision))


# ---- tokens ------------------------------------------------------------------

def setting_of(call, windows):
    """The arms/links setting a live call of arm L belongs to, by the run
    windows in evidence/live/run.json."""
    for w in windows:
        if w["arm"] == "L" and w["start"] <= call["time"][:19] + "Z" <= w["end"]:
            return w["setting"] + ("-smoke" if w.get("smoke") else "")
    return "?"


def tokens():
    """Tokens per arm and phase, with arms/links's live calls split by run
    window, and L+chk's check calls and tokens per turn."""
    ledger = load_jsonl(HERE / "evidence" / "ledger" / "calls.jsonl")
    windows = load(LIVE / "run.json") if (LIVE / "run.json").exists() else []
    agg = defaultdict(lambda: {"calls": 0, "status_429": 0, "prompt_tokens": 0, "completion_tokens": 0})
    turn = defaultdict(lambda: {"calls": 0, "prompt_tokens": 0, "completion_tokens": 0})
    for call in ledger:
        key = f"{call['arm']}/{call['phase']}"
        if call["arm"] == "L" and call["phase"] == "live":
            key = f"L/{setting_of(call, windows)}/live"
        if call["arm"] == "L" and call["phase"] == "check" and call["episode"] != "b4-model-chk":
            key = f"L/{setting_of(call, windows)}/check"
            if key != "L/b4-model-chk/check":
                a = agg[key]
                a["calls"] += 1
                a["prompt_tokens"] += call.get("prompt_tokens", 0)
                a["completion_tokens"] += call.get("completion_tokens", 0)
                continue
            t = turn[call["episode"]]
            t["calls"] += 1
            t["prompt_tokens"] += call.get("prompt_tokens", 0)
            t["completion_tokens"] += call.get("completion_tokens", 0)
        a = agg[key]
        a["calls"] += 1
        a["status_429"] += call.get("status") == 429
        a["prompt_tokens"] += call.get("prompt_tokens", 0)
        a["completion_tokens"] += call.get("completion_tokens", 0)
    n = max(len(turn), 1)
    per_turn = {"turns_with_a_check": len(turn), "calls": round(sum(t["calls"] for t in turn.values()) / n, 2),
                "prompt_tokens": round(sum(t["prompt_tokens"] for t in turn.values()) / n, 1),
                "completion_tokens": round(sum(t["completion_tokens"] for t in turn.values()) / n, 1)}
    out = {"phases": dict(sorted(agg.items())), "check_per_turn_with_a_check": per_turn}
    (HERE / "evidence" / "tokens.json").write_text(json.dumps(out, indent=1) + "\n")
    for k, v in out["phases"].items():
        print(k, v)
    print("L+chk per turn with a check:", per_turn)


if __name__ == "__main__":
    {"offline": offline, "equivalence": equivalence, "live": live, "tokens": tokens}[sys.argv[1]]()
