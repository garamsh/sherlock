"""Real OpenAI embeddings and decider calls, thread-safe and batched.

Same providers as the previous round (text-embedding-3-small, gpt-4o-mini)
so the numbers stay comparable. Differences: a lock around the caches and a
batch embedding path, because this round runs several structures in parallel
threads over a much larger dataset.
"""
from __future__ import annotations
import json
import math
import os
import threading
import urllib.error
import urllib.request
from operator import mul as _mul

API_KEY = os.environ.get("OPENAI_API_KEY", "")
HERE = os.path.dirname(os.path.abspath(__file__))
EMBED_CACHE = os.path.join(HERE, ".embed_cache.json")
CHAT_CACHE = os.path.join(HERE, ".chat_cache.json")

EMBED_MODEL = "text-embedding-3-small"
DECIDER_MODEL = "gpt-4o-mini"
# text-embedding-3-small supports truncated output dimensions. 512 keeps the
# ranking quality this task needs while making pure-Python cosine over a
# few-hundred-item store affordable; every structure uses the same embedder,
# so the comparison is unaffected.
EMBED_DIMS = 512

_lock = threading.Lock()
API_CALLS = {"embed": 0, "chat": 0, "chat_cached": 0}


def _load(path):
    if os.path.exists(path):
        try:
            with open(path) as f:
                return json.load(f)
        except json.JSONDecodeError:
            return {}
    return {}


EMBEDDINGS = _load(EMBED_CACHE)
CHAT = _load(CHAT_CACHE)


def flush():
    """Persist both caches.

    The snapshot is taken under the lock but written outside it: the embedding
    cache is tens of megabytes of JSON, and holding the lock across that write
    serialises every worker thread on its next cache read.
    """
    with _lock:
        embeddings = dict(EMBEDDINGS)
        chat = dict(CHAT)
    with open(EMBED_CACHE, "w") as f:
        json.dump(embeddings, f)
    with open(CHAT_CACHE, "w") as f:
        json.dump(chat, f)


def _post(url, payload, timeout=90):
    req = urllib.request.Request(
        url, data=json.dumps(payload).encode(),
        headers={"Authorization": f"Bearer {API_KEY}",
                 "Content-Type": "application/json"})
    last = None
    for _ in range(4):
        try:
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                return json.loads(resp.read())
        except (urllib.error.URLError, urllib.error.HTTPError, TimeoutError) as exc:
            last = exc
    raise RuntimeError(f"request failed: {last}")


def embed_batch(texts):
    """Embed many texts at once, filling the shared cache."""
    missing = [t for t in dict.fromkeys(texts) if t not in EMBEDDINGS]
    for i in range(0, len(missing), 256):
        chunk = missing[i:i + 256]
        out = _post("https://api.openai.com/v1/embeddings",
                    {"model": EMBED_MODEL, "input": chunk,
                     "dimensions": EMBED_DIMS})
        with _lock:
            for item, text in zip(out["data"], chunk):
                EMBEDDINGS[text] = _unit(item["embedding"])
            API_CALLS["embed"] += 1
    if missing:
        flush()


def embed(text: str):
    hit = EMBEDDINGS.get(text)
    if hit is not None:
        return hit
    embed_batch([text])
    return EMBEDDINGS[text]


def _unit(vec):
    norm = math.sqrt(sum(x * x for x in vec))
    return [x / norm for x in vec] if norm else vec


def cosine(a, b) -> float:
    """Cosine over unit-normalized vectors, i.e. a plain dot product.

    Everything entering the cache is normalized at insert, so this is the
    hot-loop form: a few hundred store rows scored per query, in pure Python.
    """
    if not a or not b:
        return 0.0
    return sum(map(_mul, a, b))


def decider(system: str, user: str, model: str = DECIDER_MODEL) -> str:
    key = f"{model}\n{system}\n---\n{user}"
    hit = CHAT.get(key)
    if hit is not None:
        API_CALLS["chat_cached"] += 1
        return hit
    out = _post("https://api.openai.com/v1/chat/completions", {
        "model": model,
        "messages": [{"role": "system", "content": system},
                     {"role": "user", "content": user}],
        "temperature": 0,
        "response_format": {"type": "json_object"},
    })
    text = out["choices"][0]["message"]["content"]
    with _lock:
        CHAT[key] = text
        API_CALLS["chat"] += 1
        n = API_CALLS["chat"]
    if n % 1000 == 0:
        flush()
    return text


def parse_json(text: str):
    """Best-effort JSON object extraction from a model reply."""
    try:
        return json.loads(text)
    except (json.JSONDecodeError, TypeError):
        pass
    try:
        start = text.index("{")
        end = text.rindex("}") + 1
        return json.loads(text[start:end])
    except (ValueError, json.JSONDecodeError):
        return {}
