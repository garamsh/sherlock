"""Diverse workload runner. 35 sessions across 8 cognitive categories.

Tests memory architectures against the variety of what a real human
(or agent) actually remembers: people, events, routines, opinions,
procedures, spatial, emotion, relationships. Plus recursive-retrieval
architectures (G7/G8/G9).
"""
from __future__ import annotations
import json
import os
import sys
import time
import random
import traceback

sys.path.insert(0, os.path.dirname(__file__))

from diverse_workload import SESSIONS
import architectures
import metrics


ARCH_NAMES = [
    "A0", "A1", "A2", "B1", "B2", "B3", "C3", "D2",
    "E1", "E2", "E3", "E4",
    "F1", "F2", "F3", "F4", "F5",
    "G1", "G2", "G3", "G4", "G5", "G6",
    "G7", "G8", "G9",
]

CLASS_NAMES = {
    "A0": "NoMemory", "A1": "AutoCaptureUniform", "A2": "FullTextLog",
    "B1": "KeywordOnly", "B2": "SemanticOnly", "B3": "HybridRAG",
    "C3": "EbbinghausDecay", "D2": "CurationOnly",
    "E1": "TwoTierBasic", "E2": "TwoTierReconsolidation",
    "E3": "TwoTierSalience", "E4": "FullCognitive",
    "F1": "SalienceTieredEbbinghaus", "F2": "Supersession",
    "F3": "Compression", "F4": "TieredRetention", "F5": "Combined",
    "G1": "GenerativeAgentsStyle", "G2": "EbbinghausInspired",
    "G3": "HierarchicalMemory", "G4": "ReflectionBased",
    "G5": "CognitiveIndex", "G6": "DecadeStable",
    "G7": "RecursiveRetrieval", "G8": "RecursiveRetrieval3Hop",
    "G9": "SpreadingActivation",
}


def build_queries():
    qs = []
    for s in SESSIONS:
        qs.extend(s.queries)
    return qs


def noise_filler(n: int = 5000):
    items = []
    filler_words = ["alpha", "beta", "gamma", "delta", "epsilon", "zeta", "theta", "lambda",
                    "sigma", "omega", "phi", "psi", "lunch", "weather", "weekend", "train",
                    "station", "queue", "regular", "general"]
    for i in range(n // 5):
        random.seed(i + 12345)
        words = " ".join(random.choices(filler_words, k=6))
        for j in range(5):
            items.append((f"noise.{i:04d}.{j:02d}", "user",
                         f"daily filler {words} turn {j}",
                         {"salience": 0.0, "kind": "noise"}))
    return items


def eval_one(name: str, cls, queries):
    try:
        arch = cls()
        # Real items first.
        for s in SESSIONS:
            for idx, (role, content, meta) in enumerate(s.turns):
                arch.store(f"{s.id}.{idx:02d}", role, content, meta)
        # Noise.
        for turn_id, role, content, meta in noise_filler():
            arch.store(turn_id, role, content, meta)
        rbq = {}
        for q in queries:
            try:
                rbq[q.id] = arch.retrieve(q.text, k=5)
            except Exception:
                rbq[q.id] = []
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


def render_table(rows, queries):
    headers = ["arch", "R@5", "P@5", "Acc", "Items", "KB", "qry ms", "R/KB", "LoC"]
    lines = ["| " + " | ".join(headers) + " |",
             "|" + "|".join(["-" * len(h) for h in headers]) + "|"]
    for r in rows:
        if "error" in r or "results_by_query" not in r:
            row = [r["name"], "—", "—", "—", "—", "—", "—", "—", "—"]
        else:
            rbq = r["results_by_query"]
            r5 = metrics.recall_at_k(queries, rbq, k=5)
            p5 = metrics.precision_at_k(queries, rbq, k=5)
            acc = metrics.content_accuracy(queries, rbq)
            kb = r["item_count"] * 0.05
            rkb = r5 / (kb / 1024.0) if kb > 0 else 0.0
            cls = getattr(architectures, CLASS_NAMES.get(r["name"]), None)
            loc = 0
            try:
                import inspect
                if cls:
                    loc = len(inspect.getsource(cls).splitlines())
            except Exception:
                pass
            row = [
                r["name"], f"{r5:.2f}", f"{p5:.2f}", f"{acc:.2f}",
                str(r["item_count"]), f"{kb:.1f}",
                f"{r['query_ms']:.1f}", f"{rkb:.3f}",
                str(loc),
            ]
        lines.append("| " + " | ".join(row) + " |")
    return "\n".join(lines)


def main():
    os.makedirs("results", exist_ok=True)
    queries = build_queries()
    print("Running diverse workload: 35 sessions × 8 cognitive categories + 5000 noise")
    rows = []
    for name in ARCH_NAMES:
        cls = getattr(architectures, CLASS_NAMES.get(name), None)
        if cls is None:
            print(f"  {name}: MISSING")
            continue
        print(f"  {name} ... ", end="", flush=True)
        r = eval_one(name, cls, queries)
        print(f"items={r.get('item_count', '?')} qry_ms={r.get('query_ms', 0):.1f}")
        rows.append(r)
    table = render_table(rows, queries)
    with open("results/diverse.md", "w") as f:
        f.write("# Memory Architecture — Diverse Workload (8 cognitive categories)\n\n")
        f.write("35 sessions across 8 cognitive categories: Person, Event, Routine, Opinion, Procedure, Spatial, Emotion, Relationship. Plus 5000 noise turns. Tests whether the architecture can handle the variety of what a real human (or agent) remembers.\n\n")
        f.write(table + "\n")
    print("\n" + table)
    with open("results/diverse_raw.json", "w") as f:
        json.dump(rows, f, indent=2, default=str)
    print(f"\nWrote {os.path.abspath('results/diverse.md')}")


if __name__ == "__main__":
    main()
