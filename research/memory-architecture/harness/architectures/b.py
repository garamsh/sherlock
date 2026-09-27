"""The B series, B1-B3. Classic retrieval: keyword, semantic, and the blend of both.

One file per series letter, which is how the runners, the record and the
result tables address these; the package `__init__` is the surface they
import.
"""
from __future__ import annotations
import math
from collections import Counter

from interface import Memory, RetrievalResult, StoreItem, get_now_ms
from .similarity import tokenize, hash_vector, cosine, tfidf_cosine


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
