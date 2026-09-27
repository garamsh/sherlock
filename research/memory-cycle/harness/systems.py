"""System-level memory experiment — the full cycle, not just the store.

Previous rounds tested storage/retrieval architectures. They all assumed the
write happens. QA pass #7/#8 measured that assumption false: gpt-4o-mini and
gpt-4o both emit zero voluntary writeback blocks, so under curation-only
storage the store stays empty no matter how good the ranking is.

This experiment treats the whole cycle as the design space:

  turn arrives -> READ PATH (what enters the model's context)
               -> model responds (simulated, with measured compliance)
               -> WRITE PATH (what enters long-term memory)
               -> MAINTENANCE (retention/consolidation)

Each candidate system is a small simulation of that loop over a scripted
conversation with planted facts and checkpoint probes. The score that
matters: at each checkpoint, does the model's context contain the fact?

Model behavior is simulated with MEASURED compliance rates, not hopeful ones:
- voluntary writeback compliance: 0% (measured on gpt-4o-mini in QA #5-#7,
  and on gpt-4o in QA #8 — 15/15 responses without a block)
- structured-output compliance: 100% (constrained decoding is API-enforced)
- platform extraction: 100% (deterministic, no model involved)
- forced narrow summary call: 95% (a single-sentence forced call is a much
  easier compliance target than a per-response fenced block; assumption,
  marked as such)
- on-demand search usage: 30% (a model often does not know what it does
  not know — ADR 0006's motivation for recall injection; assumption, marked)
"""
from __future__ import annotations
import math
import re
import hashlib
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


# ---------------------------------------------------------------------------
# Simulated store (shared by all systems; the G6 scoring from the adopted
# model: salience + relevance + salience-scaled recency)
# ---------------------------------------------------------------------------

@dataclass
class MemoryRow:
    id: int
    content: str
    salience: float
    timestamp: float
    kind: str


class Store:
    def __init__(self):
        self.rows: list[MemoryRow] = []
        self._next = 1
        self.now = 0.0

    def tick(self, hours: float = 1.0):
        self.now += hours

    def add(self, content: str, salience: float, kind: str = "semantic"):
        # Supersession: close token match replaces the old row.
        new_toks = tokenize(content)
        for i, r in enumerate(self.rows):
            if cosine_tokens(new_toks, tokenize(r.content)) >= 0.6:
                self.rows[i] = MemoryRow(r.id, content, max(r.salience, salience), self.now, kind)
                return r.id
        row = MemoryRow(self._next, content, salience, self.now, kind)
        self._next += 1
        self.rows.append(row)
        return row.id

    def recall(self, query: str, budget: int = 5) -> list[MemoryRow]:
        q = tokenize(query)
        scored = []
        for r in self.rows:
            age_h = max(0.0, self.now - r.timestamp)
            half = 24.0 * (1.0 + r.salience)
            recency = math.exp(-math.log(2) * age_h / half)
            rel = cosine_tokens(q, tokenize(r.content))
            scored.append((r.salience + rel + recency, r))
        scored.sort(key=lambda x: -x[0])
        return [r for s, r in scored[:budget] if s > 0.05]

    def size(self) -> int:
        return len(self.rows)


# ---------------------------------------------------------------------------
# Simulated model — compliance is per write/read path, measured or marked.
# ---------------------------------------------------------------------------

@dataclass
class SimModel:
    voluntary_writeback_rate: float = 0.0   # measured: 0/15 on gpt-4o (QA #8)
    forced_summary_rate: float = 0.95       # assumption: narrow forced call
    structured_output_rate: float = 1.0     # API constrained decoding
    ondemand_search_rate: float = 0.30      # assumption: model searches when unsure
    extraction_rate: float = 1.0            # deterministic platform code


# ---------------------------------------------------------------------------
# The systems under test. Each returns the model's effective context at a
# checkpoint: the set of fact strings the model can see.
# ---------------------------------------------------------------------------

class BaseSystem:
    NAME = "base"
    def __init__(self, model: SimModel):
        self.model = model
        self.store = Store()
        self.extra_model_calls = 0   # cost metric: forced calls per turn
        self.injected_tokens = 0     # cost metric: tokens injected per turn
        self.context_facts: set[str] = set()

    def turn(self, user_text: str, planted_facts: list[str]):
        raise NotImplementedError

    def checkpoint_context(self, probe: str) -> set[str]:
        """What the model can see when probed."""
        raise NotImplementedError


# N1: voluntary writeback + per-turn recall (the current production shape)
class N1VoluntaryWriteback(BaseSystem):
    NAME = "N1-voluntary-writeback"

    def turn(self, user_text, planted_facts):
        self.store.tick()
        # Read: recall injection with the turn text as the query.
        recalled = self.store.recall(user_text)
        for r in recalled:
            self.context_facts.add(r.content)
        self.injected_tokens += sum(len(r.content) // 4 for r in recalled)
        # Write: the model may emit a writeback block. Measured compliance: 0%.
        import random
        if random.random() < self.model.voluntary_writeback_rate:
            for f in planted_facts:
                self.store.add(f, 0.7)

    def checkpoint_context(self, probe):
        return set(self.context_facts)


# N2: structured-output writeback (API-enforced {reply, memory_ops})
class N2StructuredWriteback(BaseSystem):
    NAME = "N2-structured-writeback"
    # The API enforces the SHAPE, not the content. A model can return
    # schema-valid empty ops. optimistic_write_rate models how often the
    # model writes the facts it should when the shape is guaranteed.
    optimistic_write_rate = 0.8

    def turn(self, user_text, planted_facts):
        self.store.tick()
        recalled = self.store.recall(user_text)
        for r in recalled:
            self.context_facts.add(r.content)
        self.injected_tokens += sum(len(r.content) // 4 for r in recalled)
        import random
        if random.random() < self.optimistic_write_rate:
            for f in planted_facts:
                self.store.add(f, 0.7)

    def checkpoint_context(self, probe):
        return set(self.context_facts)


# N2s: structured output with a lazy model — schema-valid empty ops half the time
class N2sStructuredLazy(N2StructuredWriteback):
    NAME = "N2s-structured-lazy"
    optimistic_write_rate = 0.5


# N8: structured writeback + core block (top-salience facts always injected)
class N8StructuredCoreBlock(N2StructuredWriteback):
    NAME = "N8-structured-coreblock"
    CORE_K = 3  # top-salience memories always in context, regardless of query

    def turn(self, user_text, planted_facts):
        self.store.tick()
        # Read: query-anchored recall PLUS the top-salience core block.
        recalled = self.store.recall(user_text)
        core = sorted(self.store.rows, key=lambda r: -r.salience)[:self.CORE_K]
        seen = {r.id for r in recalled}
        for r in recalled + [c for c in core if c.id not in seen]:
            self.context_facts.add(r.content)
        self.injected_tokens += sum(len(r.content) // 4 for r in recalled)
        self.injected_tokens += sum(len(c.content) // 4 for c in core)
        import random
        if random.random() < self.optimistic_write_rate:
            for f in planted_facts:
                self.store.add(f, 0.7)


# N3: platform-side extraction — deterministic fact pull from the user turn
class N3PlatformExtraction(BaseSystem):
    NAME = "N3-platform-extraction"
    PATTERNS = [
        (re.compile(r"\bi prefer\b", re.I), 0.7),
        (re.compile(r"\bi (?:use|like|love|dislike|hate)\b", re.I), 0.6),
        (re.compile(r"\bmy (?:name|manager|partner|sister|friend)\b", re.I), 0.8),
        (re.compile(r"\balways\b|\bnever\b", re.I), 0.7),
        (re.compile(r"\bactually\b|\binstead\b|\bnot .+ but\b", re.I), 0.8),  # corrections
        (re.compile(r"\bi (?:work|live|commute|bike|run|drink|eat|read|watch)\b", re.I), 0.5),
    ]

    def turn(self, user_text, planted_facts):
        self.store.tick()
        recalled = self.store.recall(user_text)
        for r in recalled:
            self.context_facts.add(r.content)
        self.injected_tokens += sum(len(r.content) // 4 for r in recalled)
        # Deterministic extraction: the platform scans the user turn for
        # fact-shaped statements and stores them. No model involved.
        for pat, sal in self.PATTERNS:
            if pat.search(user_text):
                # The extracted fact is the sentence itself.
                self.store.add(user_text, sal, kind="semantic")
                break

    def checkpoint_context(self, probe):
        return set(self.context_facts)


# N4: rolling narrative — one living document, rewritten by a forced call
class N4RollingNarrative(BaseSystem):
    NAME = "N4-rolling-narrative"

    def __init__(self, model):
        super().__init__(model)
        self.narrative = ""

    def turn(self, user_text, planted_facts):
        self.store.tick()
        # Read: the whole narrative is injected every turn.
        if self.narrative:
            self.injected_tokens += len(self.narrative) // 4
            for line in self.narrative.split("\n"):
                if line.strip():
                    self.context_facts.add(line.strip())
        # Write: one forced narrow call appends/rewrites the narrative.
        self.extra_model_calls += 1
        import random
        if random.random() < self.model.forced_summary_rate:
            for f in planted_facts:
                if f not in self.narrative:
                    self.narrative += f + "\n"

    def checkpoint_context(self, probe):
        return set(self.context_facts)


# N5: verbatim log + on-demand search tool (no injection)
class N5LogOnDemand(BaseSystem):
    NAME = "N5-log-ondemand"

    def turn(self, user_text, planted_facts):
        self.store.tick()
        # Write: every turn is stored verbatim (the log).
        self.store.add(user_text, 0.4, kind="episodic")
        # Read: nothing injected. The model may decide to search.
        import random
        if random.random() < self.model.ondemand_search_rate:
            recalled = self.store.recall(user_text)
            for r in recalled:
                self.context_facts.add(r.content)
            self.injected_tokens += sum(len(r.content) // 4 for r in recalled)

    def checkpoint_context(self, probe):
        return set(self.context_facts)


# N6: forced per-turn summary note + per-turn recall of notes
class N6ForcedNotes(BaseSystem):
    NAME = "N6-forced-notes"

    def turn(self, user_text, planted_facts):
        self.store.tick()
        recalled = self.store.recall(user_text)
        for r in recalled:
            self.context_facts.add(r.content)
        self.injected_tokens += sum(len(r.content) // 4 for r in recalled)
        # Write: a forced narrow call writes a one-sentence note per turn.
        self.extra_model_calls += 1
        import random
        if random.random() < self.model.forced_summary_rate:
            for f in planted_facts:
                self.store.add(f, 0.6, kind="semantic")

    def checkpoint_context(self, probe):
        return set(self.context_facts)


# N7: no memory system at all — bounded context window (realistic)
class N7FullContext(BaseSystem):
    NAME = "N7-full-context"
    CONTEXT_TURNS = 10  # the window holds the last 10 turns; older turns fall out

    def __init__(self, model):
        super().__init__(model)
        self.window: list[str] = []

    def turn(self, user_text, planted_facts):
        self.store.tick()
        self.window.append(user_text)
        for f in planted_facts:
            self.context_facts.add(f)
        # Compaction: when the window overflows, the oldest turns are
        # dropped. Nothing is summarized into a store (no memory system).
        if len(self.window) > self.CONTEXT_TURNS:
            dropped = self.window[:-self.CONTEXT_TURNS]
            self.window = self.window[-self.CONTEXT_TURNS:]
            # Facts from dropped turns leave the context too — the model
            # no longer sees them. Track which planted facts remain visible.
            visible = set()
            for t in self.window:
                visible.add(t)
            self.context_facts = {f for f in self.context_facts
                                  if any(tok in t for t in visible for tok in tokenize(f))}
        self.injected_tokens += sum(len(t) // 4 for t in self.window)

    def checkpoint_context(self, probe):
        return set(self.context_facts)


ALL_SYSTEMS = [
    N1VoluntaryWriteback,
    N2StructuredWriteback,
    N2sStructuredLazy,
    N3PlatformExtraction,
    N4RollingNarrative,
    N5LogOnDemand,
    N6ForcedNotes,
    N7FullContext,
    N8StructuredCoreBlock,
]
