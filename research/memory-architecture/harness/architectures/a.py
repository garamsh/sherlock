"""The A series, A0-A2. Baselines: nothing stored, everything stored at
one weight, and a verbatim log.

One file per series letter, which is how the runners, the record and the
result tables address these; the package `__init__` is the surface they
import.
"""
from __future__ import annotations

from interface import Memory, RetrievalResult, StoreItem, get_now_ms


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
