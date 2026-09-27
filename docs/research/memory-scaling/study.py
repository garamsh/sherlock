"""Rank every #901 arm at every scale over recorded summaries, as preregistration.md fixes.

    uv run --no-project --with numpy==2.3.3 python3 docs/research/memory-scaling/study.py [--without-rerank]

Embeddings and reranking call this study's own text-embeddings-inference instances at EMBED_URL and
RERANK_URL; answers are cached beside this file under names .gitignore excludes. No model is called.
"""
import base64
import json
from pathlib import Path
import re
import sqlite3
import sys
import time
import urllib.request

HERE = Path(__file__).resolve().parent
RESEARCH = HERE.parent
REPLICATION = RESEARCH / 'mid-memory-rag' / 'evidence' / 'replication'
PARITY = RESEARCH / 'mid-memory-rag' / 'evidence' / 'production-parity' / 'rankings.json'
sys.path.insert(0, str(RESEARCH / 'mid-memory-rag'))
from replication import redact  # noqa: E402

EVIDENCE = HERE / 'evidence'
EMBED_CACHE = HERE / '.embed_cache-901.json'
RERANK_CACHE = HERE / '.embed_cache-901-rerank.json'
EMBED_URL = 'http://127.0.0.1:8130'
RERANK_URL = 'http://127.0.0.1:8131'
EMBED_MODEL = 'BAAI/bge-base-en-v1.5'

PRIMARY = ['exact', 'paraphrase', 'implicit', 'correction']
STREAM_MESSAGES = 4800
# (label, recorded batch-4 stream, its corpus) in the order the scales add them.
STREAMS = [
    ('#874', 'chained-recall/evidence/synthesis/stream-b4.jsonl', 'chained-recall/corpus.json'),
    ('#885', 'chained-recall/confirm/evidence/synthesis/stream-b4.jsonl', 'chained-recall/confirm/corpus.json'),
    ('#888', 'chained-recall/readtime/evidence/synthesis/stream-b4.jsonl', 'chained-recall/readtime/corpus.json'),
    ('#890', 'memory-encoding/validation/evidence/synthesis/stream-b4.jsonl', 'memory-encoding/validation/corpus.json'),
]
MATCHED_B8 = 'chained-recall/evidence/synthesis/stream-b8.jsonl'
SCALES = ['s0', 's1', 's2', 's3', 's4']
ARMS = ['S4', 'B4', 'B8', 'B16', 'T', 'K', 'X', 'R']
RECENT = 2
SHIPPED_TOTAL = 7
DEPTH = 50
RERANK_DEPTH = 30
RRF_K = 60
AGED_WINDOW = 2400
TAG = ' [mid:{id} sources:{first}-{last}; summary, not verified source text]'
STOP = frozenset('''a about after all am an and any are as at be been before but by can could did do does
for from had has have how i if in into is it its me my of on or our should so than that the their them then
there these they this to up was we were what when where which who why will with would you your'''.split())


class Unit:
    """One stored unit: its source interval, the text delivered, and the text its ranking key embeds."""

    def __init__(self, first, last, text, key=None):
        self.first, self.last, self.text, self.key = first, last, text, key or text
        self.id = 0

    def size(self):
        return len(self.text) + len(TAG.format(id=self.id, first=self.first, last=self.last))


def jsonl(path):
    return [json.loads(line) for line in Path(path).read_text().splitlines() if line.strip()]


def covers(spans, evidence):
    return any(first <= e <= last for first, last in spans for e in evidence)


def deliver(recency, ranked, budget, depth=DEPTH):
    """The budgeted fill: recency, then the first `depth` ranked units, each once, skipping any that would
    take the turn's characters above the budget."""
    out, seen, total = [], set(), 0
    for unit in list(recency) + list(ranked[:depth]):
        if id(unit) in seen:
            continue
        cost = unit.size()
        if total + cost > budget:
            continue
        seen.add(id(unit))
        out.append(unit)
        total += cost
    return out


def shipped(recency, ranked):
    """internal/agent/memory/conversation.go Recall: recent, then the top 7 similar, deduplicated, at most 7."""
    out, seen = [], set()
    for unit in list(recency) + list(ranked[:SHIPPED_TOTAL]):
        if id(unit) in seen:
            continue
        seen.add(id(unit))
        out.append(unit)
        if len(out) == SHIPPED_TOTAL:
            break
    return out


def values_in_order(keys, value_of):
    """K's ranking: each ranked key delivers the value covering it, each value once, in first-hit order."""
    out, seen = [], set()
    for key in keys:
        value = value_of[id(key)]
        if id(value) not in seen:
            seen.add(id(value))
            out.append(value)
    return out


def leak_patterns(keys):
    return [re.compile(r'(?<![A-Za-z0-9])' + re.escape(k) + r'(?![A-Za-z0-9])', re.I) for k in sorted({k for k in keys if k})]


def leaked(texts, patterns):
    """The first planted value found in any of the texts, or None."""
    for pattern in patterns:
        for text in texts:
            if pattern.search(text):
                return pattern.pattern
    return None


def lexical_query(text):
    tokens = [t for t in re.findall(r'[a-z0-9]+', text.lower()) if len(t) >= 2 and t not in STOP]
    return ' OR '.join(f'"{t}"' for t in dict.fromkeys(tokens))


def fuse(dense, lexical, dense_rank, k=RRF_K, depth=DEPTH):
    """Reciprocal rank fusion of the two top-`depth` lists; ties go to the better dense rank."""
    score = {}
    for ranked in (dense[:depth], lexical[:depth]):
        for rank, unit in enumerate(ranked, 1):
            score[id(unit)] = score.get(id(unit), 0.0) + 1.0 / (k + rank)
    units = {id(u): u for u in dense[:depth] + lexical[:depth]}
    return sorted(units.values(), key=lambda u: (-score[id(u)], dense_rank[id(u)]))


def aged(rep4, rep16, newest, window=AGED_WINDOW):
    """T's replication part: a B16 batch older than the store's newest `window` messages, else its B4 batches."""
    cutoff = newest - window
    old = [u for u in rep16 if u.last <= cutoff]
    return old + [u for u in rep4 if u.first > (old[-1].last if old else 0)]


def number(units):
    """A store: copies of the units in source order, numbered from 1 as the production store numbers mids."""
    out = [Unit(u.first, u.last, u.text, u.key) for u in sorted(units, key=lambda u: u.first)]
    for i, unit in enumerate(out, 1):
        unit.id = i
    return out


# Loading.

def replication_units(n):
    records = sorted(jsonl(REPLICATION / f'synthesis-n{n}.jsonl'), key=lambda r: r['batch'])
    assert [r['batch'] for r in records] == list(range(1, STREAM_MESSAGES // n + 1))
    return [Unit((r['batch'] - 1) * n + 1, r['batch'] * n, redact(r['summary']).strip()) for r in records]


def load_pool(patterns):
    """Each stream's kept batch-4 mids renumbered to follow the replication and earlier streams, with the
    leak check's record, and the matched batch-8 mids of the first stream."""
    streams, offset, report = [], STREAM_MESSAGES, []
    for label, stream_path, corpus_path in STREAMS:
        messages = {m['id']: m for m in json.loads((RESEARCH / corpus_path).read_text())['stream']}
        low = min(messages)
        assert low == STREAM_MESSAGES + 1 and sorted(messages) == list(range(low, low + len(messages)))
        kept, dropped = [], []
        for r in sorted(jsonl(RESEARCH / stream_path), key=lambda r: r['first']):
            sources = [messages[i]['content'] for i in range(r['first'], r['last'] + 1)]
            text = redact(r['summary']).strip()
            hit = leaked([text] + sources, patterns)
            first, last = r['first'] - STREAM_MESSAGES + offset, r['last'] - STREAM_MESSAGES + offset
            if hit:
                dropped.append({'first': first, 'last': last, 'pattern': hit})
            else:
                kept.append(Unit(first, last, text))
        chars = sum(len(m['content']) for m in messages.values())
        streams.append({'label': label, 'offset': offset, 'messages': len(messages), 'chars': chars,
                        'kept': kept, 'contents': [m['content'] for m in messages.values()]})
        report.append({'stream': label, 'file': stream_path, 'messages': len(messages), 'source_chars': chars,
                       'mids': len(kept) + len(dropped), 'kept': len(kept), 'dropped': dropped})
        offset += len(messages)
    first = streams[0]
    messages = {m['id']: m for m in json.loads((RESEARCH / STREAMS[0][2]).read_text())['stream']}
    b8, b8_dropped = [], []
    for r in sorted(jsonl(RESEARCH / MATCHED_B8), key=lambda r: r['first']):
        text = redact(r['summary']).strip()
        hit = leaked([text] + [messages[i]['content'] for i in range(r['first'], r['last'] + 1)], patterns)
        span = (r['first'] - STREAM_MESSAGES + first['offset'], r['last'] - STREAM_MESSAGES + first['offset'])
        (b8_dropped.append({'first': span[0], 'last': span[1], 'pattern': hit}) if hit else b8.append(Unit(*span, text)))
    report.append({'stream': '#874 batch 8', 'file': MATCHED_B8, 'messages': first['messages'],
                   'source_chars': first['chars'], 'mids': len(b8) + len(b8_dropped), 'kept': len(b8),
                   'dropped': b8_dropped})
    return streams, b8, report


# Servers.

def post(url, body):
    request = urllib.request.Request(url, json.dumps(body).encode(), {'Content-Type': 'application/json'})
    with urllib.request.urlopen(request, timeout=600) as response:
        return json.loads(response.read())


def info(url):
    with urllib.request.urlopen(url + '/info', timeout=30) as response:
        return json.loads(response.read())


def embed_all(texts):
    """float32 vectors from the embedding server's OpenAI route, as the production embedder calls it."""
    import numpy as np
    cache = json.loads(EMBED_CACHE.read_text()) if EMBED_CACHE.exists() else {}
    todo = [t for t in dict.fromkeys(texts) if t not in cache]
    for i in range(0, len(todo), 16):
        chunk = todo[i:i + 16]
        answer = post(EMBED_URL + '/v1/embeddings', {'model': EMBED_MODEL, 'input': chunk})
        for text, row in zip(chunk, sorted(answer['data'], key=lambda d: d['index'])):
            cache[text] = base64.b64encode(np.asarray(row['embedding'], dtype=np.float32).tobytes()).decode()
        if i % 640 == 0:
            print(f'embedded {i + len(chunk)} of {len(todo)}', flush=True)
    if todo:
        EMBED_CACHE.write_text(json.dumps(cache))
    return {t: np.frombuffer(base64.b64decode(cache[t]), dtype=np.float32) for t in texts}


class Reranker:
    def __init__(self):
        self.cache = json.loads(RERANK_CACHE.read_text()) if RERANK_CACHE.exists() else {}
        self.calls = 0

    def order(self, query, units):
        missing = [u.text for u in units if json.dumps([query, u.text]) not in self.cache]
        missing = list(dict.fromkeys(missing))
        if missing:
            self.calls += 1
            for row in post(RERANK_URL + '/rerank', {'query': query, 'texts': missing, 'truncate': True}):
                self.cache[json.dumps([query, missing[row['index']]])] = row['score']
        scores = [self.cache[json.dumps([query, u.text])] for u in units]
        return [u for _, _, u in sorted(zip([-s for s in scores], range(len(units)), units), key=lambda x: x[:2])]

    def save(self):
        RERANK_CACHE.write_text(json.dumps(self.cache))


# Ranking.

class Similarity:
    def __init__(self, queries, units):
        import numpy as np
        self.np = np
        texts = [q['text'] for q in queries] + [u.key for u in units]
        vectors = embed_all(texts)
        self.row = {q['id']: i for i, q in enumerate(queries)}
        keys = list(dict.fromkeys(u.key for u in units))
        self.col = {k: i for i, k in enumerate(keys)}

        def unit_matrix(rows):
            m = np.stack([vectors[t] for t in rows]).astype(np.float64)
            return m / np.linalg.norm(m, axis=1, keepdims=True)
        self.matrix = unit_matrix([q['text'] for q in queries]) @ unit_matrix(keys).T

    def rank(self, query, units):
        """Cosine descending, ties newest first, as production Search orders them."""
        scores = self.matrix[self.row[query], [self.col[u.key] for u in units]]
        order = self.np.lexsort(([-u.id for u in units], -scores))
        return [units[i] for i in order]

    def top(self, query, units):
        return float(self.matrix[self.row[query], [self.col[u.key] for u in units]].max())


def lexical(store, queries):
    db = sqlite3.connect(':memory:')
    db.execute('CREATE VIRTUAL TABLE mid USING fts5(summary)')
    db.executemany('INSERT INTO mid(rowid, summary) VALUES (?, ?)', [(u.id, u.text) for u in store])
    by_id = {u.id: u for u in store}
    out = {}
    for q in queries:
        expression = lexical_query(q['text'])
        rows = db.execute('SELECT rowid FROM mid WHERE mid MATCH ? ORDER BY bm25(mid), rowid DESC LIMIT ?',
                          (expression, DEPTH)).fetchall() if expression else []
        out[q['id']] = [by_id[r[0]] for r in rows]
    db.close()
    return out


def first_rank(ranked, evidence):
    for i, unit in enumerate(ranked, 1):
        if covers([(unit.first, unit.last)], evidence):
            return i
    return None




def scale_stores(k, rep, streams, b8m):
    """Every arm's store at scale k: the replication part the arm keeps, then the distractors."""
    distract = [u for s in streams[:k] for u in s['kept']]
    messages = STREAM_MESSAGES + sum(s['messages'] for s in streams[:k])
    parts = {'B4': rep[4], 'B8': rep[8], 'B16': rep[16], 'T': aged(rep[4], rep[16], messages), 'K': rep[16]}
    if k == 1:
        parts['B8m'] = rep[8]
    stores = {arm: number(list(part) + (b8m if arm == 'B8m' else distract)) for arm, part in parts.items()}
    return parts, stores, messages


def owners(keys, values):
    """K's map from each key to the stored value whose interval covers it."""
    owner = {}
    for value in values:
        for source in range(value.first, value.last + 1):
            owner[source] = value
    return {id(key): owner[key.first] for key in keys}


def rankings(q, stores, sim, lex, reranker, value_of):
    """Each arm's recency units and ranked list for one query."""
    b4 = stores['B4']
    newest = b4[-RECENT:][::-1]
    dense = sim.rank(q['id'], b4)
    dense_rank = {id(u): i for i, u in enumerate(dense)}
    out = {'S4': (newest, dense), 'B4': (newest, dense),
           'X': (newest, fuse(dense, lex[q['id']], dense_rank)),
           'K': (stores['K'][-RECENT:][::-1], values_in_order(dense, value_of))}
    if reranker:
        out['R'] = (newest, reranker.order(q['text'], dense[:RERANK_DEPTH]) + dense[RERANK_DEPTH:])
    for arm in ('B8', 'B16', 'T', 'B8m'):
        if arm in stores:
            out[arm] = (stores[arm][-RECENT:][::-1], sim.rank(q['id'], stores[arm]))
    return out


def main(rerank=True):
    queries = json.loads((REPLICATION / 'queries.json').read_text())
    items = {i['id']: i for family in json.loads((REPLICATION / 'items.json').read_text()).values() for i in family}
    planted = [v for q in queries if q['item'] in items
               for v in (items[q['item']].get('key'), items[q['item']].get('old_key'), items[q['item']].get('new_key'))]
    patterns = leak_patterns(planted)
    corpus = json.loads((REPLICATION / 'corpus.json').read_text())
    rep_chars = sum(len(m['content']) for m in corpus)
    rep = {n: replication_units(n) for n in (4, 8, 16)}
    streams, b8m, pool = load_pool(patterns)
    primary = [q for q in queries if q['family'] in PRIMARY]

    distractors = [u for s in streams for u in s['kept']] + b8m
    gold = {e for q in queries for e in q['evidence'] + q['obsolete']}
    gold_texts = {corpus[e - 1]['content'] for q in queries for e in q['evidence']}
    assert all(corpus[i]['id'] == i + 1 for i in range(len(corpus)))
    controls = {
        'planted_values': len(patterns),
        'distractors_outside_gold_ids': all(not any(u.first <= e <= u.last for e in gold) for u in distractors),
        'distractor_messages_with_gold_text': sum(c in gold_texts for s in streams for c in s['contents']),
        'servers': {'embed': info(EMBED_URL), 'rerank': info(RERANK_URL) if rerank else None},
    }

    sim = Similarity(queries, rep[4] + rep[8] + rep[16] + distractors)
    reranker = Reranker() if rerank else None
    started = time.time()
    rows, storage, reach, budget = [], [], {}, None
    for k, scale in enumerate(SCALES):
        parts, stores, messages = scale_stores(k, rep, streams, b8m)
        source_chars = rep_chars + sum(s['chars'] for s in streams[:k])
        for arm, store in stores.items():
            storage.append({'scale': scale, 'arm': arm, 'units': len(store), 'replication_units': len(parts[arm]),
                            'stored_chars': sum(len(u.text) for u in store),
                            'replication_stored_chars': sum(len(u.text) for u in parts[arm]),
                            'source_messages': messages, 'source_chars': source_chars,
                            'replication_source_chars': rep_chars})
            reach[f'{scale}/{arm}'] = all(any(covers([(u.first, u.last)], q['evidence']) for u in store) for q in primary)
        lex = lexical(stores['B4'], queries)
        value_of = owners(stores['B4'], stores['K'])
        ranked = {q['id']: rankings(q, stores, sim, lex, reranker, value_of) for q in queries}
        if reranker:
            reranker.save()
        if budget is None:
            budget = sum(u.size() for q in primary for u in shipped(*ranked[q['id']]['S4'])) / len(primary)
        for q in queries:
            for arm, (recent, order) in ranked[q['id']].items():
                delivered = shipped(recent, order) if arm == 'S4' else deliver(recent, order, budget)
                assert arm == 'S4' or sum(u.size() for u in delivered) <= budget
                rows.append({'scale': scale, 'arm': arm, 'query': q['id'],
                             'delivered': [[u.first, u.last] for u in delivered],
                             'chars': sum(u.size() for u in delivered),
                             'rank': first_rank(order, q['evidence']) if q['evidence'] else None})
        print(f'{scale}: {messages} messages, ranked at {time.time() - started:.0f}s', flush=True)
    controls['reach'] = reach

    parity = {r['id']: r['delivered'] for r in json.loads(PARITY.read_text())}
    s4 = {r['query']: r['delivered'] for r in rows if r['arm'] == 'S4' and r['scale'] == 's0'}
    controls['parity_identical_deliveries'] = sum(s4[i] == parity[i] for i in parity)
    controls['parity_differing'] = sorted(i for i in parity if s4[i] != parity[i])

    evidence_units = {id(u) for u in rep[4] for q in primary if covers([(u.first, u.last)], q['evidence'])}
    filler = [u for u in rep[4] if id(u) not in evidence_units]
    kept = [u for s in streams for u in s['kept']]
    tops = [(sim.top(q['id'], kept), sim.top(q['id'], filler)) for q in primary]
    resemblance = {
        'filler_mids': len(filler), 'distractor_mids': len(kept),
        'chars_per_source_message': {'replication': rep_chars / len(corpus),
                                     'distractors': sum(s['chars'] for s in streams) / sum(s['messages'] for s in streams)},
        'chars_per_summary': {'filler': sum(len(u.text) for u in filler) / len(filler),
                              'distractors': sum(len(u.text) for u in kept) / len(kept)},
        'mean_top1_cosine': {'distractors': sum(t[0] for t in tops) / len(tops),
                             'filler': sum(t[1] for t in tops) / len(tops)},
        'queries_where_distractor_top1_is_higher': sum(t[0] > t[1] for t in tops),
    }
    EVIDENCE.mkdir(exist_ok=True)
    with (EVIDENCE / 'rankings.jsonl').open('w') as out:
        for row in rows:
            out.write(json.dumps(row) + '\n')
    study = {'budget': budget, 'controls': controls, 'pool': pool, 'storage': storage, 'resemblance': resemblance,
             'run': {'rank_seconds': time.time() - started, 'rerank_calls': reranker.calls if reranker else None}}
    (EVIDENCE / 'study.json').write_text(json.dumps(study, indent=1) + '\n')
    print(json.dumps({k: v for k, v in controls.items() if k not in ('servers', 'reach')}),
          'reach all:', all(reach.values()), 'budget', round(budget, 1))


if __name__ == '__main__':
    main(rerank='--without-rerank' not in sys.argv[1:])
