"""The D series, D2. Curation: only what the model writes back enters the store.

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
