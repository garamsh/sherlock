"""OpenAI embeddings and decider calls, cached on disk, thread-safe."""
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

EMBED_MODEL = "text-embedding-3-small"
DECIDER_MODEL = "gpt-4o-mini"
# Output dimensions; every structure uses the same embedder. 512 truncates the
# model's native 1536, and is what every committed round-4 result was measured at.
DEFAULT_EMBED_DIMS = 512
EMBED_DIMS = int(os.environ.get("EMBED_DIMS", DEFAULT_EMBED_DIMS))


def dims_suffix() -> str:
    """File-name suffix marking a non-default dimension; empty at the default."""
    return "" if EMBED_DIMS == DEFAULT_EMBED_DIMS else f"_dims{EMBED_DIMS}"


# The cache is keyed on text, so each dimension needs its own file.
EMBED_CACHE = os.path.join(HERE, f".embed_cache{dims_suffix()}.json")
CHAT_CACHE = os.path.join(HERE, ".chat_cache.json")

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
    """Persist both caches. Snapshots under the lock, writes outside it."""
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
    """Dot product; every cached vector is unit-normalized at insert."""
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
