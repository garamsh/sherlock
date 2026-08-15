"""Follow-up structures for the decider-neighbours branch.

Kept out of `structures.py` so the comparison set stays as it was run.
"""
from __future__ import annotations

from api import decider, parse_json
from structures import DeciderNeighborsWide

# ---------------------------------------------------------------------------
# B16 — per-candidate verdict
# ---------------------------------------------------------------------------
#
# B9's residual, replayed (`b9_diagnosis.py`, seed 7), is one mistake wearing
# three costumes. All 18 corrections that left the old value live, and all 4
# coexisting facts that were destroyed, are a misjudged relation between the
# incoming message and one offered row:
#
#   14  the contradicted row was offered and the reply's retire list was
#       empty. Six of the fourteen sat at rank 1 or 2 in the offer, so the
#       decider was not failing to see the row; it was declining to act on it.
#       "Kofi adopted a turtle recently." leaves "Kofi keeps a rabbit." alone,
#       because nothing in the message logically falsifies it.
#    2  a different row was retired instead: "Nina now reports into Diane."
#       retired *Ida's* manager, and "Bruno moved on to jazz." retired Bruno's
#       shell. Same value, different person; same person, different topic.
#    4  a coexisting row was retired: "Lisa plays the cello." killed "Lisa
#       plays the drums.", and the same for Otto, Maria and Lisa's Korean.
#    2  the contradicted row was outside the offer (rank 14 and 15).
#
# The first three are the same two questions asked badly: is this row about
# the same person, and does it answer the same question as the message? A
# free retire list never forces either question to be asked. So this variant
# changes what the decider returns: a verdict for every id it was shown, from
# a vocabulary that names those two questions and the multi-valued case that
# the coexist losses fall into. Nothing is retired unless a verdict says so,
# so an omitted or malformed id still means "leave it alone" — the property
# B10 gave up and lost on. The offer is B9's, unchanged, so the only variable
# is the reply shape.

VERDICT_SYS = (
    "You are a memory decider for a long-lived assistant. You are given the "
    "latest message and a list of stored memories with ids, retrieved by "
    "similarity. They are not all about the same person or the same topic.\n"
    'Reply with JSON: {"store": ["<fact to store>", ...], "verdicts": '
    '[{"id": <id>, "verdict": "unrelated" | "still true" | "made false"}, ...]}\n'
    "Give a verdict for every memory in the list, using each id exactly once:\n"
    "  unrelated  - it is about a different person, or about a different "
    "topic than the latest message. A memory about someone else is never "
    "made false by this message, even when it mentions the same thing.\n"
    "  still true - it is about the same person and the same topic, and both "
    "it and the latest message can be true at the same time: a second "
    "language, a second instrument, a second allergy.\n"
    "  made false - the latest message replaces it. The same person's answer "
    "to the same question has changed, so the stored answer is now wrong. A "
    "person has one morning drink, one editor, one manager, one shell, so a "
    "message giving a new one replaces the stored one.\n"
    "Then store durable facts about people, preferences and systems. Store "
    "nothing for small talk."
)


class DeciderNeighborsVerdicts(DeciderNeighborsWide):
    """B16: B9's offer, but the decider must judge every candidate it is shown.

    Same candidates, same store instruction. The retire list is replaced by a
    verdict per id, so a memory the message contradicts cannot be passed over
    in silence, and a memory retired for the wrong reason has to be labelled
    with a reason that does not fit it.
    """
    NAME = "B16-decider-verdicts"
    BLURB = "B9's 10 candidates; a verdict per candidate instead of a retire list"

    def turn(self, text, is_fact):
        self.store.tick()
        rows = self.context_rows(text)
        listing = [{"id": r.id, "memory": r.content} for r in rows]
        kw = {"model": self.model} if self.model else {}
        out = decider(VERDICT_SYS,
                      f"Latest message: {text}\nMemories: {listing}", **kw)
        self.calls += 1
        ops = parse_json(out)
        for v in ops.get("verdicts", []) or []:
            if not isinstance(v, dict) or v.get("verdict") != "made false":
                continue
            for r in rows:
                if r.id == v.get("id") and not r.dead:
                    r.dead = True
        for fact in ops.get("store", []) or []:
            if isinstance(fact, str) and fact.strip():
                self.store.add(fact.strip())
