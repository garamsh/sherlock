"""Whether any similarity threshold separates supersession from coexistence.

`EMBED_DIMS` selects the embedding dimension, defaulting to the 512 the round
was measured at. A non-default dimension writes its own result files.
"""
from __future__ import annotations
import json
import os
import random
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import api
from api import cosine, dims_suffix, embed, embed_batch, flush
from dataset import CATEGORIES, COEXIST, PERSONS

# Results land beside the report, not beside the caller's shell.
RESULTS = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                       "..", "..", "results_round4")


def build_pairs(seed=7, per_family=250):
    rnd = random.Random(seed)
    fams = {"correction": [], "coexist": [], "cross-subject": [], "unrelated": []}

    def phrase(person, cat, value, corrected=False):
        name, question, values, inits, corrs = cat
        tmpl = rnd.choice(corrs if corrected else inits)
        return tmpl.format(p=person, v=value)

    while len(fams["correction"]) < per_family:
        person, cat = rnd.choice(PERSONS), rnd.choice(CATEGORIES)
        old, new = rnd.sample(cat[2], 2)
        fams["correction"].append((phrase(person, cat, old),
                                   phrase(person, cat, new, corrected=True)))

    while len(fams["cross-subject"]) < per_family:
        p1, p2 = rnd.sample(PERSONS, 2)
        cat = rnd.choice(CATEGORIES)
        val = rnd.choice(cat[2])
        fams["cross-subject"].append((phrase(p1, cat, val), phrase(p2, cat, val)))

    while len(fams["unrelated"]) < per_family:
        p1, p2 = rnd.sample(PERSONS, 2)
        c1, c2 = rnd.sample(CATEGORIES, 2)
        fams["unrelated"].append((phrase(p1, c1, rnd.choice(c1[2])),
                                  phrase(p2, c2, rnd.choice(c2[2]))))

    while len(fams["coexist"]) < per_family:
        person = rnd.choice(PERSONS)
        name, question, values, tmpl = rnd.choice(COEXIST)
        a, b = rnd.sample(values, 2)
        fams["coexist"].append((tmpl.format(p=person, v=a),
                                tmpl.format(p=person, v=b)))
    return fams


def quantiles(xs):
    xs = sorted(xs)
    q = lambda f: xs[min(len(xs) - 1, int(f * len(xs)))]
    return {"min": round(xs[0], 3), "p25": round(q(0.25), 3),
            "median": round(q(0.5), 3), "p75": round(q(0.75), 3),
            "max": round(xs[-1], 3),
            "iqr": round(q(0.75) - q(0.25), 3),
            "mean": round(sum(xs) / len(xs), 3)}


def overlap(correction, coexist):
    """Where the family a threshold must retire sits on top of the one it must not.

    Reports the score range the two share, the share of each family inside it,
    and the share of all cross pairs in which the coexisting pair scores at
    least as high as the correction — 0.5 being no ordering at all.
    """
    lo = max(min(correction), min(coexist))
    hi = min(max(correction), max(coexist))
    inverted = sum(1 for c in correction for x in coexist if x >= c)
    return {
        "shared_range": [round(lo, 3), round(hi, 3)] if lo <= hi else None,
        "correction_in_shared_range":
            round(sum(1 for s in correction if lo <= s <= hi) / len(correction), 3),
        "coexist_in_shared_range":
            round(sum(1 for s in coexist if lo <= s <= hi) / len(coexist), 3),
        "coexist_at_or_above_correction":
            round(inverted / (len(correction) * len(coexist)), 3),
    }


def main():
    print(f"embedding dimension: {api.EMBED_DIMS}", flush=True)
    fams = build_pairs()
    texts = [t for pairs in fams.values() for pair in pairs for t in pair]
    print(f"embedding {len(set(texts))} unique statements...", flush=True)
    embed_batch(texts)
    flush()

    scores = {k: [cosine(embed(a), embed(b)) for a, b in v] for k, v in fams.items()}
    report = {k: quantiles(v) for k, v in scores.items()}

    # Supersede = correction only. Everything else must survive.
    positives = scores["correction"]
    negatives = scores["coexist"] + scores["cross-subject"] + scores["unrelated"]
    best = {"threshold": None, "accuracy": -1.0}
    sweep = []
    for i in range(0, 101):
        t = i / 100.0
        tp = sum(1 for s in positives if s >= t)
        tn = sum(1 for s in negatives if s < t)
        acc = (tp + tn) / (len(positives) + len(negatives))
        sweep.append({"t": t, "correct_retirements": tp,
                      "wrong_retirements": len(negatives) - tn,
                      "recall": round(tp / len(positives), 3),
                      "false_supersede": round(1 - tn / len(negatives), 3),
                      "accuracy": round(acc, 3)})
        if acc > best["accuracy"]:
            best = {"threshold": t, "accuracy": round(acc, 3),
                    "correct_retirements": tp,
                    "wrong_retirements": len(negatives) - tn,
                    "recall": round(tp / len(positives), 3),
                    "false_supersede": round(1 - tn / len(negatives), 3)}

    adr = next(s for s in sweep if abs(s["t"] - 0.7) < 1e-9)
    exp = next(s for s in sweep if abs(s["t"] - 0.6) < 1e-9)

    out = {"dims": api.EMBED_DIMS, "families": report,
           "correction_coexist_overlap": overlap(scores["correction"],
                                                 scores["coexist"]),
           "best_possible_threshold": best,
           "adr_0006_threshold_0.70": adr, "harness_threshold_0.60": exp,
           "n_per_family": len(positives)}
    os.makedirs(RESULTS, exist_ok=True)
    stem = f"separability{dims_suffix()}"
    with open(os.path.join(RESULTS, f"{stem}.json"), "w") as f:
        json.dump({**out, "sweep": sweep}, f, indent=2)

    lines = [f"{api.EMBED_DIMS} dimensions.", "",
             "| pair family | must supersede? | min | p25 | median | p75 | max | iqr |",
             "|---|---|---|---|---|---|---|---|"]
    want = {"correction": "yes", "coexist": "no",
            "cross-subject": "no", "unrelated": "no"}
    for k in ("correction", "coexist", "cross-subject", "unrelated"):
        r = report[k]
        lines.append(f"| {k} | {want[k]} | {r['min']} | {r['p25']} | "
                     f"{r['median']} | {r['p75']} | {r['max']} | {r['iqr']} |")
    table = "\n".join(lines)
    with open(os.path.join(RESULTS, f"{stem}.md"), "w") as f:
        f.write(table + "\n")
    print("\n" + table)
    print(f"\ncorrection/coexist overlap: "
          f"{json.dumps(out['correction_coexist_overlap'])}")
    print(f"best possible threshold: {json.dumps(best)}")
    print(f"ADR 0006 (0.70): {json.dumps(adr)}")
    print(f"harness  (0.60): {json.dumps(exp)}")
    print(f"embedding requests this run: {api.API_CALLS['embed']}")


if __name__ == "__main__":
    main()
