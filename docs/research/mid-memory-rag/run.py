"""Run the preregistered mid-memory study. `preflight` makes no network call; see README."""
import hashlib
import json
import math
import os
from pathlib import Path
import random
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
CORPUS = json.loads((HERE / 'corpus.json').read_text())
QUERIES = json.loads((HERE / 'queries.json').read_text())
BRIDGE = os.environ.get('MID_RAG_BRIDGE', '/tmp/mid-rag-854-bridge')
CHAT = 'https://api.minimax.io/v1/chat/completions'
EMBEDDINGS = 'https://api.minimax.io/v1/embeddings'
FASTEMBED = 'fastembed==0.7.3'
RECENT, DELIVERED, DEPTH = 2, 7, 20
# Global ceilings from preregistration.md; every request reserves before HTTP.
LIMITS = {'calls': 470, 'input': 1_600_000, 'output': 1_200_000, 'usd': 1.80}
PRICE_IN, PRICE_OUT = 0.30, 1.20
MAX_TOKENS = {'synthesis': 2048, 'phrase': 1024, 'live': 1536}
LIVE_QUERIES = ['exact-01', 'paraphrase-01', 'implicit-01', 'correction-01', 'absent-01', 'recent-01']
LIVE_CALLS = 4
ORACLE_SAMPLE = sorted(random.Random(8540).sample(range(1, 301), 20))
LIVE_SUFFIX = ('\n\n## This task\nAnswer the current task in your final message. '
               'The only tool available is memory_inspect.')


def bridge(**request):
    result = subprocess.run([BRIDGE], input=json.dumps(request), text=True, capture_output=True, check=True)
    return json.loads(result.stdout)


def updates(episode):
    return [f"{s['role']}: {s['content']}" for s in episode['sources']]


def write(path, value):
    path.write_text(json.dumps(value, indent=1, ensure_ascii=False) + '\n')


def sha256(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def tool_response(name, arguments):
    """A provider-shaped body carrying one tool call, for deterministic stand-ins."""
    return {'choices': [{'message': {'content': '', 'tool_calls': [{'id': 'call-1', 'type': 'function', 'function': {
        'name': name, 'arguments': json.dumps(arguments)}}]}}]}


# ---- stores ----------------------------------------------------------------

def build_store(db, synthesis):
    """Rebuild the production store from recorded synthesis exchanges."""
    Path(db).unlink(missing_ok=True)
    corpus = [{'sources': ep['sources'], 'request': synthesis[i]['request'], 'response': synthesis[i]['response']}
              for i, ep in enumerate(CORPUS)]
    out = bridge(mode='build', db=str(db), corpus=corpus)
    if out.get('error'):
        raise RuntimeError(out['error'])
    return sorted(out['result'], key=lambda m: m['ID'])


def standin_synthesis(summaries):
    """Recorded-shape synthesis exchanges whose summaries are given, for preflight stores."""
    records = []
    for ep, summary in zip(CORPUS, summaries):
        request = bridge(mode='synthesize', updates=updates(ep))['requests'][0]
        records.append({'request': request, 'response': tool_response('memory_summary', {'summary': summary})})
    return records


def source_text(episode):
    return '\n'.join(updates(episode))


# ---- retrieval arms --------------------------------------------------------

def covered(mids, ids):
    return [m for m in mids if any(m['SourceFirst'] <= i <= m['SourceLast'] for i in ids)]


def cosine_rank(query_vectors, mid_vectors):
    """Mid indices by descending max cosine over the query vectors (vectors are unit length)."""
    scores = [max(sum(a * b for a, b in zip(q, v)) for q in query_vectors) for v in mid_vectors]
    order = sorted(range(len(scores)), key=lambda i: (-scores[i], i))
    return order, scores


def merge(recent, relevant):
    """The production merge: recent first, then relevant, deduplicated by ID, capped."""
    out, seen = [], set()
    for m in recent + relevant:
        if m['ID'] not in seen:
            seen.add(m['ID'])
            out.append(m)
        if len(out) == DELIVERED:
            break
    return out


def recall_mids(recalled, mids):
    by_id = {m['ID']: m for m in mids}
    return [by_id[int(m['ID'])] for m in recalled['Memories'] or [] if not m['ID'].startswith('source:')]


def arguments(call):
    """A decoded agent.ToolCall carries its input as JSON; malformed input stays a string."""
    value = call['input']
    return json.loads(value) if isinstance(value, str) else value


def phrases_used(phrase_response, text):
    """Terms production recall searched with: parsed phrases, or the task text on fallback."""
    for call in phrase_response or []:
        if call['name'] == 'memory_search_terms':
            try:
                phrases = arguments(call)['phrases']
            except (ValueError, KeyError, TypeError):
                break
            if isinstance(phrases, list) and bridge(mode='tokens', terms=phrases[:4])['result']:
                return phrases[:4], True
            break
    return [text], False


# ---- embeddings ------------------------------------------------------------

def embed(items, scratch):
    source, target = Path(scratch) / 'embed-in.json', Path(scratch) / 'embed-out.json'
    write(source, {'items': items})
    subprocess.run(['uv', 'run', '--no-project', '--python', '3.13', '--with', FASTEMBED, 'python',
                    str(HERE / 'embed.py'), str(source), str(target)], check=True, capture_output=True)
    result = json.loads(target.read_text())
    target.unlink()
    return result


# ---- metering --------------------------------------------------------------

class Meter:
    def __init__(self, output):
        self.path = Path(output) / 'calls.json'
        self.calls = json.loads(self.path.read_text()) if self.path.exists() else []
        self.input = sum(c.get('charged_input', 0) for c in self.calls)
        self.output = sum(c.get('charged_output', 0) for c in self.calls)

    def usd(self, extra_in=0, extra_out=0):
        return ((self.input + extra_in) * PRICE_IN + (self.output + extra_out) * PRICE_OUT) / 1e6

    def post(self, cell, url, body, reserve_in, reserve_out):
        if (len(self.calls) >= LIMITS['calls'] or self.input + reserve_in > LIMITS['input']
                or self.output + reserve_out > LIMITS['output'] or self.usd(reserve_in, reserve_out) > LIMITS['usd']):
            raise RuntimeError('pre-request global budget reservation refused')
        entry = {'cell': cell, 'url': url, 'request': body, 'reserved_input': reserve_in,
                 'reserved_output': reserve_out, 'started_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())}
        self.calls.append(entry)
        write(self.path, self.calls)
        started = time.monotonic()
        request = urllib.request.Request(url, data=json.dumps(body).encode(), headers={
            'Authorization': 'Bearer ' + os.environ['MINIMAX_KEY'], 'Content-Type': 'application/json'})
        try:
            with urllib.request.urlopen(request, timeout=180) as response:
                entry['status'] = response.status
                raw = response.read()
            result = json.loads(raw)
            entry['response'] = result
        except urllib.error.HTTPError as error:
            entry['status'] = error.code
            entry['error_body'] = error.read().decode(errors='replace')[:4000]
        except Exception as error:
            entry['error'] = type(error).__name__
        entry['seconds'] = round(time.monotonic() - started, 3)
        usage = (entry.get('response') or {}).get('usage') or {}
        pin = usage.get('prompt_tokens', usage.get('total_tokens'))
        pout = usage.get('completion_tokens', 0)
        if type(pin) is int and type(pout) is int and 'response' in entry:
            entry['charged_input'], entry['charged_output'] = pin, pout
        else:
            # Unknown usage is charged at its full reservation.
            entry['charged_input'], entry['charged_output'] = reserve_in, reserve_out
        self.input += entry['charged_input']
        self.output += entry['charged_output']
        write(self.path, self.calls)
        if entry['charged_input'] > reserve_in or entry['charged_output'] > reserve_out:
            raise RuntimeError('provider exceeded reserved token allowance')
        return entry

    def chat(self, cell, wire, maximum):
        body = dict(wire, max_tokens=maximum)
        entry = self.post(cell, CHAT, body, len(json.dumps(body).encode()) + 1024, maximum)
        if 'response' not in entry:
            raise RuntimeError(f'chat call failed: {entry.get("status")} {entry.get("error")}')
        return entry


# ---- phases ----------------------------------------------------------------

def manifest():
    names = ['corpus.py', 'corpus.json', 'queries.json', 'bridge.go', 'embed.py', 'run.py', 'score.py',
             'run_test.py', 'score_test.py', 'preregistration.md']
    return {'revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(),
            'inputs': {n: sha256(HERE / n) for n in names}}


def preflight(output):
    """Deterministic checks on stand-in stores; no network call."""
    output = Path(output)
    output.mkdir(parents=True, exist_ok=True)
    evidence = {'manifest': manifest()}
    families = {}
    for q in QUERIES:
        families.setdefault(q['family'], []).append(q)
    assert len(CORPUS) >= 300 and all(len(ep['sources']) == 8 for ep in CORPUS)
    assert all(len(families[f]) >= 10 for f in ['exact', 'paraphrase', 'implicit', 'correction', 'absent'])
    by_id = {s['id']: s for ep in CORPUS for s in ep['sources']}
    assert all(all(i in by_id for i in q['evidence'] + q['obsolete']) for q in QUERIES)
    assert all(not q['evidence'] for q in families['absent'])
    # Implicit queries share no searchable word with their evidence under the production tokenizer and FTS.
    overlap = {}
    for q in families['implicit']:
        text = by_id[q['evidence'][0]]['content']
        mini = output / 'overlap.db'
        ep = {'sources': [{'role': 'user', 'content': text}] * 8}
        request = bridge(mode='synthesize', updates=updates(ep))['requests'][0]
        Path(mini).unlink(missing_ok=True)
        built = bridge(mode='build', db=str(mini), corpus=[dict(ep, request=request,
                       response=tool_response('memory_summary', {'summary': text}))])
        assert not built.get('error'), built
        hits = bridge(mode='search', db=str(mini), terms=[q['text']], limit=5)['result']
        control = bridge(mode='search', db=str(mini), terms=[text], limit=5)['result']
        overlap[q['id']] = {'query_hits': len(hits or []), 'evidence_self_hits': len(control or [])}
        assert not hits and control, (q['id'], overlap[q['id']])
        mini.unlink()
    evidence['implicit_overlap'] = overlap
    # Stand-in store: summaries are the raw episode text. It exercises every arm before payment.
    db = output / 'standin.db'
    mids = build_store(db, standin_synthesis([source_text(ep) for ep in CORPUS]))
    assert len(mids) == 300 and mids[0]['SourceFirst'] == 1 and mids[-1]['SourceLast'] == 2400
    evidence['standin'] = arm_controls(db, mids, [m['Summary'] for m in mids], output)
    db.unlink()
    write(output / 'preflight.json', evidence)
    return evidence


def arm_controls(db, mids, embedded_texts, scratch):
    """Oracle reach per arm, plus recency-difference checks for the similarity list."""
    result = {'oracle_sample': ORACLE_SAMPLE}
    reach = {'A': 0, 'A0': 0, 'B': 0, 'C': 0}
    targets = [mids[i - 1] for i in ORACLE_SAMPLE]
    vectors = embed([('passage', t) for t in embedded_texts] + [('query', t['Summary']) for t in targets], scratch)
    mid_vectors, oracle_vectors = vectors['vectors'][:len(mids)], vectors['vectors'][len(mids):]
    for target, vector in zip(targets, oracle_vectors):
        # A: production recall with a phrase call that returns the target's summary.
        response = tool_response('memory_search_terms', {'phrases': [target['Summary']]})
        recalled = bridge(mode='recall', db=str(db), task='oracle', responses=[response])
        reach['A'] += target['ID'] in [m['ID'] for m in recall_mids(recalled['result'], mids)]
        direct = bridge(mode='recall', db=str(db), task=target['Summary'], direct=True)
        reach['A0'] += target['ID'] in [m['ID'] for m in recall_mids(direct['result'], mids)]
        order, _ = cosine_rank([vector], mid_vectors)
        reach['B'] += mids[order[0]]['ID'] == target['ID']
        reach['C'] += mids[order[0]]['ID'] == target['ID']
    result['oracle_reach'] = reach
    recent_ids = [m['ID'] for m in sorted(mids, key=lambda m: -m['ID'])[:RECENT]]
    query_vectors = embed([('query', q['text']) for q in QUERIES], scratch)['vectors']
    same, tops = 0, set()
    for vector in query_vectors:
        order, _ = cosine_rank([vector], mid_vectors)
        top = [mids[i]['ID'] for i in order[:RECENT]]
        same += set(top) == set(recent_ids)
        tops.add(top[0])
    result['similarity_top2_equals_recency'] = same
    result['distinct_similarity_top1'] = len(tops)
    result['queries'] = len(QUERIES)
    return result


def probe(output):
    """The first paid call: whether MiniMax serves embeddings to this key."""
    meter = Meter(output)
    body = {'model': 'embo-01', 'texts': ['probe'], 'type': 'query'}
    entry = meter.post('embedding-probe', EMBEDDINGS, body, 64, 0)
    base = (entry.get('response') or {}).get('base_resp') or {}
    usable = entry.get('status') == 200 and base.get('status_code') == 0 and bool(
        (entry.get('response') or {}).get('vectors'))
    summary = {'status': entry.get('status'), 'base_resp': base, 'usable': usable,
               'error_body': entry.get('error_body')}
    write(Path(output) / 'embedding-probe.json', summary)
    return summary


def synthesize(output):
    """One production synthesis request per episode; at most two retries on rejection."""
    output = Path(output)
    meter = Meter(output)
    path = output / 'synthesis.json'
    records = json.loads(path.read_text()) if path.exists() else []
    for index in range(len(records), len(CORPUS)):
        batch = updates(CORPUS[index])
        for attempt in range(3):
            wire = bridge(mode='synthesize', updates=batch)['requests'][0]
            entry = meter.chat(f'synthesis-{index + 1}-{attempt}', json.loads(json.dumps(wire)), MAX_TOKENS['synthesis'])
            replay = bridge(mode='synthesize', updates=batch, responses=[entry['response']])
            if not replay.get('error'):
                records.append({'episode': index + 1, 'attempts': attempt + 1, 'request': wire,
                                'response': entry['response'], 'summary': replay['result']})
                write(path, records)
                break
        else:
            raise RuntimeError(f'episode {index + 1} synthesis rejected three times: {replay["error"]}')


def phrases(output):
    """The shipped pre-turn phrase call for every query, recorded once and shared by arms A and C."""
    output = Path(output)
    meter = Meter(output)
    path = output / 'phrases.json'
    records = json.loads(path.read_text()) if path.exists() else {}
    for q in QUERIES:
        if q['id'] in records:
            continue
        wire = bridge(mode='recall', db=str(output / 'store.db'), task=q['text'])['requests'][0]
        entry = meter.chat(f'phrase-{q["id"]}', wire, MAX_TOKENS['phrase'])
        decoded = bridge(mode='chat', messages=[{'role': 'user', 'content': 'decode'}], tools=[],
                         responses=[entry['response']])['result']
        records[q['id']] = {'request': wire, 'response': entry['response'], 'tool_calls': decoded['ToolCalls'],
                            'seconds': entry['seconds'], 'usage': entry['response'].get('usage')}
        write(path, records)


def rank(output):
    """Deterministic ranking for every arm over the synthesized store; no paid call."""
    output = Path(output)
    synthesis = json.loads((output / 'synthesis.json').read_text())
    phrase = json.loads((output / 'phrases.json').read_text())
    db = output / 'store.db'
    mids = build_store(db, synthesis)
    scratch = output / 'scratch'
    scratch.mkdir(exist_ok=True)
    summaries = [m['Summary'] for m in mids]
    evidence = {'controls': {'real_store': arm_controls(db, mids, summaries, scratch)}}
    # D's oracle: the target episode's own source text as the query.
    d_vectors = embed([('passage', source_text(ep)) for ep in CORPUS], scratch)
    targets = [mids[i - 1] for i in ORACLE_SAMPLE]
    d_oracle = embed([('query', source_text(CORPUS[t['ID'] - 1])) for t in targets], scratch)['vectors']
    evidence['controls']['real_store']['oracle_reach']['D'] = sum(
        mids[cosine_rank([v], d_vectors['vectors'])[0][0]]['ID'] == t['ID'] for v, t in zip(d_oracle, targets))
    mid_vectors = embed([('passage', s) for s in summaries], scratch)
    recent = bridge(mode='recent', db=str(db), limit=RECENT)['result']
    rows = []
    for q in QUERIES:
        terms, usable = phrases_used(phrase[q['id']]['tool_calls'], q['text'])
        started = time.monotonic()
        qv = embed([('query', q['text'])] + [('query', t) for t in terms], scratch)
        raw_vector, phrase_vectors = qv['vectors'][:1], qv['vectors'][1:]
        row = {'id': q['id'], 'family': q['family'], 'terms': terms, 'phrases_usable': usable,
               'embed_seconds_per_text': qv['seconds'] / len(qv['vectors']), 'arms': {}}
        for arm, recall_request in [('A', {'task': q['text'], 'responses': [phrase[q['id']]['response']]}),
                                    ('A0', {'task': q['text'], 'direct': True})]:
            started = time.monotonic()
            recalled = bridge(mode='recall', db=str(db), **recall_request)
            seconds = time.monotonic() - started
            search_terms = terms if arm == 'A' else [q['text']]
            relevant = bridge(mode='search', db=str(db), terms=search_terms, limit=DEPTH)['result'] or []
            row['arms'][arm] = {'delivered': [m['ID'] for m in recall_mids(recalled['result'], mids)],
                                'relevant': [m['ID'] for m in relevant], 'bridge_seconds': round(seconds, 3)}
        for arm, vectors, mv in [('B', raw_vector, mid_vectors['vectors']), ('C', phrase_vectors, mid_vectors['vectors']),
                                 ('D', raw_vector, d_vectors['vectors'])]:
            order, scores = cosine_rank(vectors, mv)
            relevant = [mids[i] for i in order[:DEPTH]]
            row['arms'][arm] = {'delivered': [m['ID'] for m in merge(recent, relevant[:DELIVERED])],
                                'relevant': [m['ID'] for m in relevant],
                                'top_scores': [round(scores[i], 4) for i in order[:DELIVERED]]}
        rows.append(row)
    evidence['recent'] = [m['ID'] for m in recent]
    evidence['mids'] = [{'ID': m['ID'], 'SourceFirst': m['SourceFirst'], 'SourceLast': m['SourceLast']} for m in mids]
    evidence['embedding'] = {'model': mid_vectors['model'], 'package': FASTEMBED,
                             'summary_embed_seconds': mid_vectors['seconds'], 'source_embed_seconds': d_vectors['seconds']}
    evidence['rows'] = rows
    write(output / 'rankings.json', evidence)


def render(recalled_mids, ages):
    """The production frame (internal/agent/service.go frameContext) for a recalled set."""
    lines = []
    for m, age in zip(recalled_mids, ages):
        content = m['Summary'].strip() + f" [mid:{m['ID']} sources:{m['SourceFirst']}-{m['SourceLast']}; summary, not verified source text]"
        lines.append(f"- [{m['ID']}] ({age}): {content}\n")
    return ''.join(lines)


def system_prompt():
    text = (ROOT / 'internal/agent/instructions.go').read_text()
    section = re.search(r'(## Source-linked memory\n.*?)\n\n## ', text, re.S).group(1)
    return section.replace('` + "`memory_inspect`" + `', '`memory_inspect`') + LIVE_SUFFIX


def live(output):
    """Bounded act loops for arms A and B on the preregistered queries."""
    output = Path(output)
    meter = Meter(output)
    rankings = json.loads((output / 'rankings.json').read_text())
    db = output / 'store.db'
    mids = {m['ID']: m for m in bridge(mode='recent', db=str(db), limit=400)['result']}
    path = output / 'live.json'
    cells = json.loads(path.read_text()) if path.exists() else {}
    rows = {r['id']: r for r in rankings['rows']}
    queries = {q['id']: q for q in QUERIES}
    for qid in LIVE_QUERIES:
        for arm in ['A', 'B']:
            key = f'{arm}-{qid}'
            if key in cells:
                continue
            delivered = [mids[i] for i in rows[qid]['arms'][arm]['delivered']]
            recent_ids = set(rankings['recent'])
            ages = ['recent' if m['ID'] in recent_ids and n < RECENT else 'historical' for n, m in enumerate(delivered)]
            frame = '## Current task\n' + queries[qid]['text'] + '\n\n## Recalled memories\n' + render(delivered, ages)
            messages = [{'role': 'system', 'content': system_prompt()}, {'role': 'user', 'content': frame}]
            trace = []
            for step in range(LIVE_CALLS):
                wire = bridge(mode='chat', messages=messages, tools=['memory_inspect'])['requests'][0]
                entry = meter.chat(f'live-{key}-{step}', wire, MAX_TOKENS['live'])
                decoded = bridge(mode='chat', messages=messages, tools=['memory_inspect'], responses=[entry['response']])
                response = decoded['result']
                messages.append({'role': 'assistant', 'content': response['Content'], 'tool_calls': response['ToolCalls']})
                if not response['ToolCalls']:
                    break
                for call in response['ToolCalls']:
                    try:
                        ident = arguments(call)['id']
                        got = bridge(mode='source', db=str(db), ids=[int(ident)])
                        content = got['error'] if got.get('error') else json.dumps(got['result'][0])
                    except (ValueError, KeyError, TypeError) as error:
                        ident, content = None, f'invalid input: {error}'
                    trace.append({'step': step, 'id': ident, 'result': content})
                    messages.append({'role': 'tool', 'content': content, 'tool_call_id': call['id'],
                                     'name': call['name']})
            cells[key] = {'arm': arm, 'query': qid, 'messages': messages, 'inspections': trace,
                          'final': messages[-1]['content'] if messages[-1]['role'] == 'assistant' else None}
            write(path, cells)


def main():
    command, output = sys.argv[1], sys.argv[2]
    Path(output).mkdir(parents=True, exist_ok=True)
    if command != 'preflight' and 'MINIMAX_KEY' not in os.environ and command not in ('rank',):
        raise SystemExit('MINIMAX_KEY is required for paid phases')
    {'preflight': preflight, 'probe': probe, 'synthesize': synthesize, 'phrases': phrases,
     'rank': rank, 'live': live}[command](output)


if __name__ == '__main__':
    main()
