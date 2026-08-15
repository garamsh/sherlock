"""Where B9's two residuals come from, read off the row log.

B9 (decider-neighbours, wide) retires four simultaneously-true facts and
leaves a quarter of the corrections stale. Both are attributed here from the
trace rather than inferred from the score: every turn records what the decider
was shown and which rows it retired, so a lost coexist row is named together
with the turn that caused it, and every correction turn is classified by
whether the contradicted row reached the offer and what the reply did with it.

Every decider reply is cached, so the replay makes no API call.
"""
from __future__ import annotations
import json
import os
import random
import sys
from collections import Counter

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from api import cosine, decider, embed, embed_batch, flush, parse_json
from dataset import build, COEXIST, PERSONS
from structures import DeciderNeighborsWide, RETIRE_SYS

# Results land beside the report, not beside the caller's shell.
RESULTS = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                       "..", "..", "results_round4")


class Traced(DeciderNeighborsWide):
    """B9, recording the offer, the reply and the kill list for every turn."""

    def __init__(self, rnd, model=None):
        super().__init__(rnd, model=model)
        self.events = []
        self.last = None

    def turn(self, text, is_fact):
        self.store.tick()
        rows = self.context_rows(text)
        listing = [{"id": r.id, "memory": r.content} for r in rows]
        out = decider(RETIRE_SYS,
                      f"Latest message: {text}\nRelated memories: {listing}")
        self.calls += 1
        ops = parse_json(out)
        retire_ids = ops.get("retire", []) or []
        killed = []
        for rid in retire_ids:
            for r in rows:
                if r.id == rid and not r.dead:
                    r.dead = True
                    killed.append({"id": r.id, "content": r.content})
        stored = []
        for fact in ops.get("store", []) or []:
            if isinstance(fact, str) and fact.strip():
                stored.append(fact.strip())
                self.store.add(fact.strip())
        self.last = {"turn": text, "shown": [r.content for r in rows],
                     "retire_ids": retire_ids, "killed": killed,
                     "stored": stored, "reply": out}
        if killed:
            self.events.append(self.last)


def coexist_facts():
    """(subject, value, family) for every simultaneously-true planted fact."""
    out = []
    for person in PERSONS[:4]:
        for name, question, values, tmpl in COEXIST:
            for v in values:
                out.append((person, v, name))
    return out


def main():
    seed = int(os.environ.get("SEED", "7"))
    turns, probes = build(seed=seed)
    embed_batch([t for t, _ in turns] + [p.question for p in probes])

    # Keyed on the exact correction turn; values repeat across subjects.
    corrections = {p.correction_text: (p.subject, p.expect_absent[0])
                   for p in probes if p.kind == "corrected"}

    inst = Traced(random.Random(seed))
    gap = Counter()
    not_offered = []
    kept = []

    for text, is_fact in turns:
        target = corrections.get(text)
        if target is None:
            inst.turn(text, is_fact)
            continue

        subject, old_val = target
        old_rows = [r for r in inst.store.rows
                    if not r.dead and old_val.lower() in r.content.lower()
                    and subject.lower() in r.content.lower()]
        # The offer B9 will make, and the full ranking behind it, so a row that
        # missed the offer can be placed: was it rank 11, or rank 200?
        offer = inst.store.rank(text, inst.WIDE_K, floor=inst.WIDE_FLOOR)
        offered_ids = {r.id for r in offer}
        qv = embed(text)
        full = sorted(((cosine(qv, r.vec), r) for r in inst.store.live()),
                      key=lambda x: -x[0])
        rank_of = {r.id: i + 1 for i, (s, r) in enumerate(full)}
        score_of = {r.id: s for s, r in full}

        inst.turn(text, is_fact)

        if not old_rows:
            gap["no_live_row_for_old"] += 1
        elif not any(r.id in offered_ids for r in old_rows):
            gap["not_offered"] += 1
            not_offered.append({
                "turn": text, "subject": subject, "old_value": old_val,
                "contradicted": [
                    {"content": r.content,
                     "cosine": round(score_of.get(r.id, 0.0), 3),
                     "rank": rank_of.get(r.id)} for r in old_rows],
                "offer_size": len(offer),
            })
        elif all(r.dead for r in old_rows):
            gap["offered_and_retired"] += 1
        else:
            gap["offered_and_kept"] += 1
            ev = inst.last
            survivors = [r for r in old_rows if not r.dead]
            kept.append({
                "turn": text, "subject": subject, "old_value": old_val,
                # A row the offer never carried cannot be retired by id, even
                # when a duplicate of it was offered and was retired.
                "survivors": [{"content": r.content,
                               "offered": r.id in offered_ids,
                               "cosine": round(score_of.get(r.id, 0.0), 3),
                               "rank": rank_of.get(r.id)} for r in survivors],
                "retire_list_empty": not ev["retire_ids"],
                "retired_this_turn": ev["killed"],
                "stored_this_turn": ev["stored"],
            })

    flush()

    facts = coexist_facts()
    retired = []
    for ev in inst.events:
        for k in ev["killed"]:
            content = k["content"].lower()
            for subject, value, family in facts:
                if subject.lower() in content and value.lower() in content:
                    retired.append({"subject": subject, "value": value,
                                    "family": family, "row": k["content"],
                                    "turn": ev["turn"],
                                    "stored_same_turn": ev["stored"]})
    # A retired row only costs a probe when no other live row carries the same
    # (subject, value): the decider often retires a duplicate it just rewrote.
    live = inst.store.live()
    for k in retired:
        k["survivor"] = any(k["subject"].lower() in r.content.lower()
                            and k["value"].lower() in r.content.lower()
                            for r in live)
    lost = [k for k in retired if not k["survivor"]]
    for k in lost:
        siblings = {v for _, v, family in facts
                    if family == k["family"]} - {k["value"]}
        k["turn_names_subject"] = k["subject"].lower() in k["turn"].lower()
        k["turn_states_a_sibling"] = k["turn_names_subject"] and any(
            v.lower() in k["turn"].lower() for v in siblings)

    empty_retire_list = sum(1 for n in kept if n["retire_list_empty"])
    summary = {
        "seed": seed,
        "structure": inst.NAME,
        "rows_retired_total": sum(len(e["killed"]) for e in inst.events),
        "coexist_facts_planted": len(facts),
        "coexist_rows_retired": len(retired),
        "coexist_values_lost": len(lost),
        "correction_gap": dict(gap),
        "offered_and_kept_with_empty_retire_list": empty_retire_list,
        "coexist_lost": lost,
        "not_offered_detail": not_offered,
        "offered_and_kept_detail": kept,
    }

    os.makedirs(RESULTS, exist_ok=True)
    path = os.path.join(RESULTS, f"b9_diagnosis_seed{seed}.json")
    with open(path, "w") as f:
        json.dump(summary, f, indent=2)

    print(f"=== {inst.NAME} seed {seed} ===")
    print(f"coexist values lost: {len(lost)} of {len(facts)} planted "
          f"({len(retired)} coexist rows retired)")
    for k in lost:
        print(f"  LOST {k['subject']} / {k['value']} ({k['family']})")
        print(f"       row:  {k['row']}")
        print(f"       turn: {k['turn']}")
        print(f"       turn states a sibling of the same family: "
              f"{k['turn_states_a_sibling']}")
    print()
    print(f"correction turns: {sum(gap.values())}")
    for outcome in ("offered_and_retired", "offered_and_kept", "not_offered",
                    "no_live_row_for_old"):
        print(f"  {outcome:<24} {gap[outcome]}")
    print(f"of the {gap['offered_and_kept']} offered and kept, the retire list "
          f"came back empty in {empty_retire_list}")
    for n in kept:
        best = min((s["rank"] for s in n["survivors"] if s["rank"]), default=None)
        print(f"  {n['turn']}  (old: {n['old_value']}, rank {best}, "
              f"retire list empty: {n['retire_list_empty']})")
    print(f"\nwrote {path}")


if __name__ == "__main__":
    main()
