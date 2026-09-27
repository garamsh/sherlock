"""F's controls (../../preregistration.md §F's controls), from the recorded
facts and the selected corpus.

    python3 research/memory-encoding/validation/arms/facts/controls.py   # evidence/controls.json
"""

import importlib.util
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
STUDY = HERE.parent.parent


def _score():
    spec = importlib.util.spec_from_file_location("validation_score", STUDY / "score.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


S = _score()
SHARE = 0.9


def controls(corpus, recorded):
    by_first = {b["first"]: b["facts"] for b in recorded}
    batches = len(corpus["stream"]) // 4
    up = [c for c in corpus["chains"] if c["family"] == "UP"]
    # A fact exchange fills its batch alone, so its source opens the batch.
    user = [c["id"] for c in up if any(f["kind"] == "user" and S.states(f["text"], c["answer"]) for f in by_first.get(c["hops"][0]["source"], []))]
    out = {
        "stream_batches": batches, "batches_with_facts_array": len(by_first),
        "facts_stored_share": round(len(by_first) / batches, 4),
        "up_items": len(up), "up_with_user_fact": len(user), "up_user_share": round(len(user) / max(len(up), 1), 4),
        "up_missing": sorted(set(c["id"] for c in up) - set(user)),
    }
    out["passed"] = out["facts_stored_share"] >= SHARE and out["up_user_share"] >= SHARE
    return out


def main():
    corpus = json.loads((STUDY / "corpus.json").read_text())
    recorded = S.load_jsonl(HERE / "evidence" / "facts-b4.jsonl")
    out = controls(corpus, recorded)
    (HERE / "evidence" / "controls.json").write_text(json.dumps(out, indent=1) + "\n")
    print(json.dumps(out))


if __name__ == "__main__":
    main()
