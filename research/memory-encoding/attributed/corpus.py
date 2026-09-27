"""Build #916's corpus with #890's generator (../validation/corpus.py) at a new seed.

    python3 research/memory-encoding/attributed/corpus.py assemble   # -> candidates.json and corpus.json
    python3 research/memory-encoding/attributed/corpus.py select     # candidates + evidence/selection/check-b4.json -> corpus.json
    python3 research/memory-encoding/attributed/corpus.py fresh      # evidence/fresh.json: no probe seen before

The generator is imported unchanged; only its seed and output directory
differ. `fresh` compares every probe with the items of the corpora #874, #885,
#888 and #890 ran: under Amendment 2 a generated probe is seen when its
query and its answer (N and NC: its entity) both recur in one earlier item,
and no invented name may recur. The query-only comparison as preregistered
is kept under `as_written`. E and K are drawn from the replication's
fixed pool (70 each), so their overlap is reported and does not gate.
"""

import importlib.util
import json
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
RESEARCH = HERE.parents[1]
SEED = 916
EARLIER = ["chained-recall", "chained-recall/confirm", "chained-recall/readtime", "memory-encoding/validation"]
REPLICATION_FAMILIES = ("E", "K")


def generator():
    spec = importlib.util.spec_from_file_location("validation_corpus", HERE.parent / "validation" / "corpus.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    module.SEED, module.HERE = SEED, HERE
    return module


def probes(corpus):
    return [(x["id"], x["family"], x["query"]) for x in corpus["chains"] + corpus["guards"]]


def answer(x):
    """What a probe's item is about beyond its query (Amendment 2): the
    chain's answer, KU's new name, or the absent item's entity."""
    return x.get("answer") or (x["key"] if x["family"] == "KU" else x.get("entity") or "")


def items(corpus):
    return [(x["id"], x["family"], x["query"], answer(x)) for x in corpus["chains"] + corpus["guards"]]


def names(corpus):
    """The invented names a corpus's items carry."""
    out = set()
    for x in corpus["chains"]:
        out.add(x.get("start") or "")
        for h in x["hops"]:
            out.update(e for e in h.get("ends", []) if e and e[0].isupper())
    for g in corpus["guards"]:
        out.update(g.get(k) or "" for k in ("entity", "associated"))
        if g["family"] == "KU":
            out.update((g["key"], g["obsolete"]))
    return {n for n in out if n}


def fresh():
    ours = json.load(open(HERE / "corpus.json"))
    if ours.get("stage") != "selected":
        raise SystemExit("run after select")
    earlier_queries, earlier_names = {}, set()
    for d in EARLIER:
        c = json.load(open(RESEARCH / d / "corpus.json"))
        for _, _, q in probes(c):
            earlier_queries.setdefault(q, d)
        earlier_names |= names(c)
    seen = [{"probe": i, "family": f, "in": earlier_queries[q]} for i, f, q in probes(ours) if q in earlier_queries]
    generated = [s for s in seen if s["family"] not in REPLICATION_FAMILIES]
    shared_names = sorted(names(ours) & earlier_names)
    earlier_items = {(q, a) for d in EARLIER for _, _, q, a in items(json.load(open(RESEARCH / d / "corpus.json")))}
    seen_items = [{"probe": i, "family": f} for i, f, q, a in items(ours) if f not in REPLICATION_FAMILIES and (q, a) in earlier_items]
    out = {"earlier": EARLIER, "probes": len(probes(ours)),
           "names": len(names(ours)), "names_in_earlier_corpora": shared_names,
           "seen_items": seen_items,
           "seen_replication": {f: sum(s["family"] == f for s in seen) for f in REPLICATION_FAMILIES},
           "as_written": {"seen_generated": generated, "passed": not generated},
           "passed": not seen_items and not shared_names}
    (HERE / "evidence").mkdir(exist_ok=True)
    (HERE / "evidence" / "fresh.json").write_text(json.dumps(out, indent=1) + "\n")
    print(json.dumps({k: v if not isinstance(v, list) else len(v) for k, v in out.items()}))


if __name__ == "__main__":
    if sys.argv[1] == "fresh":
        fresh()
    else:
        getattr(generator(), sys.argv[1])()
