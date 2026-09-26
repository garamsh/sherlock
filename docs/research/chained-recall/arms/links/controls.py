"""Arm L's controls and added synthesis tokens, from its offline records.

    python3 docs/research/chained-recall/arms/links/controls.py [ledger calls.jsonl]

Reads evidence/offline/<setting>/ beside this file, the records study.Offline
writes under the study's evidence/offline/L/, and writes evidence/controls.json
beside this file. The thresholds are the
ones README.md §Controls fixed before the first offline run. With a ledger,
it also pairs each entities-b4 call with its batch's recorded synthesis.
"""

import json
import statistics
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
STUDY = HERE.parent.parent
sys.path.insert(0, str(STUDY))
import score  # noqa: E402

THRESHOLDS = {"entities": 0.90, "walked": 0.90, "beyond_similar": 0.50, "oracle": 0.95}


def controls(corpus, built, probes, oracle):
    """The mechanism controls of one setting, each with whether it passed."""
    multi = {c["id"] for c in corpus["chains"] if c["k"] >= 2}
    traced = [p["trace"] for p in probes if p["probe"] in multi]
    walked = sum(bool(t["seeds"]) and t["steps"] > 1 for t in traced)
    beyond = sum(bool(t["beyond_similar"]) for t in traced)
    beyond_seeds = sum(bool(t["beyond_seeds"]) for t in traced)
    hops = {(c["id"], i + 1): h for c in corpus["chains"] for i, h in enumerate(c["hops"])}
    reached = sum(score.hop_reached(o["items"], hops[(o["probe"], o["hop"])])[0] for o in oracle)
    shares = {
        "entities": built["built"]["notes"]["share_with_entities"],
        "walked": walked / len(traced),
        "beyond_similar": beyond / len(traced),
        "oracle": reached / len(oracle),
    }
    out = {name: {"share": round(share, 4), "threshold": THRESHOLDS[name], "passed": share >= THRESHOLDS[name]} for name, share in shares.items()}
    out["chains"] = len(traced)
    out["hops"] = len(oracle)
    out["beyond_seeds"] = beyond_seeds
    out["passed"] = all(v["passed"] for v in out.values() if isinstance(v, dict))
    return out


def added_tokens(ledger, base_usage, n=4):
    """Output and input tokens of each entities-b<n> call minus its batch's
    recorded synthesis: the replication's for the base, the ledger's
    synth-b<n> call for the stream. The last successful call of an episode
    is the one paired."""
    def successful(phase, arm):
        out = {}
        for c in ledger:
            if c["phase"] == phase and c["arm"] == arm and c["status"] == 200 and c["completion_tokens"] > 0:
                out[c["episode"]] = c
        return out

    entities = successful(f"entities-b{n}", "L")
    stream = {int(e.split("-")[1]): c for e, c in successful(f"synth-b{n}", "shared").items()}
    base_batches = len(base_usage)
    out, inp = [], []
    for episode, call in entities.items():
        batch = int(episode.split("-")[1])
        if batch <= base_batches:
            recorded = base_usage.get(batch)
        else:
            c = stream.get(batch - base_batches)
            recorded = c and {"prompt_tokens": c["prompt_tokens"], "completion_tokens": c["completion_tokens"]}
        if recorded:
            out.append(call["completion_tokens"] - recorded["completion_tokens"])
            inp.append(call["prompt_tokens"] - recorded["prompt_tokens"])
    if not out:
        return {"paired": 0}
    return {"paired": len(out), "output_mean": round(statistics.mean(out), 1), "output_median": statistics.median(out),
            "input_mean": round(statistics.mean(inp), 1), "input_median": statistics.median(inp)}


def main():
    corpus = json.load(open(STUDY / "corpus.json"))
    result = {}
    for setting in sorted((HERE / "evidence" / "offline").iterdir()):
        if not (setting / "probes.jsonl").exists():
            continue
        result[setting.name] = controls(corpus, json.load(open(setting / "built.json")),
                                        score.load_jsonl(setting / "probes.jsonl"), score.load_jsonl(setting / "oracle.jsonl"))
    if len(sys.argv) > 1:
        base = {r["batch"]: r["response"]["usage"] for r in score.load_jsonl(STUDY.parent / "mid-memory-rag" / "evidence" / "replication" / "synthesis-n4.jsonl")}
        result["added_tokens_b4"] = added_tokens(score.load_jsonl(sys.argv[1]), base)
    (HERE / "evidence").mkdir(exist_ok=True)
    (HERE / "evidence" / "controls.json").write_text(json.dumps(result, indent=1) + "\n")
    print(json.dumps(result, indent=1))


if __name__ == "__main__":
    main()
