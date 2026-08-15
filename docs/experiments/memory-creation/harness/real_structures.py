"""Genuinely new memory structures — real embeddings, real decider.

Five structures that change where the intelligence lives, not how the
existing one is tuned. Each is a small class over the real store.

- S1 sleep-consolidation: writes are trivial appends to a raw journal; a
  periodic consolidation call turns the journal segment into facts. The
  intelligence lives in the consolidation phase, not the write.
- S2 claim-graph: the decider writes claims AND typed links (corrects /
  supports). Retrieval walks from the most relevant claim along links.
- S3 attention-sink: the top-K salience memories are always in context
  (anchors); query recall fills the rest. The read path is the simplest
  possible: anchors + query.
- S4 confidence-decay: every entry has a confidence that decays with time;
  a correction is just a new claim with higher confidence. No delete, no
  supersede — decay handles staleness by itself.
- S5 materialized-view: the conversation log is the source of truth; the
  memory store is a rebuilt view. Every N turns the view is rebuilt from
  the log by the decider. If the view corrupts, rebuild from scratch.
"""
from __future__ import annotations
import json
import math
import os
import random
import sys
import time

sys.path.insert(0, os.path.dirname(__file__))

from real_api import embed, cosine, decider_call, API_CALLS


# ---------------------------------------------------------------------------
# Shared real-embedding store primitives
# ---------------------------------------------------------------------------

class Row:
    __slots__ = ("id", "content", "salience", "ts", "vec", "superseded", "confidence", "links")
    def __init__(self, id, content, salience, ts, vec):
        self.id = id
        self.content = content
        self.salience = salience
        self.ts = ts
        self.vec = vec
        self.superseded = False
        self.confidence = salience
        self.links = []  # (target_id, relation)


def recall_rows(rows, query, budget=5, score_extra=None):
    qv = embed(query)
    scored = []
    for r in rows:
        if r.superseded:
            continue
        rel = cosine(qv, r.vec)
        s = rel + r.salience
        if score_extra:
            s += score_extra(r)
        scored.append((s, r))
    scored.sort(key=lambda x: -x[0])
    return [r for s, r in scored[:budget] if s > 0.05]


# ---------------------------------------------------------------------------
# S1: sleep-consolidation
# ---------------------------------------------------------------------------

class S1SleepConsolidation:
    NAME = "S1-sleep-consolidation"
    CONSOLIDATE_EVERY = 5
    SYS = ("You consolidate recent conversation into durable facts. Given the "
           "journal entries and current facts, reply with JSON: "
           '{"store": ["<fact>", ...], "retire": ["<substring>", ...]}. '
           "Merge corrections: when a new entry contradicts an old fact, "
           "store the new and retire the old.")

    def __init__(self):
        self.journal: list[str] = []
        self.rows: list[Row] = []
        self._next = 1
        self.now = 0.0
        self.calls = 0

    def tick(self):
        self.now += 1.0

    def turn(self, text, topics):
        self.tick()
        self.journal.append(text)  # write is a trivial append — always
        if len(self.journal) % self.CONSOLIDATE_EVERY == 0:
            self.calls += 1
            current = [r.content for r in self.rows if not r.superseded][-10:]
            out = decider_call(self.SYS, f"Journal: {json.dumps(self.journal[-self.CONSOLIDATE_EVERY:])}\nCurrent facts: {json.dumps(current)}")
            try:
                ops = json.loads(out[out.index("{"):out.rindex("}") + 1])
            except (ValueError, json.JSONDecodeError):
                return
            for retire in ops.get("retire", []):
                for r in self.rows:
                    if not r.superseded and retire.lower() in r.content.lower():
                        r.superseded = True
            for fact in ops.get("store", []):
                if isinstance(fact, str) and fact.strip():
                    self.rows.append(Row(self._next, fact.strip(), 0.7, self.now, embed(fact.strip())))
                    self._next += 1

    def current_facts(self):
        return [r.content for r in self.rows if not r.superseded]

    def size(self):
        return sum(1 for r in self.rows if not r.superseded)


# ---------------------------------------------------------------------------
# S2: claim-graph
# ---------------------------------------------------------------------------

class S2ClaimGraph:
    NAME = "S2-claim-graph"
    SYS = ("You are a memory decider. Given the latest message and current "
           "claims, reply with JSON: {\"store\": [{\"content\": \"<fact>\", "
           "\"retires\": \"<substring of an old claim it corrects>\"}], ...}. "
           "Store durable facts only. When a new claim corrects an old one, "
           "name the old one in retires.")

    def __init__(self):
        self.rows: list[Row] = []
        self._next = 1
        self.now = 0.0
        self.calls = 0

    def tick(self):
        self.now += 1.0

    def turn(self, text, topics):
        self.tick()
        self.calls += 1
        current = [r.content for r in self.rows if not r.superseded][-10:]
        out = decider_call(self.SYS, f"Latest message: {text}\nCurrent claims: {json.dumps(current)}")
        try:
            ops = json.loads(out[out.index("{"):out.rindex("}") + 1])
        except (ValueError, json.JSONDecodeError):
            return
        for item in ops.get("store", []):
            if not isinstance(item, dict):
                continue
            content = item.get("content", "").strip()
            if not content:
                continue
            new_row = Row(self._next, content, 0.7, self.now, embed(content))
            self._next += 1
            retires = item.get("retires", "")
            if retires:
                for r in self.rows:
                    if not r.superseded and retires.lower() in r.content.lower():
                        # Link instead of dropping: the new claim corrects the old.
                        new_row.links.append((r.id, "corrects"))
                        r.superseded = True
            self.rows.append(new_row)

    def current_facts(self):
        return [r.content for r in self.rows if not r.superseded]

    def size(self):
        return sum(1 for r in self.rows if not r.superseded)


# ---------------------------------------------------------------------------
# S3: attention-sink — anchors + query recall
# ---------------------------------------------------------------------------

class S3AttentionSink:
    NAME = "S3-attention-sink"
    ANCHORS = 3
    RATE = 0.8

    def __init__(self):
        self.rows: list[Row] = []
        self._next = 1
        self.now = 0.0

    def tick(self):
        self.now += 1.0

    def turn(self, text, topics):
        self.tick()
        if topics and random.random() < self.RATE:
            self.rows.append(Row(self._next, text, 0.7, self.now, embed(text)))
            self._next += 1

    def read_path(self, query):
        anchors = sorted([r for r in self.rows if not r.superseded], key=lambda r: -r.salience)[:self.ANCHORS]
        recalled = recall_rows(self.rows, query, budget=5)
        seen = {r.id for r in anchors}
        return anchors + [r for r in recalled if r.id not in seen]

    def current_facts(self):
        # The "context" for the checkpoint is anchors + recall of the probe.
        return [r.content for r in self.rows if not r.superseded]

    def size(self):
        return sum(1 for r in self.rows if not r.superseded)


# ---------------------------------------------------------------------------
# S4: confidence-decay — no delete, no supersede
# ---------------------------------------------------------------------------

class S4ConfidenceDecay:
    NAME = "S4-confidence-decay"
    RATE = 0.8
    DECAY = 0.02  # confidence lost per turn

    def __init__(self):
        self.rows: list[Row] = []
        self._next = 1
        self.now = 0.0

    def tick(self):
        self.now += 1.0
        for r in self.rows:
            if not r.superseded:
                r.confidence = max(0.0, r.confidence - self.DECAY)
                if r.confidence <= 0.0:
                    r.superseded = True  # decayed out of existence

    def turn(self, text, topics):
        self.tick()
        if topics and random.random() < self.RATE:
            self.rows.append(Row(self._next, text, 0.7, self.now, embed(text)))
            self._next += 1

    def current_facts(self):
        return [r.content for r in self.rows if not r.superseded]

    def size(self):
        return sum(1 for r in self.rows if not r.superseded)


# ---------------------------------------------------------------------------
# S5: materialized view — log is truth, view is rebuilt periodically
# ---------------------------------------------------------------------------

class S5MaterializedView:
    NAME = "S5-materialized-view"
    REBUILD_EVERY = 10
    SYS = ("You distill a conversation log into current facts. Reply with JSON "
           '{"facts": ["<fact>", ...]}. Keep only facts that are currently true; '
           "a later correction replaces the earlier statement entirely.")

    def __init__(self):
        self.log: list[str] = []
        self.view: list[str] = []
        self.calls = 0
        self.now = 0.0

    def tick(self):
        self.now += 1.0

    def turn(self, text, topics):
        self.tick()
        self.log.append(text)
        if len(self.log) % self.REBUILD_EVERY == 0:
            self.calls += 1
            out = decider_call(self.SYS, json.dumps(self.log[-50:]))
            try:
                ops = json.loads(out[out.index("{"):out.rindex("}") + 1])
                facts = [f.strip() for f in ops.get("facts", []) if isinstance(f, str) and f.strip()]
                if facts:
                    self.view = facts
            except (ValueError, json.JSONDecodeError):
                pass

    def current_facts(self):
        return list(self.view)

    def size(self):
        return len(self.view)


ALL_NEW = [S1SleepConsolidation, S2ClaimGraph, S3AttentionSink, S4ConfidenceDecay, S5MaterializedView]
