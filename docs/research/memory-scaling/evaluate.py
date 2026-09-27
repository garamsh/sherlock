"""Score evidence/rankings.jsonl under preregistration.md's decision rule; offline, writes evidence/scores.json.

    python3 docs/research/memory-scaling/evaluate.py
"""
import json
from pathlib import Path
import sys

HERE = Path(__file__).resolve().parent
RESEARCH = HERE.parent
sys.path.insert(0, str(RESEARCH / 'mid-memory-rag'))
from replication_score import binomial_cdf, paired_interval  # noqa: E402

EVIDENCE = HERE / 'evidence'
QUERIES = RESEARCH / 'mid-memory-rag' / 'evidence' / 'replication' / 'queries.json'
PRIMARY = ['exact', 'paraphrase', 'implicit', 'correction']
SCALES = ['s0', 's1', 's2', 's3', 's4']
BUDGETED = ['B4', 'B8', 'B16', 'T', 'K', 'X', 'R']
COMPARED = ['B8', 'B16', 'T', 'K', 'X', 'R']
LAYOUT = {'S4': 'B4', 'X': 'B4', 'R': 'B4', 'Kr': 'K', 'DR-rel': 'DR', 'DR-new': 'DR'}
ALPHA = 0.05
NO_WORSE = -0.05
REPRODUCE = 212
KS = [1, 5, 20]


def covers(spans, evidence):
    return any(first <= e <= last for first, last in spans for e in evidence)


def mcnemar(wins, losses):
    """Exact two-sided McNemar p on the discordant pairs."""
    n = wins + losses
    return 1.0 if n == 0 else min(1.0, 2 * binomial_cdf(min(wins, losses), n, 0.5))


def holm(pvalues):
    out, running = {}, 0.0
    ordered = sorted(pvalues, key=pvalues.get)
    for i, key in enumerate(ordered):
        running = max(running, min(1.0, (len(ordered) - i) * pvalues[key]))
        out[key] = running
    return out


def compare(a, b, ids):
    """Arm a against arm b on the paired queries ids."""
    wins = sum(a[i] and not b[i] for i in ids)
    losses = sum(b[i] and not a[i] for i in ids)
    low, high = paired_interval(wins, losses, len(ids))
    return {'hits': sum(a[i] for i in ids), 'against': sum(b[i] for i in ids), 'wins': wins, 'losses': losses,
            'difference': (wins - losses) / len(ids), 'interval': [low, high], 'p': mcnemar(wins, losses)}


def hit_table(rows, queries):
    """{(scale, arm): {query: hit}} over queries with evidence."""
    evidence = {q['id']: q['evidence'] for q in queries if q['evidence']}
    table = {}
    for r in rows:
        if r['query'] in evidence:
            table.setdefault((r['scale'], r['arm']), {})[r['query']] = covers(r['delivered'], evidence[r['query']])
    return table


def storage_ratio(storage, scale, arm):
    s = next(s for s in storage if s['scale'] == scale and s['arm'] == LAYOUT.get(arm, arm))
    return {'replication': s['replication_stored_chars'] / s['replication_source_chars'],
            'total': s['stored_chars'] / s['source_chars'], 'units': s['units']}


def decide(hits, ids, storage, arms):
    """The null condition, the comparisons at the decisive scale, and the recommendation."""
    null = compare(hits['s4', 'S4'], hits['s0', 'S4'], ids)
    degrades = null['losses'] > null['wins'] and null['p'] < ALPHA
    decisive = 's4' if degrades else 's0'
    budgeted = [a for a in BUDGETED if a in arms]
    degradation = {a: compare(hits['s4', a], hits['s0', a], ids) for a in budgeted}
    for a, p in holm({a: d['p'] for a, d in degradation.items()}).items():
        degradation[a]['p_holm'] = p
    base = storage_ratio(storage, decisive, 'B4')['replication']
    comparisons = {a: compare(hits[decisive, a], hits[decisive, 'B4'], ids) for a in COMPARED if a in arms}
    for a, p in holm({a: c['p'] for a, c in comparisons.items()}).items():
        c = comparisons[a]
        c['p_holm'] = p
        c['stored_per_source'] = storage_ratio(storage, decisive, a)['replication']
        c['delivers_more'] = c['wins'] > c['losses'] and p < ALPHA
        c['no_worse_for_less_storage'] = c['interval'][0] >= NO_WORSE and c['stored_per_source'] < base
    more = [a for a, c in comparisons.items() if c['delivers_more']]
    cheaper = [a for a, c in comparisons.items() if c['no_worse_for_less_storage']]
    if more:
        pick = min(more, key=lambda a: (-comparisons[a]['hits'], comparisons[a]['stored_per_source']))
        reason = 'delivers more than B4'
    elif cheaper:
        pick = min(cheaper, key=lambda a: comparisons[a]['stored_per_source'])
        reason = 'no worse than B4 for less stored text'
    else:
        pick, reason = None, 'no arm delivers more, and none is no worse for less storage'
    return {'null_condition': dict(null, degrades=degrades), 'decisive_scale': decisive,
            'stresses_retrieval': degrades, 'degradation': degradation, 'b4_stored_per_source': base,
            'comparisons': comparisons, 'recommendation': pick, 'reason': reason}


def recall_at(rows, ids, scale, arm):
    ranks = [r['rank'] for r in rows if r['scale'] == scale and r['arm'] == arm and r['query'] in ids]
    return {f'@{k}': sum(r is not None and r <= k for r in ranks) / len(ranks) for k in KS}


def main():
    queries = json.loads(QUERIES.read_text())
    rows = [json.loads(line) for line in (EVIDENCE / 'rankings.jsonl').read_text().splitlines()]
    study = json.loads((EVIDENCE / 'study.json').read_text())
    hits = hit_table(rows, queries)
    ids = [q['id'] for q in queries if q['family'] in PRIMARY]
    family = {q['id']: q['family'] for q in queries}
    arms = sorted({arm for _, arm in hits}, key=lambda a: (['S4'] + BUDGETED + ['DR-rel', 'DR-new', 'B8m', 'B8c', 'Kr']).index(a))

    table = {}
    for (scale, arm), per in sorted(hits.items()):
        counts = {f: sum(v for q, v in per.items() if family[q] == f) for f in PRIMARY + ['recent']}
        chars = [r['chars'] for r in rows if r['scale'] == scale and r['arm'] == arm and r['query'] in ids]
        table.setdefault(scale, {})[arm] = dict(counts, primary=sum(per[q] for q in ids),
                                                mean_chars=sum(chars) / len(chars), max_chars=max(chars),
                                                stored_per_source=storage_ratio(study['storage'], scale, arm))
    mutation = (EVIDENCE / 'mutation.txt').read_text().splitlines() if (EVIDENCE / 'mutation.txt').exists() else []
    controls = study['controls']
    checks = {
        'reproduction_primary_hits': table['s0']['S4']['primary'],
        'reproduction_holds': table['s0']['S4']['primary'] == REPRODUCE,
        'parity_identical_deliveries': controls['parity_identical_deliveries'],
        'distractors_outside_gold': controls['distractors_outside_gold_ids']
        and controls['distractor_messages_with_gold_text'] == 0,
        'reach': all(controls['reach'].values()),
        'mutations_fail': bool(mutation) and all(line.startswith(('FAILS', 'control')) for line in mutation),
    }
    checks['required_pass'] = checks['distractors_outside_gold'] and checks['reach'] and checks['mutations_fail']
    result = decide(hits, ids, study['storage'], arms)
    if not checks['required_pass']:
        result['recommendation'], result['reason'] = None, 'undetermined: a required control failed'
    secondary = {'B4_against_S4_at_s0': compare(hits['s0', 'B4'], hits['s0', 'S4'], ids)}
    if ('s1', 'B8m') in hits:
        secondary['B8m_against_B8_at_s1'] = compare(hits['s1', 'B8m'], hits['s1', 'B8'], ids)
    for arm in ('DR-rel', 'DR-new'):
        for scale in ('s0', 's4'):
            if (scale, arm) in hits:
                secondary[f'amendment1_{arm}_against_B4_at_{scale}'] = compare(hits[scale, arm], hits[scale, 'B4'], ids)
    if ('s0', 'Kr') in hits:
        secondary['exploratory_Kr_against_B4_at_s0'] = compare(hits['s0', 'Kr'], hits['s0', 'B4'], ids)
    if ('s1', 'B8c') in hits:
        secondary['exploratory_B8m_against_B8c_at_s1'] = compare(hits['s1', 'B8m'], hits['s1', 'B8c'], ids)
    recall = {s: {a: recall_at(rows, set(ids), s, a) for a in arms if (s, a) in hits} for s in ('s0', 's4')}
    scores = {'budget': study['budget'], 'controls': checks, 'decision': result, 'secondary': secondary,
              'table': table, 'recall_before_budget': recall,
              'per_query': {q: {f'{s}/{a}': hits[s, a][q] for s, a in sorted(hits)} for q in ids}}
    (EVIDENCE / 'scores.json').write_text(json.dumps(scores, indent=1) + '\n')
    for scale in SCALES:
        print(scale, {a: table[scale][a]['primary'] for a in table[scale]})
    print(json.dumps(checks), '\nnull:', result['null_condition'], '\ndecisive:', result['decisive_scale'],
          '\nrecommendation:', result['recommendation'], '-', result['reason'])


if __name__ == '__main__':
    main()
