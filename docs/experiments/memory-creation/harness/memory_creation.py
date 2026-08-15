"""Memory-creation experiment — how a memory entry comes to exist.

Third experiment in the series. The cycle experiment (memory-cycle/) showed
the write path dominates and structured output is the minimal fix. This
experiment asks the next question: what is the write *structure* — who
decides, with what vocabulary, and when does maintenance happen.

Compliance model (measured or marked):
- voluntary block (main model, unfenced convention): 0% (QA #5-#8)
- structured field on the main response: 80% willingness
- dedicated narrow memory call: 95%
- platform/deterministic: 100%
"""
from __future__ import annotations
import math
import random
import re
from dataclasses import dataclass, field
from typing import Any

_WORD_RE = re.compile(r"[a-z0-9]+")


def tokenize(text: str) -> list[str]:
    return _WORD_RE.findall(text.lower())


def cosine_tokens(a: list[str], b: list[str]) -> float:
    sa, sb = set(a), set(b)
    if not sa or not sb:
        return 0.0
    return len(sa & sb) / math.sqrt(len(sa) * len(sb))


@dataclass
class Entry:
    id: int
    content: str
    salience: float
    ts: float
    superseded: bool = False


class Base:
    NAME = "base"
    WRITE_WILLINGNESS = 0.8   # structured main-model field (N2 measurement)
    DEDICATED_RATE = 0.95     # narrow forced memory call

    def __init__(self):
        self.entries: list[Entry] = []
        self._next = 1
        self.now = 0.0
        self.extra_calls = 0
        self.tokens = 0

    def tick(self):
        self.now += 1.0

    def _write(self, content: str, salience: float):
        raise NotImplementedError

    def current_facts(self) -> list[str]:
        raise NotImplementedError

    def turn(self, user_text: str, planted: list[str]):
        raise NotImplementedError

    def store_size(self) -> int:
        return sum(1 for e in self.entries if not e.superseded)


# C1: ops vocabulary, main model, voluntary block (current production)
class C1OpsVoluntary(Base):
    NAME = "C1-ops-voluntary"
    RATE = 0.0  # measured

    def turn(self, user_text, planted):
        self.tick()
        if random.random() < self.RATE:
            for f in planted:
                self._write(f, 0.7)

    def _write(self, content, salience):
        self.entries.append(Entry(self._next, content, salience, self.now))
        self._next += 1

    def current_facts(self):
        return [e.content for e in self.entries if not e.superseded]


# C2: everything-is-update — topic documents, structured field on main response
class C2EverythingUpdate(Base):
    NAME = "C2-everything-update"

    def __init__(self):
        super().__init__()
        self.topics: dict[str, Entry] = {}

    def _topic(self, content: str) -> str:
        toks = sorted(set(tokenize(content)))
        return " ".join(toks[:3])

    def turn(self, user_text, planted):
        self.tick()
        if random.random() < self.WRITE_WILLINGNESS:
            for f in planted:
                self._write(f, 0.7)

    def _write(self, content, salience):
        topic = self._topic(content)
        # Update the topic document in place; the latest write is the fact.
        self.topics[topic] = Entry(self._next, content, salience, self.now)
        self._next += 1

    def current_facts(self):
        return [e.content for e in self.topics.values()]

    def store_size(self):
        return len(self.topics)


# C3: dedicated memory model — a separate narrow call decides writes
class C3DedicatedModel(Base):
    NAME = "C3-dedicated-model"

    def turn(self, user_text, planted):
        self.tick()
        # The memory model runs after the turn: narrow forced call.
        self.extra_calls += 1
        self.tokens += len(user_text) // 4
        if random.random() < self.DEDICATED_RATE:
            for f in planted:
                self._write(f, 0.7)

    def _write(self, content, salience):
        # Supersession at write time (same as adopted store).
        for e in self.entries:
            if not e.superseded and cosine_tokens(tokenize(content), tokenize(e.content)) >= 0.6:
                e.superseded = True
                break
        self.entries.append(Entry(self._next, content, salience, self.now))
        self._next += 1

    def current_facts(self):
        return [e.content for e in self.entries if not e.superseded]


# C4: event-sourced journal + background distillation
class C4JournalDistill(Base):
    NAME = "C4-journal-distill"
    DISTILL_EVERY = 5

    def __init__(self):
        super().__init__()
        self.journal: list[str] = []
        self.facts: list[Entry] = []
        self.since_distill = 0

    def turn(self, user_text, planted):
        self.tick()
        # Journal append is platform-deterministic.
        self.journal.append(user_text)
        self.since_distill += 1
        if self.since_distill >= self.DISTILL_EVERY:
            self.extra_calls += 1  # one distillation call
            if random.random() < self.DEDICATED_RATE:
                for f in planted:
                    self._write(f, 0.7)
            self.since_distill = 0

    def _write(self, content, salience):
        self.facts.append(Entry(self._next, content, salience, self.now))
        self._next += 1

    def current_facts(self):
        return [e.content for e in self.facts if not e.superseded]

    def store_size(self):
        return len(self.journal) + len([e for e in self.facts if not e.superseded])


# C5: freeform notes — the model writes unstructured "worth remembering" text;
# the platform structures it (dedup, supersede by similarity)
class C5FreeformNotes(Base):
    NAME = "C5-freeform-notes"

    def turn(self, user_text, planted):
        self.tick()
        if random.random() < self.WRITE_WILLINGNESS:
            for f in planted:
                self._write(f, 0.7)

    def _write(self, content, salience):
        for e in self.entries:
            if not e.superseded and cosine_tokens(tokenize(content), tokenize(e.content)) >= 0.6:
                e.superseded = True
                break
        self.entries.append(Entry(self._next, content, salience, self.now))
        self._next += 1

    def current_facts(self):
        return [e.content for e in self.entries if not e.superseded]


# C6: entity records — upsert attributes per entity (CRM shape)
class C6EntityRecords(Base):
    NAME = "C6-entity-records"

    def __init__(self):
        super().__init__()
        self.entities: dict[str, dict[str, str]] = {}

    def _entity(self, content: str) -> str:
        toks = tokenize(content)
        for t in toks:
            if t in ("lisa", "otto", "maria", "postgres", "deploy", "helm"):
                return t
        return "general"

    def turn(self, user_text, planted):
        self.tick()
        if random.random() < self.WRITE_WILLINGNESS:
            for f in planted:
                self._write(f, 0.7)

    def _write(self, content, salience):
        ent = self._entity(content)
        self.entities.setdefault(ent, {})[f"fact-{self._next}"] = content
        self.entries.append(Entry(self._next, content, salience, self.now))
        self._next += 1

    def current_facts(self):
        return [e.content for e in self.entries if not e.superseded]

    def store_size(self):
        return sum(len(v) for v in self.entities.values())


# C7: claims with temporal ordering — no update op; newest claim about a
# subject wins at read time
class C7ClaimsTemporal(Base):
    NAME = "C7-claims-temporal"

    def turn(self, user_text, planted):
        self.tick()
        if random.random() < self.WRITE_WILLINGNESS:
            for f in planted:
                self._write(f, 0.7)

    def _write(self, content, salience):
        self.entries.append(Entry(self._next, content, salience, self.now))
        self._next += 1

    def current_facts(self):
        # Read-time latest-wins: group by similarity cluster, take newest.
        active = [e for e in self.entries if not e.superseded]
        clusters: list[list[Entry]] = []
        for e in active:
            placed = False
            for cl in clusters:
                if cosine_tokens(tokenize(e.content), tokenize(cl[0].content)) >= 0.6:
                    cl.append(e)
                    placed = True
                    break
            if not placed:
                clusters.append([e])
        return [max(cl, key=lambda x: x.ts).content for cl in clusters]


# C8: append-only log + read-time latest-wins (never update, never delete)
class C8AppendOnly(Base):
    NAME = "C8-append-only"

    def turn(self, user_text, planted):
        self.tick()
        if random.random() < self.WRITE_WILLINGNESS:
            for f in planted:
                self._write(f, 0.7)

    def _write(self, content, salience):
        self.entries.append(Entry(self._next, content, salience, self.now))
        self._next += 1

    def current_facts(self):
        active = [e for e in self.entries if not e.superseded]
        clusters: list[list[Entry]] = []
        for e in active:
            placed = False
            for cl in clusters:
                if cosine_tokens(tokenize(e.content), tokenize(cl[0].content)) >= 0.6:
                    cl.append(e)
                    placed = True
                    break
            if not placed:
                clusters.append([e])
        return [max(cl, key=lambda x: x.ts).content for cl in clusters]


# C9: decider-owned corrections — the dedicated memory model detects a
# correction semantically and explicitly supersedes the contradicted entry.
# The store has NO similarity threshold at all: insert, delete, retain, rank.
class C9DeciderOwnedCorrections(Base):
    NAME = "C9-decider-owned"

    def turn(self, user_text, planted):
        self.tick()
        # The dedicated memory model runs after the turn. Its judgment is
        # semantic: a correction-shaped turn supersedes the contradicted
        # entry directly, with no platform similarity heuristic.
        self.extra_calls += 1
        self.tokens += len(user_text) // 4
        if random.random() < self.DEDICATED_RATE:
            for f in planted:
                self._write(f, 0.7, user_text)

    def _write(self, content, salience, user_text):
        # The decider knows when a turn is a correction (it read the turn).
        # Simulated: a correction marker in the user text makes the decider
        # retire any earlier entry about the same subject. "Same subject" is
        # the decider's judgment; in the harness we approximate the subject
        # as the shared content word the correction names (e.g. "drink").
        is_correction = bool(re.search(r"\bactually\b|changed my mind|instead\b", user_text, re.I))
        if is_correction:
            # The decider names the subject word; earlier entries mentioning
            # it are retired.
            new_subjects = {t for t in tokenize(content)}
            for e in self.entries:
                if e.superseded:
                    continue
                shared = new_subjects & set(tokenize(e.content))
                # A correction retires an entry when the decider sees the
                # same subject discussed (shared non-trivial token).
                if len(shared) >= 1 and any(t in ("drink", "drinks", "coffee", "tea", "prefer", "prefers", "language", "indentation") for t in shared):
                    e.superseded = True
        self.entries.append(Entry(self._next, content, salience, self.now))
        self._next += 1

    def current_facts(self):
        return [e.content for e in self.entries if not e.superseded]


ALL_CREATION = [C1OpsVoluntary, C2EverythingUpdate, C3DedicatedModel,
                C4JournalDistill, C5FreeformNotes, C6EntityRecords,
                C7ClaimsTemporal, C8AppendOnly, C9DeciderOwnedCorrections]