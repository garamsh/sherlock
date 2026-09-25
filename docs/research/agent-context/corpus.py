"""Build the #862 episodes from the #856 replication corpus and authored items.

Every episode starts from the base store (the replication's 4,800 messages)
and appends seeded message pairs and live turns. Output is deterministic:
running this again writes byte-identical files.

    python3 docs/research/agent-context/corpus.py          # the stage-2 episodes
    python3 docs/research/agent-context/corpus.py pilot    # the stage-1 pilot, drawn apart
"""

import json
import random
import re
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPLICATION = HERE.parent / "mid-memory-rag" / "evidence" / "replication"
# Each run's seed and size. The pilot draws its own events, values, filler
# order and rule tasks, and takes its absent queries from items the stage-2
# set does not use.
RUNS = {
    "stage2": {"seed": 862, "out": "episodes.json", "F": 70, "G": 70, "I": 70, "K": 70, "E": 30, "N": 30, "R": 10},
    "pilot": {"seed": 8620, "out": "pilot-episodes.json", "F": 8, "G": 8, "I": 8, "K": 8, "E": 3, "N": 3, "R": 1},
}
WINDOW = 32
BLOCK_CHATS = 5  # a filler block is five 8-message chats: 40 messages
DISTRACTORS = 21  # plus the referent: 22 pairs, 11 mids sharing the attribute

NAMES = ["Mara", "Tomas", "Ines", "Kofi", "Anya", "Ravi", "Lena", "Oscar", "Priya", "Jonas",
         "Sofia", "Dmitri", "Aiko", "Bruno", "Chloe", "Emeka", "Freya", "Hugo", "Isla", "Mateo"]
KINDS = ["offsite", "baby shower", "vendor review", "book launch", "retirement party",
         "charity run", "board dinner", "hackathon", "wedding rehearsal", "product demo",
         "garden party", "team retreat", "art show", "fundraiser gala"]
ATTRIBUTES = {
    "budget": lambda r: f"€{r.randrange(12, 190) * 100:,}",
    "headcount": lambda r: f"{r.randrange(14, 240)} people",
    "venue": lambda r: f"the {r.choice(['Arden', 'Calloway', 'Marisol', 'Brightwater', 'Harlow', 'Kestrel', 'Linden', 'Oakmere', 'Pellam', 'Wrenfield'])} {r.choice(['Hall', 'Loft', 'Pavilion', 'Rooms', 'Gallery', 'Barn'])}",
    "caterer": lambda r: f"{r.choice(['Saffron', 'Juniper', 'Copper Pot', 'Blue Fig', 'Hearth', 'Olive Tree', 'Cardamom', 'Little Basil'])} {r.choice(['Kitchen', 'Catering', 'Table', 'Co.'])}",
    "start time": lambda r: f"{r.randrange(8, 20)}:{r.choice(['00', '15', '30', '45'])}",
    "dress code": lambda r: r.choice(["black tie", "smart casual", "cocktail attire", "all white", "business casual", "festive knitwear", "garden formal"]) + f" with {r.choice(['green', 'navy', 'gold', 'rust', 'teal', 'burgundy'])} accents",
    "deadline": lambda r: f"{r.randrange(1, 29)} {r.choice(['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November'])}",
    "room number": lambda r: f"room {r.randrange(101, 990)}",
}

# Each implicit rule: the rule as the person states it once, the distinctive
# phrase that shows the rule reached the context, a checker, and seven tasks it
# applies to.
RULES = [
    {"rule": "whenever you give me a list, number it with Roman numerals (I., II., III.)", "key": "Roman numerals", "check": {"kind": "roman_list"},
     "tasks": ["List three things I should pack for a beach day.", "Give me four ideas for a rainy Sunday.", "What are three quick breakfast options?", "Name three houseplants that are hard to kill.", "List three ways to save on groceries.", "Give me three warm-up exercises before a run.", "List four board games for a family night."]},
    {"rule": "always give temperatures in Celsius only, never Fahrenheit", "key": "Celsius only", "check": {"kind": "celsius"},
     "tasks": ["What oven temperature should I roast vegetables at?", "What temperature should a fridge be set to?", "At what temperature is chicken safely cooked?", "What's a comfortable bedroom temperature for sleeping?", "How hot should bath water be for a toddler?", "What temperature should I bake bread at?", "What water temperature is best for green tea?"]},
    {"rule": "write every date as YYYY-MM-DD", "key": "YYYY-MM-DD", "check": {"kind": "iso_date", "forbid": "(January|February|March|April|May|June|July|August|September|October|November|December)"},
     "tasks": ["What is the date of the first Monday in March 2027?", "When does summer officially start in the northern hemisphere in 2027?", "What date is Christmas Day in 2027, and what weekday is it?", "What is the date one week after 3 April 2027?", "When is New Year's Eve 2027?", "What date is the last day of February 2028?", "What is the date exactly 30 days after 1 June 2027?"]},
    {"rule": "answer me only in lowercase letters, no capitals at all", "key": "lowercase", "check": {"kind": "lowercase"},
     "tasks": ["Give me two tips for sleeping better.", "How do I get a red wine stain out of a shirt?", "What should I bring to a potluck?", "Suggest a name for a grey kitten.", "How long should I boil an egg for a soft yolk?", "What is a good first houseplant?", "Recommend a short stretching routine."]},
    {"rule": "whenever you write code for me, indent it with tabs, never spaces", "key": "indent it with tabs", "check": {"kind": "tab_indent"},
     "tasks": ["Write a Python function that returns the larger of two numbers.", "Write a Python loop that prints the numbers 1 to 5.", "Write a Python function that reverses a string.", "Write a JavaScript function that sums an array.", "Write a Python function that checks whether a number is even.", "Write a Go function that doubles an integer.", "Write a Python function that counts the vowels in a word."]},
    {"rule": "start every reply to me with the word 'Ahoy!'", "key": "Ahoy", "check": {"kind": "starts_with", "text": "ahoy"},
     "tasks": ["What's a quick lunch I can make in ten minutes?", "How do I keep basil alive indoors?", "What should I name my new bicycle?", "How can I stop my glasses fogging up?", "What's a good stretch for a stiff neck?", "Recommend a podcast topic for a long drive.", "How do I remove a stuck jar lid?"]},
    {"rule": "give every amount of money in euros, with the € sign before the number", "key": "€ sign", "check": {"kind": "euro"},
     "tasks": ["Roughly what does a nice dinner for two cost?", "How much should I budget for a weekend city break?", "What does a decent pair of running shoes cost?", "How much is a typical monthly gym membership?", "What should I expect to pay for a haircut?", "How much does a basic espresso machine cost?", "What's a reasonable budget for a child's birthday party?"]},
    {"rule": "put a line starting 'TL;DR:' at the very top of every answer", "key": "TL;DR", "check": {"kind": "starts_with", "text": "tl;dr:"},
     "tasks": ["How do I choose a good watermelon?", "Should I stretch before or after running?", "How often should I water a cactus?", "What's the best way to learn to touch type?", "How do I descale a kettle?", "Is it worth buying a rice cooker?", "How do I keep cut apples from browning?"]},
    {"rule": "whenever you mention a time of day, use the 24-hour clock like 14:30, never am or pm", "key": "24-hour clock", "check": {"kind": "clock24"},
     "tasks": ["Dinner is at 7 in the evening and takes 90 minutes to cook. When should I start?", "My train leaves at half past eight at night. When should I leave home if the walk is 25 minutes?", "I want eight hours of sleep and must wake at 6:30 in the morning. When should I go to bed?", "A meeting starts at 3 in the afternoon and lasts 45 minutes. When does it end?", "The shop opens at 9 in the morning and I arrive two and a half hours later. What time is that?", "If I start a 40-minute bake at 5:50 in the evening, when do I take it out?", "Sunset is at 8:10 in the evening. When should I start a one-hour hike to finish by then?"]},
    {"rule": "sign off every message to me with '— Juniper'", "key": "— Juniper", "check": {"kind": "ends_with", "text": "juniper"},
     "tasks": ["What's a good gift for a coffee lover?", "How do I fold a fitted sheet?", "Suggest a quick weekday pasta.", "What can I do about squeaky door hinges?", "How do I start journaling?", "What's an easy way to clean a microwave?", "Recommend a card game for two people."]},
]

STREETS = ["Alder", "Birch", "Cedar", "Dunmore", "Elmwood", "Fenwick", "Garnet", "Hollis", "Ivywell", "Juniper"]
SURNAMES = ["Okafor", "Lindqvist", "Moreau", "Castell", "Brennan", "Varga", "Haddad", "Sato", "Pereira", "Novak", "Quist", "Rahman"]
CORRECTION_SUBJECTS = [
    ("my gym locker number", lambda r: f"locker {r.randrange(100, 999)}"),
    ("the wifi network name at the cabin", lambda r: f"{r.choice(['Pine', 'Fern', 'Moss', 'Birch', 'Heron'])}{r.choice(['Hollow', 'Ridge', 'Cove', 'Nest'])}-{r.randrange(10, 99)}"),
    ("my dentist appointment", lambda r: f"{r.randrange(1, 29)} {r.choice(['March', 'April', 'May', 'June'])} at {r.randrange(8, 18)}:{r.choice(['00', '15', '30', '45'])}"),
    ("the plumber's name", lambda r: f"{r.choice(NAMES)} {r.choice(SURNAMES)}"),
    ("my flight number to Porto", lambda r: f"TP {r.randrange(1000, 9999)}"),
    ("my new car's colour", lambda r: f"{r.choice(['pearl', 'storm', 'sage', 'copper', 'midnight', 'sand'])} {r.choice(['grey', 'blue', 'green', 'red', 'bronze'])}"),
    ("my daughter's teacher", lambda r: f"Ms. {r.choice(SURNAMES)}"),
    ("the book club's next book", lambda r: f"The {r.choice(['Salt', 'Glass', 'Quiet', 'Paper', 'Winter', 'Amber'])} {r.choice(['Orchard', 'Harbour', 'Lantern', 'Atlas', 'Garden', 'Clock'])}"),
    ("my allotment plot number", lambda r: f"plot {r.randrange(10, 99)}{r.choice('ABCDE')}"),
    ("my running club's meeting point", lambda r: f"the {r.choice(STREETS)} {r.choice(['Bridge', 'Gate', 'Fountain', 'Bandstand'])}"),
    ("my sister's new address", lambda r: f"{r.randrange(2, 98)} {r.choice(STREETS)} Road"),
    ("the vet my cat goes to", lambda r: f"{r.choice(STREETS)} Veterinary {r.choice(['Clinic', 'Practice', 'Centre'])}"),
    ("my hotel in Kyoto", lambda r: f"the {r.choice(['Kamo', 'Gion', 'Sakura', 'Hanami', 'Tsuki'])} {r.choice(['Inn', 'House', 'Lodge'])}"),
    ("my son's football kit size", lambda r: f"size {r.choice(['XS', 'S', 'M', 'L'])}{r.randrange(1, 9)}"),
]

RSVP_EVENTS = ["the spring picnic", "Nora's farewell drinks", "the chess club dinner", "the quiz night", "the allotment open day",
               "the choir social", "the bike club ride", "the book swap", "the pottery evening", "the harbour walk"]
RSVP_NAMES = ["Alba", "Bram", "Cleo", "Dev", "Esme", "Finn", "Gita", "Hal", "Ivo", "Juno", "Kai", "Lior", "Mina", "Nils",
              "Oona", "Pax", "Quinn", "Rhea", "Sami", "Tove", "Uma", "Vik", "Wren", "Xavi", "Yara", "Zeno"]

# Sentences the store's credential redaction could rewrite are left out, so no
# item's evidence is redacted before any arm sees it.
CREDENTIAL = re.compile(r"password|passcode|passphrase|secret|token|api key|\bpin\b|bearer", re.I)

ABSTAIN = r"(don't|do not|didn't|did not|haven't|have not|can't|cannot|couldn't|could not|no record|not (?:sure|find|found|mentioned|aware|in (?:my|the))|never (?:mentioned|told|said)|unknown|no information)"


def load_jsonl(path):
    with open(path) as f:
        return [json.loads(line) for line in f if line.strip()]


def pairs(messages):
    out = []
    for i in range(0, len(messages) - 1, 2):
        u, a = messages[i], messages[i + 1]
        if u["role"] == "user" and a["role"] == "assistant":
            out.append([{"role": "user", "content": u["content"]}, {"role": "assistant", "content": a["content"]}])
    return out


def seed(msgs):
    return {"kind": "seed", "pairs": msgs}


def live(content):
    return {"kind": "live", "content": content}


def build(run):
    rng = random.Random(run["seed"])
    corpus = json.load(open(REPLICATION / "corpus.json"))
    items = json.load(open(REPLICATION / "items.json"))
    queries = json.load(open(REPLICATION / "queries.json"))
    used = {m["chat"] for m in corpus}
    base_text = "\n".join(m["content"] for m in corpus).lower()

    chats = [c for c in load_jsonl(REPLICATION / "conversations.jsonl")
             if c["id"].startswith("filler") and c["id"] not in used and "messages" in c]
    chats.sort(key=lambda c: c["id"])
    long_chats = [c for c in chats if len(c["messages"]) >= 8]
    blocks = []
    for i in range(0, len(long_chats) - BLOCK_CHATS + 1, BLOCK_CHATS):
        block = []
        for c in long_chats[i:i + BLOCK_CHATS]:
            block += pairs(c["messages"][:8])
        blocks.append(block)
    pads = []
    for c in chats:
        start = 8 if len(c["messages"]) >= 8 else 0
        pads += pairs(c["messages"][start:])

    # Steps name filler by index into the shared blocks and pads, so a block
    # seeded at the same alignment is the same synthesis request everywhere.
    def blocks_for(n):
        return [{"kind": "block", "index": i} for i in rng.sample(range(len(blocks)), n)]

    def pad():
        return {"kind": "pad", "index": rng.randrange(len(pads))}

    def pad_pair():
        return pads[rng.randrange(len(pads))]

    def absent_in_base(*keys):
        return all(not re.search(r"\b" + re.escape(k.lower()) + r"\b", base_text) for k in keys)

    episodes = []

    # F and G share the referent-among-distractors shape; only where the
    # referent sits differs: inside the window (F) or beyond it (G).
    events = [f"{n}'s {k}" for n in NAMES for k in KINDS]
    attr_names = sorted(ATTRIBUTES)
    for family in ("F", "G"):
        for i in range(run[family]):
            attr = attr_names[i % len(attr_names)]
            chosen = rng.sample(events, DISTRACTORS + 1)
            values, seen = [], set()
            while len(values) < len(chosen):
                v = ATTRIBUTES[attr](rng)
                if v not in seen:
                    seen.add(v)
                    values.append(v)
            target, value = chosen[0], values[0]
            stated = [[{"role": "user", "content": f"For {e}, the {attr} is {v}. Please keep that in mind."},
                       {"role": "assistant", "content": f"Got it: the {attr} for {e} is {v}."}]
                      for e, v in zip(chosen, values)]
            referent, distractors = stated[0], stated[1:]
            rng.shuffle(distractors)
            cue = [{"role": "user", "content": f"Let's get back to {target}."},
                   {"role": "assistant", "content": f"Sure. What would you like to do next for {target}?"}]
            probe = f"What {attr} did I give you for it?"
            b = blocks_for(2)
            if family == "G":
                mixed = distractors[:]
                mixed.insert(rng.randrange(len(mixed) + 1), referent)
                steps = [b[0], seed(mixed), b[1], seed([cue]), live(probe)]
            else:
                steps = [b[0], seed(distractors + [[{"role": "user", "content": "Thanks, that's all the details for now."}, {"role": "assistant", "content": "You're welcome."}]]),
                         b[1], seed([referent]), pad(), pad(), pad(), seed([cue]), live(probe)]
            episodes.append({"id": f"{family}-{i + 1:02d}", "family": family, "steps": steps,
                             "check": {"kind": "contains", "value": value}, "reach": [value], "oracle": referent[0]["content"]})

    # I: a standing rule stated once in a live turn, pushed beyond the window.
    tasks = [(r, t) for r in range(len(RULES)) for t in range(len(RULES[r]["tasks"]))]
    if run["I"] < len(tasks):
        tasks = sorted(rng.sample(tasks, run["I"]))
    for n, (r, t) in enumerate(tasks):
        rule = RULES[r]
        b = blocks_for(2)
        statement = f"A standing preference for everything you write for me from now on: {rule['rule']}."
        steps = [b[0], live(statement), pad(), b[1], live(rule["tasks"][t])]
        episodes.append({"id": f"I-{n + 1:02d}", "family": "I", "rule": r, "steps": steps, "check": rule["check"], "reach": [rule["key"]], "oracle": statement})

    # K: an original statement and its correction, both live, both pushed out.
    for i in range(run["K"]):
        subject, make = CORRECTION_SUBJECTS[(i + run["seed"]) % len(CORRECTION_SUBJECTS)]
        while True:
            old, new = make(rng), make(rng)
            if old != new and absent_in_base(old, new):
                break
        b = blocks_for(3)
        steps = [b[0], live(f"Just so you know, {subject} is {old}."), pad(), b[1],
                 live(f"Correction: {subject} is actually {new}, not {old}."), pad(), b[2],
                 live(f"What is {subject}?")]
        episodes.append({"id": f"K-{i + 1:02d}", "family": "K", "steps": steps,
                         "check": {"kind": "correction", "new": new, "old": old}, "reach": [new],
                         "oracle": f"Correction: {subject} is actually {new}, not {old}."})

    # E: a stated fact beyond the window, asked for exactly.
    exacts = [e for e in items["exact"] + items["paraphrase"] if e["id"] not in used and e.get("failure") is None
              and not CREDENTIAL.search(e["fact_sentence"]) and absent_in_base(e["key"])]
    rng.shuffle(exacts)
    if len(exacts) < run["E"]:
        raise SystemExit(f"only {len(exacts)} exact items qualify")
    for i, e in enumerate(exacts[:run["E"]]):
        fact = [[{"role": "user", "content": e["fact_sentence"]}, {"role": "assistant", "content": "Thanks for telling me."}], pad_pair()]
        b = blocks_for(2)
        steps = [b[0], seed(fact), b[1], live(e["exact_query"])]
        episodes.append({"id": f"E-{i + 1:02d}", "family": "E", "steps": steps, "item": e["id"],
                         "check": {"kind": "contains", "value": e["key"]}, "reach": [e["key"]], "oracle": e["fact_sentence"]})

    # N: the replication's absent queries; nothing answering them was said.
    absent = [{"item": q["item"], "text": q["text"]} for q in queries if q["family"] == "absent"]
    if run is RUNS["pilot"]:
        taken = {a["item"] for a in absent}
        absent = [{"item": a["id"], "text": a["query"]} for a in items["absent"] if a["id"] not in taken and a.get("failure") is None]
    for i, q in enumerate(absent[:run["N"]]):
        episodes.append({"id": f"N-{i + 1:02d}", "family": "N", "steps": blocks_for(1) + [live(q["text"])], "item": q["item"],
                         "check": {"kind": "abstain", "pattern": ABSTAIN}, "reach": []})

    # R: #810's continuation cases, the last input neutral in every case.
    for i in range(3 * run["R"]):
        case = ("continue", "changed", "canceled")[i // run["R"]]
        event = RSVP_EVENTS[i % 10]
        names = rng.sample(RSVP_NAMES, 4)
        slug = re.sub(r"[^a-z]+", "-", event.lower().removeprefix("the ")).strip("-")
        first, second = f"rsvp-{slug}.txt", f"guests-{slug}.csv"
        start = (f"I'm collecting RSVPs for {event}. I'll send you names as they come in. "
                 f"When I say 'that's everyone', write all the names into a file called {first}, one name per line. "
                 f"First two: {names[0]} and {names[1]}.")
        b = blocks_for(2)
        steps = [b[0], live(start), pad()]
        if case == "changed":
            steps += [live(f"Change of plan for the {event} RSVPs: when I say that's everyone, put the names in {second} instead of {first}, sorted alphabetically."), pad()]
        if case == "canceled":
            steps += [live(f"Scrap the RSVP list for {event}: the event is cancelled, so don't write any file for it."), pad()]
        steps += [b[1], live(f"Two more RSVPs: {names[2]} and {names[3]}. That's everyone.")]
        episodes.append({"id": f"R-{i + 1:02d}", "family": "R", "case": case, "steps": steps,
                         "check": {"kind": "rsvp", "case": case, "names": names, "file": first, "changed_file": second},
                         "reach": [names[0]], "oracle": start})
    return blocks, pads, episodes


def main():
    run = RUNS["pilot" if sys.argv[1:] == ["pilot"] else "stage2"]
    blocks, pads, episodes = build(run)
    out = HERE / run["out"]
    out.write_text(json.dumps({"window": WINDOW, "blocks": blocks, "pads": pads, "episodes": episodes}, indent=1, ensure_ascii=False) + "\n")
    counts = {}
    for e in episodes:
        counts[e["family"]] = counts.get(e["family"], 0) + 1
    print(json.dumps(counts))


if __name__ == "__main__":
    main()
