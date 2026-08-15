"""Large-scale real-embedder experiment.

Differences from the harness experiments:
- Real OpenAI embeddings (text-embedding-3-small) with a disk cache, so
  supersession is judged by real semantic similarity.
- A real memory-decider model (gpt-4o-mini) for the decider-owned structures,
  so correction detection is a real semantic judgment.
- 100 sessions x 10 turns = 1,000 turns with planted facts and corrections,
  checkpoint probes after corrections.

Cost estimate (2026 pricing): ~1,000 embeddings + ~1,000 narrow chat calls,
on the order of a few cents. The runner prints the actual call counts.
"""
from __future__ import annotations
import json
import math
import os
import random
import re
import sys
import time
import urllib.request

API_KEY = os.environ.get("OPENAI_API_KEY", "")
EMBED_CACHE = os.path.join(os.path.dirname(__file__), ".embed_cache.json")
CHAT_CACHE = os.path.join(os.path.dirname(__file__), ".chat_cache.json")


def _load(path):
    if os.path.exists(path):
        with open(path) as f:
            return json.load(f)
    return {}


def _save(path, data):
    with open(path, "w") as f:
        json.dump(data, f)


EMBEDDINGS = _load(EMBED_CACHE)
CHAT = _load(CHAT_CACHE)
API_CALLS = {"embed": 0, "chat": 0}


def embed(text: str) -> list[float]:
    if text in EMBEDDINGS:
        return EMBEDDINGS[text]
    req = urllib.request.Request(
        "https://api.openai.com/v1/embeddings",
        data=json.dumps({"model": "text-embedding-3-small", "input": text}).encode(),
        headers={"Authorization": f"Bearer {API_KEY}", "Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=60) as resp:
        out = json.loads(resp.read())
    vec = out["data"][0]["embedding"]
    EMBEDDINGS[text] = vec
    API_CALLS["embed"] += 1
    if API_CALLS["embed"] % 50 == 0:
        _save(EMBED_CACHE, EMBEDDINGS)
    return vec


def cosine(a: list[float], b: list[float]) -> float:
    if not a or not b or len(a) != len(b):
        return 0.0
    dot = sum(x * y for x, y in zip(a, b))
    na = math.sqrt(sum(x * x for x in a))
    nb = math.sqrt(sum(y * y for y in b))
    return dot / (na * nb) if na and nb else 0.0


def decider_call(system: str, user: str) -> str:
    key = system + "\n---\n" + user
    if key in CHAT:
        return CHAT[key]
    req = urllib.request.Request(
        "https://api.openai.com/v1/chat/completions",
        data=json.dumps({
            "model": "gpt-4o-mini",
            "messages": [{"role": "system", "content": system},
                         {"role": "user", "content": user}],
            "temperature": 0,
        }).encode(),
        headers={"Authorization": f"Bearer {API_KEY}", "Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=60) as resp:
        out = json.loads(resp.read())
    text = out["choices"][0]["message"]["content"]
    CHAT[key] = text
    API_CALLS["chat"] += 1
    if API_CALLS["chat"] % 20 == 0:
        _save(CHAT_CACHE, CHAT)
    return text


def flush_caches():
    _save(EMBED_CACHE, EMBEDDINGS)
    _save(CHAT_CACHE, CHAT)
