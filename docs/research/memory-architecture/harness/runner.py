"""Run the memory architecture experiment.

Outputs:
  results/table.md          — main summary
  results/per_architecture.md — per-architecture detail
  results/raw.json          — raw data
"""
from __future__ import annotations
import json
import os
import sys
import time
import traceback

sys.path.insert(0, os.path.dirname(__file__))

from workload_spec import SESSIONS
import record
import metrics


ALL_NAMES = [
    "A0", "A1", "A2", "B1", "B2", "B3", "C3", "D2",
    "E1", "E2", "E3", "E4",
    "F1", "F2", "F3", "F4", "F5",
]


def import_arch(name):
    """Lazy import so a missing architecture fails the row, not the whole run."""
    import architectures
    cls = getattr(architectures, {
        "A0": "NoMemory", "A1": "AutoCaptureUniform", "A2": "FullTextLog",
        "B1": "KeywordOnly", "B2": "SemanticOnly", "B3": "HybridRAG",
        "C3": "EbbinghausDecay", "D2": "CurationOnly",
        "E1": "TwoTierBasic", "E2": "TwoTierReconsolidation",
        "E3": "TwoTierSalience", "E4": "FullCognitive",
        "F1": "SalienceTieredEbbinghaus",
        "F2": "Supersession",
        "F3": "Compression",
        "F4": "TieredRetention",
        "F5": "Combined",
    }[name], None)
    if cls is None:
        raise ImportError(f"architecture {name} not found")
    return cls


def build_queries():
    all_queries = []
    for s in SESSIONS:
        all_queries.extend(s.queries)
    return all_queries


def run_one(arch_name):
    try:
        cls = import_arch(arch_name)
    except Exception as e:
        return {"name": arch_name, "error": str(e)}

    arch = cls()
    t0 = time.perf_counter()
    # Store all real sessions.
    for s in SESSIONS:
        for idx, (role, content, meta) in enumerate(s.turns):
            arch.store(f"{s.id}.{idx:02d}", role, content, meta)
    # Inject 1000 noise turns.
    for n in range(1000):
        arch.store(f"noise.{n:04d}", "user", f"noise turn {n} filler content {n*7}",
                   {"salience": 0.0, "kind": "noise"})
    storage_bytes = sum(len(s.content) for s in getattr(arch, "items", []))
    # Run all queries (across sessions).
    all_queries = build_queries()
    results_by_query: dict[str, list] = {}
    for q in all_queries:
        results_by_query[q.id] = arch.retrieve(q.text, k=5)
    t1 = time.perf_counter()
    stats = arch.stats()
    # Run anti-pollution probe.
    try:
        ap = metrics.anti_pollution(arch, "pref-indent", ["tabs"])
    except Exception:
        ap = None
    # Compute metrics.
    return {
        "name": arch_name,
        "results_by_query": results_by_query,
        "stats": stats,
        "storage_bytes": storage_bytes,
        "insert_ms": float(stats.get("total_insert_ms", 0.0)),
        "query_ms": float(stats.get("total_query_ms", 0.0)),
        "query_count": int(stats.get("query_count", 0)),
        "item_count": int(stats.get("item_count", 0)),
        "anti_pollution": ap,
        "cls": cls,
        "arch": arch,
        "wall_total_s": t1 - t0,
    }


def render_table(rows):
    headers = [
        "arch", "LoC", "items", "KB", "ins μs",
        "qry μs", "R@5", "P@5", "time@1",
        "noise", "anti-poll", "dedup",
        "acc", "R/stored-KB", "supers", "stale", "rep-keep",
    ]
    lines = ["| " + " | ".join(headers) + " |",
             "|" + "|".join(["-" * len(h) for h in headers]) + "|"]
    for r in rows:
        cls = r.get("cls")
        loc = metrics.lines_of_code(cls) if cls else 0
        row = [
            r["name"],
            str(loc),
            str(r.get("item_count", "—")),
            f"{r.get('storage_bytes', 0) / 1024:.1f}",
            f"{r.get('insert_ms', 0) / max(r.get('item_count', 1), 1) * 1000:.1f}",
            f"{r.get('query_ms', 0) / max(r.get('query_count', 1), 1) * 1000:.1f}",
            f"{r.get('R@5', 0):.2f}",
            f"{r.get('P@5', 0):.2f}",
            f"{r.get('time@1', 0):.2f}",
            f"{r.get('noise', 0):.2f}",
            f"{r.get('anti_poll', 0) if r.get('anti_poll') is not None else '—'}",
            f"{r.get('dedup', 0):.2f}",
            f"{r.get('accuracy', 0):.2f}",
            f"{r.get('R_per_KB', 0):.3f}",
            f"{r.get('supers', 0):.2f}",
            f"{r.get('stale', 0):.2f}",
            str(r.get('rep_keep', '—')),
        ]
        lines.append("| " + " | ".join(row) + " |")
    return "\n".join(lines)


def main():
    os.makedirs("results", exist_ok=True)
    rows = []
    for name in ALL_NAMES:
        print(f"== {name} ==")
        try:
            r = run_one(name)
        except Exception:
            traceback.print_exc()
            r = {"name": name, "error": "raised"}
        rows.append(r)
    # Compute metrics for each row.
    all_queries = build_queries()
    for r in rows:
        if "error" in r or "results_by_query" not in r:
            continue
        rbq = r["results_by_query"]
        r["R@5"] = metrics.recall_at_k(all_queries, rbq, k=5)
        r["P@5"] = metrics.precision_at_k(all_queries, rbq, k=5)
        r["time@1"] = metrics.time_aware_top1(all_queries, rbq)
        r["noise"] = metrics.noise_rejection(all_queries, rbq, k=5)
        r["anti_poll"] = r.get("anti_pollution")
        # Every metric below that reads the store reads the one the run filled.
        arch = r.get("arch")
        r["dedup"] = metrics.dedup_ratio(arch)
        r["accuracy"] = metrics.content_accuracy(all_queries, rbq)
        # New efficiency/precision metrics.
        if arch is not None:
            r["R_per_KB"] = metrics.effective_recall_per_kb(arch, r["R@5"])
            r["supers"] = metrics.supersession_accuracy(arch, rbq, SESSIONS)
            r["stale"] = metrics.stale_recall_rate(arch, rbq)
            r["rep_keep"] = int(metrics.compression_density(arch))
        else:
            r["R_per_KB"] = 0.0
            r["supers"] = 0.0
            r["stale"] = 0.0
            r["rep_keep"] = "—"
    # Write outputs.
    table = render_table(rows)
    with open("results/table.md", "w") as f:
        f.write("# Memory Architecture Experiment — Results\n\n")
        f.write("Architectures:<br/>")
        f.write("A0..A2 = simple & baseline; B1..B3 = retrieval; C3 = time-aware; D2 = curation; E1..E4 = cognitive; F1..F5 = efficiency.\n\n")
        f.write("Metrics:\n")
        f.write("- LoC: lines of code in the architecture class\n")
        f.write("- items: number of items stored\n")
        f.write("- KB: storage size of stored contents (KB)\n")
        f.write("- ins μs / qry μs: average insert / query time (microseconds)\n")
        f.write("- R@5: Recall@5 (any expected keyword in top-5)\n")
        f.write("- P@5: Precision@5 (% of top-5 that are relevant)\n")
        f.write("- time@1: Time-aware top-1 (recency queries return most recent at position 1)\n")
        f.write("- noise: Noise rejection (% of noise queries that return no relevant content)\n")
        f.write("- anti-poll: Anti-pollution (recall after 1000 noise turns)\n")
        f.write("- dedup: Unique items / total items\n")
        f.write("- accuracy: % of retrieved chunks with content from a known keyword family\n")
        f.write("- R/stored-KB: Recall per KB of the bytes actually stored. Not the\n")
        f.write("  R/modelled-KB the other result files report.\n")
        f.write("- supers: Supersession accuracy (latest version of fact appears in top-5)\n")
        f.write("- stale: Stale recall rate (% of retrieved items containing old/superseded content)\n")
        f.write("- rep-keep: Repetition-session items kept (lower = more compression)\n\n")
        f.write("| " + table.split("\n", 1)[0][2:] + "\n")
        f.write(table + "\n")
    print(table)
    # Per-architecture detail.
    with open("results/per_architecture.md", "w") as f:
        f.write("# Per-architecture detail\n\n")
        # The Contents list documentation.md §Format asks of a document
        # past ~50 lines. This record's section headings are the run's
        # rows, so the list is emitted from the same rows in the same
        # order; a hand-written one would go stale on the next run.
        f.write("## Contents\n")
        for r in rows:
            f.write(f"- {r['name']}\n")
        f.write("\n")
        for r in rows:
            f.write(f"## {r['name']}\n\n")
            if "error" in r:
                f.write(f"ERROR: {r['error']}\n\n")
                continue
            f.write(f"- items: {r.get('item_count')}\n")
            f.write(f"- storage: {r.get('storage_bytes', 0) / 1024:.1f} KB\n")
            f.write(f"- avg insert: {r.get('insert_ms', 0) / max(r.get('item_count', 1), 1) * 1000:.1f} μs\n")
            f.write(f"- avg query: {r.get('query_ms', 0) / max(r.get('query_count', 1), 1) * 1000:.1f} μs\n")
            f.write(f"- R@5: {r.get('R@5', 0):.3f}\n")
            f.write(f"- P@5: {r.get('P@5', 0):.3f}\n")
            f.write(f"- time@1: {r.get('time@1', 0):.3f}\n")
            f.write(f"- noise: {r.get('noise', 0):.3f}\n")
            f.write(f"- anti-poll: {r.get('anti_poll', 0) if r.get('anti_poll') is not None else '—'}\n")
            f.write(f"- dedup: {r.get('dedup', 0):.3f}\n")
            f.write(f"- accuracy: {r.get('accuracy', 0):.3f}\n")
            f.write(f"- LoC: {metrics.lines_of_code(r['cls']) if r.get('cls') else 0}\n\n")
    # Raw JSON (without the class objects or the stores they built).
    raw_rows = []
    for r in rows:
        rr = {k: v for k, v in r.items() if k not in ("cls", "arch")}
        raw_rows.append(rr)
    with open("results/raw.json", "w") as f:
        json.dump(raw_rows, f, indent=2, default=str)
    print(f"\nWrote {os.path.abspath('results/table.md')}")
    print(f"Rewrote {record.write()} from the committed results")


if __name__ == "__main__":
    main()
