"""Build the #890 validation corpus: #885's chain families from a new seed,
with #874's templates and rules, and the families the memory-encoding
survey's test plan adds (../README.md §Test plan).

    python3 docs/research/memory-encoding/validation/corpus.py assemble   # -> candidates.json and corpus.json
    python3 docs/research/memory-encoding/validation/corpus.py select     # candidates + check -> corpus.json

`assemble` writes every candidate into one stream after the base, so the
stream, and every summary any arm writes of it, is fixed before anything is
selected. `select` keeps, in candidate order, the first candidates of each
checked family that the check over the shared batch-4 store passes
(evidence/selection/check-b4.json): no later chain hop, no SC evidence and
no UP fact within the raw query's top 20. Both steps are deterministic:
running either again writes byte-identical files.

Families (the survey's §Test plan):
- C1, C2, C3, IC2, IC3 and N as #885 built them (../../chained-recall/confirm/corpus.py).
- SC, situated: an introduction exchange names a subject and the exchange
  right after it states the subject's value without naming it. The query
  names the subject and asks for the value.
- NC, situated absent: the same introduction, then an exchange stating a
  value of the asked kind for someone else, unnamed. The subject's value is
  stated nowhere.
- UP, a user standing fact: one exchange states a fact about the user's
  life with an invented name in it; a later task needs the name without
  naming what it belongs to.
- KU, a changed user fact: the user names who holds a role for them, and
  later says it has changed. The query needs the newer name.
- E and K, the replication's exact and correction queries, as #885 drew them.

Every exchange is four messages and fills one batch of four alone. Two
exchanges of one item sit at least MIN_GAP exchanges apart, in random order,
except that a KU item's change follows its first statement, and an SC or NC
item's two exchanges are adjacent, introduction first, so the introduction
lies within the eight messages before the evidence batch.
"""

import hashlib
import importlib.util
import json
import random
import re
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
CHAINED = HERE.parent.parent / "chained-recall"


def _module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


gen = _module("chained_recall_corpus", CHAINED / "corpus.py")
confirm = _module("chained_recall_confirm_corpus", CHAINED / "confirm" / "corpus.py")

SEED = 890
# Checked families: candidates drawn, and how many select keeps.
POOL = {"C1": 30, "C2": 70, "C3": 70, "IC2": 30, "IC3": 30, "SC": 70}
SIZES = {"C1": 30, "C2": 50, "C3": 50, "IC2": 25, "IC3": 25, "SC": 50, "UP": 50}
# Unchecked families, drawn at their size.
EXACT = {"N": 60, "NC": 30, "KU": 30}
# The replication guards, at #885's sizes (confirm.GUARDS).
REPLICATION = {"E": 30, "K": 40}
CHECK_TOP = 20
# The live window, as #874 and #885.
WINDOW = 32
ABSTAIN = confirm.ABSTAIN

# Per domain of the chain templates (gen.DOMAINS, by name): the kind of
# person or firm the subject is, an introduction that names it, the
# evidence that states its value without naming it, and, for NC, a value
# of the same kind that belongs to someone else. The query is the domain's
# C1 query with the subject as its entity.
SITUATED = {
    "procurement": {
        "s": "{f} {w}",
        "intro": "I finally got hold of {e} today.", "intro_ack": "Good, you reached {e}.",
        "evidence": "The best line for them is extension {v}.", "ack": "Got it, extension {v} is the best line.",
        "other": "Their predecessor still answers on extension {v}.", "other_ack": "Noted, the predecessor is on extension {v}."},
    "travel": {
        "s": "{w} Coaches",
        "intro": "We booked {e} for the airport transfer.", "intro_ack": "Noted, {e} for the transfer.",
        "evidence": "They always pick up from {v}.", "ack": "Got it, pick-up is from {v}.",
        "other": "The coach firm we used last year picked up from {v}.", "other_ack": "Noted, last year's firm used {v}."},
    "lab": {
        "s": "{f} {w}",
        "intro": "I was introduced to {e} this morning.", "intro_ack": "Nice, you met {e}.",
        "evidence": "Their office is {v}.", "ack": "Got it, the office is {v}.",
        "other": "Their supervisor's office is {v}.", "other_ack": "Noted, the supervisor sits in {v}."},
    "events": {
        "s": "{w} Catering",
        "intro": "We picked {e} for the dinner.", "intro_ack": "Noted, {e} for the dinner.",
        "evidence": "The booking with them is under reference {v}.", "ack": "Got it, the booking reference is {v}.",
        "other": "The caterer we dropped had us under reference {v}.", "other_ack": "Noted, the dropped caterer used {v}."},
    "software": {
        "s": "{f} {w}",
        "intro": "{e} joined the project this week.", "intro_ack": "Good to know, {e} is on the project.",
        "evidence": "You can find them at {v}.", "ack": "Got it, they are at {v}.",
        "other": "Their manager sits at {v}.", "other_ack": "Noted, the manager is at {v}."},
    "household": {
        "s": "{f} {w}",
        "intro": "I left the van with {e} today.", "intro_ack": "Noted, the van is with {e}.",
        "evidence": "They leave finished jobs in {v}.", "ack": "Got it, finished jobs go in {v}.",
        "other": "The garage next door leaves finished jobs in {v}.", "other_ack": "Noted, the garage next door uses {v}."},
}

# UP: facts about the user's life, each a slot of its own, so no two
# candidates state conflicting facts. The task needs the invented name {W}
# without naming the thing it belongs to.
STANDING = [
    ("Everyone in the family calls me {W}.", "Write a note from me to my aunt thanking her for the jumper."),
    ("I close every letter with the words '{W} greetings'.", "Write a short note from me thanking my landlord for fixing the heater."),
    ("My best friend is {W}.", "Write an invite from me to my oldest mate for a pint on Friday."),
    ("I restore clocks in a workshop called {W} Time.", "Write a line for the craft fair list about my stall."),
    ("My home office is a shed I call {W}.", "Tell the courier in one line where to leave parcels when I'm working."),
    ("My house has a name, {W} Cottage.", "Write the return address line for the parcel I'm sending."),
    ("My language exchange partner is {W}.", "Write a message to my Spanish practice buddy moving our call to Thursday."),
    ("I sail a small boat named {W}.", "Write a two-line note inviting my cousin out on the water with me on Sunday."),
    ("My lucky charm is a coin from {W}.", "Write two lines for my speech about what I carry to every exam."),
    ("My favourite colour is a shade I call {W} blue.", "Tell the decorator in one line what I want the hallway to look like."),
    ("My gym is {W} Fitness.", "Write a note asking to freeze my membership while I'm abroad."),
    ("I work as an engineer at {W} Instruments.", "Write a one-line introduction of me for the conference programme."),
    ("I am a member of the {W} chess circle.", "Write the line about me for the tournament entry list."),
    ("My signature dish is {W} pie.", "Suggest in one line what I should bring to the potluck."),
    ("My grandmother's stew is called {W} pot, and I cook it for every guest.", "Plan the main course for the dinner I'm hosting on Saturday, in two lines."),
    ("My retirement plan is a cottage in {W}.", "Write two lines to my sister about where I hope to end up."),
    ("My theatre company is the {W} Players.", "Write the one-line credit for me in the festival brochure."),
    ("My wedding was at {W} Hall.", "Write two lines for our anniversary party invite about where it all started."),
    ("My neighbour {W} feeds the cat whenever I'm away.", "I'm off to Lisbon for a week; who should I leave my spare key with? One line."),
    ("I buy all my bread from a bakery called {W}.", "Suggest in one line where I should pick up the rolls for Sunday's picnic."),
    ("My old university roommate {W} lives in Porto now.", "Plan one line for my Portugal trip: who could I stay with?"),
    ("My laptop is a {W} model from 2019.", "Tell the repair shop in one line what they'll be fixing."),
    ("The pub quiz team I captain is called {W}.", "Write a message to my friends about Thursday night at the Red Lion."),
    ("My allotment neighbour {W} lends me tools.", "Who can I borrow a ladder from this weekend? One line."),
    ("I keep my savings at {W} Building Society.", "Tell my sister in one line where I'll pay the deposit from."),
    ("My little one goes to {W} Academy.", "Tell the childminder in one line where to collect my kid on Friday."),
    ("My favourite band is {W}.", "Suggest in one line what I should play at my birthday party."),
    ("My grandfather's watch is engraved with the word {W}.", "Write two lines for my wedding speech about the heirloom I'll wear."),
    ("My usual seat at the stadium is in the {W} Stand.", "Tell my cousin in one line where to find me at Saturday's game."),
    ("I holiday every August in a village called {W}.", "Write an out-of-office line for my email this summer."),
    ("My book club meets at {W} Library.", "Write a two-line reminder to the group about next Tuesday's discussion."),
    ("My late father served in the {W} Rifles.", "Write two lines for the Remembrance Day notice about my family."),
    ("On the radio I go by DJ {W}.", "Write the one-line announcement for the charity night I'm hosting."),
    ("The charity I give to every month is the {W} Trust.", "Write one line for my letter of wishes about where donations should go."),
    ("My favourite walking route goes past {W} Tor.", "Suggest in one line where we should go on Sunday afternoon."),
    ("My kids' swimming lessons are at {W} Leisure Centre.", "Tell my partner in one line where to drop the children on Saturday morning."),
    ("I was born at {W} General.", "Write two lines for my birthday speech about where it all began."),
    ("My great-aunt's farm was called {W} Farm.", "Write the line for the family tree booklet about where the cousins spent summers."),
    ("I keep my canoe at the {W} boathouse.", "Tell my friend in one line where to meet me on Saturday morning."),
    ("The newsletter I edit is called the {W} Bulletin.", "Write the one-line footer for this month's issue."),
    ("My flat is on the fourth floor of {W} House.", "Tell the delivery driver in one line where to bring the sofa."),
    ("My weekly five-a-side team is {W} United.", "Write a two-line message to the lads about Wednesday's kick-off."),
    ("The tea I drink every morning is a {W} blend.", "Tell my houseguest in one line what to buy me from the shop."),
    ("My old school was {W} Grammar.", "Write two lines for the alumni page about where I studied as a teenager."),
    ("My favourite restaurant is {W} Kitchen.", "Suggest in one line where to take my parents for their anniversary."),
    ("I rent a studio at the {W} Arts Centre.", "Tell the gallery curator in one line where to collect my paintings."),
    ("My mentor at work is {W}.", "Who should I ask to review my promotion case? One line."),
    ("My sister's husband is {W}.", "Write a thank-you note from me to my brother-in-law for the lift."),
    ("My local pub is the {W} Arms.", "Suggest in one line where to hold my leaving drinks."),
    ("My favourite film is {W}.", "Suggest in one line what we should watch on Friday night."),
    ("The software I keep my accounts in is {W} Books.", "Tell my bookkeeper in one line which program to export from."),
    ("My first job was at {W} Mills.", "Write two lines for my retirement card about how my career began."),
    ("My favourite beach is {W} Sands.", "Suggest in one line where we should spend the bank holiday."),
    ("The festival I never miss is {W} Fest.", "Tell my boss in one line which weekend in July I need off."),
    ("My loyalty card is for {W} Stores.", "Tell my flatmate in one line where to do the weekly shop."),
    ("My golf club is {W} Links.", "Tell my father-in-law in one line where we're playing on Sunday."),
    ("My parish church is St {W}'s.", "Write one line for the funeral notice about where the service will be."),
    ("My charity shop shift is at the {W} Hospice shop.", "Tell my manager in one line where I am on Wednesday afternoons."),
    ("The ferry I take to the island is the {W} Star.", "Tell my aunt in one line how I'm getting to her place."),
    ("My godson is called {W}.", "Write a two-line card from me for the lad I'm godparent to."),
    ("My tailor-made suit came from {W} and Hart.", "Tell my best man in one line where to get his outfit fitted."),
    ("My sourdough starter is named {W}.", "Write a note to my neighbour on how to keep the jar in the fridge alive while I'm away."),
    ("My late grandmother's ring came from {W} Jewellers.", "Tell my fiancée in one line where the family rings are always resized."),
    ("My favourite hiking boots are {W} Treks.", "Tell my brother in one line what to get me for Christmas."),
    ("My daughter's piano exam board is {W} Music.", "Write a line to the school about which certificate she'll bring in."),
    ("My mortgage is with {W} Bank.", "Tell my partner in one line who we need to call about moving house."),
    ("My family's crest carries the motto {W}.", "Write the inscription line for the gate we're having made."),
    ("My old football coach was {W}.", "Write two lines for my award speech thanking the person who taught me the game."),
    ("I grow {W} tomatoes every summer.", "Suggest in one line what I should enter at the village show."),
    ("My grandad's fishing spot is {W} Pool.", "Tell my son in one line where we'll cast off on Saturday."),
    ("The paint brand I trust is {W} Colours.", "Tell the art shop in one line what to order for my next canvas."),
    ("My favourite poet is {W}.", "Suggest in one line what I should read at my brother's wedding."),
    ("My local dry cleaner is {W} Clean.", "Tell my partner in one line where to pick up my shirts."),
    ("My old boss {W} still writes my references.", "Who should I list as a referee on my new application? One line."),
    ("I volunteer as a guide at {W} Castle.", "Write two lines for my CV about my weekend work."),
    ("My favourite ice cream is {W} ripple.", "Tell my niece in one line what to bring for my birthday pudding."),
    ("My allotment is on {W} Road.", "Tell my friend in one line where to drop off the spare seedlings."),
    ("My cousin {W} runs a guesthouse by the sea.", "Suggest in one line who could put us up for the long weekend."),
    ("My favourite painter is {W}.", "Suggest in one line what print I should hang above the sofa."),
    ("My childhood dog was called {W}.", "Write two lines for my memoir about the pet I grew up with."),
    ("My family's summer tradition is the {W} picnic.", "Write a two-line invite to my cousins for the first Sunday in June."),
    ("My go-to taxi firm is {W} Cars.", "Tell my mother in one line who to ring for a lift home from the station."),
    ("My favourite wine is a {W} red.", "Tell the waiter in one line what I'd like with the steak."),
    ("The builder who did my extension is {W}.", "Who should I ask to quote for the new porch? One line."),
    ("My old flatmate {W} now works as a nurse.", "Who could give me advice about night shifts? One line."),
    ("My favourite garden centre is {W} Nursery.", "Tell my partner in one line where to get compost this weekend."),
    ("My grandmother's maiden name was {W}.", "Write a line for the family tree about my mother's mother."),
    ("My favourite biscuit brand is {W}.", "Tell my houseguest in one line what to put in the tin."),
    ("My singing teacher is {W}.", "Write a note moving Friday's vocal lesson to Monday."),
    ("My old maths teacher {W} inspired me to study engineering.", "Write two lines for my graduation speech about who set me on this path."),
    ("My favourite sports car is the {W} Roadster.", "Tell my brother in one line what I'd love for my fiftieth."),
    ("My great-grandfather emigrated from {W}.", "Write two lines for the heritage project about where my family came from."),
    ("My local farm shop is {W} Farm Shop.", "Tell my neighbour in one line where to get the eggs."),
    ("My favourite musical is {W}.", "Suggest in one line what show to book for my mum's birthday."),
    ("My old army mate {W} lives in Leeds.", "Who could I visit on my trip north next month? One line."),
    ("My hat was made by {W} Hatters.", "Tell my friend in one line where to get a proper hat for the races."),
    ("My favourite radio station is {W} FM.", "Tell the electrician in one line what to tune the kitchen radio to."),
    ("My tennis partner is {W}.", "Write a message to my Thursday doubles partner cancelling this week."),
    ("My childhood holiday caravan was at {W} Bay.", "Write two lines for my memoir about summers as a kid by the sea."),
    ("My favourite cheese is {W} blue.", "Tell the deli in one line what to put on my platter."),
    ("The bridge club I play at is {W} Bridge Club.", "Write a note to my partner at cards about Tuesday's game."),
    ("My mother's hometown is {W}.", "Suggest in one line where to take mum for a nostalgic day out."),
    ("My favourite museum is the {W} Museum.", "Suggest in one line where to take my nephews on a rainy Saturday."),
    ("My dance class is at {W} Studios.", "Tell my partner in one line where to pick me up after salsa."),
    ("My favourite hymn is {W}.", "Suggest in one line what the choir should sing at my wedding."),
    ("My late uncle's bakery was called {W} Bakes.", "Write two lines for the high street history walk about my family's shop."),
]
POOL["UP"] = len(STANDING)

# KU: a role someone holds for the user, and its change.
ROLES = ["dentist", "doctor", "accountant", "lawyer", "plumber", "electrician", "vet", "barber", "hairdresser", "tailor",
         "piano teacher", "personal trainer", "letting agent", "insurance broker", "bank adviser", "florist", "cleaner",
         "babysitter", "dog walker", "driving instructor", "optician", "physiotherapist", "therapist", "gardener",
         "window cleaner", "locksmith", "pharmacist", "massage therapist", "chiropodist", "travel agent"]


class Used(set):
    """The values drawn so far. A value that occurs in the base counts as
    drawn, so gen.unique_value, which every value goes through, draws again
    rather than fail confirm.check_text: #885's seed never met one, this one
    does."""

    def __init__(self, base):
        super().__init__()
        self.base = base

    def __contains__(self, value):
        return super().__contains__(value) or re.search(r"\b" + re.escape(value.lower()) + r"\b", self.base) is not None


def person(rng, names, avoid=None):
    while True:
        first = rng.choice(gen.FIRST)
        if first != avoid:
            return f"{first} {names.word()}", first


def situated_item(rng, names, domain, used, absent):
    spec = SITUATED[domain["name"]]
    if "{f}" in spec["s"]:
        e, _ = person(rng, names)
    else:
        e = spec["s"].format(w=names.word())
    v = gen.unique_value(lambda: domain["value"](rng), used)
    second = ("other", "other_ack") if absent else ("evidence", "ack")
    return {
        "domain": domain["name"], "subject": e, "value": v,
        "query": gen.cap(domain["q"][1].format(s0="", s1="", s2=e, v="")),
        "exchanges": [
            {"statement": spec["intro"].format(e=e), "ack": spec["intro_ack"].format(e=e)},
            {"statement": gen.cap(spec[second[0]].format(v=v)), "ack": gen.cap(spec[second[1]].format(v=v))},
        ],
    }


def situated_ok(item):
    """The subject is named in the introduction and the query and not in
    the second exchange; the value is in the second exchange only."""
    query = gen.marked(item["query"])
    intro, second = (gen.marked(x["statement"] + " " + x["ack"]) for x in item["exchanges"])
    s, v = gen.marked(item["subject"]), gen.marked(item["value"])
    kind = {"coaches", "catering"}
    return bool(s - kind) and s <= query and s <= intro and not ((s - kind) & second) \
        and v <= second and not (v & (query | intro))


# A standing fact comes up as an aside after an unrelated opening, as such
# facts do in conversation; a fact stated alone sits too close to its task
# for the check to keep it (README.md §The corpus).
OPENINGS = [
    "It rained all morning, so I stayed in and sorted out the loft.",
    "The train was late again today and I missed my connection.",
    "I finally finished the tax forms last night.",
    "We had the windows cleaned and the whole house feels brighter.",
    "My knee has been playing up since the weekend.",
    "The new kettle turned out to be far too loud.",
    "I spent the afternoon untangling the garden hose.",
    "The internet went down twice during my call this morning.",
    "I've been trying to cut down on coffee this week.",
    "The heating finally works properly after the repair.",
    "I tried a new route to work and it was slower.",
    "We cleared out the garage and found boxes of old photos.",
    "I've started going to bed earlier and it helps.",
    "The neighbours' building work woke me at seven.",
    "I burned the toast twice this morning.",
    "My phone screen cracked when I dropped it on the stairs.",
    "The parcel I was waiting for turned up a week late.",
    "I spent ages on hold with the energy company today.",
    "The weather forecast was wrong all week.",
    "I finally repainted the back door.",
]


def aside(fact):
    return fact if fact.startswith("I ") else fact[0].lower() + fact[1:]


def standing_item(rng, names, slot):
    fact, task = STANDING[slot]
    w = names.word()
    return {"value": w, "query": task, "statement": f"{rng.choice(OPENINGS)} Oh, and {aside(fact.format(W=w))}",
            "ack": f"Noted, I'll remember {w}."}


def changed_item(rng, names, role):
    old, first = person(rng, names)
    new, _ = person(rng, names, avoid=first)
    return {
        "role": role, "old": old, "new": new,
        "query": f"I need to see my {role} next week. Who should I contact?",
        "exchanges": [
            {"statement": f"My {role} is {old}.", "ack": f"Noted, {old} is your {role}."},
            {"statement": f"I've changed {role}: from now on it's {new}, not {old}.", "ack": f"Understood, {new} is your {role} now."},
        ],
    }


def pool(base):
    rng = random.Random(SEED)
    names = gen.Names(rng, base)
    used = Used(base)
    items = []
    for family, size in POOL.items():
        for i in range(size):
            iid = f"{family}-{i + 1:03d}"
            if family.startswith("IC"):
                c = gen.implicit_chain(rng, names, int(family[-1]), used)
            elif family.startswith("C"):
                c = gen.explicit_chain(rng, names, gen.DOMAINS[i % len(gen.DOMAINS)], int(family[-1]), used)
            elif family == "SC":
                s = situated_item(rng, names, gen.DOMAINS[i % len(gen.DOMAINS)], used, absent=False)
                if not situated_ok(s):
                    raise SystemExit(f"{iid} breaks the situated rule: {s}")
                intro, ev = s["exchanges"]
                c = {"k": 1, "domain": s["domain"], "start": s["subject"], "query": s["query"], "answer": s["value"],
                     "subject": s["subject"], "intro": intro,
                     "hops": [{"statement": ev["statement"], "ack": ev["ack"], "ends": [s["subject"], s["value"]],
                               "key": s["subject"] + " " + s["value"]}],
                     "check": {"kind": "contains", "value": s["value"]}}
            else:  # UP
                u = standing_item(rng, names, i)
                c = {"k": 1, "domain": "standing", "start": "", "query": u["query"], "answer": u["value"],
                     "hops": [{"statement": u["statement"], "ack": u["ack"], "ends": [u["value"]], "key": u["value"]}],
                     "check": {"kind": "contains", "value": u["value"]}}
            c = {"id": iid, "family": family, **c}
            if family in ("C1", "C2", "C3", "IC2", "IC3") and not gen.lexical_ok(c):
                raise SystemExit(f"{iid} breaks the lexical rule: {c}")
            confirm.check_text(iid, [h["statement"] for h in c["hops"]], c["answer"], base)
            for h in c["hops"]:
                h["exchange"] = gen.exchange(rng, h["statement"], h["ack"])
            if family == "SC":
                c["intro"]["exchange"] = gen.exchange(rng, c["intro"]["statement"], c["intro"]["ack"])
            items.append(c)
    guards = []
    for i in range(EXACT["N"]):
        a = {"id": f"N-{i + 1:03d}", "family": "N", **confirm.absent_item(rng, names, gen.DOMAINS[i % len(gen.DOMAINS)], used)}
        confirm.check_text(a["id"], [x["statement"] for x in a["exchanges"]], a["near_miss"], base)
        if not confirm.absent_ok(a):
            raise SystemExit(f"{a['id']} breaks the absent rule: {a}")
        guards.append(a)
    for i in range(EXACT["NC"]):
        s = situated_item(rng, names, gen.DOMAINS[i % len(gen.DOMAINS)], used, absent=True)
        if not situated_ok(s):
            raise SystemExit(f"NC-{i + 1:03d} breaks the situated rule: {s}")
        guards.append({"id": f"NC-{i + 1:03d}", "family": "NC", "domain": s["domain"], "entity": s["subject"],
                       "near_miss": s["value"], "query": s["query"], "exchanges": s["exchanges"]})
    for i, role in enumerate(ROLES[:EXACT["KU"]]):
        k = changed_item(rng, names, role)
        confirm.check_text(f"KU-{i + 1:03d}", [x["statement"] for x in k["exchanges"]], k["new"], base)
        guards.append({"id": f"KU-{i + 1:03d}", "family": "KU", **k})
    for g in guards:
        for x in g["exchanges"]:
            x["exchange"] = gen.exchange(rng, x["statement"], x["ack"])
    return items, guards


def units(items, guards):
    """Schedule units: (item, part, exchanges, ordered). An SC or NC item is
    one unit of two adjacent exchanges; a KU item's parts are ordered."""
    out = []
    for c in items:
        if c["family"] == "SC":
            out.append((c["id"], 0, [c["intro"]["exchange"], c["hops"][0]["exchange"]], False))
        else:
            out += [(c["id"], i, [h["exchange"]], False) for i, h in enumerate(c["hops"])]
    for g in guards:
        if g["family"] == "NC":
            out.append((g["id"], 0, [x["exchange"] for x in g["exchanges"]], False))
        else:
            out += [(g["id"], i, [x["exchange"]], g["family"] == "KU") for i, x in enumerate(g["exchanges"])]
    return out


def schedule(rng, parts):
    """Order units so that two of one item start at least MIN_GAP exchanges
    apart and, unless ordered, come in no fixed order. As confirm.schedule,
    an eligible unit of the item with the most units left goes first, the
    shuffled order breaking ties."""
    remaining = parts[:]
    rng.shuffle(remaining)
    left, placed_parts = {}, {}
    for item, *_ in remaining:
        left[item] = left.get(item, 0) + 1
    last, out, position = {}, [], 0
    while remaining:
        eligible = [j for j, (item, part, _, ordered) in enumerate(remaining)
                    if position - last.get(item, -gen.MIN_GAP) >= gen.MIN_GAP and (not ordered or part == placed_parts.get(item, 0))]
        if not eligible:
            raise SystemExit("no unit can be placed; the gap rule cannot hold")
        j = max(eligible, key=lambda j: (left[remaining[j][0]], -j))
        item, part, exchanges, _ = remaining.pop(j)
        out.append((item, part, exchanges))
        last[item] = position + len(exchanges) - 1
        position += len(exchanges)
        left[item] -= 1
        placed_parts[item] = placed_parts.get(item, 0) + 1
    return out


def assemble():
    base = gen.base_text()
    items, guards = pool(base)
    rng = random.Random(SEED + 1)
    slots = schedule(rng, units(items, guards))
    by_id = {x["id"]: x for x in items + guards}
    stream, next_id = [], gen.BASE_MESSAGES + 1
    for item_id, part, exchanges in slots:
        x = by_id[item_id]
        if x["family"] == "SC":
            targets = [(x["intro"], 0), (x["hops"][0], 1)]
        elif "hops" in x:
            targets = [(x["hops"][part], part + 1)]
        elif x["family"] == "NC":
            targets = [(e, i + 1) for i, e in enumerate(x["exchanges"])]
        else:
            targets = [(x["exchanges"][part], part + 1)]
        # "hop" numbers an item's exchanges from 1; an SC introduction is 0.
        for (target, hop), exchange in zip(targets, exchanges):
            target["source"] = next_id
            for m in exchange:
                stream.append({"id": next_id, "role": m["role"], "content": m["content"], "chain": item_id, "hop": hop})
                next_id += 1
    used = {m["chat"] for m in json.load(open(gen.REPLICATION / "corpus.json"))}
    chats = sorted((c for c in gen.load_jsonl(gen.REPLICATION / "conversations.jsonl")
                    if c["id"].startswith("filler") and c["id"] not in used and "messages" in c), key=lambda c: c["id"])
    need = gen.TAIL_MIN + (-(len(stream) + gen.TAIL_MIN)) % gen.ALIGN
    tail = [m for c in chats for m in c["messages"]][:need]
    if len(tail) < need or tail[0]["role"] != "user":
        raise SystemExit("not enough tail chat")
    for m in tail:
        stream.append({"id": next_id, "role": m["role"], "content": m["content"], "chain": "tail", "hop": 0})
        next_id += 1
    for c in items:
        for h in c["hops"]:
            del h["exchange"]
        if "intro" in c:
            del c["intro"]["exchange"]
    out_guards = confirm.replication_guards(rng)
    for g in guards:
        for x in g["exchanges"]:
            del x["exchange"]
        sources = [x["source"] for x in g["exchanges"]]
        if g["family"] == "N":
            out_guards.append({"id": g["id"], "family": "N", "query": g["query"], "evidence": [], "entity": g["entity"],
                               "associated": g["associated"], "near_miss": g["near_miss"], "domain": g["domain"],
                               "sources": sources, "exchanges": g["exchanges"],
                               "check": {"kind": "abstain", "pattern": ABSTAIN, "near_miss": g["near_miss"]}})
        elif g["family"] == "NC":
            out_guards.append({"id": g["id"], "family": "NC", "query": g["query"], "evidence": [], "entity": g["entity"],
                               "near_miss": g["near_miss"], "domain": g["domain"], "sources": sources, "exchanges": g["exchanges"],
                               "check": {"kind": "abstain", "pattern": ABSTAIN, "near_miss": g["near_miss"]}})
        else:
            out_guards.append({"id": g["id"], "family": "KU", "query": g["query"], "evidence": [sources[1]], "key": g["new"],
                               "obsolete": g["old"], "role": g["role"], "sources": sources, "exchanges": g["exchanges"],
                               "check": {"kind": "correction", "new": g["new"], "old": g["old"]}})
    out = {"base_messages": gen.BASE_MESSAGES, "window": WINDOW, "stage": "candidates", "chains": items, "guards": out_guards, "stream": stream}
    text = json.dumps(out, indent=1, ensure_ascii=False) + "\n"
    (HERE / "candidates.json").write_text(text)
    (HERE / "corpus.json").write_text(text)
    print(json.dumps({"candidates": {f: sum(c["family"] == f for c in items) for f in POOL},
                      "guards": {f: sum(g["family"] == f for g in out_guards) for f in ("E", "K", "N", "NC", "KU")},
                      "stream_messages": len(stream), "tail": len(tail)}))


def passes(check_row, family):
    """A candidate passes the check when none of the sources the family
    needs reach the raw query's top: every hop but the first of a chain, and
    the one hop of SC (the evidence) and UP (the fact)."""
    raw = check_row["raw"]
    needed = raw[1:] if family in ("C1", "C2", "C3", "IC2", "IC3") else raw
    return all(r > CHECK_TOP for r in needed)


def select():
    candidates_text = (HERE / "candidates.json").read_text()
    candidates = json.loads(candidates_text)
    check = json.load(open(HERE / "evidence" / "selection" / "check-b4.json"))
    if check["summary"]["top"] != CHECK_TOP:
        raise SystemExit("the check ran at another depth")
    rows = {r["id"]: r for r in check["chains"]}
    if set(rows) != {c["id"] for c in candidates["chains"]}:
        raise SystemExit("the check did not run over the candidates")
    chains, passed_counts = [], {}
    for family, size in SIZES.items():
        passed = [c for c in candidates["chains"] if c["family"] == family and passes(rows[c["id"]], family)]
        passed_counts[family] = len(passed)
        if len(passed) < size:
            raise SystemExit(f"{family}: only {len(passed)} of {size} candidates pass the check")
        chains += passed[:size]
    out = {**candidates, "stage": "selected", "candidates_sha256": hashlib.sha256(candidates_text.encode()).hexdigest(), "chains": chains}
    (HERE / "corpus.json").write_text(json.dumps(out, indent=1, ensure_ascii=False) + "\n")
    print(json.dumps({"selected": {f: sum(c["family"] == f for c in chains) for f in SIZES}, "passing": passed_counts}))


if __name__ == "__main__":
    {"assemble": assemble, "select": select}[sys.argv[1]]()
