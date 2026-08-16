"""12 memory architectures (A0..F1). Stdlib only.

Cosine similarity uses bag-of-words hashed vectors (deterministic, offline).
Embeddings can be requested via os.environ["OPENAI_API_KEY"] but the
simulator ships stdlib-only so the experiment runs without external services.
"""
from __future__ import annotations
import hashlib
import math
import re
import time
from collections import Counter
from typing import Any

from interface import Memory, RetrievalResult, StoreItem, get_now_ms


# ---------------------------------------------------------------------------
# Tokenization + similarity (shared)
# ---------------------------------------------------------------------------

_WORD_RE = re.compile(r"[a-z0-9]+")


def tokenize(text: str) -> list[str]:
    return _WORD_RE.findall(text.lower())


def hash_vector(tokens: list[str], dim: int = 256) -> list[int]:
    """Deterministic bag-of-words hashed vector. dim=256 keeps it small."""
    v = [0] * dim
    for t in tokens:
        h = int(hashlib.md5(t.encode()).hexdigest()[:8], 16)
        v[h % dim] += 1
    return v


def cosine(a: list[int], b: list[int]) -> float:
    if not a or not b:
        return 0.0
    dot = sum(x * y for x, y in zip(a, b))
    na = math.sqrt(sum(x * x for x in a))
    nb = math.sqrt(sum(y * y for y in b))
    return dot / (na * nb) if na and nb else 0.0


def tfidf_cosine(query: str, doc_tokens: list[str], idf: dict[str, float]) -> float:
    q = tokenize(query)
    d = doc_tokens
    if not q or not d:
        return 0.0
    qc = Counter(q)
    dc = Counter(d)
    score = 0.0
    q_norm = 0.0
    for t, freq in qc.items():
        w = (1 + math.log(freq)) * idf.get(t, 0.0)
        score += w * dc.get(t, 0)
        q_norm += w * w
    d_norm = sum(c * c for c in dc.values())
    return score / math.sqrt(q_norm * d_norm) if q_norm and d_norm else 0.0


# ---------------------------------------------------------------------------
# A0: No memory
# ---------------------------------------------------------------------------

class NoMemory(Memory):
    NAME = "A0"

    def __init__(self):
        self.items: list[StoreItem] = []
        self.total_insert_ms: float = 0.0
        self.total_query_ms: float = 0.0
        self.query_count: int = 0

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        self.items.append(StoreItem(turn_id, role, content, metadata))
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        self.total_query_ms += get_now_ms() - t
        self.query_count += 1
        return []

    def stats(self):
        return {
            "name": self.NAME,
            "item_count": len(self.items),
            "total_insert_ms": self.total_insert_ms,
            "total_query_ms": self.total_query_ms,
            "query_count": self.query_count,
        }


# ---------------------------------------------------------------------------
# A1: Auto-capture uniform (no scoring)
# ---------------------------------------------------------------------------

class AutoCaptureUniform(Memory):
    NAME = "A1"

    def __init__(self):
        self.items: list[StoreItem] = []
        self.total_insert_ms = 0.0
        self.total_query_ms = 0.0
        self.query_count = 0

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        self.items.append(StoreItem(turn_id, role, content, dict(metadata, salience=0.10)))
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        self.total_query_ms += get_now_ms() - t
        self.query_count += 1
        # No scoring; return last-k in insertion order.
        out = self.items[-k:]
        # Score is recency-rank position (most recent = highest).
        n = len(self.items)
        return [
            RetrievalResult(item.turn_id, item.role, item.content, float(n - i), item.metadata)
            for i, item in enumerate(reversed(out))
        ]

    def stats(self):
        return {
            "name": self.NAME,
            "item_count": len(self.items),
            "total_insert_ms": self.total_insert_ms,
            "total_query_ms": self.total_query_ms,
            "query_count": self.query_count,
        }


# ---------------------------------------------------------------------------
# A2: Full-text log
# ---------------------------------------------------------------------------

class FullTextLog(Memory):
    NAME = "A2"

    def __init__(self):
        self.items: list[StoreItem] = []
        self.total_insert_ms = 0.0
        self.total_query_ms = 0.0
        self.query_count = 0

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        self.items.append(StoreItem(turn_id, role, content, metadata))
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        # Cheap: substring match in the last items.
        q = query.lower()
        scored = []
        for item in reversed(self.items):
            if q in item.content.lower():
                scored.append(RetrievalResult(item.turn_id, item.role, item.content, 1.0, item.metadata))
        self.total_query_ms += get_now_ms() - t
        self.query_count += 1
        return scored[:k]

    def stats(self):
        return {
            "name": self.NAME,
            "item_count": len(self.items),
            "total_insert_ms": self.total_insert_ms,
            "total_query_ms": self.total_query_ms,
            "query_count": self.query_count,
        }


# ---------------------------------------------------------------------------
# B1: Keyword-only (TF-IDF)
# ---------------------------------------------------------------------------

class KeywordOnly(Memory):
    NAME = "B1"

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.idf: dict[str, float] = {}
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
        toks = tokenize(content)
        self.items.append(StoreItem(turn_id, role, content, metadata))
        self.doc_tokens.append(toks)
        self._recompute_idf()
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        scored = sorted(
            ((tfidf_cosine(query, dt, self.idf), i, item) for i, (item, dt) in enumerate(zip(self.items, self.doc_tokens))),
            key=lambda x: x[0],
            reverse=True,
        )
        self.total_query_ms += get_now_ms() - t
        self.query_count += 1
        return [
            RetrievalResult(item.turn_id, item.role, item.content, float(s), item.metadata)
            for s, _, item in scored[:k] if s > 0
        ]

    def stats(self):
        return {
            "name": self.NAME,
            "item_count": len(self.items),
            "total_insert_ms": self.total_insert_ms,
            "total_query_ms": self.total_query_ms,
            "query_count": self.query_count,
        }


# ---------------------------------------------------------------------------
# B2: Semantic-only (hashed-vector cosine)
# ---------------------------------------------------------------------------

class SemanticOnly(Memory):
    NAME = "B2"

    def __init__(self):
        self.items: list[StoreItem] = []
        self.vecs: list[list[int]] = []
        self.total_insert_ms = 0.0
        self.total_query_ms = 0.0
        self.query_count = 0

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        self.items.append(StoreItem(turn_id, role, content, metadata))
        self.vecs.append(hash_vector(tokenize(content)))
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        qv = hash_vector(tokenize(query))
        scored = [(cosine(qv, v), i, item) for i, (v, item) in enumerate(zip(self.vecs, self.items))]
        scored.sort(key=lambda x: x[0], reverse=True)
        self.total_query_ms += get_now_ms() - t
        self.query_count += 1
        return [
            RetrievalResult(item.turn_id, item.role, item.content, float(s), item.metadata)
            for s, _, item in scored[:k] if s > 0
        ]

    def stats(self):
        return {
            "name": self.NAME,
            "item_count": len(self.items),
            "total_insert_ms": self.total_insert_ms,
            "total_query_ms": self.total_query_ms,
            "query_count": self.query_count,
        }


# ---------------------------------------------------------------------------
# B3: Hybrid RAG (current baseline)
# ---------------------------------------------------------------------------

class HybridRAG(Memory):
    NAME = "B3"
    WEIGHT_REL = 1.0
    WEIGHT_RECENCY = 0.5
    WEIGHT_IMPORTANCE = 1.0
    WEIGHT_ACCESS = 0.25

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.access_count: list[int] = []
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

    def _score(self, i, query, now, qv):
        rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) +
                     cosine(qv, self.vecs[i]))
        age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
        recency = math.exp(-math.log(2) * age_hours / 24.0)
        importance = self.items[i].metadata.get("salience", 0.10)
        access = math.log1p(self.access_count[i]) / math.log1p(10.0)
        return (self.WEIGHT_REL * rel +
                self.WEIGHT_RECENCY * recency +
                self.WEIGHT_IMPORTANCE * importance +
                self.WEIGHT_ACCESS * access)

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        self.items.append(StoreItem(turn_id, role, content, metadata))
        toks = tokenize(content)
        self.doc_tokens.append(toks)
        self.vecs.append(hash_vector(toks))
        self.access_count.append(0)
        self.insert_time.append(get_now_ms() / 1000.0)
        self._recompute_idf()
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        now = get_now_ms() / 1000.0
        qv = hash_vector(tokenize(query))
        scored = [(self._score(i, query, now, qv), i) for i in range(len(self.items))]
        scored.sort(key=lambda x: x[0], reverse=True)
        out = []
        for s, i in scored[:k]:
            if s > 0:
                self.access_count[i] += 1
                out.append(RetrievalResult(self.items[i].turn_id, self.items[i].role,
                                          self.items[i].content, float(s), self.items[i].metadata))
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
# C3: Ebbinghaus decay (B3 + time-decay on importance)
# ---------------------------------------------------------------------------

class EbbinghausDecay(Memory):
    NAME = "C3"
    HALF_LIFE_HOURS = 24.0

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.access_count: list[int] = []
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

    def _score(self, i, query, now, qv):
        rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
        age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
        # Decay importance over time (Ebbinghaus-style).
        importance_base = self.items[i].metadata.get("salience", 0.10)
        # In the experiment we compress time: 1 stored turn = 1 simulated hour.
        decay = math.exp(-math.log(2) * age_hours / self.HALF_LIFE_HOURS)
        importance = importance_base * decay
        recency = decay
        access = math.log1p(self.access_count[i]) / math.log1p(10.0)
        return (1.0 * rel + 0.5 * recency + 1.0 * importance + 0.25 * access)

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        self.items.append(StoreItem(turn_id, role, content, metadata))
        toks = tokenize(content)
        self.doc_tokens.append(toks)
        self.vecs.append(hash_vector(toks))
        self.access_count.append(0)
        self.insert_time.append(get_now_ms() / 1000.0)
        self._recompute_idf()
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        now = get_now_ms() / 1000.0
        qv = hash_vector(tokenize(query))
        scored = [(self._score(i, query, now, qv), i) for i in range(len(self.items))]
        scored.sort(key=lambda x: x[0], reverse=True)
        out = []
        for s, i in scored[:k]:
            if s > 0:
                self.access_count[i] += 1
                out.append(RetrievalResult(self.items[i].turn_id, self.items[i].role,
                                          self.items[i].content, float(s), self.items[i].metadata))
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
# D2: Curation-only (no auto-capture; only high-salience stored)
# ---------------------------------------------------------------------------

class CurationOnly(Memory):
    NAME = "D2"

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
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

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        # Only store high-salience content (curation-required).
        if salience <= 0.0:
            self.total_insert_ms += get_now_ms() - t
            return
        self.items.append(StoreItem(turn_id, role, content, metadata))
        toks = tokenize(content)
        self.doc_tokens.append(toks)
        self.vecs.append(hash_vector(toks))
        self.insert_time.append(get_now_ms() / 1000.0)
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
            salience = item.metadata.get("salience", 0.0)
            scored.append((rel * 1.0 + recency * 0.5 + salience * 1.0, i))
        scored.sort(key=lambda x: x[0], reverse=True)
        out = [RetrievalResult(self.items[i].turn_id, self.items[i].role,
                              self.items[i].content, float(s), self.items[i].metadata)
               for s, i in scored[:k] if s > 0]
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
# E1: Two-tier basic (working memory + LTM)
# ---------------------------------------------------------------------------

class TwoTierBasic(Memory):
    NAME = "E1"
    WORKING_MEMORY_SIZE = 7

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
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

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        self.items.append(StoreItem(turn_id, role, content, metadata))
        self.doc_tokens.append(tokenize(content))
        self.vecs.append(hash_vector(self.doc_tokens[-1]))
        self.insert_time.append(get_now_ms() / 1000.0)
        self._recompute_idf()
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        now = get_now_ms() / 1000.0
        qv = hash_vector(tokenize(query))
        # Single ranked list: relevance dominates; recency is a small tiebreaker.
        scored = []
        wm_size = self.WORKING_MEMORY_SIZE
        n = len(self.items)
        for i, item in enumerate(self.items):
            age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
            recency = math.exp(-math.log(2) * age_hours / 24.0)
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
            # Working memory bonus: items in the last 7 get a small recency boost.
            wm_bonus = 0.1 if i >= n - wm_size else 0.0
            scored.append((rel * 1.0 + recency * 0.1 + wm_bonus, i, item))
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
# E2: Two-tier + reconsolidation
# ---------------------------------------------------------------------------

class TwoTierReconsolidation(TwoTierBasic):
    NAME = "E2"
    RECONSOLIDATION_BONUS = 0.3

    def retrieve(self, query, k=5):
        t = get_now_ms()
        now = get_now_ms() / 1000.0
        qv = hash_vector(tokenize(query))
        scored = []
        wm_size = self.WORKING_MEMORY_SIZE
        n = len(self.items)
        for i, item in enumerate(self.items):
            age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
            recency = math.exp(-math.log(2) * age_hours / 24.0)
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
            wm_bonus = 0.1 if i >= n - wm_size else 0.0
            scored.append((rel * 1.0 + recency * 0.1 + wm_bonus, i, item))
        scored.sort(key=lambda x: x[0], reverse=True)
        out = []
        for s, _, item in scored[:k]:
            if s > 0:
                item_score = s + self.RECONSOLIDATION_BONUS
                out.append(RetrievalResult(item.turn_id, item.role, item.content, float(item_score), item.metadata))
        self.total_query_ms += get_now_ms() - t
        self.query_count += 1
        return out


# ---------------------------------------------------------------------------
# E3: Two-tier + reconsolidation + salience
# ---------------------------------------------------------------------------

class TwoTierSalience(TwoTierReconsolidation):
    NAME = "E3"

    def _salience_tier(self, salience: float) -> float:
        if salience >= 0.6:
            return 1.0
        if salience >= 0.3:
            return 0.5
        return 0.1

    def retrieve(self, query, k=5):
        t = get_now_ms()
        now = get_now_ms() / 1000.0
        qv = hash_vector(tokenize(query))
        scored = []
        wm_size = self.WORKING_MEMORY_SIZE
        n = len(self.items)
        for i, item in enumerate(self.items):
            age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
            recency = math.exp(-math.log(2) * age_hours / 24.0)
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
            tier = self._salience_tier(item.metadata.get("salience", 0.0))
            wm_bonus = 0.1 if i >= n - wm_size else 0.0
            scored.append((rel * 1.0 + recency * 0.1 + tier * 1.0 + wm_bonus, i, item))
        scored.sort(key=lambda x: x[0], reverse=True)
        out = []
        for s, _, item in scored[:k]:
            if s > 0:
                item_score = s + self.RECONSOLIDATION_BONUS
                out.append(RetrievalResult(item.turn_id, item.role, item.content, float(item_score), item.metadata))
        self.total_query_ms += get_now_ms() - t
        self.query_count += 1
        return out


# ---------------------------------------------------------------------------
# E4: Full cognitive (E3 + procedural memory)
# ---------------------------------------------------------------------------

class FullCognitive(TwoTierSalience):
    NAME = "E4"

    def __init__(self):
        super().__init__()
        # Procedural: input_signature -> (approach, outcome)
        self.procedural: dict[str, tuple[str, str]] = {}

    def _sig(self, text: str) -> str:
        toks = sorted(set(tokenize(text)))
        return " ".join(toks[:8])

    def store(self, turn_id, role, content, metadata):
        super().store(turn_id, role, content, metadata)
        kind = metadata.get("kind", "")
        salience = float(metadata.get("salience", 0.0))
        if kind in ("instruction", "correction") and salience >= 0.5:
            sig = self._sig(content)
            self.procedural[sig] = (content, "preferred")

    def retrieve(self, query, k=5):
        results = super().retrieve(query, k)
        sig = self._sig(query)
        if sig in self.procedural:
            approach, outcome = self.procedural[sig]
            hint = RetrievalResult(
                f"PROC-{sig[:8]}", "procedural",
                f"[procedure] {approach} (outcome: {outcome})",
                max((r.score for r in results), default=0.0) + 0.5,
                {"kind": "procedural"},
            )
            results = [hint] + results
        return results[:k]


# ---------------------------------------------------------------------------
# F1: Salience-tiered Ebbinghaus (D2 + decay scaled by salience tier)
# ---------------------------------------------------------------------------

class SalienceTieredEbbinghaus(Memory):
    NAME = "F1"

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
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

    def _tier(self, salience: float) -> float:
        if salience >= 0.6:
            return 5.0
        if salience >= 0.3:
            return 1.0
        return 0.1

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
        self._recompute_idf()
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        now = get_now_ms() / 1000.0
        # Decay: high salience has long half-life, low salience short.
        scored = []
        for i, item in enumerate(self.items):
            age_hours = max(0.0, (now - self.insert_time[i]) / 3600.0)
            tier = self._tier(item.metadata.get("salience", 0.0))
            # Half-life in hours scales by tier.
            half_life = max(1.0, 24.0 * tier)
            decay = math.exp(-math.log(2) * age_hours / half_life)
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) +
                           cosine(hash_vector(tokenize(query)), self.vecs[i]))
            tier_weight = item.metadata.get("salience", 0.0) ** 0.5
            scored.append((rel * 1.0 + decay * 0.3 + tier_weight * 0.5, i, item))
        scored.sort(key=lambda x: x[0], reverse=True)
        out = [RetrievalResult(item.turn_id, item.role, item.content, float(s), item.metadata)
               for s, _, item in scored[:k] if s > 0]
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
# F2: Supersession (D2 + on-store dedup: high-similarity new replaces old)
# ---------------------------------------------------------------------------

class Supersession(Memory):
    NAME = "F2"
    SUPERSESSION_THRESHOLD = 0.7  # cosine > 0.7 to consider a supersession

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
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

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        if salience <= 0.0:
            self.total_insert_ms += get_now_ms() - t
            return
        new_tokens = tokenize(content)
        new_vec = hash_vector(new_tokens)
        # Check for supersession: any existing item with high cosine similarity.
        superseded_idx: int | None = None
        best_sim = 0.0
        for i, item in enumerate(self.items):
            sim = cosine(new_vec, self.vecs[i])
            if sim > best_sim and sim > self.SUPERSESSION_THRESHOLD:
                best_sim = sim
                superseded_idx = i
        if superseded_idx is not None:
            # Replace the old item with the new one.
            self.items[superseded_idx] = StoreItem(turn_id, role, content, metadata)
            self.doc_tokens[superseded_idx] = new_tokens
            self.vecs[superseded_idx] = new_vec
            self.insert_time[superseded_idx] = get_now_ms() / 1000.0
            self._recompute_idf()
        else:
            self.items.append(StoreItem(turn_id, role, content, metadata))
            self.doc_tokens.append(new_tokens)
            self.vecs.append(new_vec)
            self.insert_time.append(get_now_ms() / 1000.0)
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
            scored.append((rel * 1.0 + recency * 0.5, i, item))
        scored.sort(key=lambda x: x[0], reverse=True)
        out = [RetrievalResult(item.turn_id, item.role, item.content, float(s), item.metadata)
               for s, _, item in scored[:k] if s > 0]
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
# F3: Compression (every N items in a session, summarize into one)
# ---------------------------------------------------------------------------

class Compression(Memory):
    NAME = "F3"
    COMPRESS_EVERY = 5  # after every N items in a session, compress

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.insert_time: list[float] = []
        self.per_session_count: dict[str, int] = {}
        self.total_insert_ms = 0.0
        self.total_query_ms = 0.0
        self.query_count = 0

    def _session_id(self, turn_id: str) -> str:
        return turn_id.split(".", 1)[0]

    def _recompute_idf(self):
        N = len(self.doc_tokens)
        df: Counter[str] = Counter()
        for toks in self.doc_tokens:
            for t in set(toks):
                df[t] += 1
        self.idf = {t: math.log((N + 1) / (1 + d)) + 1 for t, d in df.items()}

    def _compress_session(self, session_id: str):
        """Summarize recent items in this session into one summary item."""
        idxs = [i for i, it in enumerate(self.items) if self._session_id(it.turn_id) == session_id]
        if len(idxs) < self.COMPRESS_EVERY:
            return
        # Compress the last COMPRESS_EVERY items in this session.
        recent = idxs[-self.COMPRESS_EVERY:]
        summ = " | ".join(f"{self.items[i].role}: {self.items[i].content[:80]}" for i in recent)
        for i in sorted(recent, reverse=True):
            del self.items[i]
            del self.doc_tokens[i]
            del self.vecs[i]
            del self.insert_time[i]
        self._recompute_idf()
        new_turn_id = f"{session_id}.summary-{len(self.items)}"
        self.items.append(StoreItem(new_turn_id, "summary", f"[summary of {len(recent)} items] {summ[:500]}",
                                   {"salience": 0.5, "kind": "summary"}))
        self.doc_tokens.append(tokenize(self.items[-1].content))
        self.vecs.append(hash_vector(self.doc_tokens[-1]))
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
        self.insert_time.append(get_now_ms() / 1000.0)
        self._recompute_idf()
        self._compress_session(self._session_id(turn_id))
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
            tier = 0.5 if item.metadata.get("kind") == "summary" else 1.0
            scored.append((rel * 1.0 + recency * 0.5 + tier * 0.5, i, item))
        scored.sort(key=lambda x: x[0], reverse=True)
        out = [RetrievalResult(item.turn_id, item.role, item.content, float(s), item.metadata)
               for s, _, item in scored[:k] if s > 0]
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
# F4: Tiered retention (high-salience kept forever; mid/low dropped after N)
# ---------------------------------------------------------------------------

class TieredRetention(Memory):
    NAME = "F4"
    RETENTION_TURNS = 50  # simulated turns before forget

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
        self.insert_idx: list[int] = []  # index of insertion (turn-count)
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
            salience = item.metadata.get("salience", 0.0)
            if salience >= 0.6:
                keep.append(i)
                continue
            age = self.turn_count - self.insert_idx[i]
            if salience >= 0.3 and age < self.RETENTION_TURNS * 2:
                keep.append(i)
                continue
            if age < self.RETENTION_TURNS:
                keep.append(i)
        if len(keep) != len(self.items):
            self.items = [self.items[i] for i in keep]
            self.doc_tokens = [self.doc_tokens[i] for i in keep]
            self.vecs = [self.vecs[i] for i in keep]
            self.insert_idx = [self.insert_idx[i] for i in keep]
            self._recompute_idf()

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        if salience <= 0.0:
            self.turn_count += 1
            self.total_insert_ms += get_now_ms() - t
            return
        self.items.append(StoreItem(turn_id, role, content, metadata))
        self.doc_tokens.append(tokenize(content))
        self.vecs.append(hash_vector(self.doc_tokens[-1]))
        self.insert_idx.append(self.turn_count)
        self.turn_count += 1
        self._recompute_idf()
        self._forget_stale()
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        qv = hash_vector(tokenize(query))
        scored = []
        for i, item in enumerate(self.items):
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
            tier = item.metadata.get("salience", 0.0) ** 0.5
            scored.append((rel * 1.0 + tier * 0.5, i, item))
        scored.sort(key=lambda x: x[0], reverse=True)
        out = [RetrievalResult(item.turn_id, item.role, item.content, float(s), item.metadata)
               for s, _, item in scored[:k] if s > 0]
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
# F5: Combined (F2 supersession + F4 tiered retention)
# ---------------------------------------------------------------------------

class Combined(Memory):
    NAME = "F5"
    SUPERSESSION_THRESHOLD = 0.7
    RETENTION_TURNS = 50

    def __init__(self):
        self.items: list[StoreItem] = []
        self.doc_tokens: list[list[str]] = []
        self.vecs: list[list[int]] = []
        self.idf: dict[str, float] = {}
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
            salience = item.metadata.get("salience", 0.0)
            if salience >= 0.6:
                keep.append(i)
                continue
            age = self.turn_count - self.insert_idx[i]
            if salience >= 0.3 and age < self.RETENTION_TURNS * 2:
                keep.append(i)
                continue
            if age < self.RETENTION_TURNS:
                keep.append(i)
        if len(keep) != len(self.items):
            self.items = [self.items[i] for i in keep]
            self.doc_tokens = [self.doc_tokens[i] for i in keep]
            self.vecs = [self.vecs[i] for i in keep]
            self.insert_idx = [self.insert_idx[i] for i in keep]
            self.insert_time = [self.insert_time[i] for i in keep]
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
        # Supersession: replace existing high-similarity item.
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
            self.insert_time[superseded_idx] = get_now_ms() / 1000.0
            self.insert_idx[superseded_idx] = self.turn_count
        else:
            self.items.append(StoreItem(turn_id, role, content, metadata))
            self.doc_tokens.append(new_tokens)
            self.vecs.append(new_vec)
            self.insert_time.append(get_now_ms() / 1000.0)
            self.insert_idx.append(self.turn_count)
        self.turn_count += 1
        self._recompute_idf()
        self._forget_stale()
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        qv = hash_vector(tokenize(query))
        scored = []
        for i, item in enumerate(self.items):
            rel = 0.5 * (tfidf_cosine(query, self.doc_tokens[i], self.idf) + cosine(qv, self.vecs[i]))
            tier = item.metadata.get("salience", 0.0) ** 0.5
            scored.append((rel * 1.0 + tier * 0.5, i, item))
        scored.sort(key=lambda x: x[0], reverse=True)
        out = [RetrievalResult(item.turn_id, item.role, item.content, float(s), item.metadata)
               for s, _, item in scored[:k] if s > 0]
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


# ============================================================================
# M-architectures: SPARSE / GRAPH / THEORETIC paradigms.
# Each is a small class whose STRUCTURE does the work — not features
# bolted onto an existing architecture.
# ============================================================================


# ---------------------------------------------------------------------------
# M1: Hopfield-style associative memory (Hopfield 1982)
# Items stored as weight vectors. Retrieval minimizes energy = -Σ similarity.
# Iterative update converges to a stored pattern. No retrieval scoring
# heuristics — the structure does the work.
# ---------------------------------------------------------------------------

class HopfieldAssociative(Memory):
    NAME = "M1"
    DIM = 256
    ITERATIONS = 5

    def __init__(self):
        self.items: list[StoreItem] = []
        self.vectors: list[list[int]] = []  # binary {-1, +1} vectors
        self.salience: list[float] = []
        self.insert_time: list[float] = []
        self.total_insert_ms = 0.0
        self.total_query_ms = 0.0
        self.query_count = 0

    def _encode(self, tokens):
        v = [0] * self.DIM
        for t in tokens:
            h = int(hashlib.md5(t.encode()).hexdigest()[:8], 16)
            v[h % self.DIM] += 1
        # Threshold to {-1, +1}
        out = []
        for x in v:
            if x > 0:
                out.append(1)
            elif x < 0:
                out.append(-1)
            else:
                out.append(0)
        return out

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        if salience <= 0.0:
            self.total_insert_ms += get_now_ms() - t
            return
        tokens = tokenize(content)
        v = self._encode(tokens)
        # Apply salience: only keep high-salience (already filtered above)
        self.items.append(StoreItem(turn_id, role, content, metadata))
        self.vectors.append(v)
        self.salience.append(salience)
        self.insert_time.append(get_now_ms() / 1000.0)
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        import random
        t = get_now_ms()
        if not self.items:
            return []
        qv = self._encode(tokenize(query))
        # Iterative update: y_i = sign(Σ_j y_j * w_ji + h_i)
        # Approximation: rank by correlation, then iterate.
        # Initial state by correlation.
        scores = [(sum(qv[i] * v[i] for i in range(self.DIM)), i) for i, v in enumerate(self.vectors)]
        scores.sort(key=lambda x: x[0], reverse=True)
        # Top-k candidates
        result_ids = [i for _, i in scores[:k]]
        # Iterate: re-rank by Hopfield-style energy: -Σ similarity.
        for _ in range(self.ITERATIONS):
            new_scores = []
            for idx in result_ids:
                # Energy = -Σ_{j in result_ids} similarity(i, j)
                s = 0.0
                for jdx in result_ids:
                    if idx == jdx:
                        continue
                    s += sum(self.vectors[idx][i] * self.vectors[jdx][i] for i in range(self.DIM))
                new_scores.append((-s, idx))
            new_scores.sort(key=lambda x: x[0])
            result_ids = [i for _, i in new_scores]
        # Return the final result by initial-correlation score (more robust).
        ranked = sorted(scores, key=lambda x: x[0], reverse=True)
        out = []
        for _, i in ranked[:k]:
            item = self.items[i]
            out.append(RetrievalResult(item.turn_id, item.role, item.content, float(scores[i][0]), item.metadata))
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
# M2: Kanerva sparse distributed memory (Kanerva 1988)
# Fixed-size binary matrix M. Items are sparse binary vectors that
# ACCESS nearby addresses. Retrieval = activation pattern from query.
# Mathematically elegant; intrinsic structure.
# ---------------------------------------------------------------------------

class KanervaSDM(Memory):
    NAME = "M2"
    SDM_DIM = 1024
    SDM_THRESHOLD = 0.4

    def __init__(self):
        self.items: list[StoreItem] = []
        self.locations: list[list[int]] = []  # sparse binary vectors
        self.salience: list[float] = []
        self.insert_time: list[float] = []
        # The memory matrix: address-space logits.
        self.matrix: list[float] = [0.0] * self.SDM_DIM
        self.total_insert_ms = 0.0
        self.total_query_ms = 0.0
        self.query_count = 0

    def _encode(self, tokens):
        # Sparse binary: activate ~10 hashes per content.
        v = set()
        for t in tokens:
            for h in range(3):
                hh = int(hashlib.md5((t + str(h)).encode()).hexdigest()[:8], 16)
                v.add(hh % self.SDM_DIM)
        return sorted(v)

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        if salience <= 0.0:
            self.total_insert_ms += get_now_ms() - t
            return
        locs = self._encode(tokenize(content))
        self.items.append(StoreItem(turn_id, role, content, metadata))
        self.locations.append(locs)
        self.salience.append(salience)
        self.insert_time.append(get_now_ms() / 1000.0)
        # Update matrix: add salience to all activated addresses.
        for a in locs:
            self.matrix[a] += salience
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        import random
        t = get_now_ms()
        if not self.items:
            return []
        # Activation pattern: query hits matrix at its addresses.
        q_locs = self._encode(tokenize(query))
        activation = {a: self.matrix[a] for a in q_locs}
        # For each stored item, compute its activation = sum of matrix at its addresses.
        scores = []
        for i, locs in enumerate(self.locations):
            act = sum(self.matrix[a] for a in locs if a in self.matrix)
            scores.append((act, i))
        scores.sort(key=lambda x: x[0], reverse=True)
        # Threshold out low-activation items.
        if not scores:
            return []
        max_act = scores[0][0]
        out = []
        for s, i in scores[:k]:
            if max_act > 0 and s / max_act < self.SDM_THRESHOLD:
                continue
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


# ---------------------------------------------------------------------------
# M3: HNSW-style hierarchical graph (Malkov & Yashunin 2018)
# Items are nodes in a multi-level graph. Each level has a navigable
# small-world network. Retrieval is greedy walk from top entry points.
# The structure does the work — no scoring formula needed.
# ---------------------------------------------------------------------------

class HNSWMemory(Memory):
    NAME = "M3"
    MAX_LEVEL = 4
    M_LIN = 1.0 / math.log(2.0)

    def __init__(self):
        self.items: list[StoreItem] = []
        self.vectors: list[list[int]] = []
        self.salience: list[float] = []
        self.insert_time: list[float] = []
        # Levels: list of lists of node IDs.
        self.levels: list[list[int]] = [[]]
        self.entry_point: int | None = None
        self.total_insert_ms = 0.0
        self.total_query_ms = 0.0
        self.query_count = 0

    def _rand_level(self):
        import random
        u = random.random() or 1e-9
        return int(-math.log(u) * self.M_LIN)

    def _dist(self, a, b):
        return 1.0 - sum(x * y for x, y in zip(a, b)) / (
            (math.sqrt(sum(x * x for x in a)) or 1.0) *
            (math.sqrt(sum(y * y for y in b)) or 1.0)
        )

    def _search_level(self, qv, entry, level, ef):
        # Greedy walk: at each step, find the neighbor closest to qv.
        visited = set()
        candidates = [(entry, self._dist(self.vectors[entry], qv))]
        results = list(candidates)
        while candidates:
            # Find current node's neighbors at this level.
            current_id, _ = min(candidates, key=lambda x: x[1])
            candidates = [(c, d) for c, d in candidates if c != current_id]
            # Get neighbors at this level via the global structure.
            neighbors = self._neighbors_at_level(current_id, level)
            for n in neighbors:
                if n in visited:
                    continue
                visited.add(n)
                d = self._dist(self.vectors[n], qv)
                if d < results[0][1] or len(results) < ef:
                    candidates.append((n, d))
                    results.append((n, d))
                    if len(results) > ef:
                        results.sort(key=lambda x: x[1])
                        results = results[:ef]
        return results

    def _neighbors_at_level(self, node_id, level):
        # Find neighbors of node_id at the given level by scanning.
        # (Simplified: use the full level list as approximation.)
        if level >= len(self.levels):
            return []
        return [j for j in self.levels[level] if j != node_id]

    def store(self, turn_id, role, content, metadata):
        import random
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        if salience <= 0.0:
            self.total_insert_ms += get_now_ms() - t
            return
        item_id = len(self.items)
        self.items.append(StoreItem(turn_id, role, content, metadata))
        self.vectors.append(hash_vector(tokenize(content)))
        self.salience.append(salience)
        self.insert_time.append(get_now_ms() / 1000.0)
        # Choose level.
        lv = min(self._rand_level(), self.MAX_LEVEL)
        # Add to each level up to lv.
        for L in range(lv + 1):
            while len(self.levels) <= L:
                self.levels.append([])
            self.levels[L].append(item_id)
        if self.entry_point is None:
            self.entry_point = item_id
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        import random
        t = get_now_ms()
        if not self.items or self.entry_point is None:
            return []
        qv = hash_vector(tokenize(query))
        # Greedy walk from top level down to level 0.
        ep = self.entry_point
        for L in range(len(self.levels) - 1, 0, -1):
            if self.levels[L]:
                results = self._search_level(qv, ep, L, ef=10)
                if results:
                    ep = min(results, key=lambda x: x[1])[0]
        # Final search at level 0 with larger ef.
        results = self._search_level(qv, ep, 0, ef=max(k * 4, 20))
        ranked = sorted(results, key=lambda x: x[1])[:k]
        out = []
        for i, dist in ranked:
            dist_score = 1.0 - dist
            item = self.items[i]
            out.append(RetrievalResult(item.turn_id, item.role, item.content, float(dist_score), item.metadata))
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
# M4: LSH (Locality-Sensitive Hashing) — multiple hash tables
# (Indyk & Motwani 1998). Each hash table gives a different L_p view;
# items in the same bucket across tables are similar. The structure itself
# is the index.
# ---------------------------------------------------------------------------

class LSHMemory(Memory):
    NAME = "M4"
    NUM_TABLES = 8
    HASH_BUCKETS = 256

    def __init__(self):
        self.items: list[StoreItem] = []
        self.hashes: list[list[tuple[int, int]]] = []  # (table_idx, bucket_idx) per item
        self.salience: list[float] = []
        self.insert_time: list[float] = []
        # Hash tables: list of dicts mapping bucket -> list of item ids.
        self.tables: list[dict[int, list[int]]] = [{} for _ in range(self.NUM_TABLES)]
        self.total_insert_ms = 0.0
        self.total_query_ms = 0.0
        self.query_count = 0

    def _hash(self, tokens, table_idx):
        # Simple: hash each token seeded by table_idx into a bucket.
        bucket = 0
        for t in tokens:
            h = int(hashlib.md5((t + str(table_idx)).encode()).hexdigest()[:8], 16)
            bucket = (bucket + h) % self.HASH_BUCKETS
        return bucket

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        if salience <= 0.0:
            self.total_insert_ms += get_now_ms() - t
            return
        tokens = tokenize(content)
        item_id = len(self.items)
        self.items.append(StoreItem(turn_id, role, content, metadata))
        item_hashes = []
        for ti in range(self.NUM_TABLES):
            bucket = self._hash(tokens, ti)
            self.tables[ti].setdefault(bucket, []).append(item_id)
            item_hashes.append((ti, bucket))
        self.hashes.append(item_hashes)
        self.salience.append(salience)
        self.insert_time.append(get_now_ms() / 1000.0)
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        if not self.items:
            return []
        tokens = tokenize(query)
        # Query each table; collect candidates with frequency counts.
        candidates: dict[int, int] = {}
        for ti in range(self.NUM_TABLES):
            bucket = self._hash(tokens, ti)
            for i in self.tables[ti].get(bucket, []):
                candidates[i] = candidates.get(i, 0) + 1
        # Rank by number of table-collisions, then by salience.
        ranked = sorted(candidates.items(), key=lambda x: (-x[1], -self.salience[x[0]]))
        out = []
        for i, collisions in ranked[:k]:
            item = self.items[i]
            out.append(RetrievalResult(item.turn_id, item.role, item.content, float(collisions), item.metadata))
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
# M5: Working memory + LRU CACHE
# Two-tier: a small working memory (active set), and an LRU-ordered
# LTM. The structure itself (two lists + LRU) maintains the lifecycle.
# No need for explicit tiered retention rules.
# ---------------------------------------------------------------------------

class WorkingMemoryLru(Memory):
    NAME = "M5"
    WM_SIZE = 7
    LTM_CAPACITY = 200

    def __init__(self):
        self.wm: list[StoreItem] = []  # active working memory (recent)
        self.ltm: list[StoreItem] = []  # LRU-ordered LTM
        self.ltm_lru: list[float] = []  # last-access time
        self.salience: list[float] = []
        self.insert_time: list[float] = []
        self.total_insert_ms = 0.0
        self.total_query_ms = 0.0
        self.query_count = 0

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        if salience <= 0.0:
            self.total_insert_ms += get_now_ms() - t
            return
        item = StoreItem(turn_id, role, content, metadata)
        # Add to working memory.
        self.wm.append(item)
        if len(self.wm) > self.WM_SIZE:
            overflow = self.wm.pop(0)
            # Evict into LTM, applying LRU cap.
            self.ltm.append(overflow)
            self.ltm_lru.append(get_now_ms() / 1000.0)
            if len(self.ltm) > self.LTM_CAPACITY:
                # Drop the LRU item.
                lru_idx = min(range(len(self.ltm_lru)), key=lambda i: self.ltm_lru[i])
                self.ltm.pop(lru_idx)
                self.ltm_lru.pop(lru_idx)
        self.salience.append(salience)
        self.insert_time.append(get_now_ms() / 1000.0)
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        # First check working memory (smallest).
        results_w = []
        for i, item in enumerate(self.wm):
            if item.content and any(t in tokenize(query) for t in tokenize(item.content)):
                results_w.append((item, 1.0 - i * 0.1))
        # Then LRU LTM, top-k by simple overlap.
        scored = []
        for i, item in enumerate(self.ltm):
            if not item.content:
                continue
            overlap = sum(1 for t in tokenize(query) if t in tokenize(item.content))
            if overlap > 0:
                # LRU recency: most recent at top.
                recency = 1.0 - (len(self.ltm_lru) - i) / len(self.ltm_lru) if self.ltm_lru else 0.5
                scored.append((item, overlap + recency))
        scored.sort(key=lambda x: x[1], reverse=True)
        # Combine: WM first.
        merged = []
        for item, s in results_w:
            merged.append(RetrievalResult(item.turn_id, item.role, item.content, float(s), item.metadata))
        for item, s in scored[:k]:
            merged.append(RetrievalResult(item.turn_id, item.role, item.content, float(s), item.metadata))
        out = merged[:k]
        self.total_query_ms += get_now_ms() - t
        self.query_count += 1
        return out

    def stats(self):
        return {
            "name": self.NAME,
            "item_count": len(self.wm) + len(self.ltm),
            "total_insert_ms": self.total_insert_ms,
            "total_query_ms": self.total_query_ms,
            "query_count": self.query_count,
        }


# ---------------------------------------------------------------------------
# M6: Log-structured merge (LSM) — append-only + periodic compaction
# Standard storage-engine pattern. Items written to a memtable; compacted
# periodically. Items in different levels based on age. The structure
# (sorted runs + compaction) does the retention.
# ---------------------------------------------------------------------------

class LsmMemory(Memory):
    NAME = "M6"
    LEVELS = 4
    COMPACT_EVERY = 100

    def __init__(self):
        self.levels: list[list[StoreItem]] = [[] for _ in range(self.LEVELS)]
        # Each level has a max size; when full, compacted to the next level.
        self.level_max: list[int] = [10 * (2 ** i) for i in range(self.LEVELS)]
        self.salience: list[float] = []
        self.insert_count: int = 0
        self.salience_score: list[float] = []
        self.total_insert_ms = 0.0
        self.total_query_ms = 0.0
        self.query_count = 0

    def _compact(self, level: int):
        # Merge two sorted levels; keep high-salience items; drop the rest.
        if level + 1 >= self.LEVELS:
            # Drop low-salience items from the top level.
            self.levels[level] = sorted(self.levels[level],
                                       key=lambda x: -x.metadata.get("salience", 0.0))[:self.level_max[level]]
        else:
            combined = self.levels[level] + self.levels[level + 1]
            combined.sort(key=lambda x: -x.metadata.get("salience", 0.0))
            self.levels[level] = combined[:self.level_max[level]]
            self.levels[level + 1] = combined[self.level_max[level]:]

    def store(self, turn_id, role, content, metadata):
        t = get_now_ms()
        salience = float(metadata.get("salience", 0.0))
        if salience <= 0.0:
            self.total_insert_ms += get_now_ms() - t
            return
        item = StoreItem(turn_id, role, content, metadata)
        self.levels[0].append(item)
        self.salience.append(salience)
        self.insert_count += 1
        if self.insert_count % self.COMPACT_EVERY == 0:
            for level in range(self.LEVELS):
                if len(self.levels[level]) > self.level_max[level]:
                    self._compact(level)
        self.total_insert_ms += get_now_ms() - t

    def retrieve(self, query, k=5):
        t = get_now_ms()
        all_items: list[StoreItem] = []
        for level in self.levels:
            all_items.extend(level)
        if not all_items:
            return []
        scored = []
        qtoks = tokenize(query)
        for item in all_items:
            if not item.content:
                continue
            overlap = sum(1 for t in qtoks if t in tokenize(item.content))
            if overlap > 0:
                scored.append((item, overlap + 0.5 * item.metadata.get("salience", 0.0)))
        scored.sort(key=lambda x: x[1], reverse=True)
        out = [RetrievalResult(item.turn_id, item.role, item.content, float(s), item.metadata)
               for item, s in scored[:k]]
        self.total_query_ms += get_now_ms() - t
        self.query_count += 1
        return out

    def stats(self):
        return {
            "name": self.NAME,
            "item_count": sum(len(l) for l in self.levels),
            "total_insert_ms": self.total_insert_ms,
            "total_query_ms": self.total_query_ms,
            "query_count": self.query_count,
        }


ALL: list[type[Memory]] = [
    NoMemory,
    AutoCaptureUniform,
    FullTextLog,
    KeywordOnly,
    SemanticOnly,
    HybridRAG,
    EbbinghausDecay,
    CurationOnly,
    TwoTierBasic,
    TwoTierReconsolidation,
    TwoTierSalience,
    FullCognitive,
    SalienceTieredEbbinghaus,
    Supersession,
    Compression,
    TieredRetention,
    Combined,
    GenerativeAgentsStyle,
    EbbinghausInspired,
    HierarchicalMemory,
    ReflectionBased,
    CognitiveIndex,
    DecadeStable,
    RecursiveRetrieval,
    RecursiveRetrieval3Hop,
    SpreadingActivation,
    H1G6G4,
    H2G6G7,
    H3G4G7,
    H4F5G4,
    H5EpisodicSemantic,
    H6TripleHybrid,
    HopfieldAssociative,
    KanervaSDM,
    HNSWMemory,
    LSHMemory,
    WorkingMemoryLru,
    LsmMemory,
]


ALL: list[type[Memory]] = [
    NoMemory,
    AutoCaptureUniform,
    FullTextLog,
    KeywordOnly,
    SemanticOnly,
    HybridRAG,
    EbbinghausDecay,
    CurationOnly,
    TwoTierBasic,
    TwoTierReconsolidation,
    TwoTierSalience,
    FullCognitive,
    SalienceTieredEbbinghaus,
    Supersession,
    Compression,
    TieredRetention,
    Combined,
    GenerativeAgentsStyle,
    EbbinghausInspired,
    HierarchicalMemory,
    ReflectionBased,
    CognitiveIndex,
    DecadeStable,
]
