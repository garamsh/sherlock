"""Build the #874 chain corpus: chains of associated statements appended to
the #856 replication's 4,800 messages, and the replication's exact,
correction and absent queries as guards.

    python3 docs/research/chained-recall/corpus.py pool       # candidate chains -> pool.json
    python3 docs/research/chained-recall/corpus.py assemble   # pool.json + evidence/screen.json -> corpus.json

A chain of length k answers its query only through k statements. Each sits
in an exchange of its own: statement, acknowledgement, a same-topic
follow-up and its acknowledgement, four messages that fill one batch of
four alone. Hop i's statement names the entity hop i+1 starts from, so hop
i+1's identifying words are in hop i's evidence and nowhere earlier: not in
the query and not in hop i-1's evidence. Output is deterministic: running
either step again writes byte-identical files.
"""

import json
import random
import re
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPLICATION = HERE.parent / "mid-memory-rag" / "evidence" / "replication"
SEED = 874
# Primary families and their sizes; the pool draws POOL_FACTOR times as many
# so that screening can reject chains a raw query already reaches.
SIZES = {"C1": 30, "C2": 50, "C3": 50, "IC2": 25, "IC3": 25}
POOL_FACTOR = 2
GUARDS = {"E": 20, "K": 20, "N": 20}
# Hops of one chain sit at least this many exchanges (of four messages)
# apart, so they fall in different mids at batch 4 and 8 and in different
# groups of 4 and of 8 batch-4 mids.
MIN_GAP = 16
BASE_MESSAGES = 4800
# The stream after the base ends on a multiple of this many messages, so
# batch 8 closes exactly and batch-4 mids form whole groups of 8; unrelated
# chat from the replication fills the tail.
ALIGN = 32
TAIL_MIN = 48

FIRST = ["Mara", "Tomas", "Ines", "Kofi", "Anya", "Ravi", "Lena", "Oscar", "Priya", "Jonas", "Sofia", "Dmitri",
         "Aiko", "Bruno", "Chloe", "Emeka", "Freya", "Hugo", "Isla", "Mateo", "Nadia", "Pavel", "Rosa", "Yusuf"]
ONSETS = ["b", "br", "c", "d", "dr", "f", "g", "gr", "h", "k", "l", "m", "n", "p", "qu", "r", "s", "st", "t", "tr", "v", "w", "z"]
VOWELS = ["a", "e", "i", "o", "u", "ai", "ea", "ou"]
CODAS = ["", "n", "r", "l", "s", "nd", "rk", "ll", "m", "x"]

# An explicit domain is a path S0 -r1-> S1 -r2-> S2 -r3-> value. A chain of
# length k starts at S(3-k) and asks, with q[k], for the value; the query
# names only its start and the kind of answer, never a later hop's words.
DOMAINS = [
    {"name": "procurement",
     "s": ["the {w} build", "{w} Components", "{f} {w}"],
     "value": lambda r: f"{r.randrange(2000, 9999)}",
     "r": ["Every part for {s0} is ordered from {s1}.",
           "Our account at {s1} is looked after by {s2}.",
           "{s2} answers on extension {v}."],
     "ack": ["Noted: parts for {s0} come from {s1}.",
             "Understood, {s2} looks after the {s1} account.",
             "Got it, {s2} is on extension {v}."],
     "q": {3: "Something is wrong with an order for {s0}. What number do I dial to get the right person?",
           2: "I need to talk to whoever handles us at {s1}. What number do I dial?",
           1: "What number do I dial to reach {s2}?"}},
    {"name": "travel",
     "s": ["the {w} trip", "the {w} Hotel", "{w} Coaches"],
     "value": lambda r: f"bay {r.randrange(10, 99)}",
     "r": ["For {s0} we are staying at {s1}.",
           "{s1} sends its guests to the airport with {s2}.",
           "{s2} always pick up from {v}."],
     "ack": ["Noted, {s1} for {s0}.",
             "Understood, {s1} uses {s2} for airport runs.",
             "Got it, {s2} pick up from {v}."],
     "q": {3: "When I land for {s0}, where should I wait for my ride?",
           2: "Where should I wait for the ride that {s1} arranges?",
           1: "Where should I wait when {s2} come for me?"}},
    {"name": "lab",
     "s": ["the {w} study", "the {w} lab", "{f} {w}"],
     "value": lambda r: f"room {r.randrange(110, 990)}",
     "r": ["All samples for {s0} are processed by {s1}.",
           "{s1} is run by {s2}.",
           "{s2} works out of {v}."],
     "ack": ["Noted, {s1} processes the samples for {s0}.",
             "Understood, {s2} runs {s1}.",
             "Got it, {s2} is in {v}."],
     "q": {3: "I have a question about samples for {s0}. Which door do I knock on?",
           2: "Which door do I knock on to find the person in charge at {s1}?",
           1: "Which door do I knock on to find {s2}?"}},
    {"name": "events",
     "s": ["the {w} gala", "the {w} Rooms", "{w} Catering"],
     "value": lambda r: f"{r.choice('BCDFGHJKLMNPRSTVWXZ')}{r.choice('BCDFGHJKLMNPRSTVWXZ')}-{r.randrange(1000, 9999)}",
     "r": ["{s0} is being held at {s1}.",
           "{s1} only lets {s2} cook on the premises.",
           "Our booking with {s2} is under reference {v}."],
     "ack": ["Noted, {s0} is at {s1}.",
             "Understood, only {s2} can cook at {s1}.",
             "Got it, the {s2} booking reference is {v}."],
     "q": {3: "For {s0}, which code do I quote when I call about the food?",
           2: "Which code do I quote when I call about the food for our event at {s1}?",
           1: "Which code do I quote when I call {s2}?"}},
    {"name": "software",
     "s": ["the {w} service", "the {w} team", "{f} {w}"],
     "value": lambda r: f"desk {r.choice('ABCDEFGHJKLMNPQRSTUVWXYZ')}{r.randrange(10, 99)}",
     "r": ["{s0} belongs to {s1}.",
           "{s1} is led by {s2}.",
           "{s2} sits at {v}."],
     "ack": ["Noted, {s1} owns {s0}.",
             "Understood, {s2} leads {s1}.",
             "Got it, {s2} is at {v}."],
     "q": {3: "{s0} is down. Where do I walk over to so I find the right person?",
           2: "Where do I walk over to so I find whoever leads {s1}?",
           1: "Where do I walk over to so I find {s2}?"}},
    {"name": "household",
     "s": ["my {w} van", "{w} Motors", "{f} {w}"],
     "value": lambda r: f"locker {r.randrange(100, 999)}",
     "r": ["My {s0} gets serviced at {s1}.",
           "At {s1}, the mechanic I trust is {s2}.",
           "{s2} leaves the keys of finished jobs in {v}."],
     "ack": ["Noted, {s1} services your {s0}.",
             "Understood, {s2} is your mechanic at {s1}.",
             "Got it, {s2} uses {v} for finished jobs."],
     "q": {3: "My {s0} is ready. Which number do I look for when I collect it?",
           2: "Which number do I look for when I collect a job from my trusted person at {s1}?",
           1: "Which number do I look for when I collect a job from {s2}?"}},
]

# An implicit domain is a path D -r1-> office -r2-> rulebook -r3-> rule.
# A task asks for an artifact for the start; the artifact obeys the rule
# only if the chain was followed, and the rule makes it carry a unique
# reference.
IMPLICIT = {
    "s": ["the weekly update on the {w} build", "the {w} office", "the {w} house rules"],
    "r": ["{s0} always goes to {s1}.",
          "{s1} works under {s2}.",
          "Under {s2}, every document has to include the reference {v}."],
    "ack": ["Noted, {s0} goes to {s1}.",
            "Understood, {s1} follows {s2}.",
            "Got it, {s2} require the reference {v} in every document."],
    "task": {3: "Write {s0} for me: two short lines saying we are on schedule.",
             2: "Write a two-line note for {s1} saying the delivery is on schedule."},
}

FOLLOW_UPS = [("Please keep that on file.", "It's on file."),
              ("That's a detail I will need later.", "Kept for later."),
              ("Remember that one, it matters.", "I'll remember it."),
              ("Hold on to that for me.", "Holding on to it."),
              ("Make a note of that.", "Noted.")]

CREDENTIAL = re.compile(r"password|passcode|passphrase|secret|token|api key|\bpin\b|bearer", re.I)
ABSTAIN = r"(don't|do not|didn't|did not|haven't|have not|can't|cannot|couldn't|could not|no record|not (?:sure|find|found|mentioned|aware|in (?:my|the))|never (?:mentioned|told|said)|unknown|no information)"


def load_jsonl(path):
    with open(path) as f:
        return [json.loads(line) for line in f if line.strip()]


class Names:
    """Invented words, each used once, none a word of the base corpus."""

    def __init__(self, rng, base_text):
        self.rng, self.base, self.used = rng, base_text, set()

    def word(self):
        while True:
            w = "".join(self.rng.choice(ONSETS) + self.rng.choice(VOWELS) for _ in range(2)) + self.rng.choice(CODAS)
            w = w.capitalize()
            if 5 <= len(w) <= 9 and w.lower() not in self.used and not re.search(r"\b" + w.lower() + r"\b", self.base):
                self.used.add(w.lower())
                return w

    def fill(self, template):
        return template.format(w=self.word(), f=self.rng.choice(FIRST))


def marked(text):
    """The identifying tokens of a text: numbers and capitalized words, as
    the scorer matches values (agent-context score.py value_tokens)."""
    raw = re.findall(r"[A-Za-z0-9]+", text)
    return {t.lower() for t in raw if re.search(r"\d", t) or t[0].isupper()}


def cap(s):
    return s[0].upper() + s[1:]


def exchange(rng, statement, ack):
    u, a = rng.choice(FOLLOW_UPS)
    return [{"role": "user", "content": cap(statement)}, {"role": "assistant", "content": cap(ack)},
            {"role": "user", "content": u}, {"role": "assistant", "content": a}]


def chain_from(path, k, fields, query):
    """The last k hops of a three-relation path."""
    start = 3 - k
    s = [fields["s0"], fields["s1"], fields["s2"]]
    hops = []
    for i in range(start, 3):
        ends = [s[i], s[i + 1] if i < 2 else fields["v"]]
        hops.append({"statement": cap(path["r"][i].format(**fields)), "ack": path["ack"][i].format(**fields),
                     "ends": ends, "key": " ".join(ends)})
    return {"k": k, "start": s[start], "query": cap(query.format(**fields)), "hops": hops, "answer": fields["v"]}


def unique_value(make, used):
    while True:
        v = make()
        if v not in used:
            used.add(v)
            return v


def explicit_chain(rng, names, domain, k, used):
    s = [names.fill(t) for t in domain["s"]]
    fields = {"s0": s[0], "s1": s[1], "s2": s[2], "v": unique_value(lambda: domain["value"](rng), used)}
    c = chain_from(domain, k, fields, domain["q"][k])
    c["domain"] = domain["name"]
    c["check"] = {"kind": "contains", "value": fields["v"]}
    return c


def implicit_chain(rng, names, k, used):
    s = [names.fill(t) for t in IMPLICIT["s"]]
    v = unique_value(lambda: f"REF-{names.word().upper()[:4]}{rng.randrange(100, 999)}", used)
    fields = {"s0": s[0], "s1": s[1], "s2": s[2], "v": v}
    c = chain_from(IMPLICIT, k, fields, IMPLICIT["task"][k])
    c["domain"] = "implicit"
    c["check"] = {"kind": "contains", "value": v}
    return c


def lexical_ok(chain):
    """Hop i+1's identifying words appear in hop i's statement and not in the
    query or in any earlier hop; the answer appears only in the last hop."""
    query = marked(chain["query"])
    hops = [marked(h["statement"]) for h in chain["hops"]]
    for i in range(1, len(hops)):
        new = marked(chain["hops"][i]["ends"][1])
        if new & query or any(new & hops[j] for j in range(i)):
            return False
        if not marked(chain["hops"][i]["ends"][0]) <= hops[i - 1]:
            return False
    return not (marked(chain["answer"]) & (query | set().union(*hops[:-1])))


def base_text():
    corpus = json.load(open(REPLICATION / "corpus.json"))
    return "\n".join(m["content"] for m in corpus).lower()


def pool():
    rng = random.Random(SEED)
    text = base_text()
    names = Names(rng, text)
    used = set()
    chains = []
    for family, size in SIZES.items():
        k = int(family[-1])
        for i in range(size * POOL_FACTOR):
            if family.startswith("IC"):
                c = implicit_chain(rng, names, k, used)
            else:
                c = explicit_chain(rng, names, DOMAINS[i % len(DOMAINS)], k, used)
            c = {"id": f"{family}-{i + 1:03d}", "family": family, **c}
            for h in c["hops"]:
                if CREDENTIAL.search(h["statement"]):
                    raise SystemExit(f"{c['id']}: a credential pattern would redact {h['statement']!r}")
            if re.search(r"\b" + re.escape(c["answer"].lower()) + r"\b", text):
                raise SystemExit(f"{c['id']}: answer {c['answer']!r} occurs in the base")
            if not lexical_ok(c):
                raise SystemExit(f"{c['id']} breaks the lexical rule: {c}")
            for h in c["hops"]:
                h["exchange"] = exchange(rng, h["statement"], h["ack"])
            chains.append(c)
    (HERE / "pool.json").write_text(json.dumps(chains, indent=1, ensure_ascii=False) + "\n")
    print(json.dumps({f: sum(c["family"] == f for c in chains) for f in SIZES}))


def schedule(rng, exchanges):
    """Order exchanges so that two of one chain are at least MIN_GAP apart,
    and a chain's hops come in no fixed order."""
    remaining = exchanges[:]
    rng.shuffle(remaining)
    last, out = {}, []
    while remaining:
        for j, (chain, _) in enumerate(remaining):
            if len(out) - last.get(chain, -MIN_GAP) >= MIN_GAP:
                break
        else:
            raise SystemExit("no exchange can be placed; the gap rule cannot hold")
        out.append(remaining.pop(j))
        last[chain] = len(out) - 1
    return out


def guards(rng):
    """The replication's exact, correction and absent queries; their
    evidence is in the base."""
    items = json.load(open(REPLICATION / "items.json"))
    queries = json.load(open(REPLICATION / "queries.json"))
    by_id = {i["id"]: i for f in ("exact", "correction", "absent") for i in items[f]}
    out = []
    for family, name in (("E", "exact"), ("K", "correction"), ("N", "absent")):
        chosen = sorted(rng.sample([q for q in queries if q["family"] == name], GUARDS[family]), key=lambda q: q["id"])
        for i, q in enumerate(chosen):
            item = by_id[q["item"]]
            g = {"id": f"{family}-{i + 1:02d}", "family": family, "query": q["text"], "replication": q["id"],
                 "evidence": q["evidence"]}
            if family == "E":
                g["key"] = item["key"]
                g["check"] = {"kind": "contains", "value": item["key"]}
            elif family == "K":
                g["key"] = item["new_key"]
                g["obsolete"] = q["obsolete"]
                g["check"] = {"kind": "correction", "new": item["new_key"], "old": item["old_key"]}
            else:
                g["check"] = {"kind": "abstain", "pattern": ABSTAIN}
            out.append(g)
    return out


def assemble():
    rng = random.Random(SEED + 1)
    candidates = json.load(open(HERE / "pool.json"))
    screen = {s["id"]: s for s in json.load(open(HERE / "evidence" / "screen.json"))["chains"]}
    chains = []
    for family, size in SIZES.items():
        passed = [c for c in candidates if c["family"] == family and screen[c["id"]]["pass"]]
        if len(passed) < size:
            raise SystemExit(f"{family}: only {len(passed)} of {size} candidates pass screening")
        for n, c in enumerate(passed[:size]):
            chains.append({**c, "id": f"{family}-{n + 1:02d}", "pool_id": c["id"]})
    slots = schedule(rng, [(c["id"], i) for c in chains for i in range(len(c["hops"]))])
    by_id = {c["id"]: c for c in chains}
    stream, next_id = [], BASE_MESSAGES + 1
    for chain_id, hop in slots:
        h = by_id[chain_id]["hops"][hop]
        h["source"] = next_id
        for m in h["exchange"]:
            stream.append({"id": next_id, "role": m["role"], "content": m["content"], "chain": chain_id, "hop": hop + 1})
            next_id += 1
    # The tail is unrelated chat, so the restored window and the newest mids
    # hold no evidence.
    used = {m["chat"] for m in json.load(open(REPLICATION / "corpus.json"))}
    chats = sorted((c for c in load_jsonl(REPLICATION / "conversations.jsonl")
                    if c["id"].startswith("filler") and c["id"] not in used and "messages" in c), key=lambda c: c["id"])
    need = TAIL_MIN + (-(len(stream) + TAIL_MIN)) % ALIGN
    tail = [m for c in chats for m in c["messages"]][:need]
    if len(tail) < need or tail[0]["role"] != "user":
        raise SystemExit("not enough tail chat")
    for m in tail:
        stream.append({"id": next_id, "role": m["role"], "content": m["content"], "chain": "tail", "hop": 0})
        next_id += 1
    for c in chains:
        for h in c["hops"]:
            del h["exchange"]
    out = {"base_messages": BASE_MESSAGES, "window": 32, "chains": chains, "guards": guards(rng), "stream": stream}
    (HERE / "corpus.json").write_text(json.dumps(out, indent=1, ensure_ascii=False) + "\n")
    print(json.dumps({"chains": len(chains), "stream_messages": len(stream), "tail": len(tail)}))


if __name__ == "__main__":
    {"pool": pool, "assemble": assemble}[sys.argv[1]]()
