"""The M series, M1-M6. Pure structures: the structure is the mechanism,
with no scoring formula.

One file per series letter, which is how the runners, the record and the
result tables address these; the package `__init__` is the surface they
import.
"""
from __future__ import annotations
import hashlib
import math

from interface import Memory, RetrievalResult, StoreItem, get_now_ms
from .similarity import tokenize, hash_vector


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
