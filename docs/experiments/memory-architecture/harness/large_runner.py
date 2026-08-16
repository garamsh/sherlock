"""Large-scale workload runner. Compares M-architectures (new paradigms)
plus the previous winners on a 10k real + 50k noise workload.
"""
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


# Seeds the per-session topic draw and the noise filler, so a run reproduces.
TOPIC_SEED = 0
NOISE_SEED = 0

NEW = ["M1", "M2", "M3", "M4", "M5", "M6"]
WINNERS = ["D2", "F5", "G4", "G6", "H1", "H6"]
NEW_CLASS = {
    "M1": "HopfieldAssociative", "M2": "KanervaSDM", "M3": "HNSWMemory",
    "M4": "LSHMemory", "M5": "WorkingMemoryLru", "M6": "LsmMemory",
}
WINNER_CLASS = {
    "D2": "CurationOnly", "F5": "Combined", "G4": "ReflectionBased",
    "G6": "DecadeStable", "H1": "H1G6G4", "H6": "H6TripleHybrid",
}


def make_large_workload(n_sessions=100, n_real_per_session=100, n_noise=50000):
    """Build a large workload with anchored queries.

    For each "topic" (a person + a fact), store several items that
    contain the topic keywords, and a query that asks about the topic.
    This guarantees the retained items can actually be retrieved.
    """
    topics = [
        # topic_id, list of (keyword_phrase, salience)
        ("manager", [
            ("Otto is my manager", 0.7),
            ("Otto is the architect", 0.6),
            ("I have been with Otto three years", 0.5),
            ("Otto writes architecture docs", 0.5),
            ("Otto handles deploys", 0.5),
            ("Otto is senior", 0.4),
            ("Otto likes Go", 0.4),
            ("Otto prefers Go for systems", 0.4),
            ("Otto reviews PRs", 0.4),
            ("Otto is on Slack", 0.3),
        ]),
        ("junior", [
            ("Maria is the junior engineer", 0.7),
            ("Maria writes Python", 0.6),
            ("Maria joined last month", 0.5),
            ("Maria prefers pytest", 0.5),
            ("Maria works on the platform team", 0.5),
            ("Maria skims coverage", 0.4),
            ("Maria handles deploys", 0.4),
            ("Maria bikes", 0.4),
            ("Maria drinks coffee", 0.4),
            ("Maria is on Slack", 0.3),
        ]),
        ("deploy", [
            ("Deploy uses Helm", 0.7),
            ("Deploy audit log Kafka", 0.6),
            ("Deploy needs Postgres", 0.5),
            ("Deploy uses Datadog", 0.5),
            ("Deploy uses OpenTelemetry", 0.4),
            ("Deploy uses Slack", 0.4),
            ("Deploy uses ArgoCD", 0.4),
            ("Deploy uses Postgres 14", 0.4),
            ("Deploy is weekly", 0.3),
            ("Deploy is on AWS", 0.3),
        ]),
        ("lang", [
            ("I prefer Python", 0.7),
            ("I prefer Go for systems", 0.6),
            ("I prefer Rust for hot paths", 0.5),
            ("I think Python is great for tooling", 0.5),
            ("I use Go for backend", 0.4),
            ("I avoid Java", 0.4),
            ("I like TypeScript for web", 0.3),
            ("I think Rust is great for systems", 0.3),
            ("I prefer Django for web", 0.3),
            ("I avoid PHP", 0.3),
        ]),
        ("editor", [
            ("I use VS Code", 0.7),
            ("I switched to VS Code", 0.6),
            ("I prefer VS Code", 0.5),
            ("VS Code has good Python support", 0.4),
            ("VS Code has good Go support", 0.4),
            ("I use VS Code for work", 0.4),
            ("I use VS Code for personal", 0.3),
            ("I prefer VS Code over Vim", 0.3),
            ("VS Code is open source", 0.3),
            ("VS Code is free", 0.3),
        ]),
        ("drink", [
            ("I drink coffee", 0.7),
            ("Maria drinks coffee", 0.6),
            ("I switched to coffee", 0.5),
            ("I like coffee in the morning", 0.5),
            ("I avoid energy drinks", 0.4),
            ("Maria drinks coffee too", 0.4),
            ("I like cold brew", 0.4),
            ("Otto drinks cold brew", 0.4),
            ("I don't drink tea", 0.3),
            ("I drink water", 0.3),
        ]),
        ("bike", [
            ("I bike to work", 0.7),
            ("I commute by bike", 0.6),
            ("I bike three days a week", 0.5),
            ("I bike along the Mission bike lane", 0.5),
            ("I bike to Dolores Park", 0.4),
            ("I bike to Golden Gate Park", 0.4),
            ("I bike along the Embarcadero", 0.4),
            ("Maria bikes", 0.4),
            ("I bike in the morning", 0.3),
            ("I bike on weekends", 0.3),
        ]),
        ("track", [
            ("Maria works on the platform team", 0.7),
            ("Maria manages metrics", 0.6),
            ("Maria handles deploys", 0.5),
            ("Maria works on Postgres", 0.4),
            ("Maria is on Slack", 0.3),
            ("Maria writes Python", 0.3),
            ("Maria is in the platform team", 0.3),
            ("Maria works on observability", 0.3),
            ("Maria works on the metrics service", 0.3),
            ("Maria is junior", 0.3),
        ]),
        ("review", [
            ("Otto reviews code", 0.7),
            ("Otto reviews PRs", 0.6),
            ("I prefer pair reviews", 0.5),
            ("Otto ships small PRs", 0.4),
            ("Maria reviews code too", 0.4),
            ("Review is required for merges", 0.4),
            ("Otto does the final review", 0.3),
            ("I prefer async reviews", 0.3),
            ("PR review is required", 0.3),
            ("Review happens on GitHub", 0.3),
        ]),
        ("infra", [
            ("We use Postgres 14", 0.7),
            ("We use OpenTelemetry", 0.6),
            ("We use Kafka", 0.5),
            ("We use Datadog", 0.5),
            ("We use ArgoCD", 0.4),
            ("We use Slack", 0.4),
            ("We use GitHub", 0.4),
            ("We use AWS", 0.4),
            ("We use Istio", 0.3),
            ("We use GCP", 0.3),
        ]),
        ("personal", [
            ("My best friend is Sam", 0.7),
            ("Sam and I have known each other ten years", 0.5),
            ("Sam lives in Brooklyn", 0.4),
            ("Sam is a designer", 0.4),
            ("Sam has a dog named Pixel", 0.3),
            ("Sam uses they pronouns", 0.3),
            ("Sam is non-binary", 0.3),
            ("Sam and I talk weekly", 0.3),
            ("Sam is a great friend", 0.3),
            ("Sam lives in New York", 0.3),
        ]),
        ("family", [
            ("My partner is Jordan", 0.7),
            ("Jordan and I live together", 0.5),
            ("Jordan is an architect", 0.4),
            ("Jordan has a cat named Pixel", 0.4),
            ("My sister is Anna", 0.4),
            ("Anna lives in Seattle", 0.3),
            ("Anna is a therapist", 0.3),
            ("Mom is 67", 0.3),
            ("Mom is a retired teacher", 0.3),
            ("Mom lives in Portland", 0.3),
        ]),
    ]
    cur_items: list[tuple] = []
    queries = []
    # Which topics a session draws was left to the global random state, which is
    # seeded from the OS at import, so every process built a different workload
    # and scored a different one. The draw runs off its own seeded stream now.
    topic_rng = random.Random(TOPIC_SEED)
    for s in range(n_sessions):
        # Each session uses multiple topics so it has more real items.
        n_topics = max(1, n_real_per_session // 10)
        session_topics = topic_rng.sample(topics, min(n_topics, len(topics)))
        for topic_id, snippets in session_topics:
            for idx, (text, sal) in enumerate(snippets):
                cur_items.append((f"session-{s}.{topic_id}.{idx:03d}", "user", text,
                                 {"salience": sal, "kind": topic_id}))
        # First session generates queries for the topics.
        if s == 0:
            for topic_id, snippets in session_topics:
                for text, _ in snippets:
                    if "Otto is my manager" in text or "Otto is the architect" in text:
                        queries.append(("q-Otto", "Who is Otto?", ["Otto"]))
                    if "Maria is the junior engineer" in text:
                        queries.append(("q-Maria-junior", "Who is the junior engineer?", ["Maria"]))
                    if "Maria writes Python" in text or "Maria prefers pytest" in text:
                        queries.append(("q-Maria-py", "Who writes Python?", ["Maria"]))
                    if "I prefer Python" in text:
                        queries.append(("q-py", "What language do I prefer?", ["Python"]))
                    if "I prefer Go for systems" in text:
                        queries.append(("q-go", "What language for systems?", ["Go"]))
                    if "I use VS Code" in text or "I switched to VS Code" in text:
                        queries.append(("q-vs", "What editor do I use?", ["VS Code"]))
                    if "I drink coffee" in text or "Maria drinks coffee" in text:
                        queries.append(("q-coffee", "What does Maria drink?", ["coffee"]))
                    if "I bike to work" in text:
                        queries.append(("q-bike", "How do I commute?", ["bike"]))
                    if "Postgres 14" in text or "Postgres 14" in text:
                        queries.append(("q-pg", "What database?", ["Postgres"]))
                    if "Helm" in text or "Deploy uses Helm" in text:
                        queries.append(("q-deploy", "How is deployment done?", ["Helm"]))
                    if "Kafka" in text:
                        queries.append(("q-kafka", "What about Kafka?", ["Kafka"]))
                    if "review" in text or "PRs" in text:
                        queries.append(("q-review", "PR review process?", ["review"]))
                    if "Sam" in text and "best friend" in text:
                        queries.append(("q-Sam", "Who is my best friend?", ["Sam"]))
                    if "Jordan" in text and "partner" in text:
                        queries.append(("q-Jordan", "Who is my partner?", ["Jordan"]))
    seen = set()
    unique_queries = []
    for q in queries:
        if q[0] not in seen:
            seen.add(q[0])
            unique_queries.append(q)
    queries = unique_queries
    noise_rng = random.Random(NOISE_SEED)
    filler_words = ["alpha", "beta", "gamma", "delta", "epsilon", "zeta", "theta", "lambda",
                    "sigma", "omega", "phi", "psi", "lunch", "weather", "weekend", "train",
                    "station", "queue", "regular", "general"]
    noise_items = []
    for i in range(n_noise):
        words = " ".join(noise_rng.choices(filler_words, k=5))
        noise_items.append((f"noise.{i:06d}", "user", f"filler {words}", {"salience": 0.0, "kind": "noise"}))
    return cur_items, noise_items, queries


def load_queries():
    """Unused — large workload generates its own queries."""
    return []


def eval_one(name, cls, n_sessions=100, n_real_per_session=100, n_noise=50000):
    try:
        cur_items, noise_items, queries = make_large_workload(n_sessions, n_real_per_session, n_noise)
        arch = cls()
        for tid, role, content, meta in cur_items:
            arch.store(tid, role, content, meta)
        for tid, role, content, meta in noise_items:
            arch.store(tid, role, content, meta)
        # Build Query objects for the metrics layer.
        from dataclasses import dataclass
        @dataclass
        class Q:
            id: str
            text: str
            expected_keywords: list[str]
        qs = [Q(id=q[0], text=q[1], expected_keywords=q[2]) for q in queries]
        rbq = {}
        for q in qs:
            try:
                rbq[q.id] = arch.retrieve(q.text, k=5)
            except Exception:
                rbq[q.id] = []
        s = arch.stats()
        return {
            "name": name,
            "results_by_query": rbq,
            "item_count": int(s.get("item_count", 0)),
            "query_ms": float(s.get("total_query_ms", 0)) / max(int(s.get("query_count", 1)), 1),
        }
    except Exception:
        traceback.print_exc()
        return {"name": name, "error": "raised"}


def render(rows, queries):
    headers = ["arch", "R@5", "Items", "modelled KB", "qry ms", "R/modelled-KB"]
    lines = ["| " + " | ".join(headers) + " |",
             "|" + "|".join(["-" * len(h) for h in headers]) + "|"]
    for r in rows:
        if "error" in r or "results_by_query" not in r:
            row = [r["name"], "—", "—", "—", "—", "—"]
        else:
            rbq = r["results_by_query"]
            from dataclasses import dataclass
            @dataclass
            class QQ:
                id: str
                text: str
                expected_keywords: list[str]
            qs = [QQ(id=q[0], text=q[1], expected_keywords=q[2]) for q in queries]
            r5 = metrics.recall_at_k(qs, rbq, k=5)
            kb = metrics.modelled_kb(r["item_count"])
            rkb = metrics.recall_per_modelled_kb(r["item_count"], r5)
            row = [r["name"], f"{r5:.2f}", str(r["item_count"]), f"{kb:.1f}",
                   f"{r['query_ms']:.1f}", f"{rkb:.4g}"]
        lines.append("| " + " | ".join(row) + " |")
    return "\n".join(lines)


def main():
    os.makedirs("results", exist_ok=True)
    # Probe queries to display count.
    cur_probe, _, qs = make_large_workload(10, 100, 100)
    print(f"Large workload: 100 sessions × 100 turns = 10,000 real items + 50,000 noise = 60,000 items")
    print(f"Queries: {len(qs)}")
    SMALL = ["H1", "H6"]
    REAL_SESSIONS_FULL = 100
    REAL_SESSIONS_SMALL = 30
    rows = []
    all_names = NEW + WINNERS
    for name in all_names:
        cls = None
        if name in NEW_CLASS:
            cls = getattr(architectures, NEW_CLASS[name], None)
        elif name in WINNER_CLASS:
            cls = getattr(architectures, WINNER_CLASS[name], None)
        if cls is None:
            print(f"  {name}: MISSING")
            continue
        n_sessions = REAL_SESSIONS_SMALL if name in SMALL else REAL_SESSIONS_FULL
        print(f"  {name} (nsess={n_sessions}) ... ", end="", flush=True)
        t0 = time.perf_counter()
        r = eval_one(name, cls, n_sessions=n_sessions)
        elapsed = time.perf_counter() - t0
        print(f"items={r.get('item_count', '?')} qry_ms={r.get('query_ms', 0):.1f} t={elapsed:.1f}s")
        rows.append(r)
    table = render(rows, qs)
    with open("results/large_scale.md", "w") as f:
        f.write("# Memory Architecture — Large-Scale Test (60k items)\n\n")
        f.write("10,000 real items (100 sessions × 100 turns) + 50,000 noise = 60,000 items. M-architectures (new paradigms) vs previous winners (D2, F5, G4, G6, H1, H6).\n\n")
        f.write("Note: H1 and H6 ran on a smaller workload (30 sessions × 100 turns = 3,000 real items) due to runtime cost.\n\n")
        f.write(table + "\n")
    print("\n" + table)
    with open("results/large_scale_raw.json", "w") as f:
        json.dump(rows, f, indent=2, default=str)
    print(f"\nWrote {os.path.abspath('results/large_scale.md')}")


if __name__ == "__main__":
    main()
