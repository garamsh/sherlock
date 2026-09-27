"""The F series, F1-F5. Storage discipline: decay, supersession, compression, retention.

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
