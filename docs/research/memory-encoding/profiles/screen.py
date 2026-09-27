"""Issue #906's free screen: build per-entity profile cards from #890's
recorded facts and entities, attach them to S's recorded deliveries, and
apply the gate (preregistration.md).

    python3 docs/research/memory-encoding/profiles/screen.py sample
    python3 docs/research/memory-encoding/profiles/screen.py score

`sample` draws the owner audit into evidence/owner-audit.jsonl for hand
labelling. `score` writes evidence/screen.json, evidence/cards.jsonl and
evidence/probes.jsonl. Neither calls a model or an embedder.
"""

import importlib.util
import json
import random
import re
import statistics
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
VALIDATION = HERE.parent / "validation"
EVIDENCE = HERE / "evidence"
FACTS = VALIDATION / "arms" / "facts" / "evidence" / "facts-b4.jsonl"
ENTITIES = VALIDATION / "arms" / "links" / "evidence" / "entities-b4.jsonl"
DELIVERED = VALIDATION / "evidence" / "offline" / "S" / "b4" / "probes.jsonl"
CORPUS = VALIDATION / "corpus.json"
AUDIT = EVIDENCE / "owner-audit.jsonl"


def _module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


states = _module("agent_context_score", HERE.parent.parent / "agent-context" / "score.py").states

BOUND = 400
OVERLAP = 0.5
MIN_NAME = 3
USER = "user"
USER_NAME = "The user"
PARTICIPANTS = {"user", "users", "assistant", "me", "i", "you"}
USER_WORDS = re.compile(r"(?<![A-Za-z0-9])(?:user's|user|i|me|my)(?![A-Za-z0-9])", re.I)
STOP = set("""a an the and or but of to in on at for from by with as is are was were be been being has have had
do does did this that these those it its they them their there here who whom which what when where why how
not no so than then also only just any all every each some very can will would should must may might
into over under about after before again still out up down off per via both either""".split())
BOOKKEEPING = set("""remember remembered remembering keep kept note noted wants want wanted needs need needed
later future reference detail details important information info mentioned asked requested confirmed
store stored storing save saved""".split())
CHAINS = ("C2", "C3", "IC2", "IC3", "UP")
CHAIN_MARGIN = 10
UP_MARGIN = 5
AUDIT_N = 50
AUDIT_PASS = 45
KU_PASS = 27
SEED = 906


# ---- names ------------------------------------------------------------------

def norm(name):
    name = " ".join(name.lower().split())
    return name[4:] if name.startswith("the ") else name


def position(text, name):
    """Where name occurs in text as a whole phrase, or None."""
    m = re.search(r"(?<![a-z0-9])" + re.escape(name) + r"(?![a-z0-9])", text.lower())
    return m.start() if m else None


def entity_names(names):
    """The mid's non-participant entity names, normalized, first spelling kept."""
    out = {}
    for n in names or []:
        k = norm(n)
        if len(k) >= MIN_NAME and k not in PARTICIPANTS and k not in out:
            out[k] = n
    return out


# ---- cards ------------------------------------------------------------------

def owner(fact, names):
    """The candidate the fact names earliest, the longer name on a tie; the
    user for an unnamed `user` fact; None otherwise."""
    found = []
    for k in names:
        p = position(fact["text"], k)
        if p is not None:
            found.append((p, -len(k), k))
    u = USER_WORDS.search(fact["text"])
    if u:
        found.append((u.start(), 0, USER))
    if found:
        return min(found)[2]
    return USER if fact["kind"] == "user" else None


def attribute_key(text, names):
    for k in sorted(names, key=len, reverse=True):
        text = re.sub(r"(?<![A-Za-z0-9])" + re.escape(k) + r"(?![A-Za-z0-9])", " ", text, flags=re.I)
    text = USER_WORDS.sub(" ", text)
    words = re.findall(r"[A-Za-z0-9][A-Za-z0-9'-]*", text)
    key = set()
    for i, w in enumerate(words):
        if re.search(r"\d", w) or (i > 0 and w[0].isupper()):
            continue
        w = re.sub(r"'s$", "", w.lower())
        if w not in STOP and w not in BOOKKEEPING:
            key.add(w)
    return frozenset(key)


def same_attribute(a, b):
    return bool(a) and bool(b) and len(a & b) / min(len(a), len(b)) >= OVERLAP


def bounded(facts, bound=BOUND):
    """The newest facts, whole, whose texts joined by '; ' fit in bound, in stream order."""
    kept, size = [], -2
    for f in reversed(facts):
        if size + 2 + len(f["text"]) > bound:
            break
        kept.append(f)
        size += 2 + len(f["text"])
    return kept[::-1]


def build(records, entities, bound=BOUND, overwrite=True):
    """Cards from recorded facts: {owner: {"name", "all", "facts"}}, with the
    replacement log and the dropped facts."""
    cards, replaced, dropped = {}, [], []
    for rec in sorted(records, key=lambda r: r["first"]):
        names = entity_names(entities.get((rec["first"], rec["last"])))
        for f in rec["facts"] or []:
            entry = {"first": rec["first"], "last": rec["last"], "kind": f["kind"], "text": f["text"]}
            o = owner(entry, names)
            if o is None:
                dropped.append(entry)
                continue
            entry["owner"] = o
            entry["key"] = attribute_key(entry["text"], names)
            card = cards.setdefault(o, {"name": USER_NAME if o == USER else names[o], "all": []})
            if overwrite:
                for old in [x for x in card["all"] if same_attribute(x["key"], entry["key"])]:
                    card["all"].remove(old)
                    replaced.append((old, entry))
            card["all"].append(entry)
    for card in cards.values():
        card["facts"] = bounded(card["all"], bound) if bound else list(card["all"])
    return cards, replaced, dropped


def render(card):
    return card["name"] + ": " + "; ".join(f["text"] for f in card["facts"])


# ---- attach -----------------------------------------------------------------

def attach(items, entities, cards):
    """The owners whose cards the delivered items attach, in first-named order,
    and the user's card on any probe that delivered an episode."""
    owners = []
    for it in items:
        for k in entity_names(entities.get((it["first"], it["last"]))):
            if k in cards and k not in owners:
                owners.append(k)
    if items and USER in cards and USER not in owners:
        owners.append(USER)
    return owners


def any_mention(items, entities, records):
    """Every fact naming a delivered mid's entity anywhere, and every fact the user owns."""
    delivered = {}
    for it in items:
        delivered.update(entity_names(entities.get((it["first"], it["last"]))))
    out = []
    for rec in records:
        names = entity_names(entities.get((rec["first"], rec["last"])))
        for f in rec["facts"] or []:
            entry = {"kind": f["kind"], "text": f["text"]}
            if any(position(f["text"], k) is not None for k in delivered) or owner(entry, names) == USER:
                out.append(f["text"])
    return out


# ---- measures ---------------------------------------------------------------

def probe_row(probe, delivered, cards, entities, records):
    items = delivered["items"]
    owners = attach(items, entities, cards)
    attached = [cards[o] for o in owners]
    texts = [render(c) for c in attached]
    row = {"probe": probe["id"], "family": probe["family"], "owners": owners,
           "card_chars": sum(len(t) for t in texts), "delivered_chars": delivered["context_chars"]}
    fam = probe["family"]
    if fam in CHAINS:
        row["reach"] = any(states(t, probe["answer"]) for t in texts)
        row["s_states"] = any(states(it["text"], probe["answer"]) for it in items)
    elif fam == "KU":
        facts = [f["text"] for c in attached for f in c["facts"]]
        row["current"] = any(states(t, probe["key"]) for t in facts)
        row["superseded"] = any(states(t, probe["obsolete"]) for t in facts)
        row["stale"] = any(states(t, probe["obsolete"]) and not states(t, probe["key"]) for t in facts)
    elif fam in ("N", "NC"):
        own = cards.get(norm(probe["entity"]))
        row["owner_card_states"] = bool(own) and states(render(own), probe["near_miss"])
        row["confusion"] = norm(probe["entity"]) in owners and row["owner_card_states"]
        row["near_miss_attached"] = any(states(t, probe["near_miss"]) for t in texts)
    return row


def loose_reach(probe, delivered, entities, records, unbounded):
    items = delivered["items"]
    owners = attach(items, entities, unbounded)
    texts = [render(unbounded[o]) for o in owners]
    return (any(states(t, probe["answer"]) for t in texts),
            any(states(t, probe["answer"]) for t in any_mention(items, entities, records)))


def ku_overwrite(guard, replaced, records):
    """Whether a fact stating the superseded value, from the first statement's
    batch, was replaced by overwrite; else why not."""
    def batch_facts(source, value):
        return [f["text"] for r in records if r["first"] <= source <= r["last"] for f in r["facts"] or [] if states(f["text"], value)]
    first = batch_facts(guard["sources"][0], guard["obsolete"])
    corr = batch_facts(guard["sources"][1], guard["key"])
    if any(old["text"] in first and old["first"] <= guard["sources"][0] <= old["last"] for old, _ in replaced):
        return "replaced"
    if not first:
        return "no first fact"
    if not corr:
        return "no correction fact"
    return "not replaced"


def gate(chain_reach, up_reach, audit_passed):
    admitted = chain_reach >= CHAIN_MARGIN or up_reach >= UP_MARGIN
    return {"chains_c2_c3": chain_reach, "chain_margin": CHAIN_MARGIN, "up": up_reach, "up_margin": UP_MARGIN,
            "ceiling_clears": admitted, "owner_audit_passed": audit_passed,
            "paid_phase_may_be_proposed": admitted and audit_passed}


def size(rows):
    chars = [r["card_chars"] for r in rows]
    return {"n": len(rows), "card_chars_mean": round(statistics.mean(chars), 1), "card_chars_median": statistics.median(chars),
            "card_chars_max": max(chars), "delivered_chars_mean": round(statistics.mean(r["delivered_chars"] for r in rows), 1)}


# ---- io ---------------------------------------------------------------------

def load_jsonl(path):
    with open(path) as f:
        return [json.loads(line) for line in f if line.strip()]


def inputs():
    records = load_jsonl(FACTS)
    entities = {(r["first"], r["last"]): r["entities"] for r in load_jsonl(ENTITIES)}
    delivered = {p["probe"]: p for p in load_jsonl(DELIVERED)}
    corpus = json.loads(CORPUS.read_text())
    probes = corpus["chains"] + corpus["guards"]
    missing = [(r["first"], r["last"]) for r in records if (r["first"], r["last"]) not in entities]
    undelivered = [p["id"] for p in probes if p["id"] not in delivered]
    if len(records) != 1032 or missing or undelivered:
        raise SystemExit(f"inputs incomplete: {len(records)} fact records, entities missing {missing[:5]}, deliveries missing {undelivered[:5]}")
    return records, entities, delivered, probes


def sample():
    records, entities, _, _ = inputs()
    cards, _, _ = build(records, entities, bound=None, overwrite=False)
    owned = sorted((f for c in cards.values() for f in c["all"]), key=lambda f: (f["first"], f["text"]))
    picked = random.Random(SEED).sample(owned, AUDIT_N)
    EVIDENCE.mkdir(exist_ok=True)
    with open(AUDIT, "w") as f:
        for x in picked:
            f.write(json.dumps({"first": x["first"], "kind": x["kind"], "text": x["text"],
                                "entities": entities.get((x["first"], x["last"])), "owner": x["owner"],
                                "correct": None, "note": ""}) + "\n")


def audit_result(rows):
    if any(r["correct"] is None for r in rows):
        raise SystemExit("the owner audit is not fully labelled")
    correct = sum(r["correct"] for r in rows)
    return {"n": len(rows), "correct": correct, "passed": len(rows) == AUDIT_N and correct >= AUDIT_PASS}


def score():
    records, entities, delivered, probes = inputs()
    audit = audit_result(load_jsonl(AUDIT))
    cards, replaced, dropped = build(records, entities)
    unbounded, _, _ = build(records, entities, bound=None, overwrite=False)
    rows, fams = [], {}
    for p in probes:
        if p["family"] not in CHAINS + ("KU", "N", "NC"):
            continue
        row = probe_row(p, delivered[p["id"]], cards, entities, records)
        if p["family"] in CHAINS:
            row["reach_unbounded"], row["reach_any_mention"] = loose_reach(p, delivered[p["id"]], entities, records, unbounded)
        if p["family"] == "KU":
            row["overwrite"] = ku_overwrite(p, replaced, records)
        rows.append(row)
        fams.setdefault(p["family"], []).append(row)
    per = {}
    for fam, rs in fams.items():
        out = {"n": len(rs)}
        for k in ("reach", "s_states", "reach_unbounded", "reach_any_mention", "current", "superseded", "stale",
                  "confusion", "owner_card_states", "near_miss_attached"):
            if k in rs[0]:
                out[k] = sum(bool(r[k]) for r in rs)
        if fam == "KU":
            out["overwrite"] = {v: sum(r["overwrite"] == v for r in rs) for v in sorted({r["overwrite"] for r in rs})}
        out.update(size(rs))
        per[fam] = out
    ku_replaced = per["KU"]["overwrite"].get("replaced", 0)
    result = {
        "families": per,
        "size_all": size(rows),
        "gate": gate(per["C2"]["reach"] + per["C3"]["reach"], per["UP"]["reach"], audit["passed"]),
        "gate_unbounded": gate(per["C2"]["reach_unbounded"] + per["C3"]["reach_unbounded"], per["UP"]["reach_unbounded"], audit["passed"]),
        "gate_any_mention": gate(per["C2"]["reach_any_mention"] + per["C3"]["reach_any_mention"], per["UP"]["reach_any_mention"], audit["passed"]),
        "controls": {"owner_audit": audit, "ku_overwrite": {"replaced": ku_replaced, "n": per["KU"]["n"], "passed": ku_replaced >= KU_PASS}},
        "counts": {"facts": sum(len(r["facts"] or []) for r in records), "dropped": len(dropped), "replaced": len(replaced),
                   "cards": len(cards), "user_card_facts": len(cards[USER]["facts"]) if USER in cards else 0,
                   "cut_by_bound": sum(len(c["all"]) - len(c["facts"]) for c in cards.values()),
                   "facts_per_card_mean": round(statistics.mean(len(c["facts"]) for c in cards.values()), 2)},
    }
    (EVIDENCE / "screen.json").write_text(json.dumps(result, indent=1) + "\n")
    with open(EVIDENCE / "cards.jsonl", "w") as f:
        for k, c in sorted(cards.items()):
            f.write(json.dumps({"owner": k, "card": render(c), "facts": len(c["all"]), "shown": len(c["facts"])}) + "\n")
    with open(EVIDENCE / "probes.jsonl", "w") as f:
        for r in rows:
            f.write(json.dumps(r) + "\n")
    print(json.dumps({"gate": result["gate"], "controls": result["controls"]}))


if __name__ == "__main__":
    {"sample": sample, "score": score}[sys.argv[1]]()
