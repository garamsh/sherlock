"""The matcher's hand audits (preregistration.md §Audit).

    python3 research/memory-encoding/attributed/audit.py draw 1    # evidence/audit/sample-1.jsonl, from #885, #888 and #896
    python3 research/memory-encoding/attributed/audit.py draw 2    # evidence/audit/sample-2.jsonl, from this study's live records
    python3 research/memory-encoding/attributed/audit.py check 1   # evidence/audit/audit-1.json, labels-1.jsonl against the matcher

A sample holds unabstained replies to absent probes (N and NC), with the
item's entity, associate and near-miss value, and no matcher output: the
labels are written from the sample alone, and `check` is run only after
they are committed.
"""

import json
import random
import re
import sys
from pathlib import Path

import matcher

HERE = Path(__file__).resolve().parent
RESEARCH = HERE.parents[1]
AUDIT = HERE / "evidence" / "audit"
SEED = 916
SIZE = {"1": 80, "2": 40}
AGREE = 0.90
FALSE_ACCEPT = 0.05
LABELS = ("acceptable", "failure")

# The recorded live replies of #885, #888 and #896, by study: its corpus
# directory and every live record directory. Smoke runs are excluded.
EARLIER = {
    "885": ("chained-recall/confirm", ["S/b4", "L/b4-model", "L/b4-model-a"]),
    "888": ("chained-recall/readtime", ["S/b4", "L/b4-model", "L/b4-model-g", "L/b4-model-t", "L/b4-model-v"]),
    "896": ("memory-encoding/structural", ["S/b4", "L/b4-model", "L/b4-model-ptr", "L/b4-model-attr", "L/b4-model-chk", "L/b4-model-key-ptr"]),
}
FRESH = {"916": ("memory-encoding/attributed", ["S/b4", "L/b4-model-attr"])}


def load_jsonl(path):
    with open(path) as f:
        return [json.loads(line) for line in f if line.strip()]


def visible(result):
    """What the person reads, as #885's abstention check reads it: the
    messages sent, or the reply when none was."""
    return "\n".join(result.get("sent") or []) or (result.get("reply") or "")


def absent_replies(studies):
    """Every absent-probe reply of the named studies' live records, with its item."""
    out = []
    for study, (d, records) in studies.items():
        guards = {g["id"]: g for g in json.load(open(RESEARCH / d / "corpus.json"))["guards"]}
        for rec in records:
            for r in load_jsonl(RESEARCH / d / "evidence" / "live" / rec / "results.jsonl"):
                if r["family"] in ("N", "NC"):
                    out.append({"key": f"{study}/{rec}/{r['probe']}", "guard": guards[r["probe"]], "reply": visible(r)})
    return out


def unabstained(rows):
    return [r for r in rows if not re.search(r["guard"]["check"]["pattern"], r["reply"], re.I)]


def sample_row(r):
    g = r["guard"]
    row = {"key": r["key"], "family": g["family"], "domain": g["domain"], "entity": g["entity"], "near_miss": g["near_miss"]}
    if g["family"] == "N":
        row["associated"] = g["associated"]
    return {**row, "reply": r["reply"]}


def draw(which):
    population = sorted(unabstained(absent_replies(EARLIER if which == "1" else FRESH)), key=lambda r: r["key"])
    rng = random.Random(SEED + int(which))
    chosen = population if len(population) <= SIZE[which] else rng.sample(population, SIZE[which])
    AUDIT.mkdir(parents=True, exist_ok=True)
    with open(AUDIT / f"sample-{which}.jsonl", "w") as f:
        for r in sorted(chosen, key=lambda r: r["key"]):
            f.write(json.dumps(sample_row(r), ensure_ascii=False) + "\n")
    print(json.dumps({"population": len(population), "sample": len(chosen)}))


def agreement(sample, labels, guards):
    """The matcher's reading A against the hand labels: agreement, and false
    accepts (the matcher accepts a reply labelled a failure)."""
    by_key = {l["key"]: l for l in labels}
    if set(by_key) != {s["key"] for s in sample} or any(l["label"] not in LABELS for l in labels):
        raise SystemExit("the labels do not cover the sample exactly once with a known label")
    rows = []
    for s in sample:
        j = matcher.judge(s["reply"], guards[s["key"]])
        rows.append({"key": s["key"], "label": by_key[s["key"]]["label"], "owner": j["owner"], "reading_b": j["reading_b"],
                     "agree": j["owner"] == (by_key[s["key"]]["label"] == "acceptable")})
    n = len(rows)
    agree = sum(r["agree"] for r in rows)
    false_accept = sum(r["owner"] and r["label"] == "failure" for r in rows)
    false_reject = sum(not r["owner"] and r["label"] == "acceptable" for r in rows)
    return {"n": n, "agree": agree, "false_accept": false_accept, "false_reject": false_reject,
            "labelled_acceptable": sum(r["label"] == "acceptable" for r in rows),
            "passed": n > 0 and agree >= AGREE * n and false_accept <= FALSE_ACCEPT * n,
            "disagreements": [r for r in rows if not r["agree"]]}


def check(which):
    sample = load_jsonl(AUDIT / f"sample-{which}.jsonl")
    labels = load_jsonl(AUDIT / f"labels-{which}.jsonl")
    guards = {r["key"]: r["guard"] for r in absent_replies(EARLIER if which == "1" else FRESH)}
    out = agreement(sample, labels, guards)
    (AUDIT / f"audit-{which}.json").write_text(json.dumps(out, indent=1, ensure_ascii=False) + "\n")
    print(json.dumps({k: v for k, v in out.items() if k != "disagreements"}))


if __name__ == "__main__":
    {"draw": draw, "check": check}[sys.argv[1]](sys.argv[2])
