"""What the corrected column responds to: the change marker, or the surface.

Every correction template in `dataset.py` announces the change in words —
`switched`, `moved`, `now`, `these days`, `instead`. The initial templates
carry none, so the benchmark only ever presents announced corrections.

Each correction turn is put to the decider in four phrasings of the same fact,
which vary the marker and the surface distance from the row they contradict
independently:

  A  the marked correction the dataset writes
  B  the stored sentence with the new value, no marker
  C  B with a temporal marker prepended, so C against B is the marker alone
  D  the category's other initial template with the new value, no marker

The two effects are measured apart:

  judgment  - the offer is held fixed at what the marked turn retrieved, and
              the decider is asked once per arm against that same candidate
              list. Same ids, same order, only the sentence differs.
  retrieval - what each arm's text retrieves at that same store state, and
              whether the contradicted row is still among the candidates.

The outcome is then read against the cosine between each arm's text and the
row it contradicts, with marker presence as the second factor, so a decider
that tracks surface similarity is distinguishable from one that judges
supersession.

Only arm A's reply is applied, so the store trajectory stays B9's and every
arm A call is served from the cache. The other replies are scored and
discarded.

Two guards, because the arms B, C and D cost live decider calls. The replay
pass refuses live requests, so a changed prompt fails there rather than
re-buying the cached half. The null check then asks every arm with arm A's own
text, which is a cache hit, and requires all four to return A's retire count
before anything is bought.
"""
from __future__ import annotations
import json
import math
import os
import random
import statistics
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

# (arm, what the sentence is, whether it announces the change)
ARMS = (("A", "the marked correction the dataset writes", True),
        ("B", "the stored sentence carrying the new value", False),
        ("C", "B with a temporal marker prepended", True),
        ("D", "the category's other initial template", False))
ARM_KEYS = tuple(arm for arm, _, _ in ARMS)
MARKED = {arm: marked for arm, _, marked in ARMS}
MARKER_PREFIX = "These days, "

FLUSH_EVERY = 25
BANDS = 6


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


def planted_template(probe, inits, fact_turns):
    """The initial template that planted the old value, and the other one."""
    for tmpl in inits:
        if tmpl.format(p=probe.subject, v=probe.expect_absent[0]) in fact_turns:
            other = next((t for t in inits if t != tmpl), tmpl)
            return tmpl, other
    return inits[0], inits[-1]


def arm_texts(probe, inits, fact_turns):
    """The four phrasings of one correction, and the templates behind them."""
    planted, other = planted_template(probe, inits, fact_turns)
    new = probe.expect_present[0]
    parallel = planted.format(p=probe.subject, v=new)
    return ({"A": probe.correction_text,
             "B": parallel,
             "C": MARKER_PREFIX + parallel,
             "D": other.format(p=probe.subject, v=new)},
            {"B": planted, "D": other})


def holds(row, subject, value):
    content = row.content.lower()
    return subject.lower() in content and value.lower() in content


def retired_ids(reply):
    ops = parse_json(reply)
    return {rid for rid in (ops.get("retire", []) or []) if isinstance(rid, int)}


def best_cosine(text, rows):
    """Cosine between the sentence and the nearest of the given rows."""
    qv = embed(text)
    return max((round(cosine(qv, r.vec), 6) for r in rows), default=None)


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
    """Cache-only pass: B9's trajectory, plus each arm's view of each turn."""
    inst = Traced(random.Random(seed))
    trials = []
    for text, is_fact in turns:
        item = plan.get(text)
        if item is None:
            inst.turn(text, is_fact)
            continue

        subject, old = item["subject"], item["old_value"]
        offer = inst.store.rank(text, inst.WIDE_K, floor=inst.WIDE_FLOOR)
        offered_old = [r for r in offer if holds(r, subject, old)]
        live_old = [r for r in inst.store.live() if holds(r, subject, old)]

        retrieval, cosines = {}, {}
        for arm in ARM_KEYS:
            arm_text = item["texts"][arm]
            rank, cos = rank_of(inst.store, arm_text, subject, old)
            arm_offer = inst.store.rank(arm_text, inst.WIDE_K,
                                        floor=inst.WIDE_FLOOR)
            retrieval[arm] = {
                "rank": rank, "cosine_to_nearest_live": cos,
                "offered": bool([r for r in arm_offer
                                 if holds(r, subject, old)]),
                "offer_size": len(arm_offer)}
            # The gradient's x: how close this phrasing is to the row whose
            # retirement is being judged.
            cosines[arm] = best_cosine(arm_text, offered_old)

        inst.turn(text, is_fact)
        trials.append({**item,
                       "live_rows_holding_old": len(live_old),
                       "offered_old_ids": [r.id for r in offered_old],
                       "cosine_to_contradicted": cosines,
                       "retrieval": retrieval,
                       "listing": inst.last["listing"],
                       "replies": {"A": inst.last["reply"]}})
    return trials


def ask(trials, arm, text_of):
    """Ask the decider one arm, against the offer the marked turn retrieved."""
    for i, t in enumerate(trials, 1):
        t["replies"][arm] = decider(
            RETIRE_SYS,
            f"Latest message: {text_of(t)}\nRelated memories: {t['listing']}")
        if i % FLUSH_EVERY == 0:
            flush()
    flush()


def judge(trials):
    """Score every arm on the rows the fixed offer actually carried."""
    for t in trials:
        offered = set(t["offered_old_ids"])
        t["judgeable"] = bool(offered)
        t["retired"] = {}
        for arm in ARM_KEYS:
            named = retired_ids(t["replies"][arm]) & offered
            t["retired"][arm] = bool(offered) and named == offered


def judgeable(trials):
    return [t for t in trials if t["judgeable"]]


def arm_rates(trials):
    out = {}
    rows = judgeable(trials)
    for arm, blurb, marked in ARMS:
        cosines = [t["cosine_to_contradicted"][arm] for t in rows]
        retired = sum(1 for t in rows if t["retired"][arm])
        out[arm] = {"phrasing": blurb, "marker": marked, "n": len(rows),
                    "retired": retired,
                    "rate": round(retired / len(rows), 4) if rows else 0.0,
                    "mean_cosine": round(statistics.fmean(cosines), 4),
                    "median_cosine": round(statistics.median(cosines), 4)}
    return out


def mcnemar(trials, left, right):
    """Paired outcomes for two arms on the same turns, with the exact p."""
    counts = Counter()
    for t in judgeable(trials):
        counts[(t["retired"][left], t["retired"][right])] += 1
    left_only = counts[(True, False)]
    right_only = counts[(False, True)]
    n = left_only + right_only
    lo = min(left_only, right_only)
    tail = sum(math.comb(n, k) for k in range(lo + 1)) / 2 ** n if n else 1.0
    return {"pair": f"{left} vs {right}", "left": left, "right": right,
            "both_retired": counts[(True, True)],
            "left_only": left_only, "right_only": right_only,
            "both_kept": counts[(False, False)],
            "discordant": n, "exact_p": round(min(1.0, 2 * tail), 4)}


def correlation(points):
    """Pearson r between cosine and the retire outcome."""
    if len(points) < 3:
        return None
    xs = [x for x, _ in points]
    ys = [1.0 if y else 0.0 for _, y in points]
    mx, my = statistics.fmean(xs), statistics.fmean(ys)
    num = sum((x - mx) * (y - my) for x, y in zip(xs, ys))
    den = math.sqrt(sum((x - mx) ** 2 for x in xs)
                    * sum((y - my) ** 2 for y in ys))
    return round(num / den, 4) if den else None


def gradient(trials):
    """Retire outcome against cosine, pooled over the arms.

    Bands are quantiles of the pooled cosine, so each carries the same number
    of points and the marker split inside a band compares like with like.
    """
    points = [(t["cosine_to_contradicted"][arm], t["retired"][arm],
               MARKED[arm], arm)
              for t in judgeable(trials) for arm in ARM_KEYS]
    points.sort()
    size = math.ceil(len(points) / BANDS)
    bands = []
    for start in range(0, len(points), size):
        chunk = points[start:start + size]
        marked = [p for p in chunk if p[2]]
        unmarked = [p for p in chunk if not p[2]]

        def rate(group):
            return (round(sum(1 for p in group if p[1]) / len(group), 4)
                    if group else None)

        bands.append({"cosine_from": round(chunk[0][0], 4),
                      "cosine_to": round(chunk[-1][0], 4),
                      "n": len(chunk), "retired": sum(1 for p in chunk if p[1]),
                      "rate": rate(chunk),
                      "marked_n": len(marked), "marked_rate": rate(marked),
                      "unmarked_n": len(unmarked),
                      "unmarked_rate": rate(unmarked)})
    return {
        "points": len(points),
        "r_overall": correlation([(x, y) for x, y, _, _ in points]),
        "r_marked": correlation([(x, y) for x, y, m, _ in points if m]),
        "r_unmarked": correlation([(x, y) for x, y, m, _ in points if not m]),
        "bands": bands,
    }


def by_category(trials):
    out = {}
    groups = defaultdict(list)
    for t in judgeable(trials):
        groups[t["category"]].append(t)
    for name, group in sorted(groups.items()):
        row = {"n": len(group)}
        for arm in ARM_KEYS:
            row[arm] = sum(1 for t in group if t["retired"][arm])
        out[name] = row
    return out


def retrieval_summary(trials):
    rows = [t for t in trials if t["live_rows_holding_old"]]
    out = {"corrections": len(trials),
           "with_a_live_row": len(rows),
           "no_live_row_for_old": len(trials) - len(rows)}
    for arm in ARM_KEYS:
        ranks = [t["retrieval"][arm]["rank"] for t in rows]
        out[arm] = {"offered": sum(1 for t in rows
                                   if t["retrieval"][arm]["offered"]),
                    "rank_1": sum(1 for r in ranks if r == 1),
                    "mean_rank": round(statistics.fmean(ranks), 2)}
    return out


def null_check(trials):
    """Every arm asked with arm A's text must return arm A's outcome.

    All four prompts are then arm A's prompt, so this is free, and it fails
    before anything is bought if the scoring or the offer has drifted.
    """
    for arm in ARM_KEYS[1:]:
        ask(trials, arm, lambda t: t["texts"]["A"])
    judge(trials)
    counts = {arm: sum(1 for t in judgeable(trials) if t["retired"][arm])
              for arm in ARM_KEYS}
    if len(set(counts.values())) != 1:
        raise SystemExit(f"null check failed, arms disagree on arm A's text: "
                         f"{counts}")
    n = len(judgeable(trials))
    print(f"null check passed before spending: every arm on arm A's text "
          f"retired {counts} of {n}", flush=True)
    return {"n": n, "retired_per_arm": counts}


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
        texts, templates = arm_texts(p, inits, fact_turns)
        plan[p.correction_text] = {
            "category": name, "subject": p.subject,
            "old_value": p.expect_absent[0], "new_value": p.expect_present[0],
            "texts": texts, "templates": templates}

    embeds_before = api.API_CALLS["embed"]
    embed_batch([text for item in plan.values()
                 for text in item["texts"].values()])
    embed_batches = api.API_CALLS["embed"] - embeds_before

    # Arm A is already bought; a changed prompt must fail here rather than
    # re-buy it.
    live_post = api._post
    api._post = _refuse
    trials = replay(seed, turns, plan)
    null = null_check(trials)
    api._post = live_post

    chat_before = api.API_CALLS["chat"]
    for arm in ARM_KEYS[1:]:
        ask(trials, arm, lambda t, a=arm: t["texts"][a])
    chat_calls = api.API_CALLS["chat"] - chat_before
    judge(trials)

    rates = arm_rates(trials)
    pairs = [mcnemar(trials, "A", arm) for arm in ("B", "C", "D")]
    pairs.append(mcnemar(trials, "C", "B"))
    summary = {
        "seed": seed, "structure": DeciderNeighborsWide.NAME,
        "live_chat_calls": chat_calls, "live_embed_batches": embed_batches,
        "null_check": null,
        "judgment": {"offer": "held fixed at what the marked turn retrieved",
                     "arms": rates, "paired": pairs},
        "gradient": gradient(trials),
        "retrieval": retrieval_summary(trials),
        "by_category": by_category(trials),
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
    print(f"\njudgment, offer held fixed, {rates['A']['n']} corrections whose "
          f"offer carried the contradicted row")
    print("| arm | marker | phrasing | retired | rate | mean cosine |")
    print("|---|---|---|---|---|---|")
    for arm in ARM_KEYS:
        r = rates[arm]
        print(f"| {arm} | {'yes' if r['marker'] else 'no'} | {r['phrasing']} "
              f"| {r['retired']}/{r['n']} | {r['rate']} | {r['mean_cosine']} |")
    print()
    print("| pair | both retired | left only | right only | both kept | "
          "discordant | exact p |")
    print("|---|---|---|---|---|---|---|")
    for p in pairs:
        print(f"| {p['pair']} | {p['both_retired']} | {p['left_only']} "
              f"| {p['right_only']} | {p['both_kept']} | {p['discordant']} "
              f"| {p['exact_p']} |")

    grad = summary["gradient"]
    print(f"\nretire outcome against cosine, {grad['points']} points over four "
          f"arms")
    print(f"  Pearson r: overall {grad['r_overall']}, "
          f"marked {grad['r_marked']}, unmarked {grad['r_unmarked']}")
    print("| cosine band | n | rate | marked n | marked rate | unmarked n | "
          "unmarked rate |")
    print("|---|---|---|---|---|---|---|")
    for b in grad["bands"]:
        print(f"| {b['cosine_from']}-{b['cosine_to']} | {b['n']} | {b['rate']} "
              f"| {b['marked_n']} | {b['marked_rate']} | {b['unmarked_n']} "
              f"| {b['unmarked_rate']} |")

    ret = summary["retrieval"]
    print(f"\nretrieval over the {ret['with_a_live_row']} corrections with a "
          f"live row holding the old value")
    print("| arm | offered | at rank 1 | mean rank |")
    print("|---|---|---|---|")
    for arm in ARM_KEYS:
        r = ret[arm]
        print(f"| {arm} | {r['offered']} | {r['rank_1']} | {r['mean_rank']} |")

    print("\n| category | n | " + " | ".join(ARM_KEYS) + " |")
    print("|" + "---|" * (len(ARM_KEYS) + 2))
    for name, row in summary["by_category"].items():
        print(f"| {name} | {row['n']} | "
              + " | ".join(str(row[arm]) for arm in ARM_KEYS) + " |")
    print(f"\nwrote {path}")


if __name__ == "__main__":
    main()
