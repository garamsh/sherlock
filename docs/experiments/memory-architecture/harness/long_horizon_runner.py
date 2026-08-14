"""Long-horizon workload runner. 23 architectures × 10-year time horizon.

Each turn-unit = 1 simulated day. 10 sessions across 10 years of Lisa's life.
500 real items + 5000 noise items. Tests whether the architecture can hold
10 years of accumulated memory without losing accuracy.
"""
from __future__ import annotations
import json
import os
import sys
import time
import random
import traceback

sys.path.insert(0, os.path.dirname(__file__))

from workload_spec import LONG_HORIZON_SESSIONS
import architectures
import metrics


ARCH_NAMES = [
    "A0", "A1", "A2", "B1", "B2", "B3", "C3", "D2",
    "E1", "E2", "E3", "E4",
    "F1", "F2", "F3", "F4", "F5",
    "G1", "G2", "G3", "G4", "G5", "G6",
]

CLASS_NAMES = {
    "A0": "NoMemory", "A1": "AutoCaptureUniform", "A2": "FullTextLog",
    "B1": "KeywordOnly", "B2": "SemanticOnly", "B3": "HybridRAG",
    "C3": "EbbinghausDecay", "D2": "CurationOnly",
    "E1": "TwoTierBasic", "E2": "TwoTierReconsolidation",
    "E3": "TwoTierSalience", "E4": "FullCognitive",
    "F1": "SalienceTieredEbbinghaus", "F2": "Supersession",
    "F3": "Compression", "F4": "TieredRetention", "F5": "Combined",
    "G1": "GenerativeAgentsStyle",
    "G2": "EbbinghausInspired",
    "G3": "HierarchicalMemory",
    "G4": "ReflectionBased",
    "G5": "CognitiveIndex",
    "G6": "DecadeStable",
}


def build_long_horizon_queries():
    qs = []
    for s in LONG_HORIZON_SESSIONS:
        qs.extend(s.queries)
    return qs


def long_horizon_workload(target_noise: int = 5000):
    """Storage order: 10-year agent. Real items (curated) + noise."""
    structures = []
    for s in LONG_HORIZON_SESSIONS:
        for idx, (role, content, meta) in enumerate(s.turns):
            structures.append((f"{s.id}.{idx:02d}", role, content, meta))
    # Insert noise sessions between real ones to simulate the noise of daily life.
    noise_sessions = []
    filler_words = ["alpha", "beta", "gamma", "delta", "epsilon", "zeta", "theta", "lambda",
                    "sigma", "omega", "phi", "psi", "lunch", "weather", "weekend", "train",
                    "station", "queue", "regular", "general"]
    for n in range(target_noise // 5):
        random.seed(n + 12345)
        words = " ".join(random.choices(filler_words, k=6))
        for j in range(5):
            noise_sessions.append((f"noise-s{n}.{j:02d}", "user", f"daily filler {words} turn {j}", {"salience": 0.0, "kind": "noise"}))
    return structures, noise_sessions


def eval_long_horizon(name: str, cls, target_noise: int):
    try:
        cur_items, noise_items = long_horizon_workload(target_noise)
        arch = cls()
        # Real items first (simulated years apart).
        for turn_id, role, content, meta in cur_items:
            arch.store(turn_id, role, content, meta)
        for turn_id, role, content, meta in noise_items:
            arch.store(turn_id, role, content, meta)
        queries = build_long_horizon_queries()
        rbq = {}
        for q in queries:
            rbq[q.id] = arch.retrieve(q.text, k=5)
        stats = arch.stats()
        return {
            "name": name,
            "results_by_query": rbq,
            "stats": stats,
            "item_count": int(stats.get("item_count", 0)),
            "query_ms": float(stats.get("total_query_ms", 0.0)) / max(int(stats.get("query_count", 1)), 1),
        }
    except Exception:
        traceback.print_exc()
        return {"name": name, "error": "raised"}


def render_table(rows):
    headers = ["arch", "R@5", "Acc", "Anti-poll", "Items", "KB", "qry ms", "R/KB"]
    lines = ["| " + " | ".join(headers) + " |",
             "|" + "|".join(["-" * len(h) for h in headers]) + "|"]
    queries = build_long_horizon_queries()
    for r in rows:
        if "error" in r or "results_by_query" not in r:
            row = [r["name"], "—", "—", "—", "—", "—", "—", "—"]
        else:
            rbq = r["results_by_query"]
            r5 = metrics.recall_at_k(queries, rbq, k=5)
            acc = metrics.content_accuracy(queries, rbq)
            ap = sum(1 for qid in rbq if qid.startswith("noise") and not any("alpha" in r.content for r in rbq[qid][:5])) / max(1, 1)
            # items + KB
            kb = r["item_count"] * 0.05
            rkb = r5 / (kb / 1024.0) if kb > 0 else 0
            row = [
                r["name"], f"{r5:.2f}", f"{acc:.2f}",
                f"{ap:.2f}",
                str(r["item_count"]), f"{kb:.1f}",
                f"{r['query_ms']:.1f}", f"{rkb:.3f}",
            ]
        lines.append("| " + " | ".join(row) + " |")
    return "\n".join(lines)


def main():
    os.makedirs("results", exist_ok=True)
    rows = []
    print("Running long-horizon workload: 10 sessions (10 years) + 5000 noise")
    for name in ARCH_NAMES:
        cls = getattr(architectures, CLASS_NAMES.get(name), None)
        if cls is None:
            print(f"  {name}: MISSING class")
            continue
        print(f"  {name} ... ", end="", flush=True)
        r = eval_long_horizon(name, cls, 5000)
        print(f"items={r.get('item_count', '?')} qry_ms={r.get('query_ms', 0):.1f}")
        rows.append(r)
    table = render_table(rows)
    with open("results/long_horizon.md", "w") as f:
        f.write("# Memory Architecture — Long-Horizon Test (10-year agent life)\n\n")
        f.write("10 sessions across 10 simulated years + 5000 noise turns. The architecture must hold 10 years of accumulated memory without losing accuracy.\n\n")
        f.write(table + "\n")
    print("\n" + table)
    with open("results/long_horizon_raw.json", "w") as f:
        json.dump(rows, f, indent=2, default=str)
    print(f"\nWrote {os.path.abspath('results/long_horizon.md')}")


if __name__ == "__main__":
    main()
