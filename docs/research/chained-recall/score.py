"""Score the #874 offline stage per hop, for every arm and setting.

    python3 docs/research/chained-recall/score.py offline

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
states, wilson, mcnemar = AC.states, AC.wilson, AC.mcnemar


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


def offline():
    corpus = json.load(open(HERE / "corpus.json"))
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
    out = {"baseline": "/".join(BASELINE), "results": results, "per_chain": per_chain}
    (OFFLINE / "scores.json").write_text(json.dumps(out, indent=1) + "\n")
    for name, r in results.items():
        vs = r.get("against_S_b4", {})
        print(f"{name:14s} chain k>=2 {r['primary']['chain']:3d}/{r['primary']['n']}  k1 {r['by_k']['1']['chain']:2d} k2 {r['by_k']['2']['chain']:2d} k3 {r['by_k']['3']['chain']:2d}"
              f"  ctx {r['context_chars_per_turn']:7.0f}  stored/src {r['stored_chars_per_source']:.3f}  oracle {r['oracle']['reached']}/{r['oracle']['hops']}  {vs}")


if __name__ == "__main__":
    {"offline": offline}[sys.argv[1] if len(sys.argv) > 1 else "offline"]()
