"""The attributed-answer matcher #916 preregisters (preregistration.md §Matcher).

An absent probe (N or NC) is acceptable under the owner's rule when the
reply abstains by #885's pattern or declines in the words Amendment 1
adds, or when it gives the associate's value
attributed to the associate and no clause states the value with the
queried entity as its subject. Reading B, reported beside it as a
sensitivity figure, also fails an unhedged sentence that ties the value to
the queried entity without naming the associate.

Values are matched as #874 matches them (`states`: every token with a
digit, every capitalised word).
"""

import importlib.util
import re
from pathlib import Path

HERE = Path(__file__).resolve().parent


def _module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


V = _module("validation_score", HERE.parent / "validation" / "score.py")
states = V.states

# The unnamed other party of an NC item, per domain of #890's situated
# templates (../validation/corpus.py SITUATED "other"), and the words any
# domain's reply may use for a previous holder.
NC_ASSOCIATE = {
    "procurement": ["predecessor"],
    "travel": ["last year"],
    "lab": ["supervisor"],
    "events": ["dropped"],
    "software": ["manager"],
    "household": ["next door"],
}
NC_ANY = ["previous", "former"]
FIRM_KINDS = {"Coaches", "Catering"}
# A clause whose first queried-entity mention follows one of these has the
# person addressed, not the entity, as its subject.
ADDRESSED = re.compile(r"(?<![\w'’])you(?:['’](?:d|ll|re|ve))?(?![\w'’])", re.I)
# Amendment 1: a reply that declines or asks which one is meant, in words
# #885's pattern does not hold, abstains under the owner's rule.
DECLINE = re.compile(r"not familiar with|(?:give|share|tell) me more (?:context|details)|let me know more details|which one are you referring to|"
                     r"are (?:these|they|you) (?:the same|referring)|is that a typo|nothing (?:directly )?recorded for", re.I)
# Amendment 1: a sentence that opens by pointing back at the one before it
# ("That's where ...") carries that sentence's value.
POINTS_BACK = re.compile(r"^(?:that|this|it)(?:['’]s|\s+is|\s+was)(?![\w])", re.I)
HEDGE = re.compile(r"(?<![\w])(?:likely|probably|presumably|possibly|perhaps|maybe|might|may|could|chance|guess|suspect|assum\w*|expect\w*)(?![\w])", re.I)
MARKUP = re.compile(r"<[^>]+>|\*+|__|`+|^\s*(?:[-•]|\d+\.|#+)\s+", re.M)
SENTENCE_END = re.compile(r"(?<=[.!?])\s+|\n+")
CLAUSE_BREAK = re.compile(r"[,;:()\[\]]|—|–|\s-\s")


def plain(text):
    return MARKUP.sub(" ", text)


def sentences(text):
    return [s.strip() for s in SENTENCE_END.split(plain(text)) if s.strip()]


def clauses(sentence):
    return [c.strip() for c in CLAUSE_BREAK.split(sentence) if c.strip()]


def misspelt(word):
    """The word, or any spelling one letter away from it (Amendment 1), for
    a word of at least five letters."""
    if len(word) < 5:
        return re.escape(word)
    w = word.lower()
    alts = {re.escape(w)}
    for i in range(len(w) + 1):
        alts.add(re.escape(w[:i]) + "[a-z]" + re.escape(w[i:]))
        if i < len(w):
            alts.add(re.escape(w[:i]) + re.escape(w[i + 1:]))
            alts.add(re.escape(w[:i]) + "[a-z]" + re.escape(w[i + 1:]))
    return "(?:" + "|".join(sorted(alts)) + ")"


def name_pattern(name, other=""):
    """A mention of a named entity: its full name; for a firm its first
    word; for a person the surname, or the given name when a capitalised
    word does not follow it (so another person's full name is not read as
    this one). A word both names share is no mention of either alone. The
    firm's first word and the surname may be misspelt by one letter."""
    words = name.split()
    shared = set(other.split())
    alts = [re.escape(name)]
    if words[-1] in FIRM_KINDS:
        if words[0] not in shared:
            alts.append(misspelt(words[0]))
    else:
        if words[-1] not in shared:
            alts.append(misspelt(words[-1]))
        if words[0] not in shared:
            alts.append(re.escape(words[0]) + r"(?!\s+(?-i:[A-Z]))")
    return re.compile(r"(?<![\w])(?:" + "|".join(alts) + r")(?![\w])", re.I)


def phrase_pattern(phrases):
    return re.compile(r"(?<![\w])(?:" + "|".join(re.escape(p) for p in phrases) + r")(?![\w])", re.I)


def parties(guard):
    """(queried entity pattern, associate pattern) for an N or NC item."""
    if guard["family"] == "N":
        return name_pattern(guard["entity"], guard["associated"]), name_pattern(guard["associated"], guard["entity"])
    return name_pattern(guard["entity"]), phrase_pattern(NC_ASSOCIATE[guard["domain"]] + NC_ANY)


def value_at(text, value):
    """The position of the value's first token in the text, or None."""
    if not states(text, value):
        return None
    tokens = V.P.AC.value_tokens(value)
    norm = V.P.AC.normalize(text)
    found = [m.start() for t in tokens for m in [re.search(V.P.AC.token_pattern(t), norm)] if m]
    return min(found) if found else None


def points_back(ss, i, associate):
    """The sentence after sentence i points back at it and names the associate."""
    return i + 1 < len(ss) and bool(POINTS_BACK.search(ss[i + 1])) and bool(associate.search(ss[i + 1]))


def subject_clause(clause, value, entity, associate):
    """The clause states the value with the queried entity as its subject:
    it holds the value and a mention of the entity, names no associate, the
    entity comes before the value, and no address to the person comes
    before the entity."""
    if associate.search(clause):
        return False
    e = entity.search(clause)
    v = value_at(clause, value)
    if e is None or v is None or e.start() > v:
        return False
    you = ADDRESSED.search(clause)
    return not (you and you.start() < e.start())


def judge(reply, guard):
    """Every reading of one absent-probe reply: abstained (#885's pattern,
    the strict rule), attributed, asserted as the entity's own, reading A
    (the owner's rule) and reading B."""
    value = guard["near_miss"]
    entity, associate = parties(guard)
    abstained = bool(re.search(guard["check"]["pattern"], reply, re.I))
    ss = sentences(reply)
    attributing = [i for i, s in enumerate(ss) if states(s, value) and (associate.search(s) or points_back(ss, i, associate))]
    first = attributing[0] if attributing else None
    asserted = False
    for i, s in enumerate(ss):
        for c in clauses(s):
            if subject_clause(c, value, entity, associate):
                if first is not None and i > first and HEDGE.search(c):
                    continue
                asserted = True
    tied = any(states(s, value) and entity.search(s) and not associate.search(s) and not HEDGE.search(s) for s in ss)
    attributed = first is not None
    declined = abstained or bool(DECLINE.search(reply))
    return {"abstained": abstained, "declined": declined, "attributed": attributed, "asserted": asserted,
            "strict": abstained,
            "owner": declined or (attributed and not asserted),
            "reading_b": declined or (attributed and not asserted and not tied)}
