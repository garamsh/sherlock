"""Scale test: same 5 sessions' queries, but with the store padded to 5k/25k/50k items.

Tests whether architecture accuracy and latency hold as the store grows.
"""
from __future__ import annotations
import json
import os
import sys
import time
import random
import traceback

sys.path.insert(0, os.path.dirname(__file__))

from workload_spec import SESSIONS
import record
import architectures
import metrics


ALL_NAMES = [
    "A0", "A1", "A2", "B1", "B2", "B3", "C3", "D2",
    "E1", "E2", "E3", "E4",
    "F1", "F2", "F3", "F4", "F5",
]

SCALES = [1052, 5000]
CLASS_NAMES = {
    "A0": "NoMemory", "A1": "AutoCaptureUniform", "A2": "FullTextLog",
    "B1": "KeywordOnly", "B2": "SemanticOnly", "B3": "HybridRAG",
    "C3": "EbbinghausDecay", "D2": "CurationOnly",
    "E1": "TwoTierBasic", "E2": "TwoTierReconsolidation",
    "E3": "TwoTierSalience", "E4": "FullCognitive",
    "F1": "SalienceTieredEbbinghaus", "F2": "Supersession",
    "F3": "Compression", "F4": "TieredRetention", "F5": "Combined",
}


def build_queries():
    qs = []
    for s in SESSIONS:
        qs.extend(s.queries)
    return qs


def make_noise_session(idx: int, n_turns: int = 10):
    """Generate a synthetic noise session — content that should not match the curated queries."""
    turns = []
    for j in range(n_turns):
        # Random filler content with no shared tokens with the curated queries.
        words = ["alpha", "beta", "gamma", "delta", "epsilon", "zeta", "theta", "lambda",
                 "sigma", "omega", "phi", "psi", "fox", "tree", "river", "mountain",
                 "cloud", "stone", "leaf", "breeze"]
        random.seed(idx * 1000 + j)
        content = " ".join(random.choices(words, k=8))
        turns.append((f"user", content, {"salience": 0.0, "kind": "noise"}))
    return turns


def pad_to_scale(arch_name: str, cls, target_items: int):
    """Store the curated sessions, then pad with synthetic noise sessions.

    Padding is bounded by total store attempts (not kept items), since
    selectivity-based architectures (D2, E4) drop noise.
    """
    arch = cls()
    for s in SESSIONS:
        for idx, (role, content, meta) in enumerate(s.turns):
            arch.store(f"{s.id}.{idx:02d}", role, content, meta)
    attempts = 50  # 50 curated + 50 noise sessions = 550 items attempted
    max_extra_noise_sessions = (target_items // 10) + 5
    sess_idx = 0
    while attempts < target_items and sess_idx < max_extra_noise_sessions:
        turns = make_noise_session(sess_idx)
        for idx, (role, content, meta) in enumerate(turns):
            arch.store(f"noise-s{sess_idx}.{idx:02d}", role, content, meta)
        attempts += 10
        sess_idx += 1
    return arch


def eval_at_scale(arch_name: str, cls, queries, target_items: int):
    try:
        arch = pad_to_scale(arch_name, cls, target_items)
    except Exception as e:
        traceback.print_exc()
        return {"name": arch_name, "scale": target_items, "error": str(e)}
    rbq = {}
    for q in queries:
        rbq[q.id] = arch.retrieve(q.text, k=5)
    stats = arch.stats()
    return {
        "name": arch_name,
        "scale": target_items,
        "results_by_query": rbq,
        "stats": stats,
        "item_count": int(stats.get("item_count", 0)),
        "query_ms": float(stats.get("total_query_ms", 0.0)) / max(int(stats.get("query_count", 1)), 1),
    }


def render_scale_table(rows, queries):
    headers = ["arch", "scale", "items", "R@5", "P@5", "qry μs", "R/modelled-KB"]
    lines = ["| " + " | ".join(headers) + " |",
             "|" + "|".join(["-" * len(h) for h in headers]) + "|"]
    for r in rows:
        if "error" in r or "results_by_query" not in r:
            row = [r["name"], str(r["scale"]), "—", "—", "—", "—", "—"]
        else:
            rbq = r["results_by_query"]
            r5 = metrics.recall_at_k(queries, rbq, k=5)
            p5 = metrics.precision_at_k(queries, rbq, k=5)
            kb = metrics.modelled_kb(r["item_count"])
            rkb = metrics.recall_per_modelled_kb(r["item_count"], r5)
            row = [
                r["name"], str(r["scale"]), str(r["item_count"]),
                f"{r5:.2f}", f"{p5:.2f}", f"{r['query_ms'] * 1000:.1f}",
                f"{rkb:.4g}",
            ]
        lines.append("| " + " | ".join(row) + " |")
    return "\n".join(lines)


def main():
    os.makedirs("results", exist_ok=True)
    queries = build_queries()
    all_rows = []
    for scale in SCALES:
        print(f"\n=== Scale {scale} items ===")
        for name in ALL_NAMES:
            cls = getattr(architectures, CLASS_NAMES.get(name), None)
            if cls is None:
                continue
            print(f"  {name} ... ", end="", flush=True)
            r = eval_at_scale(name, cls, queries, scale)
            print(f"items={r.get('item_count', '?')} qry_ms={r.get('query_ms', 0):.2f}")
            all_rows.append(r)
    table = render_scale_table(all_rows, queries)
    with open("results/scale.md", "w") as f:
        f.write("# Memory Architecture — Scale Test\n\n")
        f.write(f"Same {len(SESSIONS)} sessions' queries, but the store is padded with synthetic "
                "noise sessions to reach the target scale. Tests whether accuracy and latency "
                "hold as the store grows.\n\n")
        f.write("Metrics:\n")
        f.write("- R@5: Recall@5 (any expected keyword in top-5)\n")
        f.write("- P@5: Precision@5 (% of top-5 that are relevant)\n")
        f.write("- qry μs: per-query latency\n")
        f.write("- R/modelled-KB: R@5 per KB of store modelled at metrics.KB_PER_ITEM an\n")
        f.write("  item. Not the R/stored-KB results/table.md reports.\n\n")
        f.write(table + "\n")
    print("\n" + table)
    with open("results/scale_raw.json", "w") as f:
        json.dump(all_rows, f, indent=2, default=str)
    print(f"\nWrote {os.path.abspath('results/scale.md')}")
    print(f"Rewrote {record.write()} from the committed results")


if __name__ == "__main__":
    main()
