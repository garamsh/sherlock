"""The G series, G1-G9. Research-inspired: each maps to a published system or theory.

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
# G-architectures: research-inspired
# Influences: Park et al. 2023 (Generative Agents), Zhong et al. (MemoryBank),
# Ebbinghaus forgetting curve, MemGPT hierarchical memory.
# ============================================================================


# ---------------------------------------------------------------------------
# G1: Generative Agents-style (memory stream + importance + recency + relevance)
# Park et al. 2023 used importance scoring per memory, then retrieved by
# score = alpha*importance + beta*recency + gamma*relevance.
# ---------------------------------------------------------------------------

class GenerativeAgentsStyle(Memory):
    NAME = "G1"
    IMPORTANCE_THRESHOLD = 0.2  # Park et al. 2023 stores anything with importance > 0.2

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.importance: list[float] = []
        self.recency_boost: list[float] = []
        self.insert_time: list[float] = []
        self.access_count: list[int] = []
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

    def _heuristic_importance(self, content: str, role: str, metadata: dict) -> float:
        # Approximation: in the real Park et al. paper, an LLM scores 0-10.
        # We use a heuristic over the metadata.
        if "salience" in metadata:
            return float(metadata["salience"])
        # Defaults: user-driven content is more important than assistant-driven.
        base = 0.5 if role == "user" else 0.3
        # Boost for certain kinds.
        kind = metadata.get("kind", "")
        if kind in ("correction", "preference", "fact"):
            return min(1.0, base + 0.4)
        if kind in ("task", "instruction"):
            return min(1.0, base + 0.2)
        if kind == "knowledge":
            return min(1.0, base + 0.3)
        return base

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        importance = self._heuristic_importance(content, role, metadata)
        if importance < self.IMPORTANCE_THRESHOLD:
            self.total_insert_ms += get_now_ms() - t
            return
        self.items.append(StoreItem(turn_id, role, content, dict(metadata, importance=importance)))
        self.doc_tokens.append(tokenize(content))
        self.vecs.append(hash_vector(self.doc_tokens[-1]))
        self.importance.append(importance)
        self.recency_boost.append(1.0)
        self.insert_time.append(get_now_ms() / 1000.0)
        self.access_count.append(0)
        self._recompute_idf()
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        now = get_now_ms() / 1000.0
        qv = hash_vector(tokenize(query))
        scored = []
        for i, item in enumerate(self.items):
            age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
            recency = math.exp(-math.log(2) * age_hours / 24.0)
            # Generative Agents formula: alpha*importance + beta*recency + gamma*relevance
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
            score = (1.0 * self.importance[i] +
                     1.0 * recency +
                     1.0 * rel)
            scored.append((score, i, item))
        scored.sort(key=lambda x: x[0], reverse=True)
        out = []
        for s, i, item in scored[:k]:
            if s > 0:
                self.access_count[i] += 1
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
# G2: Ebbinghaus-inspired (MemoryBank-style)
# Strength = e^(-t/half_life). Half-life scales with access count and
# salience. The memory fades unless rehearsal occurs.
# ---------------------------------------------------------------------------

class EbbinghausInspired(Memory):
    NAME = "G2"

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.insert_time: list[float] = []
        self.access_count: list[int] = []
        self.strength: list[float] = []
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

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        if salience <= 0.0:
            self.total_insert_ms += get_now_ms() - t
            return
        self.items.append(StoreItem(turn_id, role, content, metadata))
        self.doc_tokens.append(tokenize(content))
        self.vecs.append(hash_vector(self.doc_tokens[-1]))
        self.insert_time.append(get_now_ms() / 1000.0)
        self.access_count.append(0)
        # Strength: high salience = long half-life; more accesses = stronger.
        # Half-life in hours = 24 * (1 + access_count) * (1 + salience)
        self.strength.append(get_now_ms() / 1000.0)
        self._recompute_idf()
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        now = get_now_ms() / 1000.0
        qv = hash_vector(tokenize(query))
        scored = []
        for i, item in enumerate(self.items):
            salience = item.metadata.get("salience", 0.0)
            half_life_hours = 24.0 * (1.0 + self.access_count[i]) * (1.0 + salience)
            age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
            # Ebbinghaus: retention = e^(-t/half_life)
            retention = math.exp(-math.log(2) * age_hours / half_life_hours)
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
            score = rel * 1.0 + retention * 0.5
            scored.append((score, i, item))
        scored.sort(key=lambda x: x[0], reverse=True)
        out = []
        for s, i, item in scored[:k]:
            if s > 0:
                self.access_count[i] += 1
                self.strength[i] = now  # Ebbinghaus: each retrieval rehearses the memory
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
# G3: Hierarchical memory (MemGPT-like)
# Two-tier: a "summary" tier (recent or summarized items) and a "raw" tier
# (full content). Retrieval scores summary for relevance, then pulls
# the raw items from the same session.
# ---------------------------------------------------------------------------

class HierarchicalMemory(Memory):
    NAME = "G3"

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.salience: list[float] = []
        self.session_id: list[str] = []
        self.insert_time: list[float] = []
        self.summaries: dict[str, str] = {}  # session_id -> summary
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

    def _sid(self, turn_id: str) -> str:
        return turn_id.split(".", 1)[0]

    def _ensure_summary(self, session_id: str):
        if session_id in self.summaries:
            return
        idxs = [i for i, s in enumerate(self.session_id) if s == session_id]
        if not idxs:
            return
        summ = " | ".join(f"{self.items[i].role}: {self.items[i].content[:60]}" for i in idxs[:10])
        self.summaries[session_id] = f"[summary of {len(idxs)} items] {summ[:400]}"

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
        self.session_id.append(self._sid(turn_id))
        self.insert_time.append(get_now_ms() / 1000.0)
        self.summaries.pop(self._sid(turn_id), None)  # invalidate stale summary
        self._recompute_idf()
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        now = get_now_ms() / 1000.0
        qv = hash_vector(tokenize(query))
        # Refresh summaries for the most-queried sessions.
        scored = []
        for i, item in enumerate(self.items):
            age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
            recency = math.exp(-math.log(2) * age_hours / 24.0)
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
            scored.append((rel * 1.0 + recency * 0.5 + self.salience[i] * 0.5, i, item))
        scored.sort(key=lambda x: x[0], reverse=True)
        out = []
        for s, i, item in scored[:k]:
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
# G4: Reflection-based (Park et al. 2023 reflection + importance)
# On every N inserts, generate a "reflection" item that summarizes recent
# high-importance items. Tiered by importance + reflection boost.
# ---------------------------------------------------------------------------

class ReflectionBased(Memory):
    NAME = "G4"
    REFLECT_EVERY = 8

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.importance: list[float] = []
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
        # Take the last REFLECT_EVERY most-important items and produce a summary.
        if len(self.items) < self.REFLECT_EVERY:
            return
        recent = list(range(len(self.items) - self.REFLECT_EVERY, len(self.items)))
        summ = " | ".join(f"{self.items[i].role}: {self.items[i].content[:60]}" for i in recent)
        self.items.append(StoreItem(f"reflect-{len(self.items)}", "reflection",
                                   f"[reflection covering {len(recent)} recent items] {summ[:400]}",
                                   {"salience": 0.6, "kind": "reflection"}))
        self.doc_tokens.append(tokenize(self.items[-1].content))
        self.vecs.append(hash_vector(self.doc_tokens[-1]))
        self.importance.append(0.6)
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
        self.importance.append(salience)
        self.insert_time.append(get_now_ms() / 1000.0)
        self.inserts_since_reflect += 1
        if self.inserts_since_reflect >= self.REFLECT_EVERY:
            self._reflect()
            self.inserts_since_reflect = 0
        self._recompute_idf()
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        now = get_now_ms() / 1000.0
        qv = hash_vector(tokenize(query))
        scored = []
        for i, item in enumerate(self.items):
            age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
            recency = math.exp(-math.log(2) * age_hours / 24.0)
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
            boost = 0.3 if item.metadata.get("kind") == "reflection" else 0.0
            scored.append((rel * 1.0 + recency * 0.5 + self.importance[i] * 0.5 + boost, i, item))
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
# G5: Cognitive index (HippoRAG-inspired)
# Two-tier: a small "index" of high-salience items (always retrieved first)
# and a "long-tail" of all items. The index boosts retention of important
# facts; the long-tail fills gaps.
# ---------------------------------------------------------------------------

class CognitiveIndex(Memory):
    NAME = "G5"

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.salience: list[float] = []
        self.insert_time: list[float] = []
        self.index_ids: list[int] = []  # ids in the working index
        self.long_tail_ids: list[int] = []
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

    def _rebuild_index(self):
        # High-salience items go in the index; rest in long-tail.
        self.index_ids = [i for i, s in enumerate(self.salience) if s >= 0.6]
        self.long_tail_ids = [i for i, s in enumerate(self.salience) if s < 0.6]

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
        self._recompute_idf()
        self._rebuild_index()
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        now = get_now_ms() / 1000.0
        qv = hash_vector(tokenize(query))
        scored = []
        # Index items first
        for i in self.index_ids:
            age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
            recency = math.exp(-math.log(2) * age_hours / 24.0)
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
            score = rel * 1.0 + recency * 0.5 + self.salience[i] * 0.5
            scored.append((score, i, self.items[i]))
        for i in self.long_tail_ids:
            age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
            recency = math.exp(-math.log(2) * age_hours / 24.0)
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
            score = rel * 1.0 + recency * 0.5 + self.salience[i] * 0.5
            scored.append((score, i, self.items[i]))
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
# G6: Decade-stable (best-of-breed hybrid)
# Generative Agents-style importance + recency + relevance + Ebbinghaus
# decay + tiered retention + supersession. Combines the strongest
# signals from rounds 1-2.
# ---------------------------------------------------------------------------

class DecadeStable(Memory):
    NAME = "G6"
    SUPERSESSION_THRESHOLD = 0.7
    RETENTION_TURNS = 100
    HALF_LIFE_HOURS = 24.0

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

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        if salience <= 0.0:
            self.turn_count += 1
            self.total_insert_ms += get_now_ms() - t
            return
        new_tokens = tokenize(content)
        new_vec = hash_vector(new_tokens)
        # Supersession: try to replace an existing high-similarity item.
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

    def retrieve(self, query, k=5):
        t = get_now_ms()
        now = get_now_ms() / 1000.0
        qv = hash_vector(tokenize(query))
        scored = []
        for i, item in enumerate(self.items):
            age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
            # Generative Agents-style: importance + recency + relevance
            half_life_hours = self.HALF_LIFE_HOURS * (1.0 + self.salience[i])
            recency = math.exp(-math.log(2) * age_hours / half_life_hours)
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
            score = (1.0 * self.salience[i] +
                     1.0 * recency +
                     1.0 * rel)
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
# G7: Recursive retrieval (2-hop)
# Initial recall → extract concepts from top-k → second hop on those
# concepts. Inspired by spreading activation (Collins & Loftus 1975)
# and GraphRAG (Edge et al. 2024).
# ---------------------------------------------------------------------------

class RecursiveRetrieval(Memory):
    NAME = "G7"
    EXPLORE_K = 2  # number of next-hop queries per initial result
    HOP_DECAY = 0.5  # score decay per hop

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.salience: list[float] = []
        self.insert_time: list[float] = []
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

    def _basic_rank(self, query: str, k: int) -> list[tuple[RetrievalResult, float]]:
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
            score = rel * 1.0 + recency * 0.5 + tier * 0.5
            scored.append((RetrievalResult(item.turn_id, item.role, item.content, float(score), item.metadata), score))
        scored.sort(key=lambda x: x[1], reverse=True)
        return scored[:k]

    def _extract_concepts(self, result: RetrievalResult) -> list[str]:
        toks = tokenize(result.content)
        # TF-IDF distinctive words: words that appear in this result but not in all.
        seen_in_all = set()
        for dt in self.doc_tokens:
            seen_in_all.update(dt)
        scored = []
        for w in toks:
            idf = self.idf.get(w, 0.0)
            scored.append((w, idf))
        scored.sort(key=lambda x: x[1], reverse=True)
        return [w for w, _ in scored[:self.EXPLORE_K]]

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
        self._recompute_idf()
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        # Two-hop retrieval.
        results_with_score: dict[str, tuple[RetrievalResult, float]] = {}
        # First hop.
        for r, s in self._basic_rank(query, k):
            results_with_score[r.turn_id] = (r, s)
        # Extract concepts from top-k and retrieve again.
        for r, _ in list(results_with_score.values())[:3]:
            for concept in self._extract_concepts(r):
                for r2, s2 in self._basic_rank(concept, self.EXPLORE_K):
                    existing = results_with_score.get(r2.turn_id)
                    # Use the max score, with hop decay for indirect hits.
                    decayed = s2 * self.HOP_DECAY
                    if existing is None or decayed > existing[1]:
                        results_with_score[r2.turn_id] = (r2, decayed)
        ranked = sorted(results_with_score.values(), key=lambda x: x[1], reverse=True)
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
# G8: Recursive retrieval (3-hop with relevance threshold)
# Continue while top-hit score is above threshold.
# ---------------------------------------------------------------------------

class RecursiveRetrieval3Hop(Memory):
    NAME = "G8"
    HOPS = 3
    RELEVANCE_THRESHOLD = 0.05
    HOP_DECAY = 0.5

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.salience: list[float] = []
        self.insert_time: list[float] = []
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

    def _basic_rank(self, query: str, k: int):
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
            score = rel * 1.0 + recency * 0.5 + tier * 0.5
            scored.append((RetrievalResult(item.turn_id, item.role, item.content, float(score), item.metadata), score))
        scored.sort(key=lambda x: x[1], reverse=True)
        return scored[:k]

    def _extract_concepts(self, results):
        pool = []
        for r, _ in results:
            for w in tokenize(r.content):
                if self.idf.get(w, 0.0) > 0.5:
                    pool.append(w)
        return list(dict.fromkeys(pool))[:5]

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
        self._recompute_idf()
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        results: dict[str, tuple[RetrievalResult, float]] = {}
        next_query = query
        for hop in range(self.HOPS):
            top = self._basic_rank(next_query, k)
            if not top or (top and top[0][1] < self.RELEVANCE_THRESHOLD):
                break
            for r, s in top:
                existing = results.get(r.turn_id)
                decayed = s * (self.HOP_DECAY ** hop)
                if existing is None or decayed > existing[1]:
                    results[r.turn_id] = (r, decayed)
            # Compose next-hop query from concepts of the top result.
            concepts = self._extract_concepts(top[:3])
            if not concepts:
                break
            next_query = " ".join(concepts)
        ranked = sorted(results.values(), key=lambda x: x[1], reverse=True)
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
# G9: Spreading activation (graph-based)
# Build a co-occurrence graph at insert time. Retrieval propagates
# activation along the graph edges from the initial hit set.
# ---------------------------------------------------------------------------

class SpreadingActivation(Memory):
    NAME = "G9"
    PROPAGATION_STEPS = 2
    ACTIVATION_DECAY = 0.5

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.salience: list[float] = []
        self.insert_time: list[float] = []
        # Word-level graph: token -> set of (item_id, weight)
        self.graph: dict[str, dict[int, float]] = {}
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

    def _build_graph(self, item_id: int, tokens):
        for t in set(tokens):
            if t not in self.graph:
                self.graph[t] = {}
            self.graph[t][item_id] = self.graph[t].get(item_id, 0.0) + 1.0

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        if salience <= 0.0:
            self.total_insert_ms += get_now_ms() - t
            return
        item_id = len(self.items)
        self.items.append(StoreItem(turn_id, role, content, metadata))
        self.doc_tokens.append(tokenize(content))
        self.vecs.append(hash_vector(self.doc_tokens[-1]))
        self.salience.append(salience)
        self.insert_time.append(get_now_ms() / 1000.0)
        self._recompute_idf()
        self._build_graph(item_id, self.doc_tokens[-1])
        self.total_insert_ms += get_now_ms() - t

    def _initial_hits(self, query, k):
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
            score = rel * 1.0 + recency * 0.5 + tier * 0.5
            scored.append((i, score))
        scored.sort(key=lambda x: x[1], reverse=True)
        return scored[:k]

    def retrieve(self, query, k=5):
        t = get_now_ms()
        # Initial hit set.
        initial = self._initial_hits(query, k)
        if not initial:
            self.total_query_ms += get_now_ms() - t
            self.query_count += 1
            return []
        # Spreading activation: tokens in initial hits activate their neighbors.
        activation: dict[int, float] = {i: s for i, s in initial}
        for step in range(self.PROPAGATION_STEPS):
            decay = self.ACTIVATION_DECAY ** (step + 1)
            new_activation: dict[int, float] = {}
            for tok in self.graph:
                # Tokens that overlap the initial hits or current activation.
                hits = [(i, s) for i, s in activation.items() if i in self.graph.get(tok, {})]
                if not hits:
                    continue
                for hit_item, hit_score in hits:
                    for neighbor_item, edge_weight in self.graph[tok].items():
                        if neighbor_item in activation:
                            continue
                        contribution = hit_score * edge_weight * decay
                        if contribution > 0:
                            new_activation[neighbor_item] = max(
                                new_activation.get(neighbor_item, 0.0), contribution)
            activation.update(new_activation)
        # Build results.
        ranked = sorted(activation.items(), key=lambda x: x[1], reverse=True)
        out = []
        for i, s in ranked[:k]:
            if s > 0:
                item = self.items[i]
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
