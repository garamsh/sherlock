"""The E series, E1-E4. Cognition: a working tier, rehearsal, a salience
tier, a procedural table.

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
