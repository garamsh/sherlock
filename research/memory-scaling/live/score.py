"""Score #913's free screen under preregistration.md's gate; offline, writes evidence/scores.json.

    python3 research/memory-scaling/live/score.py
"""
import json
from pathlib import Path
import sys

HERE = Path(__file__).resolve().parent
SCALING = HERE.parent
sys.path.insert(0, str(SCALING))
from evaluate import compare, covers  # noqa: E402

EVIDENCE = HERE / 'evidence'
QUERIES = SCALING.parent / 'mid-memory-rag' / 'evidence' / 'replication' / 'queries.json'
PRIMARY = ['exact', 'paraphrase', 'implicit', 'correction']
AGAINST = ['S4', 'B4']
MARGIN = -0.05
ALLOWANCE = 3
REPRODUCE = 212
EXPECTED_MODEL = ('BAAI/bge-base-en-v1.5', 'a5beb1e3e68b9ab74eb54cfd186867f64f240e1a')


def measures(delivered, query):
    """Per-query screen measures from the delivered intervals: hit, stale (an obsolete source delivered with no
    evidence source) and near-miss exposure."""
    hit = covers(delivered, query['evidence'])
    return {'hit': hit,
            'stale': covers(delivered, query['obsolete']) and not hit,
            'near_miss': covers(delivered, query.get('near_miss', []))}


def net_worse(a, b, ids, bad):
    """Queries where arm a is worse than arm b, minus the reverse; `bad` says whether a measure is a loss."""
    return sum(bad(a[i]) and not bad(b[i]) for i in ids) - sum(bad(b[i]) and not bad(a[i]) for i in ids)


STATED_FIELDS = ['stated', 'covered_stated', 'stale_stated', 'near_miss_stated']
DELIVERY = {'gold': 'hit', 'stale': 'stale', 'near_miss': 'near_miss'}
STATED = {'gold': 'stated', 'stale': 'stale_stated', 'near_miss': 'near_miss_stated'}


def gate(per, families, names=DELIVERY, allowance=ALLOWANCE, margin=MARGIN):
    """Preregistration §Gate, without the controls: the margin and every guardrail against S4 and B4, over the
    delivery measures or, with names=STATED, Amendment 1's stated measures."""
    gold, stale, near = names['gold'], names['stale'], names['near_miss']
    primary = [i for f in PRIMARY for i in families[f]]
    kr, checks = per['Kr'], {}
    for other in AGAINST:
        b = per[other]
        pair = {i: kr[i][gold] for i in primary}, {i: b[i][gold] for i in primary}
        delivery = compare(*pair, primary)
        guard = {
            f'exact_{gold}': net_worse(kr, b, families['exact'], lambda m: not m[gold]),
            f'correction_{gold}': net_worse(kr, b, families['correction'], lambda m: not m[gold]),
            f'correction_{stale}': net_worse(kr, b, families['correction'], lambda m: m[stale]),
            f'absent_{near}': net_worse(kr, b, families['absent'], lambda m: m[near]),
        }
        checks[other] = {'delivery': delivery, 'margin_holds': delivery['interval'][0] >= margin,
                         'guardrails': guard, 'guardrails_hold': all(v <= allowance for v in guard.values())}
    return checks, all(c['margin_holds'] and c['guardrails_hold'] for c in checks.values())


def load(path):
    return [json.loads(line) for line in Path(path).read_text().splitlines() if line.strip()]


def main():
    queries = {q['id']: q for q in json.loads(QUERIES.read_text())}
    families = {}
    for q in queries.values():
        families.setdefault(q['family'], []).append(q['id'])
    rows = load(EVIDENCE / 'rankings.jsonl')
    screen = json.loads((EVIDENCE / 'screen.json').read_text())
    earlier = {(r['scale'], r['arm'], r['query']): r['delivered'] for r in load(SCALING / 'evidence' / 'rankings.jsonl')
               if r['scale'] in ('s0', 's4') and r['arm'] in ('S4', 'B4', 'K')}
    earlier_budget = json.loads((SCALING / 'evidence' / 'study.json').read_text())['budget']

    per = {}
    for r in rows:
        per.setdefault(r['scale'], {}).setdefault(r['arm'], {})[r['query']] = dict(
            measures(r['delivered'], queries[r['query']]), states_near_miss=r.get('states_near_miss'), chars=r['chars'],
            rank=r['rank'], **{k: r.get(k, False) for k in STATED_FIELDS})
    s4 = per['s4']
    primary = [i for f in PRIMARY for i in families[f]]

    same = {}
    for scale, arm, mine in (('s0', 'S4', 'S4'), ('s4', 'S4', 'S4'), ('s4', 'B4', 'B4'), ('s4', 'K', 'Kr')):
        delivered = {r['query']: r['delivered'] for r in rows if r['scale'] == scale and r['arm'] == mine}
        same[f'{scale}/{mine}=#901 {arm}'] = sum(delivered[q] == earlier[(scale, arm, q)] for q in queries)
    server = screen['controls']['server']
    controls = {
        'reproduction_hits_s0_S4': sum(per['s0']['S4'][i]['hit'] for i in primary),
        'identical_to_901': same,
        'budget': screen['budget'], 'budget_901': earlier_budget,
        'reach': screen['controls']['reach'],
        'kr_recency_spans': screen['controls']['kr_recency_spans'],
        'server': [server.get('model_id'), server.get('model_sha')],
    }
    passed = {
        'reproduction': controls['reproduction_hits_s0_S4'] == REPRODUCE
                        and same['s0/S4=#901 S4'] == len(queries) and screen['budget'] == earlier_budget,
        'identical_at_s4': all(v == len(queries) for k, v in same.items() if k.startswith('s4')),
        'reach': all(screen['controls']['reach'].values()),
        'kr_recency_batch4': all(s <= 4 for spans in controls['kr_recency_spans'].values() for s in spans),
        'server': tuple(controls['server']) == EXPECTED_MODEL,
    }
    checks, holds = gate(s4, families)
    stated_checks, stated_holds = gate(s4, families, STATED)

    table = {}
    for arm in ('S4', 'B4', 'Kr'):
        m = s4[arm]
        table[arm] = {
            'primary': sum(m[i]['hit'] for i in primary),
            'by_family': {f: sum(m[i]['hit'] for i in families[f]) for f in PRIMARY},
            'correction_stale': sum(m[i]['stale'] for i in families['correction']),
            'absent_near_miss': sum(m[i]['near_miss'] for i in families['absent']),
            'absent_states_near_miss_key': sum(bool(m[i]['states_near_miss']) for i in families['absent']),
            'stated': {f: sum(m[i]['stated'] for i in families[f]) for f in PRIMARY},
            'covered_stated': {f: sum(m[i]['covered_stated'] for i in families[f]) for f in PRIMARY},
            'stated_primary': sum(m[i]['stated'] for i in primary),
            'correction_stale_stated': sum(m[i]['stale_stated'] for i in families['correction']),
            'absent_near_miss_stated': sum(m[i]['near_miss_stated'] for i in families['absent']),
            'mean_chars_primary': sum(m[i]['chars'] for i in primary) / len(primary),
            'recall_before_budget': {k: sum(m[i]['rank'] is not None and m[i]['rank'] <= k for i in primary) / len(primary)
                                     for k in (1, 5, 20)},
        }
    storage = {f"{s['scale']}/{s['arm']}": {'replication': s['replication_stored_chars'] / s['replication_source_chars'],
                                            'whole_store': s['stored_chars'] / s['source_chars']}
               for s in screen['storage']}
    verdict = ('undetermined: a control failed' if not all(passed.values())
               else 'pass: the paid phase may go ahead on the PM\'s go' if holds and stated_holds
               else 'fail: no paid call is made')
    per_query = {arm: {i: s4[arm][i] for i in queries} for arm in s4}
    out = {'verdict': verdict, 'controls': controls, 'controls_pass': passed, 'gate': checks, 'stated_gate': stated_checks,
           'table': table, 'storage': storage, 'per_query_s4': per_query}
    (EVIDENCE / 'scores.json').write_text(json.dumps(out, indent=1) + '\n')
    print(json.dumps({'verdict': verdict, 'controls_pass': passed, 'controls': {k: v for k, v in controls.items()},
                      'gate': checks, 'stated_gate': stated_checks, 'table': table, 'storage': storage}, indent=1))


if __name__ == '__main__':
    main()
