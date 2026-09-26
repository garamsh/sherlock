"""Score the #885 confirmatory run with #874's scorer (../score.py).

    python3 docs/research/chained-recall/confirm/score.py power     # evidence/power.json, from #874's live records
    python3 docs/research/chained-recall/confirm/score.py offline   # evidence/offline/scores.json
    python3 docs/research/chained-recall/confirm/score.py live      # evidence/live/scores.json
    python3 docs/research/chained-recall/confirm/score.py tokens    # evidence/tokens.json

Hops, chains and answers are matched exactly as ../score.py matches them;
this file adds what preregistration.md fixes for #885: the primary set (C2
and C3), the guardrails, the controls, and the absent query's near-miss.
"""

import importlib.util
import json
import random
import re
import sys
from collections import defaultdict
from pathlib import Path

HERE = Path(__file__).resolve().parent


def _parent_score():
    spec = importlib.util.spec_from_file_location("chained_recall_score", HERE.parent / "score.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


P = _parent_score()
states, wilson, mcnemar, holm, load_jsonl = P.states, P.wilson, P.mcnemar, P.holm, P.load_jsonl

PRIMARY = ("C2", "C3")
REPORTED = ("C1", "IC2", "IC3")
BASELINE = "S/b4"
ARMS = ("L/b4-model", "L/b4-model-a")
# Lost probes each guard family may show against S (preregistration.md):
# 5 percentage points of N (60) and K (40), and 3 probes on C1 and E.
ALLOWED_LOSS = {"N": 3, "K": 2, "C1": 3, "E": 3}
# Net production lines of each arm, before tests (preregistration.md).
NET_LINES = {"L/b4-model": 270, "L/b4-model-a": 292}
ALPHA = 0.05
LABEL = "Associated through the named entity"


def load(path):
    with open(path) as f:
        return json.load(f)


def corpus():
    return load(HERE / "corpus.json")


# ---- power ------------------------------------------------------------------

def exact_power(n, arm_only, s_only, alpha, runs=20000, seed=885):
    """Share of simulated runs of n paired probes, each discordant for the
    arm with probability arm_only and for S with s_only, in which the exact
    two-sided McNemar test is significant at alpha with the arm ahead."""
    rng = random.Random(seed)
    hits = 0
    for _ in range(runs):
        b = c = 0
        for _ in range(n):
            u = rng.random()
            if u < arm_only:
                b += 1
            elif u < arm_only + s_only:
                c += 1
        if b > c and mcnemar([True] * b + [False] * c, [False] * b + [True] * c)[2] < alpha:
            hits += 1
    return hits / runs


def guard_failure(n, discordant, net_loss, allowed, runs=20000, seed=886):
    """Share of simulated runs in which a guard family of n loses more than
    allowed probes, when a share `discordant` of probes differ between the
    arms and the arm's expected net loss is a share net_loss of n: a
    discordant probe goes against the arm with probability
    0.5 + net_loss / (2 * discordant)."""
    against = 0.5 + net_loss / (2 * discordant)
    rng = random.Random(seed)
    fails = 0
    for _ in range(runs):
        net = 0
        for _ in range(n):
            if rng.random() < discordant:
                net += -1 if rng.random() < against else 1
        fails += net < -allowed
    return fails / runs


def power():
    """The power the family sizes give, from #874's paired live records."""
    per = load(HERE.parent / "evidence" / "live" / "scores.json")["per_probe"]
    s, m, l = per["S/b4"], per["M/b4"], per["L/b4-model"]

    def disc(a, b, fams):
        ids = [i for i, r in b.items() if r["family"] in fams]
        return (sum(a[i]["applied"] and not b[i]["applied"] for i in ids) / len(ids),
                sum(b[i]["applied"] and not a[i]["applied"] for i in ids) / len(ids), len(ids))

    l_only, s_only, n874 = disc(l, s, PRIMARY)
    n = 100
    scenarios = {
        "874 L against S on C2 and C3": (l_only, s_only),
        "a quarter of that gain": (s_only + (l_only - s_only) / 4, s_only),
        "a tenth of that gain": (s_only + (l_only - s_only) / 10, s_only),
    }
    # Holm across two comparisons: the smaller p must clear alpha / 2.
    out = {"primary_n": n, "alpha_first_holm_step": ALPHA / 2, "from_874": {"arm_only": l_only, "s_only": s_only, "n": n874}, "primary": {}}
    for name, (a, b) in scenarios.items():
        out["primary"][name] = {"arm_only": round(a, 4), "s_only": round(b, 4), "power": exact_power(n, a, b, ALPHA / 2)}
    # The smallest arm-only rate, at #874's S-only rate, with 80% power.
    rate = s_only
    while exact_power(n, rate, s_only, ALPHA / 2, runs=4000) < 0.8:
        rate += 0.01
    out["primary"]["smallest arm-only rate with 80% power"] = {"arm_only": round(rate, 2), "s_only": round(s_only, 4)}
    sizes = {"N": 60, "K": 40, "C1": 30, "E": 30}
    out["guards"] = {}
    for fam, size in sizes.items():
        ids = [i for i, r in s.items() if r["family"] == fam]
        # Between two arms that differ only by an instruction (#874's M
        # against S), the share of probes that differ on this family, at
        # least 5%; and between L and S, that share and L's net loss.
        d_null = max(sum(m[i]["applied"] != s[i]["applied"] for i in ids) / len(ids), 0.05)
        d_l = max(sum(l[i]["applied"] != s[i]["applied"] for i in ids) / len(ids), 0.05)
        harm = (sum(s[i]["applied"] and not l[i]["applied"] for i in ids) - sum(l[i]["applied"] and not s[i]["applied"] for i in ids)) / len(ids)
        out["guards"][fam] = {
            "n": size, "allowed_loss": ALLOWED_LOSS[fam],
            "discordant_share_874_M_vs_S": round(d_null, 4),
            "failure_with_no_true_loss": guard_failure(size, d_null, 0.0, ALLOWED_LOSS[fam]),
            "discordant_share_874_L_vs_S": round(d_l, 4), "net_loss_share_874_L": round(harm, 4),
            "failure_at_874_L_net_loss": guard_failure(size, d_l, max(harm, 0.0), ALLOWED_LOSS[fam]),
        }
    (HERE / "evidence").mkdir(exist_ok=True)
    (HERE / "evidence" / "power.json").write_text(json.dumps(out, indent=1) + "\n")
    print(json.dumps(out, indent=1))


# ---- offline ----------------------------------------------------------------

OFFLINE = HERE / "evidence" / "offline"


def walk_items(items):
    return [it for it in items if it["kind"] in ("linked", "associated")]


def offline():
    c = corpus()
    results, per = {}, {}
    for built_path in sorted(OFFLINE.glob("*/*/built.json")):
        d = built_path.parent
        name = f"{d.parent.name}/{d.name}"
        built = load(built_path)
        probes = load_jsonl(d / "probes.jsonl")
        chains, guards = P.score_setting(c, probes, load_jsonl(d / "oracle.jsonl"))
        r = P.summarize(chains, guards, built)
        prim = [x for x in chains.values() if x["family"] in PRIMARY]
        r["primary"] = {"n": len(prim), "chain": sum(x["chain"] for x in prim), "wilson": wilson(sum(x["chain"] for x in prim), len(prim))}
        by_probe = {p["probe"]: p for p in probes}
        absent = [g for g in c["guards"] if g["family"] == "N"]
        r["absent_with_walk_item"] = sum(bool(walk_items(by_probe[g["id"]]["items"])) for g in absent)
        r["absent_with_associated_episode"] = sum(any(it["first"] <= g["sources"][1] <= it["last"] for it in walk_items(by_probe[g["id"]]["items"])) for g in absent)
        r["absent_with_near_miss_in_view"] = sum(any(states(it["text"], g["near_miss"]) for it in by_probe[g["id"]]["items"]) for g in absent)
        r["primary_with_walk_item"] = sum(bool(walk_items(by_probe[x]["items"])) for x, v in chains.items() if v["family"] in PRIMARY)
        labelled = [it for p in probes for it in walk_items(p["items"])]
        r["walk_items"] = len(labelled)
        r["walk_items_labelled"] = sum(it["kind"] == "associated" and it["text"].startswith(LABEL) for it in labelled)
        if "notes" in built["built"]:
            r["share_with_entities"] = built["built"]["notes"].get("share_with_entities")
        results[name] = r
        per[name] = {"chains": chains, "guards": guards}
    base = per.get(BASELINE)
    if base:
        ids = sorted(i for i, x in base["chains"].items() if x["family"] in PRIMARY)
        compared = [n for n in ARMS if n in per]
        ps = []
        for name in compared:
            b, cc, p = mcnemar([per[name]["chains"][i]["chain"] for i in ids], [base["chains"][i]["chain"] for i in ids])
            guard = {f: (results[name]["families"][f]["chain"] - results[BASELINE]["families"][f]["chain"]) for f in ("C1",)}
            for f in ("E", "K"):
                guard[f] = results[name]["guards"][f]["reached"] - results[BASELINE]["guards"][f]["reached"]
            results[name]["against_S_b4"] = {"arm_only": b, "S_only": cc, "p": p, "guard_change": guard,
                                             "guardrails_hold": all(v >= -ALLOWED_LOSS[f] for f, v in guard.items())}
            ps.append(p)
        for name, adjusted in zip(compared, holm(ps)):
            results[name]["against_S_b4"]["holm_p"] = adjusted
    out = {"baseline": BASELINE, "results": results, "per_chain": per}
    (OFFLINE / "scores.json").write_text(json.dumps(out, indent=1) + "\n")
    for name, r in results.items():
        print(f"{name:14s} primary {r['primary']['chain']:3d}/{r['primary']['n']}  k1 {r['by_k']['1']['chain']:2d}  E {r['guards']['E']['reached']} K {r['guards']['K']['reached']}"
              f"  ctx {r['context_chars_per_turn']:7.0f}  stored/src {r['stored_chars_per_source']:.3f}  oracle {r['oracle']['reached']}/{r['oracle']['hops']}"
              f"  N walk {r['absent_with_associated_episode']}  {r.get('against_S_b4', {})}")


# ---- live -------------------------------------------------------------------

LIVE = HERE / "evidence" / "live"


def walk_in_recall(recall):
    return "] (linked):" in recall or "] (associated):" in recall


def label_ok(recall):
    """Every walk-delivered line of an L+A recall block carries the label,
    and none is delivered unlabelled."""
    lines = [ln for ln in recall.splitlines() if "] (associated):" in ln or "] (linked):" in ln]
    return bool(lines) and all("] (associated): " + LABEL in ln for ln in lines)


def score_live(c, results):
    rows = P.score_live_setting(c, results)
    by_probe = {r["probe"]: r for r in results}
    old = next(g for g in load(HERE.parent / "corpus.json")["guards"] if g["family"] == "N")["check"]["pattern"]
    for g in c["guards"]:
        if g["family"] != "N" or g["id"] not in rows:
            continue
        r = by_probe[g["id"]]
        text = P.artifact(r)
        rows[g["id"]]["near_miss_stated"] = states(text, g["near_miss"])
        rows[g["id"]]["abstained_874_pattern"] = P.applied({"kind": "abstain", "pattern": old}, r)
        rows[g["id"]]["walk_in_recall"] = walk_in_recall(r.get("recall") or "")
    for i, row in rows.items():
        recall = by_probe[i].get("recall") or ""
        row["arm_items"] = walk_in_recall(recall)
        row["label_ok"] = label_ok(recall) if row["arm_items"] else None
    return rows


def compare(per):
    """Each arm against S on the primary set: exact McNemar on applied, Holm
    across the arms compared, and each guard family's change in probes."""
    base = per.get(BASELINE)
    if not base:
        return {}
    compared = [n for n in ARMS if n in per]
    out, ps = {}, []
    for n in compared:
        ids = sorted(i for i, r in base.items() if r["family"] in PRIMARY and i in per[n])
        b, c, p = mcnemar([per[n][i]["applied"] for i in ids], [base[i]["applied"] for i in ids])
        guard = {}
        for f in ALLOWED_LOSS:
            fid = [i for i, r in base.items() if r["family"] == f and i in per[n]]
            guard[f] = sum(per[n][i]["applied"] for i in fid) - sum(base[i]["applied"] for i in fid)
        out[n] = {"arm_only": b, "S_only": c, "p": p, "guard_change": guard,
                  "guardrails_hold": all(guard[f] >= -ALLOWED_LOSS[f] for f in guard)}
        ps.append(p)
    for n, adjusted in zip(compared, holm(ps)):
        a = out[n]
        a["holm_p"] = adjusted
        a["significant"] = a["arm_only"] > a["S_only"] and adjusted < ALPHA
        a["valid"] = a["significant"] and a["guardrails_hold"]
    return out


def smallest_valid(comparisons):
    """The valid arm with the fewest net production lines, or None."""
    valid = [n for n, a in comparisons.items() if a["valid"]]
    return min(valid, key=lambda n: NET_LINES[n]) if valid else None


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
        prim = [r for f in PRIMARY for r in fam[f]]
        absent = fam["N"]
        walked = [r for r in rows.values() if r.get("arm_items")]
        summary[name] = {
            "families": {f: {"n": len(rs), "applied": sum(r["applied"] for r in rs),
                             "all_seen": sum(r.get("all_seen", False) for r in rs) if f.startswith(("C", "IC")) else None}
                         for f, rs in sorted(fam.items())},
            "primary": {"n": len(prim), "applied": sum(r["applied"] for r in prim), "wilson": wilson(sum(r["applied"] for r in prim), len(prim)),
                        "all_seen": sum(r["all_seen"] for r in prim)},
            "absent": {"n": len(absent), "abstained": sum(r["applied"] for r in absent),
                       "abstained_874_pattern": sum(r["abstained_874_pattern"] for r in absent),
                       "near_miss_stated": sum(r["near_miss_stated"] for r in absent),
                       "near_miss_stated_without_abstaining": sum(r["near_miss_stated"] and not r["applied"] for r in absent),
                       "walk_in_recall": sum(r["walk_in_recall"] for r in absent)},
            "searches_per_probe": round(sum(r["searches"] for r in rows.values()) / max(len(rows), 1), 2),
            "primary_with_walk_items_in_recall": sum(r["arm_items"] for r in prim),
            "recall_blocks_with_walk_items": len(walked),
            "walk_blocks_labelled": sum(bool(r["label_ok"]) for r in walked),
            "rerun": sum(r["attempt"] > 1 for r in rows.values()),
            "errors": sum(bool(r["error"]) for r in rows.values()),
        }
    comparisons = compare(per)
    for n, a in comparisons.items():
        summary[n]["against_S_b4"] = a
    decision = smallest_valid(comparisons)
    out = {"summary": summary, "smallest_valid_arm": decision, "per_probe": per}
    (LIVE / "scores.json").write_text(json.dumps(out, indent=1) + "\n")
    for n, s in summary.items():
        print(n, json.dumps({k: v for k, v in s.items() if k != "families"}))
    print("smallest valid arm:", decision)


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
    {"power": power, "offline": offline, "live": live, "tokens": tokens}[sys.argv[1]]()
