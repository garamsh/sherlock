"""C's controls (preregistration.md beside this file), from the recorded
syntheses, the selected corpus and C's offline oracle, with C's write-call
tokens against the shipped call's.

    python3 docs/research/memory-encoding/validation/arms/context/controls.py [ledger calls.jsonl]   # evidence/controls.json

The ledger defaults to the study's evidence/ledger/calls.jsonl; the proxy
keeps it where its -dir points.
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
THRESHOLDS = {"context_share": 0.90, "sc_subject_share": 0.80, "oracle_share": 0.95}


def share(n, d):
    return round(n / max(d, 1), 4)


def controls(corpus, recorded):
    """The two synthesis controls, and the diagnostics beside them."""
    by_first = {b["first"]: b for b in recorded}
    batches = len(corpus["stream"]) // 4
    with_context = sum(bool(b["context"]) for b in recorded)

    def batch_of(source):
        # An exchange fills its batch alone, so its source opens the batch.
        return by_first.get(source, {"summary": "", "context": ""})

    sc = [c for c in corpus["chains"] if c["family"] == "SC"]
    named = [c["id"] for c in sc if S.states(batch_of(c["hops"][0]["source"])["context"], c["subject"])]
    in_summary = [c["id"] for c in sc if S.states(batch_of(c["hops"][0]["source"])["summary"], c["subject"])]
    nc = [g for g in corpus["guards"] if g["family"] == "NC"]
    nc_named = [g["id"] for g in nc if S.states(batch_of(g["sources"][1])["context"], g["entity"])]
    out = {
        "stream_batches": batches, "recorded": len(by_first),
        "with_context": with_context, "context_share": share(with_context, batches),
        "context_chars_mean": round(statistics.mean(len(b["context"]) for b in recorded), 1) if recorded else 0,
        "sc_items": len(sc), "sc_context_names_subject": len(named), "sc_subject_share": share(len(named), len(sc)),
        "sc_missing": sorted(set(c["id"] for c in sc) - set(named)),
        # Diagnostics, not controls: the subject carried into the summary,
        # which the prompt forbids, and the situated absent risk.
        "sc_summary_names_subject": len(in_summary),
        "nc_items": len(nc), "nc_context_names_entity": len(nc_named),
    }
    out["passed"] = out["context_share"] >= THRESHOLDS["context_share"] and out["sc_subject_share"] >= THRESHOLDS["sc_subject_share"]
    return out


def oracle(corpus, records):
    """Each hop's own statement delivers its mid within 5."""
    hops = {(c["id"], i + 1): h for c in corpus["chains"] for i, h in enumerate(c["hops"])}
    reached = sum(S.P.hop_reached(o["items"], hops[(o["probe"], o["hop"])])[0] for o in records)
    return {"hops": len(records), "reached": reached, "oracle_share": share(reached, len(records)),
            "passed": reached >= THRESHOLDS["oracle_share"] * len(records)}


def tokens(ledger, phases=("synth-b4", "context-b4-w8")):
    """Mean input and output tokens of the last successful call of each
    episode, per phase: the shipped call's and C's, over the same batches."""
    out = {}
    for phase in phases:
        last = {}
        for c in ledger:
            if c["phase"] == phase and c.get("status") == 200 and c.get("completion_tokens", 0) > 0:
                last[c["episode"]] = c
        calls = list(last.values())
        out[phase] = {"batches": len(calls),
                      "prompt_tokens_mean": round(statistics.mean(c["prompt_tokens"] for c in calls), 1) if calls else None,
                      "completion_tokens_mean": round(statistics.mean(c["completion_tokens"] for c in calls), 1) if calls else None,
                      "prompt_tokens": sum(c["prompt_tokens"] for c in calls),
                      "completion_tokens": sum(c["completion_tokens"] for c in calls)}
    return out


def main():
    corpus = json.loads((STUDY / "corpus.json").read_text())
    out = controls(corpus, S.load_jsonl(HERE / "evidence" / "context-b4-w8.jsonl"))
    records = STUDY / "evidence" / "offline" / "C" / "b4-w8" / "oracle.jsonl"
    if records.exists():
        out["oracle"] = oracle(corpus, S.load_jsonl(records))
        out["passed"] = out["passed"] and out["oracle"]["passed"]
    ledger = Path(sys.argv[1]) if len(sys.argv) > 1 else STUDY / "evidence" / "ledger" / "calls.jsonl"
    if ledger.exists():
        out["tokens"] = tokens(S.load_jsonl(ledger))
    (HERE / "evidence" / "controls.json").write_text(json.dumps(out, indent=1) + "\n")
    print(json.dumps(out))


if __name__ == "__main__":
    main()
