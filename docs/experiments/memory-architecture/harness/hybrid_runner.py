"""Hybrid runner. Tests H1-H6 against diverse workload + 10-year workload."""
from __future__ import annotations
import json
import os
import sys
import time
import random
import traceback

sys.path.insert(0, os.path.dirname(__file__))

import architectures
import metrics
from diverse_workload import SESSIONS
from workload_spec import LONG_HORIZON_SESSIONS


HYBRIDS = ["H1", "H2", "H3", "H4", "H5", "H6"]
CLASS_NAMES = {
    "H1": "H1G6G4", "H2": "H2G6G7", "H3": "H3G4G7",
    "H4": "H4F5G4", "H5": "H5EpisodicSemantic", "H6": "H6TripleHybrid",
}


def noise_filler(n):
    items = []
    filler_words = ["alpha", "beta", "gamma", "delta", "epsilon", "zeta", "theta", "lambda",
                    "sigma", "omega", "phi", "psi", "lunch", "weather", "weekend", "train",
                    "station", "queue", "regular", "general"]
    for i in range(n // 5):
        random.seed(i + 12345)
        words = " ".join(random.choices(filler_words, k=6))
        for j in range(5):
            items.append((f"noise.{i:04d}.{j:02d}", "user", f"daily filler {words} turn {j}",
                         {"salience": 0.0, "kind": "noise"}))
    return items


def eval_diverse(name, cls):
    try:
        arch = cls()
        for s in SESSIONS:
            for idx, (role, content, meta) in enumerate(s.turns):
                arch.store(f"{s.id}.{idx:02d}", role, content, meta)
        for tid, role, content, meta in noise_filler(5000):
            arch.store(tid, role, content, meta)
        queries = []
        for s in SESSIONS:
            queries.extend(s.queries)
        rbq = {q.id: arch.retrieve(q.text, k=5) for q in queries}
        s = arch.stats()
        return {
            "name": name, "results_by_query": rbq,
            "item_count": int(s.get("item_count", 0)),
            "query_ms": float(s.get("total_query_ms", 0)) / max(int(s.get("query_count", 1)), 1),
        }
    except Exception:
        traceback.print_exc()
        return {"name": name, "error": "raised"}


def eval_long_horizon(name, cls):
    try:
        arch = cls()
        for s in LONG_HORIZON_SESSIONS:
            for idx, (role, content, meta) in enumerate(s.turns):
                arch.store(f"{s.id}.{idx:02d}", role, content, meta)
        for tid, role, content, meta in noise_filler(5000):
            arch.store(tid, role, content, meta)
        queries = []
        for s in LONG_HORIZON_SESSIONS:
            queries.extend(s.queries)
        rbq = {q.id: arch.retrieve(q.text, k=5) for q in queries}
        s = arch.stats()
        return {
            "name": name, "results_by_query": rbq,
            "item_count": int(s.get("item_count", 0)),
            "query_ms": float(s.get("total_query_ms", 0)) / max(int(s.get("query_count", 1)), 1),
        }
    except Exception:
        traceback.print_exc()
        return {"name": name, "error": "raised"}


def render(rows, queries, workload):
    headers = ["arch", "R@5", "P@5", "Acc", "Items", "KB", "qry ms", "R/KB"]
    lines = ["| " + " | ".join(headers) + " |",
             "|" + "|".join(["-" * len(h) for h in headers]) + "|"]
    for r in rows:
        if "error" in r or "results_by_query" not in r:
            row = [r["name"], "—", "—", "—", "—", "—", "—", "—"]
        else:
            rbq = r["results_by_query"]
            r5 = metrics.recall_at_k(queries, rbq, k=5)
            p5 = metrics.precision_at_k(queries, rbq, k=5)
            acc = metrics.content_accuracy(queries, rbq)
            kb = r["item_count"] * 0.05
            rkb = r5 / (kb / 1024.0) if kb > 0 else 0.0
            row = [r["name"], f"{r5:.2f}", f"{p5:.2f}", f"{acc:.2f}",
                   str(r["item_count"]), f"{kb:.1f}",
                   f"{r['query_ms']:.1f}", f"{rkb:.3f}"]
        lines.append("| " + " | ".join(row) + " |")
    return "\n".join(lines)


def main():
    os.makedirs("results", exist_ok=True)
    print("=" * 60)
    print("Diverse workload")
    print("=" * 60)
    diverse_rows = []
    diverse_queries = []
    for s in SESSIONS:
        diverse_queries.extend(s.queries)
    for name in HYBRIDS:
        cls = getattr(architectures, CLASS_NAMES.get(name), None)
        if cls is None:
            print(f"  {name}: MISSING")
            continue
        print(f"  {name} ... ", end="", flush=True)
        r = eval_diverse(name, cls)
        print(f"items={r.get('item_count','?')} qry_ms={r.get('query_ms',0):.1f}")
        diverse_rows.append(r)
    table_diverse = render(diverse_rows, diverse_queries, "diverse")
    with open("results/hybrid_diverse.md", "w") as f:
        f.write("# Hybrid Architectures — Diverse Workload\n\n")
        f.write(table_diverse + "\n")
    print("\n" + table_diverse)

    print("\n" + "=" * 60)
    print("Long-horizon workload (10-year)")
    print("=" * 60)
    long_rows = []
    long_queries = []
    for s in LONG_HORIZON_SESSIONS:
        long_queries.extend(s.queries)
    for name in HYBRIDS:
        cls = getattr(architectures, CLASS_NAMES.get(name), None)
        if cls is None:
            continue
        print(f"  {name} ... ", end="", flush=True)
        r = eval_long_horizon(name, cls)
        print(f"items={r.get('item_count','?')} qry_ms={r.get('query_ms',0):.1f}")
        long_rows.append(r)
    table_long = render(long_rows, long_queries, "long-horizon")
    with open("results/hybrid_long_horizon.md", "w") as f:
        f.write("# Hybrid Architectures — Long-Horizon Workload\n\n")
        f.write(table_long + "\n")
    print("\n" + table_long)

    # Cross-workload comparison.
    print("\n" + "=" * 60)
    print("Cross-workload comparison")
    print("=" * 60)
    lines = ["| arch | R@5 (diverse) | R@5 (long) | avg | R/KB (diverse) |",
             "|---|---:|---:|---:|---:|"]
    for d, l in zip(diverse_rows, long_rows):
        if "error" in d or "error" in l:
            continue
        d_r5 = metrics.recall_at_k(diverse_queries, d["results_by_query"], k=5)
        l_r5 = metrics.recall_at_k(long_queries, l["results_by_query"], k=5)
        avg = (d_r5 + l_r5) / 2
        kb = d["item_count"] * 0.05
        rkb = d_r5 / (kb / 1024.0) if kb > 0 else 0.0
        lines.append(f"| {d['name']} | {d_r5:.2f} | {l_r5:.2f} | {avg:.2f} | {rkb:.3f} |")
    print("\n" + "\n".join(lines))
    with open("results/hybrid_summary.md", "w") as f:
        f.write("# Hybrid Architectures — Cross-workload Summary\n\n")
        f.write("\n".join(lines) + "\n")

    with open("results/hybrid_diverse_raw.json", "w") as f:
        json.dump(diverse_rows, f, indent=2, default=str)
    with open("results/hybrid_long_raw.json", "w") as f:
        json.dump(long_rows, f, indent=2, default=str)


if __name__ == "__main__":
    main()
