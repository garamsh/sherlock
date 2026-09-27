"""The free pre-screen of #890's candidates, before any synthesis: the raw
query's rank of each candidate's needed exchanges, with the exchange's own
text standing in for its mid's summary, as #874's harness screen does.

    python3 docs/research/memory-encoding/validation/screen.py <scratch dir>   # evidence/screen.json

It embeds the replication's recorded batch-4 summaries for the base and each
stream batch's text ("role: content" lines) with the local
text-embeddings-inference server, caching vectors under the scratch
directory. The UP templates were rewritten once against it before any call
(README.md §Corpus); corpus.py select decides on the real summaries.
"""

import hashlib
import json
import math
import sys
import urllib.request
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPLICATION = HERE.parent.parent / "mid-memory-rag" / "evidence" / "replication"
TEI = "http://127.0.0.1:8100/embed"
TOP = 20


def unit(v):
    n = math.sqrt(sum(x * x for x in v))
    return [x / n for x in v]


class Cache:
    def __init__(self, path):
        self.path, self.vectors = path, {}
        if path.exists():
            for line in path.read_text().splitlines():
                e = json.loads(line)
                self.vectors[e["key"]] = e["vector"]

    def embed(self, texts):
        keys = [hashlib.sha256(t.encode()).hexdigest() for t in texts]
        missing = [(k, t) for k, t in dict(zip(keys, texts)).items() if k not in self.vectors]
        with self.path.open("a") as f:
            for i in range(0, len(missing), 32):
                chunk = missing[i:i + 32]
                req = urllib.request.Request(TEI, data=json.dumps({"inputs": [t for _, t in chunk], "truncate": True}).encode(),
                                             headers={"Content-Type": "application/json"})
                for (k, _), v in zip(chunk, json.load(urllib.request.urlopen(req))):
                    self.vectors[k] = unit(v)
                    f.write(json.dumps({"key": k, "vector": self.vectors[k]}) + "\n")
        return [self.vectors[k] for k in keys]


def main(work):
    work = Path(work)
    work.mkdir(parents=True, exist_ok=True)
    cache = Cache(work / "screen-vectors.jsonl")
    corpus = json.loads((HERE / "candidates.json").read_text())
    base = [json.loads(line)["summary"] for line in (REPLICATION / "synthesis-n4.jsonl").read_text().splitlines()]
    stream = corpus["stream"]
    batches = ["\n".join(m["role"] + ": " + m["content"] for m in stream[i:i + 4]) for i in range(0, len(stream), 4)]
    vectors = cache.embed(base + batches)
    first = stream[0]["id"]

    def rank(query, source):
        q = cache.embed([query])[0]
        sims = [sum(a * b for a, b in zip(q, v)) for v in vectors]
        target = sims[len(base) + (source - first) // 4]
        return 1 + sum(s > target for s in sims)

    rows = []
    for c in corpus["chains"]:
        raw = [rank(c["query"], h["source"]) for h in c["hops"]]
        needed = raw[1:] if c["family"] in ("C1", "C2", "C3", "IC2", "IC3") else raw
        rows.append({"id": c["id"], "family": c["family"], "raw": raw, "pass": all(r > TOP for r in needed)})
    summary = {}
    for r in rows:
        s = summary.setdefault(r["family"], {"candidates": 0, "pass": 0})
        s["candidates"] += 1
        s["pass"] += r["pass"]
    out = {"top": TOP, "summary": summary, "chains": rows}
    (HERE / "evidence").mkdir(exist_ok=True)
    (HERE / "evidence" / "screen.json").write_text(json.dumps(out, indent=1) + "\n")
    print(json.dumps(summary))


if __name__ == "__main__":
    main(sys.argv[1])
