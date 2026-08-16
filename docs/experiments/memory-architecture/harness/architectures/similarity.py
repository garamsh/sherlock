"""The similarity the architectures share: tokens, hashed vectors, two cosines.

Bag-of-words hashed vectors, deterministic and offline, so the experiment runs
without an external service. Every series file imports from here, so the one
thing every architecture has in common is stated once.
"""
from __future__ import annotations
import hashlib
import math
import re
from collections import Counter


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
