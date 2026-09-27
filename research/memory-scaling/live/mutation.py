"""Remove each rule a test names, run the tests, restore it, and record that they failed (evidence/mutation.txt).

    python3 research/memory-scaling/live/mutation.py
"""
import os
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent

# (file, text, replacement, what the mutation removes)
MUTATIONS = [
    ('score.py', "    hit = covers(delivered, query['evidence'])", '    hit = bool(delivered)', 'the hit rule'),
    ('score.py', "'stale': covers(delivered, query['obsolete']) and not hit,",
     "'stale': covers(delivered, query['obsolete']),", 'the stale rule: no evidence delivered'),
    ('score.py', "'near_miss': covers(delivered, query.get('near_miss', []))", "'near_miss': False", 'the near-miss rule'),
    ('screen.py', '    return [held.get((u.first, u.last), u) for u in newest]', '    return k_store[-len(newest):][::-1]',
     "Kr's batch-4 recency"),
    ('score.py', "'margin_holds': delivery['interval'][0] >= margin,", "'margin_holds': True,", 'the delivery margin'),
    ('score.py', "all(v <= allowance for v in guard.values())", 'True', 'the guardrail allowance'),
    ('screen.py', "        out['stated'] = says(gold)", "        out['stated'] = True", 'the stated rule'),
    ('screen.py', "out['stale_stated'] = says(item['old_key']) and not out['stated']",
     "out['stale_stated'] = says(item['old_key'])", 'the stale-stated rule: the new value not stated'),
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
