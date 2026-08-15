"""Real-embedder store + decider runner.

Store finalists (G6 scoring, curation-only) over the 1,000-turn dataset,
with real OpenAI embeddings. Then the creation-structure finalists with a
real gpt-4o-mini decider for correction handling.

Measures: checkpoint recall, latest-wins (real semantic supersession),
store size, query latency, API call counts.
"""
from __future__ import annotations
import json
import math
import os
import random
import sys
import time

sys.path.insert(0, os.path.dirname(__file__))

from real_api import embed, cosine, decider_call, flush_caches, API_CALLS
from real_dataset import build, TOPICS


# ---------------------------------------------------------------------------
# Store with real embeddings, G6 scoring, supersession, tiered retention
# ---------------------------------------------------------------------------

class RealStore:
    """The adopted G6 store shape, backed by real embeddings."""

    def __init__(self, supersession: bool = True, retention: bool = True):
        self.rows = []  # (id, content, salience, ts, vec, superseded)
        self._next = 1
        self.now = 0.0
        self.supersession = supersession
        self.retention = retention

    def tick(self):
        self.now += 1.0

    def add(self, content, salience):
        vec = embed(content)
        if self.supersession:
            for r in self.rows:
                if not r[5] and cosine(vec, r[4]) >= 0.6:
                    r[5] = True
                    break
        self.rows.append([self._next, content, salience, self.now, vec, False])
        self._next += 1

    def retain(self):
        if not self.retention:
            return
        for r in self.rows:
            if r[5]:
                continue
            age = self.now - r[3]
            if r[2] >= 0.6:
                continue
            if r[2] >= 0.3 and age <= 100:
                continue
            if r[2] < 0.3 or age > 100:
                r[5] = True

    def recall(self, query, budget=5):
        qv = embed(query)
        scored = []
        for r in self.rows:
            if r[5]:
                continue
            age = max(0.0, self.now - r[3])
            half = 24.0 * (1.0 + r[2])
            recency = math.exp(-math.log(2) * age / half)
            rel = cosine(qv, r[4])
            scored.append((r[2] + rel + recency, r))
        scored.sort(key=lambda x: -x[0])
        return [r for s, r in scored[:budget] if s > 0.05]

    def size(self):
        return sum(1 for r in self.rows if not r[5])


# ---------------------------------------------------------------------------
# Creation structures over the real store
# ---------------------------------------------------------------------------

class SysC2:
    """Everything-is-update: the main model's structured field writes."""
    NAME = "C2-real"
    RATE = 0.8

    def __init__(self):
        self.store = RealStore(supersession=True, retention=True)

    def turn(self, text, topics):
        self.store.tick()
        import random
        if topics and random.random() < self.RATE:
            self.store.add(text, 0.7)
        self.store.retain()


class SysC3:
    """Dedicated memory model: a real narrow gpt-4o-mini call per turn."""
    NAME = "C3-real"
    SYS = ("You are a memory decider. Given the user's latest message and the "
           "currently stored memories, decide what to store. Reply with JSON: "
           '{"store": ["<fact>", ...], "retire": ["<substring of an old memory to retire>", ...]}. '
           "Store only durable facts and corrections. Store nothing for chatter.")

    def __init__(self, decider_budget=None):
        self.store = RealStore(supersession=True, retention=True)
        self.calls = 0
        self.decider_budget = decider_budget  # None = every turn; int = sample N turns

    def turn(self, text, topics):
        self.store.tick()
        if self.decider_budget is not None and self.calls >= self.decider_budget:
            self.store.retain()
            return
        current = [r[1] for r in self.store.rows if not r[5]][-10:]
        out = decider_call(self.SYS, f"Latest message: {text}\nCurrent memories: {json.dumps(current)}")
        self.calls += 1
        try:
            start = out.index("{")
            end = out.rindex("}") + 1
            ops = json.loads(out[start:end])
        except (ValueError, json.JSONDecodeError):
            self.store.retain()
            return
        for retire in ops.get("retire", []):
            for r in self.store.rows:
                if not r[5] and retire.lower() in r[1].lower():
                    r[5] = True
        for fact in ops.get("store", []):
            if isinstance(fact, str) and fact.strip():
                self.store.add(fact.strip(), 0.7)
        self.store.retain()


class SysC1:
    """Voluntary writeback (the current production shape)."""
    NAME = "C1-real"
    RATE = 0.0  # measured

    def __init__(self):
        self.store = RealStore(supersession=True, retention=True)

    def turn(self, text, topics):
        self.store.tick()
        self.store.retain()


def probe_recall(store: RealStore, question: str, expected_new: str, expected_old: str):
    recalled = store.recall(question)
    blob = " ".join(r[1] for r in recalled).lower()
    return {
        "new_present": expected_new.lower() in blob,
        "old_present": expected_old.lower() in blob,
        "recalled": [r[1] for r in recalled],
    }


def main():
    turns, probes = build()
    print(f"dataset: {len(turns)} turns, {len(probes)} probes")
    os.makedirs("results", exist_ok=True)
    systems = [SysC1, SysC2, SysC3]
    rows = []
    for cls in systems:
        random.seed(11)
        sys_inst = cls()
        t0 = time.perf_counter()
        for text, topics in turns:
            sys_inst.turn(text, topics)
        elapsed = time.perf_counter() - t0
        # Probe at the end (all corrections planted by then).
        correct = 0
        stale = 0
        for _, question, new, old in probes:
            r = probe_recall(sys_inst.store, question, new, old)
            if r["new_present"]:
                correct += 1
            if r["old_present"]:
                stale += 1
        rows.append({
            "system": cls.NAME,
            "checkpoint_recall": correct / len(probes),
            "latest_wins": (correct - stale) / len(probes),
            "stale_present": stale / len(probes),
            "store_items": sys_inst.store.size(),
            "elapsed_s": elapsed,
            "decider_calls": getattr(sys_inst, "calls", 0),
        })
        flush_caches()
    headers = ["system", "recall", "latest-wins-net", "stale-present", "items", "elapsed s", "decider calls"]
    lines = ["| " + " | ".join(headers) + " |",
             "|" + "|".join(["-" * len(h) for h in headers]) + "|"]
    for r in rows:
        lines.append("| {} | {:.2f} | {:.2f} | {:.2f} | {} | {:.1f} | {} |".format(
            r["system"], r["checkpoint_recall"], r["latest_wins"], r["stale_present"],
            r["store_items"], r["elapsed_s"], r["decider_calls"]))
    table = "\n".join(lines)
    print(table)
    print(f"\nAPI calls: {API_CALLS}")
    with open("results/real_scale.md", "w") as f:
        f.write("# Real-embedder large-scale run (1,000 turns, real decider)\n\n" + table + "\n")
    with open("results/real_scale_raw.json", "w") as f:
        json.dump(rows, f, indent=2)
    print(f"Wrote {os.path.abspath('results/real_scale.md')}")


if __name__ == "__main__":
    main()
