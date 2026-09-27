"""Run the fixed memory read-policy study; preflight never contacts a model."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
import urllib.request

HERE = Path(__file__).resolve().parent
CORPUS = json.loads((HERE / 'corpus.json').read_text())
TASKS = json.loads((HERE / 'tasks.json').read_text())
BINARY = '/tmp/recall-842-store'
ENDPOINT = 'https://api.minimax.io/v1/chat/completions'
SYSTEM = ('Complete the user task using relevant older conversation. Memory summaries are '
          'hints, not evidence: inspect original sources before applying their facts or rules. '
          'Later corrections supersede earlier claims. Apply relevant cross-project rules. '
          'Cite source IDs and short exact quotations after the finished artifact. Do not '
          'invent unavailable facts. Use tools economically and batch inspections. Keep '
          'reasoning brief and deliver the artifact within the available calls.')
REWRITE = ('Give up to four short search phrases for the older conversation that could '
           'answer this task. Include alternate wording and each topic separately. '
           'Call memory_search_terms; do not answer the task.')


def tool(name, description, properties):
    return {'type': 'function', 'function': {'name': name, 'description': description,
            'parameters': {'type': 'object', 'properties': properties,
                           'required': list(properties), 'additionalProperties': False}}}


PHRASES = {'phrases': {'type': 'array', 'items': {'type': 'string'}, 'maxItems': 4}}
INSPECT = tool('memory_inspect', 'Read up to eight exact source IDs from retrieved intervals.',
               {'ids': {'type': 'array', 'items': {'type': 'integer'}, 'maxItems': 8}})
SEARCH = tool('memory_search', 'Search older memory again when another information need emerges.', PHRASES)
TERMS = tool('memory_search_terms', 'Supply search phrases for older conversation.', PHRASES)


def store(**kwargs):
    result = subprocess.run([BINARY], input=json.dumps({'corpus': CORPUS, **kwargs}),
                            text=True, capture_output=True, check=True)
    return json.loads(result.stdout)


def intervals(result):
    found = set()
    for hit in result.get('Memories') or []:
        first, last = map(int, re.search(r'sources:(\d+)-(\d+)', hit['Content']).groups())
        found.update(range(first, last + 1))
    return found


def inspect(ids, allowed):
    if (not isinstance(ids, list) or not 1 <= len(ids) <= 8
            or any(type(i) is not int or i not in allowed for i in ids)):
        return {'error': 'inspect requires one to eight IDs in retrieved source intervals'}
    return store(ids=ids)


def preflight():
    evidence = {'revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(),
                'direct': {}, 'facets': [], 'controls': {}}
    for task in TASKS:
        direct = store(terms=[task['prompt']])
        evidence['direct'][task['id']] = direct
        if task['id'] in ('essay', 'export'):
            assert sum(f['source'] not in intervals(direct) for f in task['facets']) >= 2
        for facet in task['facets']:
            hit = store(terms=[facet['query']])
            assert facet['source'] in intervals(hit), facet
            source = inspect([facet['source']], intervals(hit))[0]
            ep, offset = divmod(facet['source'] - 1, 8)
            assert source['Content'] == CORPUS[ep]['sources'][offset]
            evidence['facets'].append({'task': task['id'], **facet, 'candidates': hit, 'source': source})
    absent = store(terms=['Zephyr launch date'])
    assert {int(x['ID']) for x in absent['Memories']} == {11, 12}
    assert 'error' in inspect([25], intervals(absent))
    assert inspect([89], intervals(absent))[0]['Content'] == CORPUS[11]['sources'][0]
    evidence['controls'] = {'absent_only_recent': absent, 'unretrieved_refused': True,
                            'retrieved_source_accepted': True}
    # Removing the relevant summary must break the reachability assertion.
    saved = CORPUS[1]['summary']
    CORPUS[1]['summary'] = 'Unrelated dormant archive.'
    assert 9 not in intervals(store(terms=['Lantern composition method']))
    CORPUS[1]['summary'] = saved
    evidence['controls']['summary_removal_loses_required_source'] = True
    assert not store(terms=['the and', '!!!'], check_terms=True)
    assert store(terms=['Lantern'], check_terms=True) == ['lantern']
    evidence['controls']['production_stopword_usability'] = True
    return evidence


def write(path, value):
    path.write_text(json.dumps(value, indent=2, ensure_ascii=False) + '\n')


class Meter:
    def __init__(self, output):
        self.output = output
        self.calls = []
        self.input = 0
        self.completion = 0

    def chat(self, cell, messages, tools, maximum):
        request = {'model': 'MiniMax-M2', 'temperature': 0, 'max_tokens': maximum,
                   'messages': messages, 'tools': tools, 'tool_choice': 'auto'}
        body = json.dumps(request).encode()
        reserve = len(body) + 1024
        if (len(body) > 14000 or len(self.calls) >= 48 or self.input + reserve > 120000
                or self.completion + maximum > 20000
                or ((self.input + reserve) * .30 + (self.completion + maximum) * 1.20) / 1e6 > .08):
            raise RuntimeError('pre-request global budget or context reservation refused')
        entry = {'cell': cell, 'request': request, 'input_reservation': reserve,
                 'started_utc': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())}
        self.calls.append(entry)
        write(self.output / 'calls.json', self.calls)
        started = time.monotonic()
        req = urllib.request.Request(ENDPOINT, data=body, headers={
            'Authorization': 'Bearer ' + os.environ['MINIMAX_KEY'], 'Content-Type': 'application/json'})
        try:
            with urllib.request.urlopen(req, timeout=120) as response:
                raw = response.read()
            result = json.loads(raw)
            usage = result['usage']
            pin, pout = usage['prompt_tokens'], usage['completion_tokens']
            if type(pin) is not int or type(pout) is not int or min(pin, pout) < 0:
                raise ValueError('invalid model usage')
            entry['response'] = result
            self.input += pin
            self.completion += pout
            if pin > reserve or pout > maximum:
                raise RuntimeError('provider exceeded reserved token allowance')
        except Exception as error:
            entry['error'] = type(error).__name__
            # An uncertain request consumes its full reservation and ends collection.
            if 'response' not in entry:
                self.input += reserve
                self.completion += maximum
            raise
        finally:
            entry['latency_seconds'] = time.monotonic() - started
            write(self.output / 'calls.json', self.calls)
            write(self.output / 'spend.json', {'http_calls': len(self.calls), 'input_tokens': self.input,
                  'output_tokens': self.completion,
                  'usd_at_standard_uncached_rates': (self.input * .30 + self.completion * 1.20) / 1e6})
        return result['choices'][0], pout


def phrases(call):
    value = json.loads(call['function']['arguments'])['phrases']
    if not isinstance(value, list) or any(not isinstance(v, str) for v in value):
        raise ValueError('invalid search phrases')
    return value[:4]


def run_cell(meter, task, policy):
    cell = policy + '-' + task['id']
    result = {'cell': cell, 'task': task['prompt'], 'trace': [], 'answer': '', 'status': 'call_limit'}
    allowance = 1600
    calls = 0
    terms = [task['prompt']]
    if policy == 'B':
        choice, used = meter.chat(cell, [{'role': 'system', 'content': REWRITE},
                                      {'role': 'user', 'content': task['prompt']}], [TERMS], 400)
        allowance -= used
        calls += 1
        for call in choice['message'].get('tool_calls') or []:
            if call['function']['name'] == 'memory_search_terms':
                terms = phrases(call)
                break
        # The production recall falls back on task text for unusable phrases.
        if not store(terms=terms, check_terms=True):
            terms = [task['prompt']]
    candidates = store(terms=terms)
    allowed = intervals(candidates)
    result['trace'].append({'operation': 'initial_search', 'terms': terms, 'result': candidates})
    messages = [{'role': 'system', 'content': SYSTEM},
                {'role': 'user', 'content': task['prompt']},
                {'role': 'user', 'content': 'Retrieved memory: ' + json.dumps(candidates)}]
    while calls < 4 and allowance > 0:
        offered = [INSPECT] + ([SEARCH] if policy == 'C' else [])
        if calls == 3:
            messages.append({'role': 'user', 'content': 'This is the final call. Deliver the completed answer now.'})
        choice, used = meter.chat(cell, messages, offered, min(800, allowance))
        calls += 1
        allowance -= used
        message = choice['message']
        messages.append(message)
        if choice.get('finish_reason') == 'length':
            result['answer'] = message.get('content') or ''
            result['status'] = 'output_limit'
            break
        tool_calls = message.get('tool_calls') or []
        if not tool_calls:
            result['answer'] = message.get('content') or ''
            result['status'] = 'completed' if result['answer'] else 'empty_answer'
            break
        for call in tool_calls:
            name = call['function']['name']
            args = json.loads(call['function']['arguments'])
            if name == 'memory_search' and policy == 'C':
                output = store(terms=phrases(call))
                allowed.update(intervals(output))
            elif name == 'memory_inspect':
                output = inspect(args.get('ids'), allowed)
            else:
                output = {'error': 'tool not available'}
            result['trace'].append({'operation': name, 'arguments': args, 'result': output})
            messages.append({'role': 'tool', 'tool_call_id': call['id'], 'content': json.dumps(output)})
    result['calls'] = calls
    result['output_tokens'] = 1600 - allowance
    return result


def main():
    if len(sys.argv) != 3 or sys.argv[1] not in ('preflight', 'collect'):
        raise SystemExit('usage: python3 run.py preflight|collect OUTPUT_DIRECTORY')
    mode, target = sys.argv[1:]
    output = Path(target)
    output.mkdir(parents=True, exist_ok=True)
    if mode == 'preflight':
        write(output / 'preflight.json', preflight())
        print('preflight: 7 facet-source paths, 4 direct queries and 5 controls passed')
        return
    if (output / 'calls.json').exists():
        raise SystemExit('refusing to repeat an existing collection')
    preflight()
    hashes = {name: hashlib.sha256((HERE / name).read_bytes()).hexdigest()
              for name in ['run.py', 'store.go', 'corpus.json', 'tasks.json', 'preregistration.md']}
    write(output / 'manifest.json', {'hashes': hashes, 'source_revision': subprocess.check_output(
          ['git', 'rev-parse', 'HEAD'], text=True).strip(), 'endpoint': ENDPOINT,
          'model': 'MiniMax-M2', 'runtime': sys.version, 'system': SYSTEM, 'rewrite': REWRITE})
    meter = Meter(output)
    results = []
    for task in TASKS:
        for policy in ['A', 'B', 'C']:
            try:
                result = run_cell(meter, task, policy)
            except Exception as error:
                results.append({'cell': policy + '-' + task['id'], 'status': 'collection_error',
                                'error': type(error).__name__})
                write(output / 'results.json', results)
                raise
            results.append(result)
            write(output / 'results.json', results)
            print(result['cell'], result['status'], result['calls'], flush=True)


if __name__ == '__main__':
    main()
