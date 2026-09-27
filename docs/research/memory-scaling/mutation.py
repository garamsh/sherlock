"""Remove each rule a test names, run the tests, restore it, and record that they failed (evidence/mutation.txt).

    python3 docs/research/memory-scaling/mutation.py
"""
import os
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent

# (file, text, replacement, what the mutation removes)
MUTATIONS = [
    ('study.py', 'return any(first <= e <= last for first, last in spans for e in evidence)',
     'return any(first < e <= last for first, last in spans for e in evidence)', 'delivery: the hit rule\'s interval'),
    ('evaluate.py', 'return any(first <= e <= last for first, last in spans for e in evidence)', 'return bool(spans)',
     'delivery: scoring hits from the delivered intervals'),
    ('study.py', '        if total + cost > budget:\n', '        if False:\n', 'budget: the per-turn cap'),
    ('study.py', '    for unit in list(recency) + list(ranked[:depth]):', '    for unit in list(recency) + list(ranked):',
     'budget: the candidate depth'),
    ('study.py', '        if id(unit) in seen:\n            continue\n        cost = unit.size()',
     '        cost = unit.size()', 'dedupe: the budgeted fill'),
    ('study.py', '        if id(unit) in seen:\n            continue\n        seen.add(id(unit))\n        out.append(unit)\n        if len(out)',
     '        seen.add(id(unit))\n        out.append(unit)\n        if len(out)', 'dedupe: the shipped rule'),
    ('study.py', '        if id(value) not in seen:', '        if True:', 'dedupe: one delivery per value under K'),
    ('study.py', "re.compile(r'(?<![A-Za-z0-9])' + re.escape(k) + r'(?![A-Za-z0-9])', re.I)",
     're.compile(re.escape(k), re.I)', 'leak check: the flanking rule'),
    ('study.py', '            if pattern.search(text):', '            if False:', 'leak check: matching a planted value'),
    ('evaluate.py', "    degrades = null['losses'] > null['wins'] and null['p'] < ALPHA", '    degrades = False',
     'decision: the null condition'),
    ('evaluate.py', "c['interval'][0] >= NO_WORSE and ", '', 'decision: the no-worse bound'),
]


def tests():
    command = [sys.executable, '-m', 'unittest', 'discover', '-s', str(HERE), '-p', '*_test.py']
    env = dict(os.environ, PYTHONDONTWRITEBYTECODE='1')
    return subprocess.run(command, cwd=HERE, capture_output=True, text=True, env=env).returncode


def main():
    lines = []
    for name, old, new, what in MUTATIONS:
        file = HERE / name
        original = file.read_text()
        if original.count(old) != 1:
            raise SystemExit(f'{name}: the text to mutate occurs {original.count(old)} times')
        file.write_text(original.replace(old, new))
        try:
            failed = tests() != 0
        finally:
            file.write_text(original)
        lines.append(f"{'FAILS' if failed else 'PASSES'}  {name}: {what}")
    if tests() != 0:
        raise SystemExit('the unmutated tests do not pass')
    lines.append('control: every unmutated test passes')
    (HERE / 'evidence').mkdir(exist_ok=True)
    (HERE / 'evidence' / 'mutation.txt').write_text('\n'.join(lines) + '\n')
    print('\n'.join(lines))


if __name__ == '__main__':
    main()
