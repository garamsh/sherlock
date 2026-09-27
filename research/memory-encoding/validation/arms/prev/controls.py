"""P's controls (preregistration.md §Controls beside this file), from the
recorded links, the selected corpus, P's offline records and the ledger.

    python3 research/memory-encoding/validation/arms/prev/controls.py [ledger calls.jsonl]   # evidence/controls.json

The ledger defaults to the study's evidence/ledger/calls.jsonl, which the
proxy writes and the repository does not hold.
"""

import importlib.util
import json
import statistics
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
STUDY = HERE.parent.parent


def _score():
    spec = importlib.util.spec_from_file_location("validation_score", STUDY / "score.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


S = _score()
SETTINGS = ("b4-prev", "b4-k5")
CONTINUING = 0.10
HOP_PAIRS = 0.50
ORACLE = 0.95


def mid_of(source):
    """The batch-4 mid over a source: mids number the corpus's batches."""
    return (source - 1) // 4 + 1


def continuing(corpus, recorded):
    """Share of stream batches whose answer links at least one mid."""
    batches = len(corpus["stream"]) // 4
    linked = sum(bool(b["links"]) for b in recorded)
    return {"stream_batches": batches, "recorded": len(recorded), "failed": sum(bool(b.get("failed")) for b in recorded),
            "continuing": linked, "share": round(linked / batches, 4)}


def hop_pairs(corpus, recorded):
    """Of every pair of consecutive hops of a chain with two or more hops,
    those whose mids are linked, in either direction since hops lie in
    random order; and those where the later mid's batch was given the
    earlier mid, which separates the candidate lookup from the model's
    choice. Per family and pooled."""
    by_mid = {mid_of(b["first"]): b for b in recorded}
    out = {}
    for c in corpus["chains"]:
        if c["k"] < 2:
            continue
        f = out.setdefault(c["family"], {"pairs": 0, "linked": 0, "given": 0})
        for a, b in zip(c["hops"], c["hops"][1:]):
            x, y = sorted((mid_of(a["source"]), mid_of(b["source"])))
            later = by_mid.get(y, {"given": [], "links": []})
            f["pairs"] += 1
            f["linked"] += x in later["links"]
            f["given"] += x in later["given"]
    pooled = {k: sum(f[k] for f in out.values()) for k in ("pairs", "linked", "given")}
    pooled["share"] = round(pooled["linked"] / max(pooled["pairs"], 1), 4)
    return {"families": out, "pooled": pooled}


def oracle_share(corpus, oracle):
    hops = {(c["id"], i + 1): h for c in corpus["chains"] for i, h in enumerate(c["hops"])}
    reached = sum(bool(S.P.hop_reached(o["items"], hops[(o["probe"], o["hop"])])[0]) for o in oracle)
    return round(reached / max(len(oracle), 1), 4)


def thread_trace(corpus, probes):
    """Of the chain probes with two or more hops, those whose recall
    delivered a thread mid beyond the similar list."""
    multi = {c["id"] for c in corpus["chains"] if c["k"] >= 2}
    traced = [p["trace"] for p in probes if p["probe"] in multi]
    return {"probes": len(traced), "thread_delivered": sum(bool(t["thread"]) for t in traced),
            "beyond_similar": sum(bool(t["beyond_similar"]) for t in traced)}


def added_tokens(ledger, setting, base_batches):
    """Input and output tokens of each links-<setting> call minus the shared
    synth-b4 call of the same batch; the last successful paid call of an
    episode is the one paired."""
    def paid(phase):
        out = {}
        for c in ledger:
            if c["phase"] == phase and c["status"] == 200 and c.get("completion_tokens", 0) > 0:
                out[int(c["episode"].split("-")[1])] = c
        return out

    shipped = paid("synth-b4")
    inp, out = [], []
    for batch, call in paid(f"links-{setting}").items():
        s = shipped.get(batch - base_batches)
        if s:
            inp.append(call["prompt_tokens"] - s["prompt_tokens"])
            out.append(call["completion_tokens"] - s["completion_tokens"])
    if not out:
        return {"paired": 0}
    return {"paired": len(out), "input_mean": round(statistics.mean(inp), 1), "input_median": statistics.median(inp),
            "output_mean": round(statistics.mean(out), 1), "output_median": statistics.median(out)}


def controls(corpus, setting, recorded, probes, oracle, ledger):
    out = {"continuing": continuing(corpus, recorded), "oracle_share": oracle_share(corpus, oracle),
           "thread": thread_trace(corpus, probes),
           "added_tokens": added_tokens(ledger, setting, (corpus["stream"][0]["id"] - 1) // 4)}
    passed = out["continuing"]["share"] >= CONTINUING and out["oracle_share"] >= ORACLE
    if setting == "b4-k5":
        out["hop_pairs"] = hop_pairs(corpus, recorded)
        passed = passed and out["hop_pairs"]["pooled"]["share"] >= HOP_PAIRS
    out["passed"] = passed
    return out


def main():
    corpus = json.loads((STUDY / "corpus.json").read_text())
    ledger = S.load_jsonl(sys.argv[1] if len(sys.argv) > 1 else STUDY / "evidence" / "ledger" / "calls.jsonl")
    result = {}
    for setting in SETTINGS:
        if not (HERE / "evidence" / f"links-{setting}.jsonl").exists():
            # preregistration.md §Amendments, 1: b4-k5 made no call, and its
            # control failed on the lookup's ceiling.
            lookup = json.loads((HERE / "evidence" / "lookup-b4-k5.json").read_text())
            ceiling = lookup["pooled"]["given"] / lookup["pooled"]["pairs"]
            result[setting] = {"run": False, "hop_pairs_ceiling": {**lookup["pooled"], "share": round(ceiling, 4)}, "passed": ceiling >= HOP_PAIRS}
            continue
        records = STUDY / "evidence" / "offline" / "P" / setting
        result[setting] = controls(corpus, setting, S.load_jsonl(HERE / "evidence" / f"links-{setting}.jsonl"),
                                   S.load_jsonl(records / "probes.jsonl"), S.load_jsonl(records / "oracle.jsonl"), ledger)
    (HERE / "evidence" / "controls.json").write_text(json.dumps(result, indent=1) + "\n")
    print(json.dumps(result, indent=1))


if __name__ == "__main__":
    main()
