"""Creation-structure runner: how memory entries come to exist.

Runs the 8 creation structures over the same scripted conversation as the
cycle experiment (system_runner.py's SCRIPT) and scores each on:
- checkpoint recall (is the right fact in the store at probe time)
- correction handling (after "Actually, ..." does the latest version win,
  and is the outdated version gone)
- store size at the end
- extra model calls per turn
- structure size (LoC of the class)
"""
from __future__ import annotations
import inspect
import json
import os
import random
import sys

sys.path.insert(0, os.path.dirname(__file__))

from memory_creation import ALL_CREATION
from system_runner import SCRIPT, CHECKPOINTS


def run_structure(cls, seed: int):
    random.seed(seed)
    sys_inst = cls()
    cp_hits = 0
    cp_total = 0
    correction_latest = 0
    correction_total = 0
    for i, (user_text, planted) in enumerate(SCRIPT):
        sys_inst.turn(user_text, planted)
        for (after_idx, probe_id, question, keywords, _r) in CHECKPOINTS:
            if after_idx == i:
                facts = sys_inst.current_facts()
                blob = " ".join(facts).lower()
                cp_total += 1
                if any(kw.lower() in blob for kw in keywords):
                    cp_hits += 1
                # Correction handling: after "I changed my mind — I drink tea
                # now", the store should carry the tea fact, and the coffee
                # fact should be gone or superseded.
                if probe_id == "cp3b":
                    correction_total += 1
                    active = [e for e in sys_inst.entries if not e.superseded]
                    has_new = any("tea" in e.content.lower() for e in active)
                    has_old = any("coffee" in e.content.lower() for e in active)
                    if has_new and not has_old:
                        correction_latest += 1
    return {
        "system": cls.NAME,
        "checkpoint_recall": cp_hits / cp_total if cp_total else 0.0,
        "correction_latest": correction_latest / correction_total if correction_total else 0.0,
        "store_items": sys_inst.store_size(),
        "extra_calls_per_turn": sys_inst.extra_calls / len(SCRIPT),
        "loc": len(inspect.getsource(cls).splitlines()),
    }


def main():
    os.makedirs("results", exist_ok=True)
    rows = []
    SEEDS = [7, 42, 1234, 2026]
    for cls in ALL_CREATION:
        acc = {"recall": 0.0, "corr": 0.0, "items": 0.0, "calls": 0.0}
        for seed in SEEDS:
            r = run_structure(cls, seed)
            acc["recall"] += r["checkpoint_recall"]
            acc["corr"] += r["correction_latest"]
            acc["items"] += r["store_items"]
            acc["calls"] += r["extra_calls_per_turn"]
        n = len(SEEDS)
        rows.append({
            "system": cls.NAME,
            "checkpoint_recall": acc["recall"] / n,
            "correction_latest": acc["corr"] / n,
            "store_items": acc["items"] / n,
            "extra_calls_per_turn": acc["calls"] / n,
            "loc": len(inspect.getsource(cls).splitlines()),
        })
    headers = ["system", "recall", "latest-wins", "items", "calls/turn", "LoC"]
    lines = ["| " + " | ".join(headers) + " |",
             "|" + "|".join(["-" * len(h) for h in headers]) + "|"]
    for r in rows:
        lines.append("| {} | {:.2f} | {:.2f} | {:.0f} | {:.2f} | {} |".format(
            r["system"], r["checkpoint_recall"], r["correction_latest"],
            r["store_items"], r["extra_calls_per_turn"], r["loc"]))
    table = "\n".join(lines)
    print(table)
    with open("results/creation_raw.json", "w") as f:
        json.dump(rows, f, indent=2)
    with open("results/creation.md", "w") as f:
        f.write("# Memory-creation structures\n\n")
        f.write("Question: how should a memory entry come to exist — who decides, with what vocabulary, and when does maintenance happen?\n\n")
        f.write(table + "\n")
    print(f"\nWrote {os.path.abspath('results/creation.md')}")


if __name__ == "__main__":
    main()
