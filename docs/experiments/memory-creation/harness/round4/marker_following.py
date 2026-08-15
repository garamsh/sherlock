"""How much of the corrected column is judgment, and how much is the marker.

Every correction template in `dataset.py` announces the change in words —
`switched`, `moved`, `now`, `these days`, `instead`. The initial templates
carry none, so the benchmark never presents a correction that is not announced
and `latest_wins` may be measuring marker-following rather than supersession
judgment.

Each correction turn gets an unmarked twin: the same fact restated with the new
value in the initial template that planted the old one, so "Alma drinks coffee
every morning." is contradicted by "Alma drinks chai every morning." and by
nothing else.

The two effects are measured apart:

  judgment  - the offer is held fixed at what the marked turn retrieved, and
              the decider is asked once with the marked text and once with the
              unmarked text. Same candidates, same ids, one word of difference.
  retrieval - what the unmarked text retrieves at that same store state, and
              whether the contradicted row is still among the candidates.

Only the marked reply is applied, so the store trajectory stays B9's and every
marked call is served from the cache. The unmarked replies are scored and
discarded.

This script spends live decider calls: one per correction turn, plus one
embedding batch for the unmarked sentences. The replay pass refuses live
requests, so a changed prompt fails there rather than silently re-buying the
cached half.
"""
from __future__ import annotations
import json
import os
import random
import sys
from collections import Counter, defaultdict

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import api
from api import cosine, decider, embed, embed_batch, flush, parse_json
from dataset import build, CATEGORIES, PERSONS
from structures import DeciderNeighborsWide, RETIRE_SYS

# Results land beside the report, not beside the caller's shell.
RESULTS = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                       "..", "..", "results_round4")

FLUSH_EVERY = 25


def _refuse(url, payload, timeout=90):
    raise RuntimeError(f"live request to {url}: this pass is cache-only")


class Traced(DeciderNeighborsWide):
    """B9, keeping the offer it made and the reply it got for each turn."""

    def __init__(self, rnd, model=None):
        super().__init__(rnd, model=model)
        self.last = None

    def turn(self, text, is_fact):
        self.store.tick()
        rows = self.context_rows(text)
        listing = [{"id": r.id, "memory": r.content} for r in rows]
        out = decider(RETIRE_SYS,
                      f"Latest message: {text}\nRelated memories: {listing}")
        self.calls += 1
        self.last = {"listing": listing, "reply": out}
        ops = parse_json(out)
        for rid in ops.get("retire", []) or []:
            for r in rows:
                if r.id == rid and not r.dead:
                    r.dead = True
        for fact in ops.get("store", []) or []:
            if isinstance(fact, str) and fact.strip():
                self.store.add(fact.strip())


def category_index():
    """Probe question text -> (category name, initial and correction forms)."""
    out = {}
    for person in PERSONS:
        for name, question, values, inits, corrs in CATEGORIES:
            out[question.format(p=person)] = (name, inits, corrs)
    return out


def unmarked_form(probe, inits, fact_turns):
    """The old fact's own sentence, restated with the new value.

    The initial template that planted the old value is the one whose rendering
    is in the stream, so the unmarked turn differs from the row it contradicts
    in the value alone.
    """
    old, new = probe.expect_absent[0], probe.expect_present[0]
    for tmpl in inits:
        if tmpl.format(p=probe.subject, v=old) in fact_turns:
            return tmpl.format(p=probe.subject, v=new), tmpl
    return inits[0].format(p=probe.subject, v=new), inits[0]


def holds(row, subject, value):
    content = row.content.lower()
    return subject.lower() in content and value.lower() in content


def retired_ids(reply):
    ops = parse_json(reply)
    return {rid for rid in (ops.get("retire", []) or []) if isinstance(rid, int)}


def rank_of(store, text, subject, value):
    """Rank and cosine of the best live row holding the value, or None."""
    qv = embed(text)
    ranking = sorted(((cosine(qv, r.vec), r) for r in store.live()),
                     key=lambda x: -x[0])
    for i, (score, row) in enumerate(ranking):
        if holds(row, subject, value):
            return i + 1, round(score, 6)
    return None, None


def replay(seed, turns, plan):
    """Cache-only pass: B9's trajectory, with each correction turn's offer."""
    inst = Traced(random.Random(seed))
    trials = []
    for text, is_fact in turns:
        item = plan.get(text)
        if item is None:
            inst.turn(text, is_fact)
            continue

        subject, old = item["subject"], item["old_value"]
        offer = inst.store.rank(text, inst.WIDE_K, floor=inst.WIDE_FLOOR)
        offered_old = [r.id for r in offer if holds(r, subject, old)]
        live_old = [r for r in inst.store.live() if holds(r, subject, old)]
        marked_rank, marked_cos = rank_of(inst.store, text, subject, old)
        un_offer = inst.store.rank(item["unmarked"], inst.WIDE_K,
                                   floor=inst.WIDE_FLOOR)
        un_offered_old = [r.id for r in un_offer if holds(r, subject, old)]
        un_rank, un_cos = rank_of(inst.store, item["unmarked"], subject, old)

        inst.turn(text, is_fact)
        trials.append({**item,
                       "live_rows_holding_old": len(live_old),
                       "offered_old_ids": offered_old,
                       "marked_rank": marked_rank, "marked_cosine": marked_cos,
                       "marked_offer_size": len(offer),
                       "unmarked_offered_old_ids": un_offered_old,
                       "unmarked_rank": un_rank, "unmarked_cosine": un_cos,
                       "unmarked_offer_size": len(un_offer),
                       "listing": inst.last["listing"],
                       "marked_reply": inst.last["reply"]})
    return trials


def ask_unmarked(trials):
    """Live pass: the same offer, the unmarked sentence."""
    for i, t in enumerate(trials, 1):
        t["unmarked_reply"] = decider(
            RETIRE_SYS,
            f"Latest message: {t['unmarked']}\nRelated memories: {t['listing']}")
        if i % FLUSH_EVERY == 0:
            flush()
    flush()


def judge(trials):
    """Score both arms on the rows the fixed offer actually carried."""
    for t in trials:
        offered = set(t["offered_old_ids"])
        t["judgeable"] = bool(offered)
        for arm in ("marked", "unmarked"):
            named = retired_ids(t[f"{arm}_reply"]) & offered
            t[f"{arm}_retired"] = bool(offered) and named == offered
            t[f"{arm}_retired_any"] = bool(named)


def rates(trials, key):
    judgeable = [t for t in trials if t["judgeable"]]
    return {"n": len(judgeable),
            "retired": sum(1 for t in judgeable if t[f"{key}_retired"]),
            "retired_any": sum(1 for t in judgeable if t[f"{key}_retired_any"]),
            "rate": round(sum(1 for t in judgeable if t[f"{key}_retired"])
                          / len(judgeable), 4) if judgeable else 0.0}


def breakdown(trials, field):
    out = {}
    groups = defaultdict(list)
    for t in trials:
        if t["judgeable"]:
            groups[t[field]].append(t)
    for name, group in sorted(groups.items()):
        marked = sum(1 for t in group if t["marked_retired"])
        unmarked = sum(1 for t in group if t["unmarked_retired"])
        out[name] = {"n": len(group), "marked": marked, "unmarked": unmarked,
                     "delta": unmarked - marked}
    return out


def main():
    seed = int(os.environ.get("SEED", "7"))
    turns, probes = build(seed=seed)
    embed_batch([t for t, _ in turns] + [p.question for p in probes])

    fact_turns = {t for t, is_fact in turns if is_fact}
    index = category_index()
    plan = {}
    for p in probes:
        if p.kind != "corrected":
            continue
        name, inits, corrs = index[p.question]
        text, tmpl = unmarked_form(p, inits, fact_turns)
        marked_tmpl = next(
            c for c in corrs
            if c.format(p=p.subject, v=p.expect_present[0]) == p.correction_text)
        plan[p.correction_text] = {
            "category": name, "subject": p.subject,
            "old_value": p.expect_absent[0], "new_value": p.expect_present[0],
            "marked": p.correction_text, "unmarked": text,
            "marked_template": marked_tmpl, "unmarked_template": tmpl}

    embeds_before = api.API_CALLS["embed"]
    embed_batch([item["unmarked"] for item in plan.values()])
    embed_batches = api.API_CALLS["embed"] - embeds_before

    # The marked half is already bought; a changed prompt must fail here
    # rather than re-buy it.
    live_post = api._post
    api._post = _refuse
    trials = replay(seed, turns, plan)
    api._post = live_post

    chat_before = api.API_CALLS["chat"]
    ask_unmarked(trials)
    chat_calls = api.API_CALLS["chat"] - chat_before
    judge(trials)

    marked, unmarked = rates(trials, "marked"), rates(trials, "unmarked")
    judgeable = [t for t in trials if t["judgeable"]]
    swing = Counter()
    for t in judgeable:
        swing[f"marked_{'retired' if t['marked_retired'] else 'kept'}"
              f"_unmarked_{'retired' if t['unmarked_retired'] else 'kept'}"] += 1

    with_live_row = [t for t in trials if t["live_rows_holding_old"]]
    retrieval = {
        "corrections": len(trials),
        "no_live_row_for_old": len(trials) - len(with_live_row),
        "marked_offered": sum(1 for t in with_live_row if t["offered_old_ids"]),
        "unmarked_offered": sum(1 for t in with_live_row
                                if t["unmarked_offered_old_ids"]),
        "marked_rank_1": sum(1 for t in with_live_row if t["marked_rank"] == 1),
        "unmarked_rank_1": sum(1 for t in with_live_row
                               if t["unmarked_rank"] == 1),
        "mean_marked_rank": round(sum(t["marked_rank"] for t in with_live_row)
                                  / len(with_live_row), 2),
        "mean_unmarked_rank": round(sum(t["unmarked_rank"]
                                        for t in with_live_row)
                                    / len(with_live_row), 2),
    }

    summary = {
        "seed": seed, "structure": DeciderNeighborsWide.NAME,
        "live_chat_calls": chat_calls, "live_embed_batches": embed_batches,
        "judgment": {"offer": "held fixed at what the marked turn retrieved",
                     "marked": marked, "unmarked": unmarked,
                     "paired_outcomes": dict(swing)},
        "retrieval": retrieval,
        "by_category": breakdown(trials, "category"),
        "by_correction_template": breakdown(trials, "marked_template"),
        "trials": [{k: v for k, v in t.items() if k != "listing"}
                   for t in trials],
    }

    os.makedirs(RESULTS, exist_ok=True)
    path = os.path.join(RESULTS, f"marker_following_seed{seed}.json")
    with open(path, "w") as f:
        json.dump(summary, f, indent=2)

    print(f"=== marker following, seed {seed} ===")
    print(f"live decider calls spent: {chat_calls}; "
          f"embedding batches: {embed_batches}")
    print(f"\njudgment, offer held fixed, {marked['n']} corrections whose offer "
          f"carried the contradicted row")
    print(f"  marked   retired {marked['retired']}/{marked['n']} "
          f"= {marked['rate']}")
    print(f"  unmarked retired {unmarked['retired']}/{unmarked['n']} "
          f"= {unmarked['rate']}")
    print(f"  paired outcomes: {dict(swing)}")
    print(f"\nretrieval, {retrieval['corrections']} correction turns "
          f"({retrieval['no_live_row_for_old']} had no live row holding the old "
          f"value)")
    print(f"  contradicted row offered: marked {retrieval['marked_offered']}, "
          f"unmarked {retrieval['unmarked_offered']} "
          f"(of {len(with_live_row)})")
    print(f"  at rank 1: marked {retrieval['marked_rank_1']}, "
          f"unmarked {retrieval['unmarked_rank_1']}; mean rank "
          f"{retrieval['mean_marked_rank']} -> "
          f"{retrieval['mean_unmarked_rank']}")
    print("\n| category | n | marked | unmarked | delta |")
    print("|---|---|---|---|---|")
    for name, row in summary["by_category"].items():
        print(f"| {name} | {row['n']} | {row['marked']} | {row['unmarked']} "
              f"| {row['delta']:+d} |")
    print(f"\nwrote {path}")


if __name__ == "__main__":
    main()
