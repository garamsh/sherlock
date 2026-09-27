"""The b4-k5 candidate lookup's ceiling on chain hop pairs, free: no model
call (preregistration.md §Amendments, 1).

    python3 docs/research/memory-encoding/validation/arms/prev/lookup.py <shared b4 memory.db> [embed URL]

For each pair of consecutive hops of every chain of two or more hops, it
ranks, as main.go requestsFor does, the mids before the later hop's mid by
cosine to that batch's text ("role: content" joined by newlines, embedded
by the local TEI), newest first among equals, and records where the earlier
hop's mid falls. b4-k5 gives the first 5, and the model can link only a mid
it is given. Writes evidence/lookup-b4-k5.json beside this file.
"""

import json
import math
import sqlite3
import statistics
import struct
import sys
import urllib.request
from collections import defaultdict
from pathlib import Path

HERE = Path(__file__).resolve().parent
STUDY = HERE.parent.parent
CANDIDATES = 5
SHOWN = (5, 10, 20, 50, 100)


def unit(v):
    n = math.sqrt(sum(x * x for x in v))
    return [x / n for x in v] if n else v


def embedder(url):
    def embed(text):
        body = json.dumps({"model": "BAAI/bge-base-en-v1.5", "input": text}).encode()
        req = urllib.request.Request(url.rstrip("/") + "/embeddings", body, {"content-type": "application/json"})
        return unit(json.load(urllib.request.urlopen(req))["data"][0]["embedding"])
    return embed


def mid_of(source):
    return (source - 1) // 4 + 1


def main():
    store, url = sys.argv[1], sys.argv[2] if len(sys.argv) > 2 else "http://127.0.0.1:8100/v1"
    corpus = json.loads((STUDY / "corpus.json").read_text())
    messages = {m["id"]: m for m in corpus["stream"]}
    db = sqlite3.connect(f"file:{store}?mode=ro", uri=True)
    vectors = {i: unit(struct.unpack(f"<{len(b) // 4}f", b)) for i, b in db.execute("SELECT id, embedding FROM conversation_mid")}
    embed = embedder(url)
    pairs = []
    for c in corpus["chains"]:
        if c["k"] < 2:
            continue
        for i, (a, b) in enumerate(zip(c["hops"], c["hops"][1:])):
            earlier, later = sorted((mid_of(a["source"]), mid_of(b["source"])))
            first = (later - 1) * 4 + 1
            q = embed("\n".join(messages[j]["role"] + ": " + messages[j]["content"] for j in range(first, first + 4)))
            ranked = sorted((m for m in vectors if m < later), key=lambda m: (-sum(x * y for x, y in zip(q, vectors[m])), -m))
            pairs.append({"chain": c["id"], "family": c["family"], "hops": [i + 1, i + 2], "earlier_mid": earlier, "later_mid": later,
                          "rank": ranked.index(earlier) + 1, "candidates": len(ranked)})
    families = defaultdict(lambda: {"pairs": 0, "given": 0})
    for p in pairs:
        families[p["family"]]["pairs"] += 1
        families[p["family"]]["given"] += p["rank"] <= CANDIDATES
    ranks = [p["rank"] for p in pairs]
    out = {"store": "shared b4 at the corpus selection", "candidates": CANDIDATES, "families": dict(families),
           "pooled": {"pairs": len(pairs), "given": sum(r <= CANDIDATES for r in ranks)},
           "within": {str(k): sum(r <= k for r in ranks) for k in SHOWN}, "median_rank": statistics.median(ranks), "pairs": pairs}
    (HERE / "evidence").mkdir(exist_ok=True)
    (HERE / "evidence" / "lookup-b4-k5.json").write_text(json.dumps(out, indent=1) + "\n")
    print(json.dumps({k: v for k, v in out.items() if k != "pairs"}))


if __name__ == "__main__":
    main()
