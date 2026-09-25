"""Run the preregistered replication (issue #856); `assemble` and `rank` make no network call. See README."""
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
from pathlib import Path
import random
import re
import sys
import tempfile
import time

import run

HERE = run.HERE
OUT = HERE / 'evidence' / 'replication'
SEED = 856
PRIMARY = ['exact', 'paraphrase', 'implicit', 'correction']
PER_FAMILY, ABSENT = 70, 30
DESIGN_ITEMS = {'exact': 90, 'paraphrase': 130, 'implicit': 160, 'correction': 90, 'absent': 40}
# Chats are written for the first passing items only; selection order makes a later raise change nothing earlier.
CONVERSE_CAP = {'exact': 100, 'paraphrase': 160, 'implicit': 120, 'correction': 100, 'absent': 38, 'filler': 240}
# Amendments 1 and 2: supplementary rounds with the same prompts, appended after the original calls.
SUPPLEMENT_ITEMS = {'exact': 40, 'paraphrase': 80, 'implicit': 100, 'correction': 40, 'absent': 10}
SECOND_SUPPLEMENT_ITEMS = {'paraphrase': 100}
FILLER_TOPICS = 240
ITEMS_PER_CALL = 10
BATCHES = [4, 8, 16]
STREAM = 4800
CONCURRENCY = 4
LIMITS = {'calls': 4200, 'input': 6_000_000, 'output': 2_600_000, 'usd': 2.90}
MAX_TOKENS = {'design': 6000, 'converse': 4096, 'synthesis': 2048, 'phrase': 1024}
BACKOFF = [10, 30, 60]
SYNTHESIS_ATTEMPTS = 5
LENGTHS = [6, 8, 10, 12, 14]
DOMAINS = ['household chores and repairs', 'office work and meetings', 'doctor visits and health', 'travel plans',
           'personal finance and bills', 'cooking and recipes', 'pets', 'hobbies and crafts', 'family events',
           'software projects', 'school and courses', 'parties and celebrations', 'cars and commuting',
           'shopping and returns', 'gardening', 'forms and paperwork', 'fitness and sport', 'music practice',
           'books and podcasts', 'volunteering', 'moving house', 'childcare', 'freelance clients', 'photography',
           'games and puzzles', 'a local sports club', 'insurance and warranties', 'home technology support',
           'neighbours and community', 'writing projects']
# The newest conversation is authored whole; its four queries refer to it without naming it.
RECENT = [
    {'role': 'user', 'content': 'Before I forget, the plumber is coming back on Friday morning to fit the new valve.'},
    {'role': 'assistant', 'content': 'Friday morning for the valve, got it. Do you need to be home?'},
    {'role': 'user', 'content': 'Yes, and he said to leave the water off overnight. His name is Callum, by the way.'},
    {'role': 'assistant', 'content': 'Water off overnight, Callum on Friday. Anything else for the bathroom?'},
    {'role': 'user', 'content': 'We settled on the matte white tiles in the end, not the grey ones.'},
    {'role': 'assistant', 'content': 'Matte white it is. I will stop suggesting the grey.'},
    {'role': 'user', 'content': 'And remind me to cancel the skip hire once the tiles are in.'},
    {'role': 'assistant', 'content': 'I will remind you about the skip hire after tiling.'},
]
RECENT_QUERIES = [('Who did I say is coming to do that job?', 'Callum'),
                  ('Which ones did we end up choosing?', 'matte white'),
                  ('What did I need to do the night before?', 'water off'),
                  ('What was I supposed to cancel afterwards?', 'skip hire')]

SCHEMAS = {
    'fact': {'topic': 'string', 'fact_sentence': 'string', 'key': 'string', 'exact_query': 'string',
             'paraphrase_query': 'string'},
    'implicit': {'topic': 'string', 'rule_sentence': 'string', 'key': 'string', 'task': 'string'},
    'correction': {'topic': 'string', 'old_sentence': 'string', 'old_key': 'string', 'new_sentence': 'string',
                   'new_key': 'string', 'query': 'string'},
    'absent': {'topic': 'string', 'near_miss_sentence': 'string', 'key': 'string', 'query': 'string'},
    'filler': {'topic': 'string', 'gist': 'string'},
}
INSTRUCTIONS = {
    'fact': ('Each item is a personal fact a user tells an assistant in passing, with one specific, uncommon value '
             '(a code, date, name, amount, place or model number). fact_sentence: one first-person sentence the '
             'user says, containing key verbatim. key: that value, 3 to 30 characters, unlikely to occur by chance. '
             'exact_query: a question about the fact that reuses the sentence\'s main words. paraphrase_query: the '
             'same question sharing no word with fact_sentence, not even small words; use synonyms or descriptions, '
             'and do not include key.'),
    'implicit': ('Each item is a standing rule or preference a user states once (diet, allergy, formatting, tone, '
                 'accessibility, schedule, budget, coding style and so on). rule_sentence: the first-person sentence '
                 'stating it. key: a short phrase copied from rule_sentence that names the rule. task: a later request '
                 'whose correct fulfilment depends on the rule but which shares no word with rule_sentence, not even '
                 'small words like my, to, me, a or the; it must not mention the rule.'),
    'correction': ('Each item is a fact the user states and later corrects. old_sentence: first-person sentence with '
                   'old_key verbatim. new_sentence: a later, natural correction with new_key verbatim that does not '
                   'contain old_key. old_key and new_key: the distinctive values, 2 to 30 characters, different. query: '
                   'a question asking for the current value.'),
    'absent': ('Each item is a fact the user states, and a question about the same subject asking for a different '
               'detail that the fact does not contain, so the answer is unknown. near_miss_sentence: the first-person '
               'sentence, containing key verbatim. key: its distinctive value. query: the unanswerable question; it '
               'must not contain key.'),
    'filler': ('Each item is a distinct everyday topic someone might chat to an assistant about, with a one-sentence '
               'gist of what the chat covers. Avoid personal codes, numbers and names in the gist.'),
}


def items_tool(kind):
    props = {k: {'type': v} for k, v in SCHEMAS[kind].items()}
    item = {'type': 'object', 'properties': props, 'required': list(props), 'additionalProperties': False}
    schema = {'type': 'object', 'properties': {'items': {'type': 'array', 'items': item}}, 'required': ['items'],
              'additionalProperties': False}
    return {'type': 'function', 'function': {'name': 'submit_items', 'description': 'Submit the items',
                                             'parameters': schema}}


CONVERSATION_TOOL = {'type': 'function', 'function': {'name': 'submit_conversation', 'description': 'Submit the chat',
                     'parameters': {'type': 'object', 'properties': {'messages': {'type': 'array', 'items': {
                         'type': 'object', 'properties': {'role': {'type': 'string', 'enum': ['user', 'assistant']},
                                                          'content': {'type': 'string'}},
                         'required': ['role', 'content'], 'additionalProperties': False}}},
                         'required': ['messages'], 'additionalProperties': False}}}


def sha(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True).encode()).hexdigest()


class Meter(run.Meter):
    """Appends start and finish records to calls.jsonl, so thousands of calls do not rewrite one file."""

    def load(self, output):
        self.path = output / 'calls.jsonl'
        records = {}
        if self.path.exists():
            for line in self.path.read_text().splitlines():
                record = json.loads(line)
                records.setdefault(record['index'], {}).update(record)
        return [records[i] for i in sorted(records)]

    def save(self, entry):
        if 'index' not in entry:
            entry['index'] = len(self.calls) - 1
        stored = dict(entry)
        # Synthesis and phrase requests are regenerated by the bridge and verified by hash.
        if entry['cell'].startswith(('synthesis-', 'phrase-')):
            stored['request'] = {'sha256': sha(entry['request'])}
        with self.path.open('a') as f:
            f.write(json.dumps(stored, ensure_ascii=False) + '\n')

    def call(self, cell, body, maximum):
        """One chat request, retried only on transport, rate-limit or server failure, with backoff."""
        body = dict(body, max_tokens=maximum)
        reserve = len(json.dumps(body).encode()) + 1024
        for attempt in range(len(BACKOFF) + 1):
            entry = self.reserve(f'{cell}#{attempt}', run.CHAT, body, reserve, maximum)
            done = self.send(entry, run.CHAT, body, reserve, maximum)
            base = (done.get('response') or {}).get('base_resp') or {}
            status = done.get('status')
            transient = ('response' not in done and (status is None or status == 429 or status >= 500)
                         ) or base.get('status_code') == 1002
            if not transient:
                return done
            if attempt < len(BACKOFF):
                time.sleep(BACKOFF[attempt])
        raise RuntimeError(f'{cell}: transient failures exhausted retries')


def tool_arguments(response, name):
    message = (response.get('choices') or [{}])[0].get('message') or {}
    for call in message.get('tool_calls') or []:
        if call['function']['name'] == name:
            try:
                return json.loads(call['function']['arguments'])
            except ValueError:
                return None
    return None


def normalize(text):
    return re.sub(r'\s+', ' ', text.replace('’', "'").replace('‘', "'").replace('“', '"')
                  .replace('”', '"')).strip()


def contains(text, key):
    """Case-insensitive match of key in text, bounded by non-alphanumerics."""
    return re.search(r'(?<![A-Za-z0-9])' + re.escape(normalize(key).lower()) + r'(?![A-Za-z0-9])',
                     normalize(text).lower()) is not None


def jsonl(path):
    return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []


def append(path, record):
    with path.open('a') as f:
        f.write(json.dumps(record, ensure_ascii=False) + '\n')


def parallel(jobs):
    with ThreadPoolExecutor(CONCURRENCY) as pool:
        for future in [pool.submit(job) for job in jobs]:
            future.result()


# The design phase: model-written items per family, then filler topics.

def design_calls():
    rng = random.Random(SEED)
    calls = []
    for family, count in DESIGN_ITEMS.items():
        kind = {'exact': 'fact', 'paraphrase': 'fact'}.get(family, family)
        for i in range(count // ITEMS_PER_CALL):
            calls.append((f'{family}-{i:02d}', family, kind, rng.choice(DOMAINS)))
    for i in range(FILLER_TOPICS // 20):
        calls.append((f'filler-{i:02d}', 'filler', 'filler', DOMAINS[i % len(DOMAINS)] + ' and ' +
                      DOMAINS[(i * 7 + 3) % len(DOMAINS)]))
    supplement = random.Random(SEED + 10)
    for family, count in SUPPLEMENT_ITEMS.items():
        kind = {'exact': 'fact', 'paraphrase': 'fact'}.get(family, family)
        for i in range(count // ITEMS_PER_CALL):
            calls.append((f'{family}-s{i:02d}', family, kind, supplement.choice(DOMAINS)))
    second = random.Random(SEED + 20)
    for family, count in SECOND_SUPPLEMENT_ITEMS.items():
        for i in range(count // ITEMS_PER_CALL):
            calls.append((f'{family}-t{i:02d}', family, 'fact', second.choice(DOMAINS)))
    return calls


def design(output=OUT):
    output.mkdir(parents=True, exist_ok=True)
    meter = Meter(output, LIMITS)
    path = output / 'design.jsonl'
    done = {r['call'] for r in jsonl(path)}

    def job(call, family, kind, domain):
        count = 20 if kind == 'filler' else ITEMS_PER_CALL
        body = {'model': 'MiniMax-M2', 'messages': [
            {'role': 'system', 'content': 'You write test material for a memory study. ' + INSTRUCTIONS[kind]},
            {'role': 'user', 'content': f'Write {count} varied items about {domain}. Call submit_items once.'}],
            'tools': [items_tool(kind)]}
        for attempt in range(3):
            entry = meter.call(f'design-{call}-{attempt}', body, MAX_TOKENS['design'])
            parsed = tool_arguments(entry.get('response') or {}, 'submit_items')
            if parsed and isinstance(parsed.get('items'), list):
                append(path, {'call': call, 'family': family, 'domain': domain, 'items': parsed['items']})
                return
        append(path, {'call': call, 'family': family, 'domain': domain, 'items': [], 'failed': True})

    parallel([lambda c=c: job(*c) for c in design_calls() if c[0] not in done])


def fts_miss(query, text):
    """Production Search finds nothing for query in a store holding only text, and text finds itself."""
    ep = {'sources': [{'role': 'user', 'content': text}] * 8}
    request = run.bridge(mode='synthesize', updates=run.updates(ep))['requests'][0]
    db = Path(tempfile.mkdtemp()) / 'fts-check.db'
    built = run.bridge(mode='build', db=str(db), corpus=[dict(ep, request=request, response=run.tool_response(
        'memory_summary', {'summary': text}))])
    try:
        if built.get('error'):
            raise RuntimeError(built['error'])
        hits = run.bridge(mode='search', db=str(db), terms=[query], limit=5)['result']
        itself = run.bridge(mode='search', db=str(db), terms=[text], limit=5)['result']
        return not hits and bool(itself)
    finally:
        db.unlink(missing_ok=True)
        db.parent.rmdir()


def tokens(text):
    return set(run.bridge(mode='tokens', terms=[text])['result'] or [])


def check_item(family, item):
    """Deterministic acceptance of one designed item; returns the failed check or None."""
    s = {k: str(v) for k, v in item.items()}
    if family in ('exact', 'paraphrase'):
        sentence, key = s['fact_sentence'], s['key']
        if len(key) < 3 or not contains(sentence, key):
            return 'key not in sentence'
        if family == 'exact' and len(tokens(s['exact_query']) & tokens(sentence)) < 2:
            return 'exact query shares fewer than two tokens'
        if family == 'paraphrase' and (contains(s['paraphrase_query'], key) or not fts_miss(s['paraphrase_query'], sentence)):
            return 'paraphrase query reaches its sentence lexically'
    elif family == 'implicit':
        if not contains(s['rule_sentence'], s['key']) or not fts_miss(s['task'], s['rule_sentence']):
            return 'implicit task reaches its rule lexically'
    elif family == 'correction':
        if (normalize(s['old_key']).lower() == normalize(s['new_key']).lower() or not contains(s['old_sentence'], s['old_key'])
                or not contains(s['new_sentence'], s['new_key']) or contains(s['new_sentence'], s['old_key'])
                or contains(s['old_sentence'], s['new_key'])):
            return 'correction keys malformed'
    elif family == 'absent':
        if not contains(s['near_miss_sentence'], s['key']) or contains(s['query'], s['key']):
            return 'absent key malformed'
    return None


def keys_of(family, item):
    if family == 'correction':
        return [item['old_key'], item['new_key']]
    return [item['key']]


def accepted_items(output=OUT):
    """Designed items in call order that pass their checks and repeat no key; cached in items.json."""
    path = output / 'items.json'
    if path.exists():
        return json.loads(path.read_text())
    order = {c[0]: n for n, c in enumerate(design_calls())}
    records = sorted(jsonl(output / 'design.jsonl'), key=lambda r: order[r['call']])
    seen, out = set(), {'filler': []}
    for record in records:
        for n, item in enumerate(record['items']):
            family = record['family']
            if family == 'filler':
                out['filler'].append({'id': f'{record["call"]}-{n:02d}', **item})
                continue
            if not isinstance(item, dict) or set(item) != set(SCHEMAS[{'exact': 'fact', 'paraphrase': 'fact'}.get(
                    family, family)]):
                continue
            keys = [normalize(str(k)).lower() for k in keys_of(family, item)]
            failure = 'key repeated' if any(k in seen for k in keys) else check_item(family, item)
            entry = {'id': f'{record["call"]}-{n:02d}', 'family': family, 'failure': failure, **item}
            out.setdefault(family, []).append(entry)
            if failure is None:
                seen.update(keys)
    run.write(path, out)
    return out


# The conversation phase: one chat per evidence sentence or filler topic.

def conversation_specs(items):
    """Every chat to write, in a fixed order, with its required sentence and length."""
    def length(cid):
        return random.Random(f'{SEED}-{cid}').choice(LENGTHS)

    specs = []
    for family in PRIMARY + ['absent']:
        passing = [i for i in items.get(family, []) if i['failure'] is None][:CONVERSE_CAP[family]]
        for item in passing:
            if family == 'correction':
                specs.append({'id': item['id'] + '-old', 'topic': item['topic'], 'sentence': item['old_sentence'],
                              'length': length(item['id'] + '-old')})
                specs.append({'id': item['id'] + '-new', 'topic': item['topic'], 'sentence': item['new_sentence'],
                              'length': length(item['id'] + '-new'), 'follow_up': True})
            else:
                sentence = item.get('fact_sentence') or item.get('rule_sentence') or item.get('near_miss_sentence')
                specs.append({'id': item['id'], 'topic': item['topic'], 'sentence': sentence,
                              'length': length(item['id'])})
    for item in items['filler'][:CONVERSE_CAP['filler']]:
        specs.append({'id': item['id'], 'topic': item['topic'] + ': ' + item['gist'], 'sentence': None,
                      'length': length(item['id'])})
    return specs


def conversation_prompt(spec):
    lines = [f'Write a natural chat between a user and an AI assistant about {spec["topic"]}.',
             f'Exactly {spec["length"]} messages, alternating, starting with the user.',
             'Vary sentence length and tone; the assistant is helpful and concrete.']
    if spec['sentence']:
        lines.append('In one of the user messages, the user says this sentence word for word, as part of that '
                     f'message: "{spec["sentence"]}"')
        if spec.get('follow_up'):
            lines.append('The user is correcting something they said in an earlier, separate chat.')
    else:
        lines.append('Do not include personal codes, reference numbers or account details.')
    lines.append('Call submit_conversation once with the messages.')
    return '\n'.join(lines)


def check_conversation(spec, messages):
    if not isinstance(messages, list) or len(messages) != spec['length']:
        return 'wrong length'
    for n, m in enumerate(messages):
        if not isinstance(m, dict) or m.get('role') != ('user' if n % 2 == 0 else 'assistant') or not str(
                m.get('content', '')).strip():
            return 'roles do not alternate'
    if spec['sentence'] and not any(normalize(spec['sentence']) in normalize(m['content'])
                                    for m in messages if m['role'] == 'user'):
        return 'sentence not verbatim in a user message'
    return None


def converse(output=OUT):
    meter = Meter(output, LIMITS)
    path = output / 'conversations.jsonl'
    done = {r['id'] for r in jsonl(path)}
    specs = conversation_specs(accepted_items(output))

    def job(spec):
        body = {'model': 'MiniMax-M2', 'messages': [
            {'role': 'system', 'content': 'You write realistic chat transcripts for a memory study.'},
            {'role': 'user', 'content': conversation_prompt(spec)}], 'tools': [CONVERSATION_TOOL]}
        failure = None
        for attempt in range(3):
            entry = meter.call(f'converse-{spec["id"]}-{attempt}', body, MAX_TOKENS['converse'])
            parsed = tool_arguments(entry.get('response') or {}, 'submit_conversation') or {}
            messages = parsed.get('messages')
            failure = check_conversation(spec, messages)
            if failure is None:
                append(path, {'id': spec['id'], 'attempts': attempt + 1, 'messages': [
                    {'role': m['role'], 'content': m['content'].strip()} for m in messages]})
                return
        append(path, {'id': spec['id'], 'attempts': 3, 'failure': failure})

    parallel([lambda s=s: job(s) for s in specs if s['id'] not in done])


# Assembly: select, order and index the corpus deterministically.

def chat_ids(family, item):
    return [item['id'] + '-old', item['id'] + '-new'] if family == 'correction' else [item['id']]


def evidence_texts(family, item, chats):
    """The messages of an item's evidence chat that carry its key: what a query must reach."""
    cid, key = (item['id'] + '-new', item['new_key']) if family == 'correction' else (item['id'], item['key'])
    return [m['content'] for m in chats[cid]['messages'] if contains(m['content'], key)]


def usable(family, item, chats):
    """Chats exist, and paraphrase and implicit queries miss every evidence message under production FTS."""
    if not all(c in chats for c in chat_ids(family, item)):
        return False
    if family in ('paraphrase', 'implicit'):
        query = item['paraphrase_query'] if family == 'paraphrase' else item['task']
        return all(fts_miss(query, text) for text in evidence_texts(family, item, chats))
    return True


def sentence_of(family, item):
    return {'correction': item.get('new_sentence'), 'implicit': item.get('rule_sentence'),
            'absent': item.get('near_miss_sentence')}.get(family) or item['fact_sentence']


def select(items, chats, token_set=tokens):
    """First passing items per family in design order. An item leaks when a message of another selected chat
    carries its key and at least two of its sentence's other search tokens of four or more characters
    (amendment 2); a leaking item is
    banned and the next one taken, until none leaks."""
    cache = {}

    def message_tokens(text):
        if text not in cache:
            cache[text] = token_set(text)
        return cache[text]

    candidates = {f: [i for i in items.get(f, []) if i['failure'] is None and usable(f, i, chats)]
                  for f in PRIMARY + ['absent']}
    banned, dropped = set(), []
    while True:
        selected = {f: [i for i in candidates[f] if i['id'] not in banned][:PER_FAMILY if f in PRIMARY else ABSENT]
                    for f in candidates}
        owner = {cid: i['id'] for f, chosen in selected.items() for i in chosen for cid in chat_ids(f, i)}
        leaks = set()
        for f, chosen in selected.items():
            for item in chosen:
                for key in keys_of(f, item):
                    own = {t for t in token_set(sentence_of(f, item)) - token_set(key) if len(t) >= 4}
                    for cid, iid in owner.items():
                        if iid != item['id'] and any(contains(m['content'], key) and len(
                                own & message_tokens(m['content'])) >= 2 for m in chats[cid]['messages']):
                            leaks.add(item['id'])
        if not leaks:
            return selected, dropped
        banned |= leaks
        dropped += [{'id': i, 'reason': 'key appears in another selected chat'} for i in sorted(leaks)]


def assemble(output=OUT):
    items = accepted_items(output)
    chats = {r['id']: r for r in jsonl(output / 'conversations.jsonl') if 'messages' in r}
    rng = random.Random(SEED + 2)
    selected, dropped = select(items, chats)
    keys = {k for f, chosen in selected.items() for i in chosen for k in keys_of(f, i)}
    filler = sorted((c for c in chats.values() if c['id'].startswith('filler-')
                     and not any(contains(' '.join(m['content'] for m in c['messages']), k) for k in keys)),
                    key=lambda c: c['id'])
    # Placement ranks in [0, 1); a correction's newer chat sits at least a quarter of the stream after the older.
    placed = []
    for family, chosen in selected.items():
        for item in chosen:
            if family == 'correction':
                old = rng.uniform(0, 0.5)
                placed.append((old, item['id'] + '-old'))
                placed.append((rng.uniform(old + 0.25, 0.97), item['id'] + '-new'))
            else:
                placed.append((rng.uniform(0, 0.97), item['id']))
    budget = STREAM - len(RECENT) - sum(len(chats[cid]['messages']) for _, cid in placed)
    if budget < 0:
        raise RuntimeError('evidence chats exceed the stream')
    fill = []
    for chat in filler:
        if budget <= 0:
            break
        messages = chat['messages'][:budget]
        budget -= len(messages)
        fill.append((rng.uniform(0, 0.97), chat['id'], messages))
    if budget:
        raise RuntimeError(f'filler short by {budget} messages')
    ordered = sorted([(r, cid, chats[cid]['messages']) for r, cid in placed] + fill, key=lambda x: (x[0], x[1]))
    stream, spans = [], {}
    for _, cid, messages in ordered + [(1, 'recent', RECENT)]:
        spans[cid] = (len(stream) + 1, len(stream) + len(messages))
        stream += [{'id': len(stream) + n + 1, 'role': m['role'], 'content': m['content'], 'chat': cid}
                   for n, m in enumerate(messages)]
    assert len(stream) == STREAM

    def matching(cid, key):
        first, last = spans[cid]
        return [m['id'] for m in stream[first - 1:last] if contains(m['content'], key)]

    out = []
    for family, chosen in selected.items():
        for n, item in enumerate(chosen, 1):
            q = {'id': f'{family}-{n:02d}', 'family': family, 'item': item['id'], 'evidence': [], 'obsolete': []}
            if family == 'correction':
                q.update(text=item['query'], evidence=matching(item['id'] + '-new', item['new_key']),
                         obsolete=matching(item['id'] + '-old', item['old_key']))
            elif family == 'absent':
                q.update(text=item['query'], near_miss=matching(item['id'], item['key']))
            else:
                field = {'exact': 'exact_query', 'paraphrase': 'paraphrase_query', 'implicit': 'task'}[family]
                q.update(text=item[field], evidence=matching(item['id'], item['key']))
            out.append(q)
    for n, (text, key) in enumerate(RECENT_QUERIES, 1):
        out.append({'id': f'recent-{n:02d}', 'family': 'recent', 'item': 'recent', 'text': text,
                    'evidence': matching('recent', key), 'obsolete': []})
    assert all(q['evidence'] for q in out if q['family'] != 'absent')
    counts = {f: len(v) for f, v in selected.items()}
    run.write(output / 'corpus.json', stream)
    run.write(output / 'queries.json', out)
    run.write(output / 'assembly.json', {'counts': counts, 'dropped': dropped, 'filler_used': len(fill),
                                         'messages': len(stream)})
    return counts


# Synthesis at every batch size, then the shipped phrase call.

def corpus():
    return json.loads((OUT / 'corpus.json').read_text())


def queries():
    return json.loads((OUT / 'queries.json').read_text())


def batches(stream, n):
    return [stream[i:i + n] for i in range(0, len(stream), n)]


def synthesize(output=OUT):
    meter = Meter(output, LIMITS)
    stream = corpus()
    for n in BATCHES:
        path = output / f'synthesis-n{n}.jsonl'
        done = {r['batch'] for r in jsonl(path)}

        def job(index, batch, n=n, path=path):
            updates = [f"{m['role']}: {m['content']}" for m in batch]
            wire = run.bridge(mode='synthesize', updates=updates)['requests'][0]
            for attempt in range(SYNTHESIS_ATTEMPTS):
                entry = meter.call(f'synthesis-n{n}-{index}-{attempt}', json.loads(json.dumps(wire)),
                                   MAX_TOKENS['synthesis'])
                if 'response' not in entry:
                    continue
                replay = run.bridge(mode='synthesize', updates=updates, responses=[entry['response']])
                if not replay.get('error'):
                    append(path, {'batch': index, 'attempts': attempt + 1, 'request_sha256': sha(wire),
                                  'response': entry['response'], 'summary': replay['result']})
                    return
            # A batch the production summarizer never accepts has no mid: fatal for the n=8 store, and an
            # unreachable empty summary for B at n=4 and n=16.
            append(path, {'batch': index, 'attempts': SYNTHESIS_ATTEMPTS, 'request_sha256': sha(wire),
                          'failed': True, 'summary': ''})
            if n == 8:
                raise RuntimeError(f'n=8 batch {index} synthesis rejected {SYNTHESIS_ATTEMPTS} times')

        parallel([lambda i=i, b=b, j=job: j(i, b) for i, b in enumerate(batches(stream, n), 1) if i not in done])


def phrases(output=OUT):
    meter = Meter(output, LIMITS)
    path = output / 'phrases.jsonl'
    done = {r['id'] for r in jsonl(path)}
    def job(q):
        db = output / f'capture-{q["id"]}.db'
        wire = run.bridge(mode='recall', db=str(db), task=q['text'])['requests'][0]
        db.unlink(missing_ok=True)
        entry = meter.call(f'phrase-{q["id"]}', wire, MAX_TOKENS['phrase'])
        if 'response' not in entry:
            raise RuntimeError(f'phrase {q["id"]} failed')
        decoded = run.bridge(mode='chat', messages=[{'role': 'user', 'content': 'decode'}], tools=[],
                             responses=[entry['response']])['result']
        append(path, {'id': q['id'], 'request_sha256': sha(wire), 'response': entry['response'],
                      'tool_calls': decoded['ToolCalls'], 'seconds': entry['seconds'],
                      'usage': entry['response'].get('usage')})

    parallel([lambda q=q: job(q) for q in queries() if q['id'] not in done])


# Ranking: deterministic over recorded exchanges; no network call.

def summaries(n, output=OUT):
    records = sorted(jsonl(output / f'synthesis-n{n}.jsonl'), key=lambda r: r['batch'])
    assert [r['batch'] for r in records] == list(range(1, STREAM // n + 1)), n
    return records


def build_n8(output, stream):
    """Production store over the n=8 batches; each replayed request must hash to the recorded one."""
    records = summaries(8, output)
    corpus8 = []
    for record, batch in zip(records, batches(stream, 8)):
        ep = {'sources': [{'role': m['role'], 'content': m['content']} for m in batch]}
        request = run.bridge(mode='synthesize', updates=run.updates(ep))['requests'][0]
        assert sha(request) == record['request_sha256'], record['batch']
        corpus8.append(dict(ep, request=request, response=record['response']))
    db = output / 'store.db'
    db.unlink(missing_ok=True)
    built = run.bridge(mode='build', db=str(db), corpus=corpus8)
    if built.get('error'):
        raise RuntimeError(built['error'])
    return db, sorted(built['result'], key=lambda m: m['ID'])


def interleave(first, second):
    """H's relevant list: alternate A's and B's ranks, A first, deduplicated by interval."""
    out, seen = [], set()
    for pair in zip(first, second):
        for span in pair:
            if tuple(span) not in seen:
                seen.add(tuple(span))
                out.append(span)
    for span in first[len(second):] + second[len(first):]:
        if tuple(span) not in seen:
            seen.add(tuple(span))
            out.append(span)
    return out


def deliver(recent, relevant):
    """The production merge over intervals: recent first, deduplicated, capped at the delivered budget."""
    merged = run.merge([{'ID': tuple(s)} for s in recent], [{'ID': tuple(s)} for s in relevant])
    return [list(m['ID']) for m in merged]


def rank(output=OUT):
    stream = corpus()
    qs = queries()
    scratch = output / 'scratch'
    scratch.mkdir(exist_ok=True)
    phrase = {r['id']: r for r in jsonl(output / 'phrases.jsonl')}
    db, mids = build_n8(output, stream)
    span8 = {m['ID']: [m['SourceFirst'], m['SourceLast']] for m in mids}
    recent8 = [span8[m['ID']] for m in run.bridge(mode='recent', db=str(db), limit=run.RECENT)['result']]
    texts = {n: [r['summary'] for r in summaries(n, output)] for n in BATCHES}
    spans = {n: [[i * n + 1, (i + 1) * n] for i in range(STREAM // n)] for n in BATCHES}
    evidence_batch = {n: {} for n in BATCHES}
    for q in qs:
        if q['evidence'] and q['family'] in PRIMARY:
            for n in BATCHES:
                evidence_batch[n][q['id']] = (q['evidence'][0] - 1) // n
    items = [('passage', t) for n in BATCHES for t in texts[n]] + [('query', q['text']) for q in qs]
    oracle_items = [('query', texts[n][b]) for n in BATCHES for b in evidence_batch[n].values()]
    vectors = run.embed(items + oracle_items, scratch)
    offset, mid_vectors = 0, {}
    for n in BATCHES:
        mid_vectors[n] = vectors['vectors'][offset:offset + len(texts[n])]
        offset += len(texts[n])
    query_vectors = vectors['vectors'][offset:offset + len(qs)]
    oracle_vectors = vectors['vectors'][offset + len(qs):]
    seconds_per_text = vectors['seconds'] / len(vectors['vectors'])
    rows = []
    b_tops = {n: [] for n in BATCHES}
    for q, qv in zip(qs, query_vectors):
        terms, usable = run.phrases_used(phrase[q['id']]['tool_calls'], q['text'])
        row = {'id': q['id'], 'family': q['family'], 'terms': terms, 'phrases_usable': usable, 'arms': {}}
        recalled = run.bridge(mode='recall', db=str(db), task=q['text'], responses=[phrase[q['id']]['response']])
        a_relevant = [span8[m['ID']] for m in run.bridge(mode='search', db=str(db), terms=terms,
                                                         limit=run.DEPTH)['result'] or []]
        row['arms']['A'] = {'delivered': [span8[m['ID']] for m in run.recall_mids(recalled['result'], mids)],
                            'relevant': a_relevant}
        direct = run.bridge(mode='recall', db=str(db), task=q['text'], direct=True)
        row['arms']['A0'] = {'delivered': [span8[m['ID']] for m in run.recall_mids(direct['result'], mids)],
                             'relevant': [span8[m['ID']] for m in run.bridge(
                                 mode='search', db=str(db), terms=[q['text']], limit=run.DEPTH)['result'] or []]}
        for n in BATCHES:
            order, scores = run.cosine_rank([qv], mid_vectors[n])
            relevant = [spans[n][i] for i in order[:run.DEPTH]]
            recent = spans[n][-run.RECENT:][::-1]
            b_tops[n].append((order[:run.RECENT], order[0]))
            row['arms'][f'B{n}'] = {'delivered': deliver(recent, relevant[:run.DELIVERED]), 'relevant': relevant,
                                    'top_score': round(scores[order[0]], 4)}
        hybrid = interleave(a_relevant[:run.DELIVERED], row['arms']['B8']['relevant'][:run.DELIVERED])
        row['arms']['H'] = {'delivered': deliver(recent8, hybrid), 'relevant': interleave(
            a_relevant, row['arms']['B8']['relevant'])}
        rows.append(row)
    controls = oracle_controls(db, mids, span8, recent8, texts, spans, evidence_batch, mid_vectors, oracle_vectors)
    for n in BATCHES:
        recent_idx = set(range(STREAM // n - run.RECENT, STREAM // n))
        controls[f'B{n}_top2_equals_recency'] = sum(set(t) == recent_idx for t, _ in b_tops[n])
        controls[f'B{n}_distinct_top1'] = len({top for _, top in b_tops[n]})
    controls['queries'] = len(qs)
    run.write(output / 'rankings.json', {'controls': controls, 'rows': rows, 'recent8': recent8,
                                         'embedding': {'model': vectors['model'], 'package': run.FASTEMBED,
                                                       'seconds_per_text': seconds_per_text}})


def oracle_controls(db, mids, span8, recent8, texts, spans, evidence_batch, mid_vectors, oracle_vectors):
    """Each arm's path must reach every primary evidence mid when handed that mid's own summary."""
    reach = {'A': 0, 'A0': 0, 'H': 0, **{f'B{n}': 0 for n in BATCHES}}
    total = {k: 0 for k in reach}
    vectors = iter(oracle_vectors)
    b8_first = {}
    for n in BATCHES:
        for qid, b in evidence_batch[n].items():
            order, _ = run.cosine_rank([next(vectors)], mid_vectors[n])
            reach[f'B{n}'] += order[0] == b
            total[f'B{n}'] += 1
            if n == 8:
                b8_first[qid] = [spans[8][i] for i in order[:run.DELIVERED]]
    for qid, b in evidence_batch[8].items():
        target = mids[b]
        summary = texts[8][b]
        response = run.tool_response('memory_search_terms', {'phrases': [summary]})
        recalled = run.bridge(mode='recall', db=str(db), task='oracle', responses=[response])
        delivered = [span8[m['ID']] for m in run.recall_mids(recalled['result'], mids)]
        reach['A'] += span8[target['ID']] in delivered
        direct = run.bridge(mode='recall', db=str(db), task=summary, direct=True)
        reach['A0'] += span8[target['ID']] in [span8[m['ID']] for m in run.recall_mids(direct['result'], mids)]
        a_relevant = [span8[m['ID']] for m in run.bridge(mode='search', db=str(db), terms=[summary],
                                                         limit=run.DELIVERED)['result'] or []]
        reach['H'] += span8[target['ID']] in deliver(recent8, interleave(a_relevant, b8_first[qid]))
        for arm in ['A', 'A0', 'H']:
            total[arm] += 1
    return {'oracle_reach': reach, 'oracle_total': total}


def probe(output=OUT):
    """The first paid call: one embo-01 request, never retried."""
    meter = Meter(output, LIMITS)
    entry = meter.post('embedding-probe', run.EMBEDDINGS, {'model': 'embo-01', 'texts': ['probe'], 'type': 'query'},
                       64, 0)
    base = (entry.get('response') or {}).get('base_resp') or {}
    usable = entry.get('status') == 200 and base.get('status_code') == 0 and bool(
        (entry.get('response') or {}).get('vectors'))
    run.write(output / 'embedding-probe.json', {'status': entry.get('status'), 'base_resp': base, 'usable': usable,
                                                'error_body': entry.get('error_body')})
    if usable:
        raise SystemExit('embo-01 answered: the preregistration stops collection here for the owner to decide')


def main():
    command = sys.argv[1]
    output = Path(sys.argv[2]) if len(sys.argv) > 2 else OUT
    output.mkdir(parents=True, exist_ok=True)
    {'probe': probe, 'design': design, 'converse': converse, 'assemble': assemble, 'synthesize': synthesize,
     'phrases': phrases, 'rank': rank}[command](output)


if __name__ == '__main__':
    main()
