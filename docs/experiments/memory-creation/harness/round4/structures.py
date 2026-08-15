"""The candidate memory-creation structures."""
from __future__ import annotations

from api import cosine, decider, embed, parse_json

RECALL_K = 5
# Relevance floor for candidates offered to a decider; calibrated in separability.py.
CANDIDATE_FLOOR = 0.4


class Row:
    __slots__ = ("id", "key", "content", "vec", "ts", "dead", "corrected_by")

    def __init__(self, rid, key, content, vec, ts):
        self.id = rid
        self.key = key
        self.content = content
        self.vec = vec
        self.ts = ts
        self.dead = False
        self.corrected_by = None


class Store:
    """Rows plus relevance ranking. No decay, retention or salience."""

    def __init__(self):
        self.rows: list[Row] = []
        self.by_key: dict[str, Row] = {}
        self._next = 1
        self.now = 0

    def tick(self):
        self.now += 1

    def add(self, content, key=None) -> Row:
        row = Row(self._next, key, content, embed(content), self.now)
        self._next += 1
        self.rows.append(row)
        return row

    def live(self):
        return [r for r in self.rows if not r.dead]

    def rank(self, query, k=RECALL_K, rows=None, floor=None):
        qv = embed(query)
        pool = self.live() if rows is None else rows
        scored = [(cosine(qv, r.vec), r) for r in pool]
        scored.sort(key=lambda x: -x[0])
        return [r for s, r in scored[:k] if floor is None or s >= floor]

    def recall(self, query, k=RECALL_K):
        return self.rank(query, k)

    def size(self):
        return len(self.live())


class Structure:
    """Default probe answering: rank the live rows."""

    def recall(self, query, k=RECALL_K):
        return self.store.rank(query, k)

    def size(self):
        return self.store.size()


class StoreThreshold(Structure):
    NAME = "B1-store-threshold"
    BLURB = "main-model field writes; the store supersedes at cosine >= 0.6"
    EXTRA_CALLS = 0.0
    THRESHOLD = 0.6

    def __init__(self, rnd, compliance=0.8):
        self.store = Store()
        self.rnd = rnd
        self.compliance = compliance
        self.calls = 0

    def turn(self, text, is_fact):
        self.store.tick()
        if not is_fact or self.rnd.random() > self.compliance:
            return
        vec = embed(text)
        for row in self.store.live():
            if cosine(vec, row.vec) >= self.THRESHOLD:
                row.dead = True
                break
        self.store.add(text)


RETIRE_SYS = (
    "You are a memory decider for a long-lived assistant. You are given the "
    "latest message and a list of possibly-related stored memories with ids. "
    "Decide what to store and what to retire.\n"
    'Reply with JSON: {"store": ["<fact to store>", ...], "retire": [<id>, ...]}\n'
    "Rules: store durable facts about people, preferences and systems. Store "
    "nothing for small talk. Retire a memory only when the latest message "
    "contradicts it and makes it false. Two facts that can both be true at "
    "once are both kept."
)


class DeciderWindow(Structure):
    """B2: the decider is shown the most recent memories."""
    NAME = "B2-decider-window"
    BLURB = "decider sees the 10 most recently written memories"
    EXTRA_CALLS = 1.0
    WINDOW = 10

    def __init__(self, rnd, model=None):
        self.store = Store()
        self.calls = 0
        self.model = model

    def context_rows(self, text):
        return self.store.live()[-self.WINDOW:]

    def turn(self, text, is_fact):
        self.store.tick()
        rows = self.context_rows(text)
        listing = [{"id": r.id, "memory": r.content} for r in rows]
        kw = {"model": self.model} if self.model else {}
        out = decider(RETIRE_SYS,
                      f"Latest message: {text}\nRelated memories: {listing}", **kw)
        self.calls += 1
        ops = parse_json(out)
        for rid in ops.get("retire", []) or []:
            for r in rows:
                if r.id == rid and not r.dead:
                    r.dead = True
        for fact in ops.get("store", []) or []:
            if isinstance(fact, str) and fact.strip():
                self.store.add(fact.strip())


class DeciderNeighbors(DeciderWindow):
    """B3: the decider is shown the retrieval for the incoming turn."""
    NAME = "B3-decider-neighbors"
    BLURB = "decider sees the retrieved neighbours of the incoming message"

    def context_rows(self, text):
        return self.store.rank(text, RECALL_K, floor=CANDIDATE_FLOOR)


KEY_SYS = (
    "You maintain a key-value memory for a long-lived assistant. Given the "
    "latest message, decide which facts to write.\n"
    'Reply with JSON: {"writes": [{"key": "<topic key>", "value": "<the fact '
    'as a full sentence>"}, ...]}\n'
    "The key names the topic, not the value: use a stable lowercase key like "
    '"lisa.editor" or "sam.database" so a later message about the same topic '
    "produces the same key and replaces the old value. Give facts that can "
    "both be true at once distinct keys. Write nothing for small talk: "
    '{"writes": []}'
)

KEY_SYS_NEIGHBOURS = KEY_SYS + (
    "\nYou are also shown keys already in the store that look related. If the "
    "message is about one of those topics, reuse that key exactly."
)


class KeyUpsert(Structure):
    """B4: the decider names a topic key; the same key replaces."""
    NAME = "B4-key-upsert"
    BLURB = "decider writes key->value; same key replaces. No retire op"
    EXTRA_CALLS = 1.0
    SUGGEST_KEYS = False

    def __init__(self, rnd, compliance=1.0, model=None):
        self.store = Store()
        self.rnd = rnd
        self.compliance = compliance
        self.calls = 0
        self.model = model

    def _writes(self, text):
        sys_prompt = KEY_SYS
        user = f"Latest message: {text}"
        if self.SUGGEST_KEYS:
            near = self.store.rank(text, RECALL_K, floor=CANDIDATE_FLOOR)
            keys = sorted({r.key for r in near if r.key})
            sys_prompt = KEY_SYS_NEIGHBOURS
            user = f"Latest message: {text}\nRelated keys already stored: {keys}"
        kw = {"model": self.model} if self.model else {}
        out = decider(sys_prompt, user, **kw)
        self.calls += 1
        return parse_json(out).get("writes", []) or []

    def apply(self, key, value):
        old = self.store.by_key.get(key)
        if old is not None and not old.dead:
            old.dead = True
        row = self.store.add(value, key=key)
        self.store.by_key[key] = row

    def turn(self, text, is_fact):
        self.store.tick()
        if self.compliance < 1.0 and self.rnd.random() > self.compliance:
            return
        for w in self._writes(text):
            if not isinstance(w, dict):
                continue
            key, value = w.get("key"), w.get("value")
            if isinstance(key, str) and isinstance(value, str) and key and value:
                self.apply(key.strip().lower(), value.strip())


class KeyUpsertNeighbours(KeyUpsert):
    """B5: B4 with the store's related keys offered back."""
    NAME = "B5-key-upsert-neighbours"
    BLURB = "key-upsert, with the store offering related existing keys"
    SUGGEST_KEYS = True


class KeyAppend(KeyUpsertNeighbours):
    """B6: B5 append-only; recall keeps the newest row per key."""
    NAME = "B6-key-append-readtime"
    BLURB = "append-only; recall keeps the newest row per key"

    def apply(self, key, value):
        row = self.store.add(value, key=key)
        self.store.by_key[key] = row

    def _recall_rows(self):
        newest: dict[str, Row] = {}
        loose = []
        for r in self.store.rows:
            if r.key is None:
                loose.append(r)
            elif r.key not in newest or r.ts >= newest[r.key].ts:
                newest[r.key] = r
        return list(newest.values()) + loose

    def recall(self, query, k=RECALL_K):
        return self.store.rank(query, k, rows=self._recall_rows())

    def size(self):
        return len(self._recall_rows())


class KeyUpsertMainModel(KeyUpsertNeighbours):
    """B7: B5 written as a field on the main response."""
    NAME = "B7-key-mainmodel-field"
    BLURB = "key-upsert written as a field on the main response (no extra call)"
    EXTRA_CALLS = 0.0

    def __init__(self, rnd, compliance=0.8, model=None):
        super().__init__(rnd, compliance=compliance, model=model)


LINK_SYS = (
    "You maintain a claim graph for a long-lived assistant. Given the latest "
    "message and possibly-related claims with ids, decide what to add.\n"
    'Reply with JSON: {"claims": [{"text": "<claim>", "corrects": <id or null>}]}\n'
    "Set corrects to the id of a claim this one makes false; use null when the "
    "claim stands on its own or when both can be true. Add no claims for small "
    'talk: {"claims": []}'
)


class ClaimGraph(Structure):
    NAME = "B8-claim-graph"
    BLURB = "claims with a typed 'corrects' link; retrieval hides corrected claims"
    EXTRA_CALLS = 1.0

    def __init__(self, rnd, model=None):
        self.store = Store()
        self.calls = 0
        self.model = model

    def turn(self, text, is_fact):
        self.store.tick()
        near = self.store.rank(text, RECALL_K, floor=CANDIDATE_FLOOR)
        listing = [{"id": r.id, "claim": r.content} for r in near]
        kw = {"model": self.model} if self.model else {}
        out = decider(LINK_SYS,
                      f"Latest message: {text}\nRelated claims: {listing}", **kw)
        self.calls += 1
        for c in parse_json(out).get("claims", []) or []:
            if not isinstance(c, dict):
                continue
            body = c.get("text")
            if not isinstance(body, str) or not body.strip():
                continue
            row = self.store.add(body.strip())
            target = c.get("corrects")
            if isinstance(target, int):
                for r in near:
                    if r.id == target and not r.dead:
                        r.dead = True
                        r.corrected_by = row.id


class DeciderNeighborsWide(DeciderNeighbors):
    """B9: B3 with a wider candidate offer."""
    NAME = "B9-decider-neighbors-wide"
    BLURB = "as B3, but offering 10 candidates down to cosine 0.3"
    WIDE_K = 10
    WIDE_FLOOR = 0.3

    def context_rows(self, text):
        return self.store.rank(text, self.WIDE_K, floor=self.WIDE_FLOOR)
