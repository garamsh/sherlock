"""Rank #913's free screen, S4, B4 and Kr at s0 and s4, over #901's recorded material, as preregistration.md fixes.

    uv run --no-project --with numpy==2.3.3 python3 research/memory-scaling/live/screen.py

Embeddings come from this study's own text-embeddings-inference instance at EMBED_URL, cached beside this file
under a name .gitignore excludes. No model is called. Writes evidence/rankings.jsonl and evidence/screen.json.
"""
import json
from pathlib import Path
import sys
import time
import urllib.request

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent))
import study as scaling  # noqa: E402

EVIDENCE = HERE / 'evidence'
EMBED_URL = 'http://127.0.0.1:8132'
INFO_CACHE = HERE / '.embed_cache-913-info.json'
SCALES = {'s0': 0, 's4': 4}
RECENCY_SPAN = 4

scaling.EMBED_CACHE = HERE / '.embed_cache-913.json'
scaling.EMBED_URL = EMBED_URL


def kr_recency(newest, k_store):
    """Kr's recency: the store's 2 newest batch-4 units, taken as the K store's own unit where it holds the same
    interval (a distractor mid), so the fill delivers it once."""
    held = {(u.first, u.last): u for u in k_store}
    return [held.get((u.first, u.last), u) for u in newest]


def arms(qid, b4, k, sim, value_of):
    """Each arm's recency and ranking for one query; delivery is applied by the caller."""
    newest = b4[-scaling.RECENT:][::-1]
    dense = sim.rank(qid, b4)
    values = scaling.values_in_order(dense, value_of)
    return {'S4': (newest, dense), 'B4': (newest, dense), 'Kr': (kr_recency(newest, k), values)}


def info():
    """The server's /info, cached so a rerun from the embedding cache does not need the server up."""
    try:
        with urllib.request.urlopen(EMBED_URL + '/info', timeout=30) as response:
            INFO_CACHE.write_bytes(response.read())
    except OSError:
        if not INFO_CACHE.exists():
            raise
    return json.loads(INFO_CACHE.read_text())


def main():
    rep_dir = scaling.REPLICATION
    queries = json.loads((rep_dir / 'queries.json').read_text())
    items = {i['id']: i for family in json.loads((rep_dir / 'items.json').read_text()).values() for i in family}
    planted = [v for q in queries if q['item'] in items
               for v in (items[q['item']].get('key'), items[q['item']].get('old_key'), items[q['item']].get('new_key'))]
    patterns = scaling.leak_patterns(planted)
    corpus = json.loads((rep_dir / 'corpus.json').read_text())
    rep_chars = sum(len(m['content']) for m in corpus)
    rep = {n: scaling.replication_units(n) for n in (4, 16)}
    streams, _, pool = scaling.load_pool(patterns)
    primary = [q for q in queries if q['family'] in scaling.PRIMARY]
    near_miss_key = {q['id']: scaling.leak_patterns([items[q['item']]['key']])[0]
                     for q in queries if q['family'] == 'absent'}

    distractors = [u for s in streams for u in s['kept']]
    sim = scaling.Similarity(queries, rep[4] + distractors)
    started = time.time()
    rows, storage, controls, budget = [], [], {'reach': {}, 'kr_recency_spans': {}}, None
    for scale, k in SCALES.items():
        distract = [u for s in streams[:k] for u in s['kept']]
        b4, kv = scaling.number(rep[4] + distract), scaling.number(rep[16] + distract)
        value_of = scaling.owners(b4, kv)
        for arm, part, store in (('B4', rep[4], b4), ('Kr', rep[16], kv)):
            storage.append({'scale': scale, 'arm': arm, 'units': len(store), 'stored_chars': sum(len(u.text) for u in store),
                            'replication_stored_chars': sum(len(u.text) for u in part),
                            'replication_source_chars': rep_chars,
                            'source_chars': rep_chars + sum(s['chars'] for s in streams[:k])})
        controls['reach'][scale] = all(any(scaling.covers([(u.first, u.last)], q['evidence']) for u in kv) for q in primary)
        ranked = {q['id']: arms(q["id"], b4, kv, sim, value_of) for q in queries}
        if budget is None:
            budget = sum(u.size() for q in primary for u in scaling.shipped(*ranked[q['id']]['S4'])) / len(primary)
        spans = set()
        for q in queries:
            for arm, (recent, order) in ranked[q['id']].items():
                delivered = scaling.shipped(recent, order) if arm == 'S4' else scaling.deliver(recent, order, budget)
                assert arm == 'S4' or sum(u.size() for u in delivered) <= budget
                if arm == 'Kr':
                    spans |= {u.last - u.first + 1 for u in delivered if u in recent}
                row = {'scale': scale, 'arm': arm, 'query': q['id'],
                       'delivered': [[u.first, u.last] for u in delivered],
                       'chars': sum(u.size() for u in delivered),
                       'rank': scaling.first_rank(order, q['evidence']) if q['evidence'] else None}
                if q['id'] in near_miss_key:
                    row['states_near_miss'] = any(near_miss_key[q['id']].search(u.text) for u in delivered)
                rows.append(row)
        controls['kr_recency_spans'][scale] = sorted(spans)
        print(f'{scale}: ranked at {time.time() - started:.0f}s', flush=True)
    controls['server'] = info()

    EVIDENCE.mkdir(exist_ok=True)
    with (EVIDENCE / 'rankings.jsonl').open('w') as out:
        for row in rows:
            out.write(json.dumps(row) + '\n')
    screen = {'budget': budget, 'controls': controls, 'pool': [{k: v for k, v in p.items() if k != 'dropped'} for p in pool],
              'storage': storage, 'run': {'rank_seconds': time.time() - started}}
    (EVIDENCE / 'screen.json').write_text(json.dumps(screen, indent=1) + '\n')
    print('budget', round(budget, 1), 'reach', controls['reach'], 'recency spans', controls['kr_recency_spans'])


if __name__ == '__main__':
    main()
