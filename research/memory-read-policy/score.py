"""Reproduce artifact checks and combine them with the author's rubric judgments."""
import ast
import csv
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent
EVIDENCE = HERE / 'evidence'


def artifact_check(answer):
    blocks = re.findall(r'```python\n(.*?)```', answer, re.S)
    if not blocks:
        return {'runnable': False, 'reason': 'no closed Python block'}
    code = blocks[0]
    try:
        tree = ast.parse(code)
    except SyntaxError as error:
        return {'runnable': False, 'reason': f'SyntaxError line {error.lineno}: {error.msg}'}
    # Execution is limited to the two reviewed, import-only/function-only artifacts.
    if any(not isinstance(n, (ast.Import, ast.ImportFrom, ast.FunctionDef, ast.Expr, ast.If))
           for n in tree.body):
        raise ValueError('artifact requires manual execution review')
    for n in ast.walk(tree):
        if isinstance(n, ast.Import) and any(a.name not in ('csv', 'io') for a in n.names):
            raise ValueError('unreviewed import')
        if isinstance(n, ast.ImportFrom):
            raise ValueError('unreviewed import')
    rows = [('z,west', 3), ('_private', 8), ('a"east', 2), ('middle', 0)]
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory) / 'artifact.py'
        path.write_text(code)
        driver = ('import json,runpy; f=runpy.run_path("artifact.py")["render_export"]; '
                  'print(json.dumps(f(' + repr(rows) + ')))')
        process = subprocess.run(['python3', '-I', '-c', driver], cwd=directory,
                                 env={'PATH': os.environ['PATH']}, capture_output=True,
                                 text=True, timeout=3)
        if process.returncode:
            return {'runnable': False, 'reason': process.stderr}
    output = json.loads(process.stdout)
    parsed = list(csv.reader(io.StringIO(output)))
    expected = [['z,west', '3'], ['a"east', '2'], ['middle', '0']]
    public = [r for r in parsed[1:] if not r[0].startswith('_')]
    return {'runnable': True, 'input': rows, 'output': output,
            'current_header': parsed[0] == ['label', 'total'],
            'privacy': parsed[1:] == expected, 'order_and_quoting': public == expected,
            'lf_only': '\r' not in output and output.endswith('\n'),
            'stdlib_csv_and_stringio': 'csv.writer' in code and 'io.StringIO' in code}


def main():
    results = json.loads((EVIDENCE / 'results.json').read_text())
    calls = json.loads((EVIDENCE / 'calls.json').read_text())
    tasks = {t['id']: t for t in json.loads((HERE / 'tasks.json').read_text())}
    corpus = json.loads((HERE / 'corpus.json').read_text())
    judgments = json.loads((HERE / 'judgments.json').read_text())
    manifest = json.loads((EVIDENCE / 'manifest.json').read_text())
    assert all(hashlib.sha256((HERE / f).read_bytes()).hexdigest() == digest
               for f, digest in manifest['hashes'].items())
    out = []
    answers = EVIDENCE / 'answers'
    answers.mkdir(exist_ok=True)
    for result in results:
        cell = result['cell']
        task = tasks[cell.split('-', 1)[1]]
        answer = re.sub(r'<think>.*?</think>\s*', '', result['answer'], flags=re.S).strip()
        (answers / (cell + '.txt')).write_text(answer + '\n')
        inspected = set()
        exposed = set()
        errors = []
        searches = 0
        for event in result['trace']:
            value = event['result']
            if event['operation'] == 'memory_inspect':
                if isinstance(value, list):
                    inspected.update(s['ID'] for s in value)
                else:
                    errors.append(value['error'])
            else:
                searches += event['operation'] == 'memory_search'
                for hit in value.get('Memories') or []:
                    first, last = map(int, re.search(r'sources:(\d+)-(\d+)', hit['Content']).groups())
                    exposed.update(range(first, last + 1))
        facets = []
        judgment = judgments[cell]
        for i, facet in enumerate(task['facets']):
            sid = facet['source']
            ep, offset = divmod(sid - 1, 8)
            original = corpus[ep]['sources'][offset]
            quoted = judgment['quotes'].get(str(sid), '')
            assert not quoted or (quoted in answer and quoted in original), cell
            verified = sid in inspected and bool(quoted)
            if task['id'] == 'exact':
                assert judgment['applied'][i] == (original in answer), cell
            facets.append({'name': facet['name'], 'source': sid,
                           'exposed': sid in exposed, 'inspected': sid in inspected,
                           'exact_quote': quoted, 'verified': verified,
                           'applied': judgment['applied'][i]})
        relevant_calls = [c for c in calls if c['cell'] == cell]
        pin = sum(c['response']['usage']['prompt_tokens'] for c in relevant_calls)
        pout = sum(c['response']['usage']['completion_tokens'] for c in relevant_calls)
        code = artifact_check(answer) if cell.endswith('export') else None
        if code is not None:
            expected = [code.get('runnable', False) and code.get('stdlib_csv_and_stringio', False)
                        and code.get('order_and_quoting', False) and code.get('lf_only', False),
                        code.get('privacy', False), code.get('current_header', False)]
            assert judgment['applied'] == expected
        success = judgment['artifact_complete'] and all(f['applied'] and f['verified'] for f in facets)
        out.append({'cell': cell, 'status': result['status'], **judgment, 'facets': facets,
                    'full_success': success, 'code_check': code,
                    'inspection_refusals': len(errors), 'followup_searches': searches,
                    'calls': len(relevant_calls), 'input_tokens': pin, 'output_tokens': pout,
                    'cost_usd': (pin * .30 + pout * 1.20) / 1e6,
                    'http_seconds': sum(c['latency_seconds'] for c in relevant_calls)})
    (EVIDENCE / 'scores.json').write_text(json.dumps(out, indent=2) + '\n')
    for s in out:
        print(s['cell'], s['full_success'], sum(f['applied'] for f in s['facets']),
              s['calls'], s['input_tokens'], s['output_tokens'], round(s['http_seconds'], 2))


if __name__ == '__main__':
    main()
