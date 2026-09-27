"""The H series, H1-H6. Explicit hybrids: direct combinations of the mechanisms above.

One file per series letter, which is how the runners, the record and the
result tables address these; the package `__init__` is the surface they
import.
"""
from __future__ import annotations
import math
from collections import Counter

from interface import Memory, RetrievalResult, StoreItem, get_now_ms
from .similarity import tokenize, hash_vector, cosine, tfidf_cosine


# ============================================================================
# H-architectures: explicit hybrids — combining proven mechanisms.
# These are NOT hypothetical combinations. They are concrete classes that
# combine named mechanisms and are tested against the same workloads.
# ============================================================================


# ---------------------------------------------------------------------------
# H1: G6 (DecadeStable) + G4 (Reflection) hybrid
# Storage discipline from G6 (tiered retention + supersession + Ebbinghaus).
# Periodic reflection on the curated store from G4 (summary every N inserts).
# ---------------------------------------------------------------------------

class H1G6G4(Memory):
    NAME = "H1"
    SUPERSESSION_THRESHOLD = 0.7
    RETENTION_TURNS = 100
    HALF_LIFE_HOURS = 24.0
    REFLECT_EVERY = 6

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.salience: list[float] = []
        self.insert_time: list[float] = []
        self.insert_idx: list[int] = []
        self.turn_count: int = 0
        self.inserts_since_reflect: int = 0
        self.total_insert_ms = 0.0
        self.total_query_ms = 0.0
        self.query_count = 0

    def _recompute_idf(self):
        N = len(self.doc_tokens)
        df: Counter[str] = Counter()
        for toks in self.doc_tokens:
            for t in set(toks):
                df[t] += 1
        self.idf = {t: math.log((N + 1) / (1 + d)) + 1 for t, d in df.items()}

    def _forget_stale(self):
        keep = []
        for i, item in enumerate(self.items):
            salience = self.salience[i]
            if salience >= 0.6:
                keep.append(i)
                continue
            age = self.turn_count - self.insert_idx[i]
            if age < self.RETENTION_TURNS:
                keep.append(i)
        if len(keep) != len(self.items):
            self.items = [self.items[i] for i in keep]
            self.doc_tokens = [self.doc_tokens[i] for i in keep]
            self.vecs = [self.vecs[i] for i in keep]
            self.salience = [self.salience[i] for i in keep]
            self.insert_time = [self.insert_time[i] for i in keep]
            self.insert_idx = [self.insert_idx[i] for i in keep]
            self._recompute_idf()

    def _reflect(self):
        if len(self.items) < self.REFLECT_EVERY:
            return
        recent = list(range(len(self.items) - self.REFLECT_EVERY, len(self.items)))
        high_sal = [i for i in recent if self.salience[i] >= 0.5]
        if not high_sal:
            return
        summ = " | ".join(f"{self.items[i].role}: {self.items[i].content[:60]}" for i in high_sal)
        self.items.append(StoreItem(f"reflect-{len(self.items)}", "reflection",
                                   f"[reflection of {len(high_sal)} items] {summ[:400]}",
                                   {"salience": 0.6, "kind": "reflection"}))
        self.doc_tokens.append(tokenize(self.items[-1].content))
        self.vecs.append(hash_vector(self.doc_tokens[-1]))
        self.salience.append(0.6)
        self.insert_time.append(get_now_ms() / 1000.0)
        self.insert_idx.append(self.turn_count)
        self._recompute_idf()

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        if salience <= 0.0:
            self.turn_count += 1
            self.total_insert_ms += get_now_ms() - t
            return
        new_tokens = tokenize(content)
        new_vec = hash_vector(new_tokens)
        superseded_idx: int | None = None
        best_sim = 0.0
        for i, item in enumerate(self.items):
            sim = cosine(new_vec, self.vecs[i])
            if sim > best_sim and sim > self.SUPERSESSION_THRESHOLD:
                best_sim = sim
                superseded_idx = i
        if superseded_idx is not None:
            self.items[superseded_idx] = StoreItem(turn_id, role, content, metadata)
            self.doc_tokens[superseded_idx] = new_tokens
            self.vecs[superseded_idx] = new_vec
            self.salience[superseded_idx] = salience
            self.insert_time[superseded_idx] = get_now_ms() / 1000.0
            self.insert_idx[superseded_idx] = self.turn_count
        else:
            self.items.append(StoreItem(turn_id, role, content, metadata))
            self.doc_tokens.append(new_tokens)
            self.vecs.append(new_vec)
            self.salience.append(salience)
            self.insert_time.append(get_now_ms() / 1000.0)
            self.insert_idx.append(self.turn_count)
        self.turn_count += 1
        self.inserts_since_reflect += 1
        if self.inserts_since_reflect >= self.REFLECT_EVERY:
            self._reflect()
            self.inserts_since_reflect = 0
        self._recompute_idf()
        self._forget_stale()
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        now = get_now_ms() / 1000.0
        qv = hash_vector(tokenize(query))
        scored = []
        for i, item in enumerate(self.items):
            age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
            half_life_hours = self.HALF_LIFE_HOURS * (1.0 + self.salience[i])
            recency = math.exp(-math.log(2) * age_hours / half_life_hours)
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
            boost = 0.3 if item.metadata.get("kind") == "reflection" else 0.0
            score = (1.0 * self.salience[i] +
                     1.0 * recency +
                     1.0 * rel +
                     boost)
            scored.append((score, i, item))
        scored.sort(key=lambda x: x[0], reverse=True)
        out = []
        for s, _, item in scored[:k]:
            if s > 0:
                out.append(RetrievalResult(item.turn_id, item.role, item.content, float(s), item.metadata))
        self.total_query_ms += get_now_ms() - t
        self.query_count += 1
        return out

    def stats(self):
        return {
            "name": self.NAME,
            "item_count": len(self.items),
            "total_insert_ms": self.total_insert_ms,
            "total_query_ms": self.total_query_ms,
            "query_count": self.query_count,
        }


# ---------------------------------------------------------------------------
# H2: G6 + G7 (Recursive 2-hop) hybrid
# Storage discipline from G6. Retrieval uses G7's 2-hop on the curated store.
# ---------------------------------------------------------------------------

class H2G6G7(Memory):
    NAME = "H2"
    SUPERSESSION_THRESHOLD = 0.7
    RETENTION_TURNS = 100
    HALF_LIFE_HOURS = 24.0
    HOP_DECAY = 0.5

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.salience: list[float] = []
        self.insert_time: list[float] = []
        self.insert_idx: list[int] = []
        self.turn_count: int = 0
        self.total_insert_ms = 0.0
        self.total_query_ms = 0.0
        self.query_count = 0

    def _recompute_idf(self):
        N = len(self.doc_tokens)
        df: Counter[str] = Counter()
        for toks in self.doc_tokens:
            for t in set(toks):
                df[t] += 1
        self.idf = {t: math.log((N + 1) / (1 + d)) + 1 for t, d in df.items()}

    def _forget_stale(self):
        keep = []
        for i, item in enumerate(self.items):
            if self.salience[i] >= 0.6:
                keep.append(i)
                continue
            if self.turn_count - self.insert_idx[i] < self.RETENTION_TURNS:
                keep.append(i)
        if len(keep) != len(self.items):
            self.items = [self.items[i] for i in keep]
            self.doc_tokens = [self.doc_tokens[i] for i in keep]
            self.vecs = [self.vecs[i] for i in keep]
            self.salience = [self.salience[i] for i in keep]
            self.insert_time = [self.insert_time[i] for i in keep]
            self.insert_idx = [self.insert_idx[i] for i in keep]
            self._recompute_idf()

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        if salience <= 0.0:
            self.turn_count += 1
            self.total_insert_ms += get_now_ms() - t
            return
        new_tokens = tokenize(content)
        new_vec = hash_vector(new_tokens)
        superseded_idx: int | None = None
        best_sim = 0.0
        for i, item in enumerate(self.items):
            sim = cosine(new_vec, self.vecs[i])
            if sim > best_sim and sim > self.SUPERSESSION_THRESHOLD:
                best_sim = sim
                superseded_idx = i
        if superseded_idx is not None:
            self.items[superseded_idx] = StoreItem(turn_id, role, content, metadata)
            self.doc_tokens[superseded_idx] = new_tokens
            self.vecs[superseded_idx] = new_vec
            self.salience[superseded_idx] = salience
            self.insert_time[superseded_idx] = get_now_ms() / 1000.0
            self.insert_idx[superseded_idx] = self.turn_count
        else:
            self.items.append(StoreItem(turn_id, role, content, metadata))
            self.doc_tokens.append(new_tokens)
            self.vecs.append(new_vec)
            self.salience.append(salience)
            self.insert_time.append(get_now_ms() / 1000.0)
            self.insert_idx.append(self.turn_count)
        self.turn_count += 1
        self._recompute_idf()
        self._forget_stale()
        self.total_insert_ms += get_now_ms() - t

    def _rank(self, query, k):
        if not self.items:
            return []
        qv = hash_vector(tokenize(query))
        now = get_now_ms() / 1000.0
        scored = []
        for i, item in enumerate(self.items):
            age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
            half_life_hours = self.HALF_LIFE_HOURS * (1.0 + self.salience[i])
            recency = math.exp(-math.log(2) * age_hours / half_life_hours)
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
            score = self.salience[i] + recency + rel
            scored.append((RetrievalResult(item.turn_id, item.role, item.content, float(score), item.metadata), score))
        scored.sort(key=lambda x: x[1], reverse=True)
        return scored[:k]

    def _extract_concepts(self, results):
        seen = set()
        out = []
        for r, _ in results:
            for w in tokenize(r.content):
                if self.idf.get(w, 0) > 0.5 and w not in seen:
                    seen.add(w)
                    out.append(w)
                if len(out) >= 5:
                    break
            if len(out) >= 5:
                break
        return out

    def retrieve(self, query, k=5):
        t = get_now_ms()
        results_w: dict[str, tuple[RetrievalResult, float]] = {}
        # First hop.
        for r, s in self._rank(query, k):
            results_w[r.turn_id] = (r, s)
        # Second hop on concepts from top-k.
        for r, _ in list(results_w.values())[:3]:
            for concept in self._extract_concepts([r]):
                for r2, s2 in self._rank(concept, 2):
                    decayed = s2 * self.HOP_DECAY
                    existing = results_w.get(r2.turn_id)
                    if existing is None or decayed > existing[1]:
                        results_w[r2.turn_id] = (r2, decayed)
        ranked = sorted(results_w.values(), key=lambda x: x[1], reverse=True)
        out = [r for r, _ in ranked[:k]]
        self.total_query_ms += get_now_ms() - t
        self.query_count += 1
        return out

    def stats(self):
        return {
            "name": self.NAME,
            "item_count": len(self.items),
            "total_insert_ms": self.total_insert_ms,
            "total_query_ms": self.total_query_ms,
            "query_count": self.query_count,
        }


# ---------------------------------------------------------------------------
# H3: G4 (Reflection) + G7 (Recursive) hybrid
# Periodic summary items (G4) become the higher-level concepts for the
# 2-hop traversal. The first hop surfaces relevant raw items + summaries;
# the second hop follows references from the summaries.
# ---------------------------------------------------------------------------

class H3G4G7(Memory):
    NAME = "H3"
    REFLECT_EVERY = 8
    HOP_DECAY = 0.5

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.salience: list[float] = []
        self.insert_time: list[float] = []
        self.inserts_since_reflect: int = 0
        self.total_insert_ms = 0.0
        self.total_query_ms = 0.0
        self.query_count = 0

    def _recompute_idf(self):
        N = len(self.doc_tokens)
        df: Counter[str] = Counter()
        for toks in self.doc_tokens:
            for t in set(toks):
                df[t] += 1
        self.idf = {t: math.log((N + 1) / (1 + d)) + 1 for t, d in df.items()}

    def _reflect(self):
        if len(self.items) < self.REFLECT_EVERY:
            return
        recent = list(range(len(self.items) - self.REFLECT_EVERY, len(self.items)))
        high_sal = [i for i in recent if self.salience[i] >= 0.5]
        if not high_sal:
            return
        summ = " | ".join(f"{self.items[i].role}: {self.items[i].content[:60]}" for i in high_sal)
        self.items.append(StoreItem(f"reflect-{len(self.items)}", "reflection",
                                   f"[reflection of {len(high_sal)} items] {summ[:400]}",
                                   {"salience": 0.6, "kind": "reflection"}))
        self.doc_tokens.append(tokenize(self.items[-1].content))
        self.vecs.append(hash_vector(self.doc_tokens[-1]))
        self.salience.append(0.6)
        self.insert_time.append(get_now_ms() / 1000.0)
        self._recompute_idf()

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        if salience <= 0.0:
            self.total_insert_ms += get_now_ms() - t
            return
        self.items.append(StoreItem(turn_id, role, content, metadata))
        self.doc_tokens.append(tokenize(content))
        self.vecs.append(hash_vector(self.doc_tokens[-1]))
        self.salience.append(salience)
        self.insert_time.append(get_now_ms() / 1000.0)
        self.inserts_since_reflect += 1
        if self.inserts_since_reflect >= self.REFLECT_EVERY:
            self._reflect()
            self.inserts_since_reflect = 0
        self._recompute_idf()
        self.total_insert_ms += get_now_ms() - t

    def _rank(self, query, k):
        if not self.items:
            return []
        qv = hash_vector(tokenize(query))
        now = get_now_ms() / 1000.0
        scored = []
        for i, item in enumerate(self.items):
            age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
            recency = math.exp(-math.log(2) * age_hours / 24.0)
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
            tier = self.salience[i] ** 0.5
            boost = 0.3 if item.metadata.get("kind") == "reflection" else 0.0
            score = rel * 1.0 + recency * 0.5 + tier * 0.5 + boost
            scored.append((RetrievalResult(item.turn_id, item.role, item.content, float(score), item.metadata), score))
        scored.sort(key=lambda x: x[1], reverse=True)
        return scored[:k]

    def _extract_concepts(self, results):
        seen = set()
        out = []
        for r, _ in results:
            for w in tokenize(r.content):
                if self.idf.get(w, 0) > 0.5 and w not in seen:
                    seen.add(w)
                    out.append(w)
                if len(out) >= 5:
                    break
            if len(out) >= 5:
                break
        return out

    def retrieve(self, query, k=5):
        t = get_now_ms()
        results_w: dict[str, tuple[RetrievalResult, float]] = {}
        for r, s in self._rank(query, k):
            results_w[r.turn_id] = (r, s)
        for r, _ in list(results_w.values())[:3]:
            for concept in self._extract_concepts([r]):
                for r2, s2 in self._rank(concept, 2):
                    decayed = s2 * self.HOP_DECAY
                    existing = results_w.get(r2.turn_id)
                    if existing is None or decayed > existing[1]:
                        results_w[r2.turn_id] = (r2, decayed)
        ranked = sorted(results_w.values(), key=lambda x: x[1], reverse=True)
        out = [r for r, _ in ranked[:k]]
        self.total_query_ms += get_now_ms() - t
        self.query_count += 1
        return out

    def stats(self):
        return {
            "name": self.NAME,
            "item_count": len(self.items),
            "total_insert_ms": self.total_insert_ms,
            "total_query_ms": self.total_query_ms,
            "query_count": self.query_count,
        }


# ---------------------------------------------------------------------------
# H4: F5 (Compression + Supersession + TieredRetention) + G4 (Reflection)
# Combine both compression mechanisms. F5's tiered retention + supersession
# + G4's reflection together.
# ---------------------------------------------------------------------------

class H4F5G4(Memory):
    NAME = "H4"
    SUPERSESSION_THRESHOLD = 0.7
    RETENTION_TURNS = 50
    REFLECT_EVERY = 5

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.salience: list[float] = []
        self.insert_time: list[float] = []
        self.insert_idx: list[int] = []
        self.turn_count: int = 0
        self.inserts_since_reflect: int = 0
        self.total_insert_ms = 0.0
        self.total_query_ms = 0.0
        self.query_count = 0

    def _recompute_idf(self):
        N = len(self.doc_tokens)
        df: Counter[str] = Counter()
        for toks in self.doc_tokens:
            for t in set(toks):
                df[t] += 1
        self.idf = {t: math.log((N + 1) / (1 + d)) + 1 for t, d in df.items()}

    def _forget_stale(self):
        keep = []
        for i, item in enumerate(self.items):
            if self.salience[i] >= 0.6:
                keep.append(i)
                continue
            if self.turn_count - self.insert_idx[i] < self.RETENTION_TURNS:
                keep.append(i)
        if len(keep) != len(self.items):
            self.items = [self.items[i] for i in keep]
            self.doc_tokens = [self.doc_tokens[i] for i in keep]
            self.vecs = [self.vecs[i] for i in keep]
            self.salience = [self.salience[i] for i in keep]
            self.insert_time = [self.insert_time[i] for i in keep]
            self.insert_idx = [self.insert_idx[i] for i in keep]
            self._recompute_idf()

    def _reflect(self):
        if len(self.items) < self.REFLECT_EVERY:
            return
        recent = list(range(len(self.items) - self.REFLECT_EVERY, len(self.items)))
        high_sal = [i for i in recent if self.salience[i] >= 0.5]
        if not high_sal:
            return
        summ = " | ".join(f"{self.items[i].role}: {self.items[i].content[:60]}" for i in high_sal)
        self.items.append(StoreItem(f"reflect-{len(self.items)}", "reflection",
                                   f"[reflection of {len(high_sal)} items] {summ[:400]}",
                                   {"salience": 0.6, "kind": "reflection"}))
        self.doc_tokens.append(tokenize(self.items[-1].content))
        self.vecs.append(hash_vector(self.doc_tokens[-1]))
        self.salience.append(0.6)
        self.insert_time.append(get_now_ms() / 1000.0)
        self.insert_idx.append(self.turn_count)
        self._recompute_idf()

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        if salience <= 0.0:
            self.turn_count += 1
            self.total_insert_ms += get_now_ms() - t
            return
        new_tokens = tokenize(content)
        new_vec = hash_vector(new_tokens)
        superseded_idx: int | None = None
        best_sim = 0.0
        for i, item in enumerate(self.items):
            sim = cosine(new_vec, self.vecs[i])
            if sim > best_sim and sim > self.SUPERSESSION_THRESHOLD:
                best_sim = sim
                superseded_idx = i
        if superseded_idx is not None:
            self.items[superseded_idx] = StoreItem(turn_id, role, content, metadata)
            self.doc_tokens[superseded_idx] = new_tokens
            self.vecs[superseded_idx] = new_vec
            self.salience[superseded_idx] = salience
            self.insert_time[superseded_idx] = get_now_ms() / 1000.0
            self.insert_idx[superseded_idx] = self.turn_count
        else:
            self.items.append(StoreItem(turn_id, role, content, metadata))
            self.doc_tokens.append(new_tokens)
            self.vecs.append(new_vec)
            self.salience.append(salience)
            self.insert_time.append(get_now_ms() / 1000.0)
            self.insert_idx.append(self.turn_count)
        self.turn_count += 1
        self.inserts_since_reflect += 1
        if self.inserts_since_reflect >= self.REFLECT_EVERY:
            self._reflect()
            self.inserts_since_reflect = 0
        self._recompute_idf()
        self._forget_stale()
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        qv = hash_vector(tokenize(query))
        scored = []
        for i, item in enumerate(self.items):
            age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
            recency = math.exp(-math.log(2) * age_hours / 24.0)
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
            tier = self.salience[i] ** 0.5
            boost = 0.3 if item.metadata.get("kind") == "reflection" else 0.0
            score = rel * 1.0 + recency * 0.5 + tier * 0.5 + boost
            scored.append((score, i, item))
        scored.sort(key=lambda x: x[0], reverse=True)
        out = []
        for s, _, item in scored[:k]:
            if s > 0:
                out.append(RetrievalResult(item.turn_id, item.role, item.content, float(s), item.metadata))
        self.total_query_ms += get_now_ms() - t
        self.query_count += 1
        return out

    def stats(self):
        return {
            "name": self.NAME,
            "item_count": len(self.items),
            "total_insert_ms": self.total_insert_ms,
            "total_query_ms": self.total_query_ms,
            "query_count": self.query_count,
        }


# ---------------------------------------------------------------------------
# H5: Episodic + Semantic split (HippoRAG-inspired)
# Two-tier: a high-recall "episodic" stream (raw items) + a "semantic"
# index (high-salience items + summaries). Retrieval first hits the index
# for relevance, then pulls episo­dic items for context.
# ---------------------------------------------------------------------------

class H5EpisodicSemantic(Memory):
    NAME = "H5"
    INDEX_THRESHOLD = 0.5
    REFLECT_EVERY = 8

    def __init__(self):
        self.episodic: list[StoreItem] = []
        self.episodic_tokens: list[list[str]] = []
        self.episodic_vecs: list[list[int]] = []
        self.index: list[StoreItem] = []
        self.index_tokens: list[list[str]] = []
        self.index_vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.salience: list[float] = []  # mirrors episodic
        self.insert_time: list[float] = []
        self.inserts_since_reflect: int = 0
        self.total_insert_ms = 0.0
        self.total_query_ms = 0.0
        self.query_count = 0

    def _recompute_idf(self):
        all_tokens = self.episodic_tokens + self.index_tokens
        N = len(all_tokens)
        df: Counter[str] = Counter()
        for toks in all_tokens:
            for t in set(toks):
                df[t] += 1
        self.idf = {t: math.log((N + 1) / (1 + d)) + 1 for t, d in df.items()}

    def _reflect(self):
        if len(self.episodic) < self.REFLECT_EVERY:
            return
        recent = list(range(len(self.episodic) - self.REFLECT_EVERY, len(self.episodic)))
        high_sal = [i for i in recent if self.salience[i] >= 0.5]
        if not high_sal:
            return
        summ = " | ".join(f"{self.episodic[i].role}: {self.episodic[i].content[:60]}" for i in high_sal)
        idx = (f"reflect-{len(self.index)}", "reflection",
               f"[reflection of {len(high_sal)} items] {summ[:400]}",
               {"salience": 0.6, "kind": "reflection"})
        self.index.append(StoreItem(*idx))
        self.index_tokens.append(tokenize(self.index[-1].content))
        self.index_vecs.append(hash_vector(self.index_tokens[-1]))
        self._recompute_idf()

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        if salience <= 0.0:
            self.total_insert_ms += get_now_ms() - t
            return
        toks = tokenize(content)
        # Always store in episodic.
        self.episodic.append(StoreItem(turn_id, role, content, metadata))
        self.episodic_tokens.append(toks)
        self.episodic_vecs.append(hash_vector(toks))
        self.salience.append(salience)
        self.insert_time.append(get_now_ms() / 1000.0)
        # Promote high-salience items to the index.
        if salience >= self.INDEX_THRESHOLD:
            self.index.append(StoreItem(turn_id, role, content, metadata))
            self.index_tokens.append(toks)
            self.index_vecs.append(hash_vector(toks))
        self.inserts_since_reflect += 1
        if self.inserts_since_reflect >= self.REFLECT_EVERY:
            self._reflect()
            self.inserts_since_reflect = 0
        self._recompute_idf()
        self.total_insert_ms += get_now_ms() - t

    def _rank(self, query, items, tokens, vecs, k):
        if not items:
            return []
        qv = hash_vector(tokenize(query))
        now = get_now_ms() / 1000.0
        scored = []
        for i, item in enumerate(items):
            age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
            recency = math.exp(-math.log(2) * age_hours / 24.0)
            rel = 0.5 * (tfidf_cosine(query, tokens[i], self.idf) + cosine(qv, vecs[i]))
            score = rel * 1.0 + recency * 0.5
            scored.append((RetrievalResult(item.turn_id, item.role, item.content, float(score), item.metadata), score))
        scored.sort(key=lambda x: x[1], reverse=True)
        return scored[:k]

    def retrieve(self, query, k=5):
        t = get_now_ms()
        # 1. Hit the index first.
        index_results = self._rank(query, self.index, self.index_tokens, self.index_vecs, k)
        # 2. Then pull episodic items tied to the same query.
        episodic_results = self._rank(query, self.episodic, self.episodic_tokens, self.episodic_vecs, k)
        # Merge, prefer index items.
        seen = set()
        merged = []
        for r, s in index_results + episodic_results:
            if r.turn_id not in seen:
                seen.add(r.turn_id)
                merged.append(r)
        out = merged[:k]
        self.total_query_ms += get_now_ms() - t
        self.query_count += 1
        return out

    def stats(self):
        return {
            "name": self.NAME,
            "item_count": len(self.episodic),
            "total_insert_ms": self.total_insert_ms,
            "total_query_ms": self.total_query_ms,
            "query_count": self.query_count,
        }


# ---------------------------------------------------------------------------
# H6: Triple hybrid (G6 + G4 + G7): tiered retention + reflection + 2-hop.
# All three proven mechanisms combined.
# ---------------------------------------------------------------------------

class H6TripleHybrid(Memory):
    NAME = "H6"
    SUPERSESSION_THRESHOLD = 0.7
    RETENTION_TURNS = 100
    REFLECT_EVERY = 8
    HOP_DECAY = 0.5

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.salience: list[float] = []
        self.insert_time: list[float] = []
        self.insert_idx: list[int] = []
        self.turn_count: int = 0
        self.inserts_since_reflect: int = 0
        self.total_insert_ms = 0.0
        self.total_query_ms = 0.0
        self.query_count = 0

    def _recompute_idf(self):
        N = len(self.doc_tokens)
        df: Counter[str] = Counter()
        for toks in self.doc_tokens:
            for t in set(toks):
                df[t] += 1
        self.idf = {t: math.log((N + 1) / (1 + d)) + 1 for t, d in df.items()}

    def _forget_stale(self):
        keep = []
        for i, item in enumerate(self.items):
            if self.salience[i] >= 0.6:
                keep.append(i)
                continue
            if self.turn_count - self.insert_idx[i] < self.RETENTION_TURNS:
                keep.append(i)
        if len(keep) != len(self.items):
            self.items = [self.items[i] for i in keep]
            self.doc_tokens = [self.doc_tokens[i] for i in keep]
            self.vecs = [self.vecs[i] for i in keep]
            self.salience = [self.salience[i] for i in keep]
            self.insert_time = [self.insert_time[i] for i in keep]
            self.insert_idx = [self.insert_idx[i] for i in keep]
            self._recompute_idf()

    def _reflect(self):
        if len(self.items) < self.REFLECT_EVERY:
            return
        recent = list(range(len(self.items) - self.REFLECT_EVERY, len(self.items)))
        high_sal = [i for i in recent if self.salience[i] >= 0.5]
        if not high_sal:
            return
        summ = " | ".join(f"{self.items[i].role}: {self.items[i].content[:60]}" for i in high_sal)
        self.items.append(StoreItem(f"reflect-{len(self.items)}", "reflection",
                                   f"[reflection of {len(high_sal)} items] {summ[:400]}",
                                   {"salience": 0.6, "kind": "reflection"}))
        self.doc_tokens.append(tokenize(self.items[-1].content))
        self.vecs.append(hash_vector(self.doc_tokens[-1]))
        self.salience.append(0.6)
        self.insert_time.append(get_now_ms() / 1000.0)
        self.insert_idx.append(self.turn_count)
        self._recompute_idf()

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        if salience <= 0.0:
            self.turn_count += 1
            self.total_insert_ms += get_now_ms() - t
            return
        new_tokens = tokenize(content)
        new_vec = hash_vector(new_tokens)
        superseded_idx: int | None = None
        best_sim = 0.0
        for i, item in enumerate(self.items):
            sim = cosine(new_vec, self.vecs[i])
            if sim > best_sim and sim > self.SUPERSESSION_THRESHOLD:
                best_sim = sim
                superseded_idx = i
        if superseded_idx is not None:
            self.items[superseded_idx] = StoreItem(turn_id, role, content, metadata)
            self.doc_tokens[superseded_idx] = new_tokens
            self.vecs[superseded_idx] = new_vec
            self.salience[superseded_idx] = salience
            self.insert_time[superseded_idx] = get_now_ms() / 1000.0
            self.insert_idx[superseded_idx] = self.turn_count
        else:
            self.items.append(StoreItem(turn_id, role, content, metadata))
            self.doc_tokens.append(new_tokens)
            self.vecs.append(new_vec)
            self.salience.append(salience)
            self.insert_time.append(get_now_ms() / 1000.0)
            self.insert_idx.append(self.turn_count)
        self.turn_count += 1
        self.inserts_since_reflect += 1
        if self.inserts_since_reflect >= self.REFLECT_EVERY:
            self._reflect()
            self.inserts_since_reflect = 0
        self._recompute_idf()
        self._forget_stale()
        self.total_insert_ms += get_now_ms() - t

    def _rank(self, query, k):
        if not self.items:
            return []
        qv = hash_vector(tokenize(query))
        now = get_now_ms() / 1000.0
        scored = []
        for i, item in enumerate(self.items):
            age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
            recency = math.exp(-math.log(2) * age_hours / 24.0)
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
            tier = self.salience[i] ** 0.5
            boost = 0.3 if item.metadata.get("kind") == "reflection" else 0.0
            score = rel * 1.0 + recency * 0.5 + tier * 0.5 + boost
            scored.append((RetrievalResult(item.turn_id, item.role, item.content, float(score), item.metadata), score))
        scored.sort(key=lambda x: x[1], reverse=True)
        return scored[:k]

    def _extract_concepts(self, results):
        seen = set()
        out = []
        for r, _ in results:
            for w in tokenize(r.content):
                if self.idf.get(w, 0) > 0.5 and w not in seen:
                    seen.add(w)
                    out.append(w)
                if len(out) >= 5:
                    break
            if len(out) >= 5:
                break
        return out

    def retrieve(self, query, k=5):
        t = get_now_ms()
        results_w: dict[str, tuple[RetrievalResult, float]] = {}
        for r, s in self._rank(query, k):
            results_w[r.turn_id] = (r, s)
        for r, _ in list(results_w.values())[:3]:
            for concept in self._extract_concepts([r]):
                for r2, s2 in self._rank(concept, 2):
                    decayed = s2 * self.HOP_DECAY
                    existing = results_w.get(r2.turn_id)
                    if existing is None or decayed > existing[1]:
                        results_w[r2.turn_id] = (r2, decayed)
        ranked = sorted(results_w.values(), key=lambda x: x[1], reverse=True)
        out = [r for r, _ in ranked[:k]]
        self.total_query_ms += get_now_ms() - t
        self.query_count += 1
        return out

    def stats(self):
        return {
            "name": self.NAME,
            "item_count": len(self.items),
            "total_insert_ms": self.total_insert_ms,
            "total_query_ms": self.total_query_ms,
            "query_count": self.query_count,
        }
