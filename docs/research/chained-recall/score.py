"""Score the #874 offline stage per hop, for every arm and setting.

    python3 docs/research/chained-recall/score.py offline
    python3 docs/research/chained-recall/score.py live
    python3 docs/research/chained-recall/score.py tokens

Reads evidence/offline/<arm>/<setting>/{built.json,probes.jsonl,oracle.jsonl}
as study.Offline writes them and writes evidence/offline/scores.json.

A hop is reached when a delivered item's source interval holds the hop's
statement and the item's text states both entities the hop joins: the
interval says the item came from the evidence, the entities say the item
still carries it after summarization. A chain is reached when every hop
is. Values are matched as the agent-context scorer matches them.
"""

import importlib.util
import json
import re
import sys
from collections import defaultdict
from pathlib import Path

HERE = Path(__file__).resolve().parent
OFFLINE = HERE / "evidence" / "offline"
BASELINE = ("S", "b4")


def _agent_context_score():
    spec = importlib.util.spec_from_file_location("agent_context_score", HERE.parent / "agent-context" / "score.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


AC = _agent_context_score()
states, wilson, mcnemar, holm = AC.states, AC.wilson, AC.mcnemar, AC.holm


def load_jsonl(path):
    with open(path) as f:
        return [json.loads(line) for line in f if line.strip()]


def covers(item, source):
    return item["first"] <= source <= item["last"]


def hop_reached(items, hop):
    """(interval, stated): some item holds the statement; some item holding
    it also states both of the hop's entities."""
    holding = [it for it in items if covers(it, hop["source"])]
    return bool(holding), any(states(it["text"], hop["key"]) for it in holding)


def guard_reached(items, guard):
    holding = [it for it in items for s in guard["evidence"] if covers(it, s)]
    if guard["family"] == "N":
        return None
    return any(states(it["text"], guard["key"]) for it in holding)


def score_setting(corpus, probes, oracle):
    by_probe = {p["probe"]: p for p in probes}
    chains = {}
    for c in corpus["chains"]:
        p = by_probe[c["id"]]
        hops = [hop_reached(p["items"], h) for h in c["hops"]]
        chains[c["id"]] = {
            "family": c["family"], "k": c["k"],
            "interval": [h[0] for h in hops], "stated": [h[1] for h in hops],
            "chain": all(h[1] for h in hops), "context_chars": p["context_chars"], "items": len(p["items"]),
        }
    for o in oracle:
        c = next(c for c in corpus["chains"] if c["id"] == o["probe"])
        interval, stated = hop_reached(o["items"], c["hops"][o["hop"] - 1])
        chains[o["probe"]].setdefault("oracle", [None] * c["k"])[o["hop"] - 1] = interval
    guards = {}
    for g in corpus["guards"]:
        p = by_probe[g["id"]]
        guards[g["id"]] = {"family": g["family"], "reached": guard_reached(p["items"], g), "context_chars": p["context_chars"]}
    return chains, guards


def summarize(chains, guards, built):
    out = {"families": {}, "by_k": {}}
    fam = defaultdict(list)
    for c in chains.values():
        fam[c["family"]].append(c)
    for f, cs in sorted(fam.items()):
        n = len(cs)
        hops = len(cs[0]["stated"])
        out["families"][f] = {
            "n": n,
            "chain": sum(c["chain"] for c in cs), "chain_wilson": wilson(sum(c["chain"] for c in cs), n),
            "hop_stated": [sum(c["stated"][h] for c in cs) for h in range(hops)],
            "hop_interval": [sum(c["interval"][h] for c in cs) for h in range(hops)],
            "hop_oracle": [sum(bool(c.get("oracle", [False] * hops)[h]) for c in cs) for h in range(hops)],
        }
    for k in (1, 2, 3):
        cs = [c for c in chains.values() if c["k"] == k]
        out["by_k"][str(k)] = {"n": len(cs), "chain": sum(c["chain"] for c in cs)}
    multi = [c for c in chains.values() if c["k"] >= 2]
    out["primary"] = {"n": len(multi), "chain": sum(c["chain"] for c in multi), "wilson": wilson(sum(c["chain"] for c in multi), len(multi))}
    gf = defaultdict(list)
    for g in guards.values():
        gf[g["family"]].append(g)
    out["guards"] = {f: {"n": len(gs), "reached": sum(bool(g["reached"]) for g in gs) if f != "N" else None} for f, gs in sorted(gf.items())}
    everything = list(chains.values()) + list(guards.values())
    out["context_chars_per_turn"] = round(sum(x["context_chars"] for x in everything) / len(everything), 1)
    out["stored_chars_per_source"] = round(built["stored_chars_per_source"], 4)
    out["stored_chars"] = built["built"]["stored_chars"]
    out["vectors"] = built["built"]["vectors"]
    hops = sum(len(c["stated"]) for c in chains.values())
    out["oracle"] = {"hops": hops, "reached": sum(sum(bool(x) for x in c.get("oracle", [])) for c in chains.values())}
    return out


def clean_chains():
    """Chains none of whose later hops ranks within the check's top 20 at
    batch 4 or 8 (preregistration.md Amendment 2); fixed from the check
    alone."""
    leaky = set()
    for n in (4, 8):
        check = json.load(open(HERE / "evidence" / f"check-b{n}.json"))
        top = check["summary"]["top"]
        leaky |= {r["id"] for r in check["chains"] if any(x <= top for x in r["raw"][1:])}
    corpus = json.load(open(HERE / "corpus.json"))
    return {c["id"] for c in corpus["chains"] if c["id"] not in leaky}


def offline():
    corpus = json.load(open(HERE / "corpus.json"))
    clean = clean_chains()
    results = {}
    per_chain = {}
    for arm_dir in sorted(p for p in OFFLINE.iterdir() if p.is_dir()):
        for setting_dir in sorted(p for p in arm_dir.iterdir() if p.is_dir()):
            if not (setting_dir / "probes.jsonl").exists():
                continue
            built = json.load(open(setting_dir / "built.json"))
            chains, guards = score_setting(corpus, load_jsonl(setting_dir / "probes.jsonl"), load_jsonl(setting_dir / "oracle.jsonl"))
            name = f"{arm_dir.name}/{setting_dir.name}"
            results[name] = summarize(chains, guards, built)
            results[name]["clean"] = summarize({i: c for i, c in chains.items() if i in clean}, guards, built)["primary"]
            per_chain[name] = {"chains": chains, "guards": guards}
    base = per_chain.get("/".join(BASELINE))
    if base:
        ids = sorted(i for i, c in base["chains"].items() if c["k"] >= 2)
        for name, r in results.items():
            if name == "/".join(BASELINE):
                continue
            arm = [per_chain[name]["chains"][i]["chain"] for i in ids]
            shipped = [base["chains"][i]["chain"] for i in ids]
            b, c, p = mcnemar(arm, shipped)
            r["against_S_b4"] = {"arm_only": b, "S_only": c, "p": p}
            clean_ids = [i for i in ids if i in clean]
            b, c, p = mcnemar([per_chain[name]["chains"][i]["chain"] for i in clean_ids], [base["chains"][i]["chain"] for i in clean_ids])
            r["clean_against_S_b4"] = {"arm_only": b, "S_only": c, "p": p}
        compared = [r for n, r in results.items() if "against_S_b4" in r]
        for r, adjusted in zip(compared, holm([r["against_S_b4"]["p"] for r in compared])):
            r["against_S_b4"]["holm_p"] = adjusted
    out = {"baseline": "/".join(BASELINE), "clean_chains": sorted(clean), "results": results, "per_chain": per_chain}
    (OFFLINE / "scores.json").write_text(json.dumps(out, indent=1) + "\n")
    for name, r in results.items():
        vs = r.get("against_S_b4", {})
        print(f"{name:14s} chain k>=2 {r['primary']['chain']:3d}/{r['primary']['n']} clean {r['clean']['chain']:3d}/{r['clean']['n']}  k1 {r['by_k']['1']['chain']:2d} k2 {r['by_k']['2']['chain']:2d} k3 {r['by_k']['3']['chain']:2d}"
              f"  ctx {r['context_chars_per_turn']:7.0f}  stored/src {r['stored_chars_per_source']:.3f}  oracle {r['oracle']['reached']}/{r['oracle']['hops']}  {vs}")


# ---- live -------------------------------------------------------------------

LIVE = HERE / "evidence" / "live"
PRIMARY_FAMILIES = ("C2", "C3", "IC2", "IC3")
GUARD_LOSS = 3


def artifact(result):
    """Everything the turn produced for the person: the reply, the messages
    sent and the files written."""
    return "\n".join([result.get("reply") or ""] + list(result.get("sent") or []) + list((result.get("written") or {}).values()))


def applied(check, result):
    text = artifact(result)
    kind = check["kind"]
    if kind == "contains":
        return states(text, check["value"])
    if kind == "correction":
        return states(text, check["new"])
    if kind == "abstain":
        visible = "\n".join(list(result.get("sent") or [])) or (result.get("reply") or "")
        return bool(re.search(check["pattern"], visible, re.I))
    raise ValueError(kind)


def seen(result):
    """What the turn had in view from memory: the recall block and every
    memory tool result."""
    return [result.get("recall") or ""] + [t["result"] for t in result.get("tools") or [] if t["name"] in ("memory_search", "memory_inspect")]


def followed(result):
    """memory_search calls whose query names something the query of the
    turn did not and an earlier view did: the model following a name."""
    views = [result.get("recall") or ""]
    count = 0
    for t in result.get("tools") or []:
        if t["name"] == "memory_search":
            try:
                q = json.loads(t["input"]).get("query", "") if isinstance(t["input"], str) else t["input"].get("query", "")
            except (ValueError, AttributeError):
                q = ""
            names = {w for w in re.findall(r"[A-Z][a-z]+", q)}
            if any(any(re.search(r"\b" + n + r"\b", v) for v in views) for n in names - PROBE_NAMES.get(result["probe"], set())):
                count += 1
        views.append(t.get("result") or "")
    return count


PROBE_NAMES = {}


def score_live_setting(corpus, results):
    by_probe = {r["probe"]: r for r in results}
    out = {}
    for c in corpus["chains"]:
        PROBE_NAMES[c["id"]] = set(re.findall(r"[A-Z][a-z]+", c["query"]))
    for c in corpus["chains"]:
        r = by_probe.get(c["id"])
        if r is None:
            continue
        views = seen(r)
        hops = [any(states(v, h["key"]) for v in views) for h in c["hops"]]
        out[c["id"]] = {"family": c["family"], "k": c["k"], "applied": applied(c["check"], r), "hops_seen": hops,
                        "all_seen": all(hops), "searches": sum(t["name"] == "memory_search" for t in r.get("tools") or []),
                        "followed": followed(r), "error": r.get("error", ""), "attempt": r.get("attempt", 1)}
    for g in corpus["guards"]:
        r = by_probe.get(g["id"])
        if r is None:
            continue
        out[g["id"]] = {"family": g["family"], "applied": applied(g["check"], r), "error": r.get("error", ""), "attempt": r.get("attempt", 1),
                        "searches": sum(t["name"] == "memory_search" for t in r.get("tools") or [])}
    return out


def live():
    corpus = json.load(open(HERE / "corpus.json"))
    per = {}
    for arm_dir in sorted(p for p in LIVE.iterdir() if p.is_dir()):
        for setting_dir in sorted(p for p in arm_dir.iterdir() if p.is_dir()):
            if (setting_dir / "results.jsonl").exists():
                per[f"{arm_dir.name}/{setting_dir.name}"] = score_live_setting(corpus, load_jsonl(setting_dir / "results.jsonl"))
    clean = clean_chains()
    summary = {}
    for name, rows in per.items():
        fam = defaultdict(list)
        for row in rows.values():
            fam[row["family"]].append(row)
        # The primary set is the clean chains of k >= 2 (live-preregistration.md);
        # every chain of k >= 2 is reported beside it.
        every = [r for f in PRIMARY_FAMILIES for r in fam[f]]
        primary = [r for i, r in rows.items() if r["family"] in PRIMARY_FAMILIES and i in clean]
        summary[name] = {
            "families": {f: {"n": len(rs), "applied": sum(r["applied"] for r in rs),
                             "all_seen": sum(r.get("all_seen", False) for r in rs) if f.startswith(("C", "IC")) else None}
                         for f, rs in sorted(fam.items())},
            "primary": {"n": len(primary), "applied": sum(r["applied"] for r in primary), "wilson": wilson(sum(r["applied"] for r in primary), len(primary)),
                        "all_seen": sum(r["all_seen"] for r in primary)},
            "every_k2": {"n": len(every), "applied": sum(r["applied"] for r in every), "all_seen": sum(r["all_seen"] for r in every)},
            "searches_per_probe": round(sum(r["searches"] for r in rows.values()) / max(len(rows), 1), 2),
            "probes_following_a_name": sum(r.get("followed", 0) > 0 for r in rows.values()),
            "primary_following_a_name": sum(r.get("followed", 0) > 0 for r in primary),
            "rerun": sum(r["attempt"] > 1 for r in rows.values()),
            "errors": sum(bool(r["error"]) for r in rows.values()),
        }
    base = per.get("S/b4")
    if base:
        others = [n for n in per if n != "S/b4"]
        ps = []
        for n in others:
            ids = sorted(i for i, r in base.items() if r["family"] in PRIMARY_FAMILIES and i in clean and i in per[n])
            b, c, p = mcnemar([per[n][i]["applied"] for i in ids], [base[i]["applied"] for i in ids])
            guard = {}
            for f in ("C1", "E", "K", "N"):
                fid = [i for i, r in base.items() if r["family"] == f and i in per[n]]
                guard[f] = sum(per[n][i]["applied"] for i in fid) - sum(base[i]["applied"] for i in fid)
            recalls = {a: {r["probe"]: r["recall"] for r in load_jsonl(LIVE / a / "results.jsonl")} for a in ("S/b4", n)}
            summary[n]["recall_blocks_differing_from_S"] = sum(recalls[n].get(i) != recalls["S/b4"][i] for i in recalls["S/b4"])
            summary[n]["against_S_b4"] = {"arm_only": b, "S_only": c, "p": p, "guard_change": guard,
                                          "guardrails_hold": all(v >= -GUARD_LOSS for v in guard.values())}
            ps.append(p)
        for n, adjusted in zip(others, holm(ps)):
            summary[n]["against_S_b4"]["holm_p"] = adjusted
    (LIVE / "scores.json").write_text(json.dumps({"summary": summary, "per_probe": per}, indent=1) + "\n")
    for n, s in summary.items():
        print(n, json.dumps({k: v for k, v in s.items() if k != "families"}))


def tokens():
    """Tokens per arm and phase, from the ledger."""
    agg = defaultdict(lambda: {"calls": 0, "cached": 0, "status_429": 0, "prompt_tokens": 0, "completion_tokens": 0})
    rows = []
    for ledger in ("ledger", "ledger-live"):
        path = HERE / "evidence" / ledger / "calls.jsonl"
        if path.exists():
            rows += [(ledger, c) for c in load_jsonl(path)]
    for ledger, c in rows:
        a = agg[f"{ledger}:{c['arm']}/{c['phase']}"]
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
    {"offline": offline, "live": live, "tokens": tokens}[sys.argv[1] if len(sys.argv) > 1 else "offline"]()
