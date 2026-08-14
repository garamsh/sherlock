"""Metric definitions for the memory architecture experiment."""
from __future__ import annotations
import inspect
from typing import Iterable


def recall_at_k(queries, results_by_query, k=5) -> float:
    """Across all queries, fraction where any expected_keyword appears in top-k."""
    hits = 0
    total = 0
    for q in queries:
        if not q.expected_keywords:
            continue
        total += 1
        results = results_by_query.get(q.id, [])[:k]
        if any(any(kw.lower() in r.content.lower() for kw in q.expected_keywords) for r in results):
            hits += 1
    return hits / total if total else 0.0


def precision_at_k(queries, results_by_query, k=5) -> float:
    """Of top-k, fraction that match expected keywords."""
    total_top = 0
    total_relevant = 0
    for q in queries:
        if not q.expected_keywords:
            continue
        results = results_by_query.get(q.id, [])
        for r in results[:k]:
            total_top += 1
            if any(kw.lower() in r.content.lower() for kw in q.expected_keywords):
                total_relevant += 1
    return total_relevant / total_top if total_top else 0.0


def time_aware_top1(queries, results_by_query) -> float:
    hits = 0
    total = 0
    for q in queries:
        if not q.recency:
            continue
        total += 1
        results = results_by_query.get(q.id, [])
        if results and results[0].turn_id == q.expect_turn_id:
            hits += 1
    return hits / total if total else 0.0


def noise_rejection(queries, results_by_query, k=5) -> float:
    """Fraction of noise queries where top-k contains no content from any session 1-4 query."""
    hits = 0
    total = 0
    for q in queries:
        if not q.noise:
            continue
        total += 1
        results = results_by_query.get(q.id, [])
        # Any match with the noise query's own keywords means it returned
        # relevant content (which is a failure for noise).
        relevant_present = any(any(kw.lower() in r.content.lower() for kw in q.expected_keywords) for r in results[:k])
        if not relevant_present:
            hits += 1
    return hits / total if total else 0.0


def anti_pollution(arch, probe_query_id: str, probe_expected_keywords: list[str], k=5) -> float:
    """After 1000 noise turns, can the architecture still recall curated memories?"""
    results = arch.retrieve("What indentation does the user prefer?", k)
    if not results:
        return 0.0
    return 1.0 if any(any(kw.lower() in r.content.lower() for kw in probe_expected_keywords) for r in results[:k]) else 0.0


def dedup_ratio(arch) -> float:
    """Unique items / total items."""
    items = getattr(arch, "items", None)
    if not items:
        return 1.0
    seen = set()
    total = 0
    for it in items:
        total += 1
        seen.add(it.content)
    return len(seen) / total if total else 0.0


def content_accuracy(queries, results_by_query) -> float:
    """Fraction of retrieved chunks that mention a recognized keyword family."""
    all_kw = set()
    for q in queries:
        for kw in q.expected_keywords:
            all_kw.add(kw.lower())
    if not all_kw:
        return 0.0
    matches = 0
    total = 0
    for q in queries:
        for r in results_by_query.get(q.id, []):
            total += 1
            if any(kw in r.content.lower() for kw in all_kw):
                matches += 1
    return matches / total if total else 0.0


def lines_of_code(arch_class) -> int:
    try:
        src = inspect.getsource(arch_class)
    except (OSError, TypeError):
        return 0
    return len(src.splitlines())


def component_count(arch_class) -> int:
    """Rough heuristic: count attribute definitions and data structures."""
    try:
        src = inspect.getsource(arch_class)
    except (OSError, TypeError):
        return 0
    # Count class-level dict/list instantiations and Counter calls — proxies for data structures.
    return sum(src.count(name) for name in ("Counter(", "dict(", "{}"))


def effective_recall_per_kb(arch, R5: float) -> float:
    """Recall per KB of storage. Higher = more useful memory per byte."""
    items = getattr(arch, "items", None)
    if not items:
        return 0.0
    bytes_stored = sum(len(it.content) for it in items)
    if bytes_stored == 0:
        return 0.0
    return R5 / (bytes_stored / 1024.0)


def supersession_accuracy(arch, queries: dict, sessions: list) -> float:
    """For the supersession session, does the latest version of a preference appear in top-5?"""
    arch_items = {it.turn_id: it for it in getattr(arch, "items", [])}
    # The supersession session's last user-preference turn is the latest.
    # Queries are: "sup-final-indent", "sup-final-hints", "sup-final-state".
    targets = {
        "sup-final-indent": ("Supersession", "4-space"),
        "sup-final-hints": ("Supersession", "public"),
        "sup-final-state": ("Supersession", "4-space"),
    }
    hits = 0
    total = 0
    for qid, (sid, kw) in targets.items():
        results = queries.get(qid, [])
        total += 1
        if any(kw in r.content for r in results[:5]):
            hits += 1
    return hits / total if total else 0.0


def compression_density(arch) -> float:
    """For the repetition session, count stored items. Less = more compressed."""
    arch_items = [it for it in getattr(arch, "items", []) if it.turn_id.startswith("repetition.")]
    return float(len(arch_items))


def retention_correctness(arch, queries: dict) -> float:
    """Recall on the curated sessions (project-conventions, domain-knowledge, task-progress, correction)
    restricted to the architecture's stored items. Higher = better at keeping what matters."""
    arch_items = {it.turn_id for it in getattr(arch, "items", [])}
    curated_prefixes = ("project-conventions", "domain-knowledge", "task-progress", "correction")
    filtered = {qid: [r for r in results if any(r.turn_id.startswith(p) for p in curated_prefixes)]
                for qid, results in queries.items()}
    hits = 0
    total = 0
    for qid, results in filtered.items():
        if not results:
            continue
        total += 1
        if any(r.metadata.get("salience", 0) > 0 and any(kw in r.content.lower() for kw in ("tabs", "test", "PEP 8", "setuid", "capability token", "audit", "report")) for r in results[:5]):
            hits += 1
    return hits / total if total else 0.0


def stale_recall_rate(arch, queries: dict) -> float:
    """Fraction of top-5 results that match a 'currently-stored' item but represent a SUPERSEDED
    version of the fact. We approximate: any retrieved item whose content contains '2-space' or
    'tabs' (the OLD preferences) when the current state is 4-space — that's stale."""
    arch_items = {it.turn_id: it.content for it in getattr(arch, "items", [])}
    query_ids = ["sup-final-indent", "sup-final-hints"]
    stale = 0
    total = 0
    for qid in query_ids:
        results = queries.get(qid, [])
        for r in results[:5]:
            total += 1
            if "2-space" in r.content.lower() or "always use tabs" in r.content.lower():
                stale += 1
    return stale / total if total else 0.0


def content_density(arch) -> float:
    """Ratio of distinct content chars to stored chars. indicator of dedup."""
    items = getattr(arch, "items", [])
    if not items:
        return 0.0
    total = sum(len(it.content) for it in items)
    distinct = sum(len(set(it.content)) for it in items)
    return distinct / total if total else 0.0
