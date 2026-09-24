"""Turn stream and probes with a coexist set that is redrawn per seed."""
from __future__ import annotations
import random

from round4 import dataset as r4

PERSONS = r4.PERSONS
CATEGORIES = r4.CATEGORIES
CHATTER = r4.CHATTER

# Simultaneously-true facts: (family, probe question, value pool, values drawn
# per subject, statement template). Every subject carries every family and every
# family draws a fixed count, so a family's share of the coexist column does not
# move with the seed. No family names or answers a CATEGORIES topic.
COEXIST = [
    ("allergy", "What is {p} allergic to?",
     ["peanuts", "cashews", "pollen", "penicillin", "latex"], 3,
     "{p} is allergic to {v}."),
    ("instrument", "Which instruments does {p} play?",
     ["the cello", "the drums", "the trumpet", "the banjo", "the flute"], 3,
     "{p} plays {v}."),
    ("certification", "Which certifications does {p} hold?",
     ["a lifeguard certificate", "a pilot licence", "a scuba licence",
      "a first-aid certificate", "a radio operator licence"], 2,
     "{p} holds {v}."),
    ("committee", "Which committees does {p} sit on?",
     ["the hiring panel", "the safety board", "the accessibility guild",
      "the records working group", "the ethics council"], 2,
     "{p} sits on {v}."),
    ("cuisine", "Which cuisines does {p} cook?",
     ["Thai food", "Ethiopian food", "Sicilian food", "Georgian food",
      "Peruvian food"], 2,
     "{p} cooks {v}."),
    ("craft", "Which crafts does {p} practise?",
     ["woodworking", "pottery", "bookbinding", "weaving", "glassblowing"], 2,
     "{p} practises {v}."),
    ("garden", "Which vegetables does {p} grow?",
     ["tomatoes", "chillies", "garlic", "courgettes", "rhubarb"], 2,
     "{p} grows {v}."),
    ("volunteer", "Where does {p} volunteer?",
     ["the food bank", "the night shelter", "the river cleanup",
      "the youth centre", "the crisis line"], 2,
     "{p} volunteers at {v}."),
]


class CoexistProbe(r4.Probe):
    """A coexist probe carrying the family its values were drawn from."""
    __slots__ = ("family",)

    def __init__(self, family, subject, question, values):
        super().__init__("coexist", subject, question, values)
        self.family = family


def build(seed: int = 7, n_corrected: int = 100, n_control: int = 40,
          n_distractor: int = 120, n_coexist_subjects: int = 8,
          chatter_ratio: float = 0.35):
    """Return (turns, probes).

    Stream order is round 4's: every initial statement and distractor first,
    then chatter, then every correction. The corrected, control and distractor
    draws consume the seeded generator in round 4's order too, so for a given
    seed those assignments are round 4's and the coexist set is the only
    difference between the two datasets.
    """
    rnd = random.Random(seed)
    pairs = [(p, c) for p in PERSONS for c in CATEGORIES]
    rnd.shuffle(pairs)

    corrected = pairs[:n_corrected]
    control = pairs[n_corrected:n_corrected + n_control]
    distract = pairs[n_corrected + n_control:][:n_distractor]

    initial_turns, correction_turns, probes = [], [], []

    for person, cat in corrected:
        name, question, values, inits, corrs = cat
        old, new = rnd.sample(values, 2)
        initial_turns.append((rnd.choice(inits).format(p=person, v=old), True))
        correction = rnd.choice(corrs).format(p=person, v=new)
        correction_turns.append((correction, True))
        probes.append(r4.Probe("corrected", person, question.format(p=person),
                               [new], [old], correction_text=correction))

    for person, cat in control:
        name, question, values, inits, corrs = cat
        val = rnd.choice(values)
        initial_turns.append((rnd.choice(inits).format(p=person, v=val), True))
        probes.append(r4.Probe("control", person, question.format(p=person), [val]))

    for person, cat in distract:
        name, question, values, inits, corrs = cat
        initial_turns.append(
            (rnd.choice(inits).format(p=person, v=rnd.choice(values)), True))

    for person in rnd.sample(PERSONS, n_coexist_subjects):
        for family, question, pool, n_values, tmpl in COEXIST:
            values = rnd.sample(pool, n_values)
            for v in values:
                initial_turns.append((tmpl.format(p=person, v=v), True))
            probes.append(CoexistProbe(family, person,
                                       question.format(p=person), values))

    rnd.shuffle(initial_turns)
    rnd.shuffle(correction_turns)

    n_chatter = int(len(initial_turns) * chatter_ratio)
    chatter = [(rnd.choice(CHATTER), False) for _ in range(n_chatter)]

    turns = initial_turns + chatter + correction_turns
    return turns, probes
