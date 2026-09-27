"""Track A follow-ups to B14 (`structures.KeyUpsertPluralAware`).

B14's residual is latest-wins at 0.75. `upsert_diagnosis.py` replays the 100
correction turns and classifies what the decider did with the key the old
value lives under:

  reused the old key       80   the upsert replaced the value; correction landed
  coined a new key         14   the old row stays live; the probe reads stale
  wrote nothing             0
  no live row held the old  6   (never written, or already replaced)

So nothing is unwritten and nothing is over-retired: every correction that
misses, misses because the decider named the topic differently the second
time. The 14 split evenly on whether the store even offered the old key:

  not offered   7   the old row was outside the top-5 similarity candidates,
                    so the decider never saw the key it should have reused
                    (alma.manager, sam.manager, vera.programming_language,
                    tariq.terminal, maria.terminal, maria.shell, bruno.manager)
  offered       7   the key was on the table with its value and the decider
                    coined anyway (alma.coffee -> alma.tea, maria.coffee ->
                    maria.tea, tariq.pet.dog -> tariq.pet.cat, bruno.shell ->
                    bruno.diet.fish, kofi.pets -> kofi.pets.turtle,
                    kofi.testing -> kofi.testing.snapshot, sena.builds ->
                    sena.web_tier)

Both halves are one failure — the key is not reproducible — with two causes.
The offered half is dominated by keys that name the *value*: `alma.coffee`
cannot survive a switch to tea, and B14's own narrowing instruction, whose
example key is `otto.language.korean`, teaches exactly that shape and gets it
applied to single-valued corrections (`kofi.pets.turtle`). The not-offered
half is a retrieval miss: similarity ranked four other people's `reports_to`
above this person's `manager`.
"""
from __future__ import annotations

from api import decider, parse_json
from structures import (CANDIDATE_FLOOR, RECALL_K, KeyUpsertPluralAware)


KEY_SYS_NAMESPACE = (
    "You maintain a key-value memory for a long-lived assistant. Given the "
    "latest message, decide which facts to write.\n"
    'Reply with JSON: {"writes": [{"key": "<topic key>", "value": "<the fact '
    'as a full sentence>"}, ...]}\n'
    "The key names the question the fact answers, never the answer itself: "
    'write "alma.morning_drink", never "alma.coffee" or "alma.tea", so a '
    "later message about that topic produces the same key and replaces the "
    "value.\n"
    "You are shown every key the store already holds about the people this "
    "message may concern, each with the value it holds. If the message gives "
    "a new answer to a question one of those keys already answers, reuse that "
    "key exactly as written, however it happens to be named. Only when the new "
    "fact and the stored value can both be true at the same time — someone "
    "speaks several languages, plays several instruments — coin a narrower "
    'key such as "otto.language.korean"; a changed answer to a single-valued '
    "question is never that.\n"
    'Write nothing for small talk: {"writes": []}'
)


class KeyUpsertNamespace(KeyUpsertPluralAware):
    """B15: B14 shown the whole key namespace of the subjects in play.

    One change with two parts, one per half of the diagnosis.

    *What is shown.* B14 offers the keys of the top-5 similar rows. Similarity
    ranks four other people's `reports_to` above this person's `manager`, so
    the key that should be reused is absent from a third of the misses. The
    store already carries the namespace in the keys it assigned: every offered
    key's first segment names a subject, and every live key under those
    subjects is offered with its value. The candidate set stops being "rows
    that look like this sentence" and becomes "everything the store knows
    about the people this sentence is about". Widening an offer cannot delete
    anything — only the upsert can — so this carries the same risk profile as
    B9's wide offer.

    *What the naming rule is.* The key is redefined as the question the fact
    answers rather than a topic label, which makes `alma.coffee` unwritable,
    and B14's narrowing clause is scoped to facts that are simultaneously true
    so it stops firing on single-valued corrections. B14's coexist score of
    1.00 depends on that clause, so it is narrowed, not removed.

    No threshold, no guard, no fallback: a decider that reuses a key still
    replaces exactly one value, and a decider that coins one still adds a row.
    """
    NAME = "B15-key-upsert-namespace"
    BLURB = "key-upsert, offering every key held about the subjects in play"

    def _writes(self, text):
        near = self.store.rank(text, RECALL_K, floor=CANDIDATE_FLOOR)
        subjects = {r.key.split(".")[0] for r in near if r.key}
        pairs = {k: row.content for k, row in self.store.by_key.items()
                 if not row.dead and k.split(".")[0] in subjects}
        kw = {"model": self.model} if self.model else {}
        out = decider(KEY_SYS_NAMESPACE,
                      f"Latest message: {text}\nExisting keys: {pairs}", **kw)
        self.calls += 1
        return parse_json(out).get("writes", []) or []


# ---------------------------------------------------------------------------
# B17 — the half of B15 that worked, at B14's offer size
# ---------------------------------------------------------------------------
#
# B15 changed two things at once and they pulled in opposite directions. The
# key definition worked: coined-key misses fell 14 -> 9, the not-offered half
# 7 -> 1, stale 0.22 -> 0.11, latest-wins 0.75 -> 0.77. The namespace-wide
# hint offer lost it: empty writes rose from 5 to 49 of 388 fact turns, and
# control fell 0.975 -> 0.850 entirely on `never_written`, not on `retired`.
#
# B17 keeps B14's offer — the top-5 retrieved rows' keys with their values,
# byte for byte — and takes only the key definition.
#
# It also replaces B14's narrowing example. `otto.language.korean` names a
# value, and the decider copied the shape onto single-valued questions:
# `alma.coffee` -> `alma.tea`, `kofi.pets` -> `kofi.pets.turtle`,
# `kofi.testing` -> `kofi.testing.snapshot`, `tariq.pet.dog` -> `tariq.pet.cat`
# — 7 of B14's 14 coined-key misses. Worse, its stem word collides with the
# stream's *single-valued* programming-language slot, which is corrected 100
# times: the B15 replay shows the decider had stored `otto.language.python`
# under exactly that pattern, so the example taught value-naming on the one
# category most likely to be corrected.
#
# The replacement is `maria.allergies.peanuts` beside
# `maria.allergies.shellfish`. It keeps parity with B14 (a real subject and a
# real coexisting family from the stream) while removing what did the damage:
# the stem is plural and names a question that genuinely takes several answers
# at once, and `allergies` has no single-valued counterpart anywhere in the
# stream, so copying the shape onto a correction has nothing to land on. The
# example is also paired with its own negative — a new drink, editor or pet is
# a new answer to the same question, not a second simultaneous one.

KEY_SYS_QUESTION = (
    "You maintain a key-value memory for a long-lived assistant. Given the "
    "latest message, decide which facts to write.\n"
    'Reply with JSON: {"writes": [{"key": "<topic key>", "value": "<the fact '
    'as a full sentence>"}, ...]}\n'
    # This is `structures.KEY_SYS` with one sentence replaced: "the key names
    # the topic, not the value" becomes "the key names the question the fact
    # answers". Everything around it, including the small-talk instruction
    # that keeps write-selectivity high, is unchanged.
    "The key names the question the fact answers, never the answer itself: "
    'write "alma.morning_drink", never "alma.coffee" or "alma.tea", so a '
    "later message answering that question produces the same key and replaces "
    "the old value. Give facts that can both be true at once distinct keys. "
    'Write nothing for small talk: {"writes": []}'
)

B17_HINTS = (
    "\nKeys already in the store are shown with the value they hold. Reuse a "
    "key only when the message gives a new answer to the question that key "
    "already answers; then reuse it exactly as written, however it happens to "
    "be named. When a question takes several answers at the same time, give "
    'each answer its own key: "maria.allergies.peanuts" beside '
    '"maria.allergies.shellfish". A question that takes one answer at a time '
    "never gets that treatment — a new drink, editor or pet is a new answer "
    "to the same question. Otherwise coin a new key."
)


class KeyUpsertQuestionKey(KeyUpsertPluralAware):
    """B17: B14's hint offer, B15's key definition, a narrowing example that
    cannot teach value-naming.

    `_writes` is B14's, unchanged, except for the system prompt: the same
    top-5 retrieval at the same floor, the same `{key: value}` offer, the same
    upsert. One variable against B14 (how a key is defined), one against B15
    (how many keys are offered).

    No threshold, no guard, no fallback.
    """
    NAME = "B17-key-upsert-question-key"
    BLURB = "B14's 5-key offer, with the key defined as the question a fact answers"

    def _writes(self, text):
        near = self.store.rank(text, RECALL_K, floor=CANDIDATE_FLOOR)
        pairs = {r.key: r.content for r in near if r.key}
        kw = {"model": self.model} if self.model else {}
        out = decider(KEY_SYS_QUESTION + B17_HINTS,
                      f"Latest message: {text}\nExisting keys: {pairs}", **kw)
        self.calls += 1
        return parse_json(out).get("writes", []) or []


# ---------------------------------------------------------------------------
# B18 — the narrowing example names a family the probes never measure
# ---------------------------------------------------------------------------
#
# B17 showed the example is the mechanism: its example named `allergies`, which
# survived 9/9, and `languages`, which it stopped naming, collapsed to 4/12.
# But `instrument` is named nowhere in B17's prompt and survived 8/8, so the
# split does generalise some of the time and the B17 result cannot say how far.
#
# It also cannot settle B14's coexist 1.00, because every example tried so far
# names a family the probe set measures. Naming all three would raise the score
# by teaching to the test.
#
# B18 changes exactly one thing in B17: the example names `passports`, a
# multi-answer family that appears nowhere in `dataset.py` — not in COEXIST,
# not in CATEGORIES, not in CHATTER — so no probe can be answered by recalling
# it. Coexist then measures whether the decider carries the split across to
# families it was not shown. Everything else, including the subject name in the
# example and the single-answer negatives, is B17 byte for byte.

B18_HINTS = B17_HINTS.replace(
    '"maria.allergies.peanuts" beside "maria.allergies.shellfish"',
    '"maria.passports.brazilian" beside "maria.passports.irish"',
)


class KeyUpsertUnseenExample(KeyUpsertQuestionKey):
    """B18: B17 with the narrowing example moved off the probe set.

    Reads two ways, both of them results. Coexist near 1.00 means the structure
    generalises the plural split and B14's 1.00 is sound, which makes B17/B18's
    0.87 latest-wins the leader. Coexist collapsing means B14's 1.00 was an
    artifact of an example that happened to name the largest coexist family,
    and a key-addressed store needs a per-family signal it does not have.
    """
    NAME = "B18-key-upsert-unseen-example"
    BLURB = "B17, with the narrowing example naming a family the probes never see"

    def _writes(self, text):
        near = self.store.rank(text, RECALL_K, floor=CANDIDATE_FLOOR)
        pairs = {r.key: r.content for r in near if r.key}
        kw = {"model": self.model} if self.model else {}
        out = decider(KEY_SYS_QUESTION + B18_HINTS,
                      f"Latest message: {text}\nExisting keys: {pairs}", **kw)
        self.calls += 1
        return parse_json(out).get("writes", []) or []
