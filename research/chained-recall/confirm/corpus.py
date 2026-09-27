"""Build the #885 confirmatory corpus from a new seed, with #874's chain
templates and rules (../corpus.py), a larger guard set, and absent queries
built so that the entity walk delivers an associated episode.

    python3 docs/research/chained-recall/confirm/corpus.py assemble   # -> candidates.json and corpus.json
    python3 docs/research/chained-recall/confirm/corpus.py select     # candidates + check + L's offline records -> corpus.json

`assemble` writes every candidate: 30 C1, 100 C2, 100 C3, 30 IC2 and 30
IC3 chains, 90 absent items, and the replication's exact and correction
guards. Every candidate's exchanges are in the stream, so the stream, and
every summary of it, is fixed before anything is selected. `select` keeps,
in candidate order, the first chains of each family none of whose later
hops the raw query ranks within the check's top 20 at batch 4
(evidence/selection/check-b4.json, the check over the candidates), and the first absent items whose query makes arm
L's recall deliver the associated episode as a walk item
(L's offline records over the candidates, evidence/selection/L-b4-model-probes.jsonl). Both steps
are deterministic: running either again writes byte-identical files.

An absent item joins two entities of one domain's final kind, E and F, in
one exchange ("E shares an office with F") and states F's value in another
("F answers on extension 4417"). Its query asks for E's value in the form of
a C1 query. E's value is stated nowhere, so the right answer is to say so;
the walk from E reaches F's episode, whose value is the wrong answer an
arm that answers from associations gives.
"""

import hashlib
import importlib.util
import json
import random
import re
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent


def _parent_corpus():
    spec = importlib.util.spec_from_file_location("chained_recall_corpus", HERE.parent / "corpus.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


gen = _parent_corpus()

SEED = 885
# Primary families (C2, C3) and every reported one, at their sizes, and how
# many candidates each draws so that the check can reject leaking chains.
SIZES = {"C1": 30, "C2": 50, "C3": 50, "IC2": 25, "IC3": 25}
POOL = {"C1": 30, "C2": 100, "C3": 100, "IC2": 30, "IC3": 30}
GUARDS = {"E": 30, "K": 40, "N": 60}
N_POOL = 90
# The check's depth (harness/screen.go ScreenTop).
CHECK_TOP = 20

# The absent query also counts a clarifying question as not answering:
# #874's pattern missed N-14 and N-17, which asked one.
CLARIFY = r"(could you (?:tell|share|clarify|confirm|remind|let me know)|can you (?:tell|share|clarify|confirm|remind|let me know)|do you (?:mean|know|have)|which one do you mean|who do you mean)"
ABSTAIN = gen.ABSTAIN + "|" + CLARIFY

# Per domain of ../corpus.py DOMAINS, by name: the final entity kind both E
# and F take, the exchange that joins them without making F's value E's,
# and the query for E's value (the domain's C1 query).
ABSENT = {
    "procurement": {"s": "{f} {w}", "join": "{e} shares an office with {x}.", "ack": "Noted, {e} and {x} share an office."},
    "travel": {"s": "{w} Coaches", "join": "{e} and {x} park their buses at the same depot.", "ack": "Noted, {e} and {x} share a depot."},
    "lab": {"s": "{f} {w}", "join": "{e} trained under {x}.", "ack": "Understood, {e} trained under {x}."},
    "events": {"s": "{w} Catering", "join": "{e} and {x} are owned by the same family.", "ack": "Noted, one family owns {e} and {x}."},
    "software": {"s": "{f} {w}", "join": "{e} used to be on a team with {x}.", "ack": "Understood, {e} and {x} once shared a team."},
    "household": {"s": "{f} {w}", "join": "{e} learned the trade from {x}.", "ack": "Noted, {e} learned from {x}."},
}


def fill_person(rng, names, template, avoid_first=None):
    """A domain's final entity; two in one item never share a first name,
    so a model's first-name mention cannot join them."""
    while True:
        first = rng.choice(gen.FIRST)
        if first != avoid_first:
            return template.format(w=names.word(), f=first), first


def absent_item(rng, names, domain, used):
    spec = ABSENT[domain["name"]]
    e, e_first = fill_person(rng, names, spec["s"])
    x, _ = fill_person(rng, names, spec["s"], avoid_first=e_first if "{f}" in spec["s"] else None)
    v = gen.unique_value(lambda: domain["value"](rng), used)
    fields = {"s0": "", "s1": "", "s2": x, "v": v}
    statement = gen.cap(domain["r"][2].format(**fields))
    return {
        "domain": domain["name"], "entity": e, "associated": x, "near_miss": v,
        "query": gen.cap(domain["q"][1].format(s0="", s1="", s2=e, v="")),
        "exchanges": [
            {"statement": gen.cap(spec["join"].format(e=e, x=x)), "ack": gen.cap(spec["ack"].format(e=e, x=x))},
            {"statement": statement, "ack": gen.cap(domain["ack"][2].format(**fields))},
        ],
    }


def absent_ok(item):
    """E is named in the joining exchange and nowhere else but the query; F's
    value appears only in F's own exchange, and never in the query."""
    query = gen.marked(item["query"])
    join, value = (gen.marked(x["statement"]) for x in item["exchanges"])
    # A kind word both names share ("Catering") identifies neither.
    e = gen.marked(item["entity"]) - gen.marked(item["associated"])
    x = gen.marked(item["associated"]) - gen.marked(item["entity"])
    v = gen.marked(item["near_miss"])
    return bool(e) and bool(x) and e <= query and e <= join and not (e & value) and x <= join and x <= value \
        and not (x & query) and v <= value and not (v & (query | join))


def pool(base):
    rng = random.Random(SEED)
    names = gen.Names(rng, base)
    used = set()
    chains = []
    for family, size in POOL.items():
        k = int(family[-1])
        for i in range(size):
            if family.startswith("IC"):
                c = gen.implicit_chain(rng, names, k, used)
            else:
                c = gen.explicit_chain(rng, names, gen.DOMAINS[i % len(gen.DOMAINS)], k, used)
            c = {"id": f"{family}-{i + 1:03d}", "family": family, **c}
            check_text(c["id"], [h["statement"] for h in c["hops"]], c["answer"], base)
            if not gen.lexical_ok(c):
                raise SystemExit(f"{c['id']} breaks the lexical rule: {c}")
            for h in c["hops"]:
                h["exchange"] = gen.exchange(rng, h["statement"], h["ack"])
            chains.append(c)
    absent = []
    for i in range(N_POOL):
        a = {"id": f"N-{i + 1:03d}", "family": "N", **absent_item(rng, names, gen.DOMAINS[i % len(gen.DOMAINS)], used)}
        check_text(a["id"], [x["statement"] for x in a["exchanges"]], a["near_miss"], base)
        if not absent_ok(a):
            raise SystemExit(f"{a['id']} breaks the absent rule: {a}")
        for x in a["exchanges"]:
            x["exchange"] = gen.exchange(rng, x["statement"], x["ack"])
        absent.append(a)
    return chains, absent


def schedule(rng, exchanges):
    """Order exchanges so that two of one item are at least MIN_GAP apart
    and an item's parts come in no fixed order. ../corpus.py's greedy order
    strands an item's last parts at this corpus's size, so an eligible part
    of the item with the most parts left goes first, the shuffled order
    breaking ties."""
    remaining = exchanges[:]
    rng.shuffle(remaining)
    left = {}
    for item, _ in remaining:
        left[item] = left.get(item, 0) + 1
    last, out = {}, []
    while remaining:
        eligible = [j for j, (item, _) in enumerate(remaining) if len(out) - last.get(item, -gen.MIN_GAP) >= gen.MIN_GAP]
        if not eligible:
            raise SystemExit("no exchange can be placed; the gap rule cannot hold")
        j = max(eligible, key=lambda j: (left[remaining[j][0]], -j))
        item, part = remaining.pop(j)
        out.append((item, part))
        last[item] = len(out) - 1
        left[item] -= 1
    return out


def check_text(item, statements, value, base):
    for s in statements:
        if gen.CREDENTIAL.search(s):
            raise SystemExit(f"{item}: a credential pattern would redact {s!r}")
    if re.search(r"\b" + re.escape(value.lower()) + r"\b", base):
        raise SystemExit(f"{item}: value {value!r} occurs in the base")


def replication_guards(rng):
    """The replication's exact and correction queries; their evidence is in
    the base."""
    items = json.load(open(gen.REPLICATION / "items.json"))
    queries = json.load(open(gen.REPLICATION / "queries.json"))
    by_id = {i["id"]: i for f in ("exact", "correction") for i in items[f]}
    out = []
    for family, name in (("E", "exact"), ("K", "correction")):
        chosen = sorted(rng.sample([q for q in queries if q["family"] == name], GUARDS[family]), key=lambda q: q["id"])
        for i, q in enumerate(chosen):
            item = by_id[q["item"]]
            g = {"id": f"{family}-{i + 1:02d}", "family": family, "query": q["text"], "replication": q["id"], "evidence": q["evidence"]}
            if family == "E":
                g["key"] = item["key"]
                g["check"] = {"kind": "contains", "value": item["key"]}
            else:
                g["key"] = item["new_key"]
                g["obsolete"] = q["obsolete"]
                g["check"] = {"kind": "correction", "new": item["new_key"], "old": item["old_key"]}
            out.append(g)
    return out


def assemble():
    base = gen.base_text()
    chains, absent = pool(base)
    rng = random.Random(SEED + 1)
    exchanges = [(c["id"], i) for c in chains for i in range(len(c["hops"]))] + [(a["id"], i) for a in absent for i in range(2)]
    slots = schedule(rng, exchanges)
    by_id = {c["id"]: c for c in chains}
    by_absent = {a["id"]: a for a in absent}
    stream, next_id = [], gen.BASE_MESSAGES + 1
    for item_id, part in slots:
        h = by_id[item_id]["hops"][part] if item_id in by_id else by_absent[item_id]["exchanges"][part]
        h["source"] = next_id
        for m in h["exchange"]:
            stream.append({"id": next_id, "role": m["role"], "content": m["content"], "chain": item_id, "hop": part + 1})
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
    for c in chains:
        for h in c["hops"]:
            del h["exchange"]
    guards = replication_guards(rng)
    for a in absent:
        for x in a["exchanges"]:
            del x["exchange"]
        guards.append({"id": a["id"], "family": "N", "query": a["query"], "evidence": [], "entity": a["entity"],
                       "associated": a["associated"], "near_miss": a["near_miss"], "domain": a["domain"],
                       "sources": [x["source"] for x in a["exchanges"]], "exchanges": a["exchanges"],
                       "check": {"kind": "abstain", "pattern": ABSTAIN, "near_miss": a["near_miss"]}})
    out = {"base_messages": gen.BASE_MESSAGES, "window": 32, "stage": "candidates", "chains": chains, "guards": guards, "stream": stream}
    text = json.dumps(out, indent=1, ensure_ascii=False) + "\n"
    (HERE / "candidates.json").write_text(text)
    (HERE / "corpus.json").write_text(text)
    print(json.dumps({"chains": len(chains), "absent": len(absent), "stream_messages": len(stream), "tail": len(tail)}))


def leaking(check):
    """Chains the check ranks a later hop of within its top."""
    return {r["id"] for r in check["chains"] if any(x <= check["summary"]["top"] for x in r["raw"][1:])}


def delivers_associated(probe, guard):
    """Whether arm L's recall for an absent query holds F's episode as a walk
    item."""
    source = guard["sources"][1]
    return any(it["kind"] == "linked" and it["first"] <= source <= it["last"] for it in probe["items"])


def select():
    candidates_text = (HERE / "candidates.json").read_text()
    candidates = json.loads(candidates_text)
    check = json.load(open(HERE / "evidence" / "selection" / "check-b4.json"))
    if check["summary"]["top"] != CHECK_TOP:
        raise SystemExit("the check ran at another depth")
    checked = {r["id"] for r in check["chains"]}
    if checked != {c["id"] for c in candidates["chains"]}:
        raise SystemExit("the check did not run over the candidates")
    leaks = leaking(check)
    chains = []
    for family, size in SIZES.items():
        passed = [c for c in candidates["chains"] if c["family"] == family and c["id"] not in leaks]
        if len(passed) < size:
            raise SystemExit(f"{family}: only {len(passed)} of {size} candidates pass the check")
        chains += passed[:size]
    probes = {p["probe"]: p for p in gen.load_jsonl(HERE / "evidence" / "selection" / "L-b4-model-probes.jsonl")}
    absent = [g for g in candidates["guards"] if g["family"] == "N"]
    delivered = [g for g in absent if delivers_associated(probes[g["id"]], g)]
    if len(delivered) < GUARDS["N"]:
        raise SystemExit(f"N: only {len(delivered)} of {GUARDS['N']} absent candidates have the associated episode delivered")
    guards = [g for g in candidates["guards"] if g["family"] != "N"] + delivered[:GUARDS["N"]]
    out = {**candidates, "stage": "selected", "candidates_sha256": hashlib.sha256(candidates_text.encode()).hexdigest(),
           "chains": chains, "guards": guards}
    (HERE / "corpus.json").write_text(json.dumps(out, indent=1, ensure_ascii=False) + "\n")
    print(json.dumps({"chains": {f: sum(c["family"] == f for c in chains) for f in SIZES}, "leaking_candidates": len(leaks),
                      "absent_delivered": len(delivered), "absent_candidates": len(absent)}))


if __name__ == "__main__":
    {"assemble": assemble, "select": select}[sys.argv[1]]()
