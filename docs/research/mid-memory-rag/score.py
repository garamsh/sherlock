"""Score recorded evidence offline under the preregistered rule; makes no network call."""
import json
import math
from pathlib import Path
import re
import sys

HERE = Path(__file__).resolve().parent
QUERIES = {q['id']: q for q in json.loads((HERE / 'queries.json').read_text())}
FAMILIES = ['exact', 'paraphrase', 'implicit', 'correction', 'absent', 'recent']
PRIMARY = ['exact', 'paraphrase', 'implicit', 'correction']
ARMS = ['A', 'A0', 'B', 'C', 'D']
KS = [1, 3, 5, 7, 10, 20]
ORACLE_PASS = 19
# Answer keys fixed in preregistration.md; absent and correction also need the author's judgment.
LIVE_KEYS = {
    'exact-01': [r'4471'],
    'paraphrase-01': [r'22', r'3:30|half past three|15:30'],
    'implicit-01': [r'\n\t'],
    'correction-01': [r'19'],
    'absent-01': [],
    'recent-01': [r'sage green'],
}


def wilson(hits, n):
    if n == 0:
        return [None, None]
    z = 1.959964
    p = hits / n
    centre = (p + z * z / (2 * n)) / (1 + z * z / n)
    half = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / (1 + z * z / n)
    return [round(max(0, centre - half), 3), round(min(1, centre + half), 3)]


def sign_test(wins, losses):
    """Exact two-sided binomial p over discordant pairs."""
    n = wins + losses
    if n == 0:
        return 1.0
    tail = sum(math.comb(n, i) for i in range(0, min(wins, losses) + 1)) / 2 ** n
    return min(1.0, 2 * tail)


def intervals(ids, mids):
    return [(mids[i]['SourceFirst'], mids[i]['SourceLast']) for i in ids]


def reach(sources, spans):
    """Fraction of evidence sources inside any returned mid's inclusive interval."""
    if not sources:
        return None
    return sum(any(a <= s <= b for a, b in spans) for s in sources) / len(sources)


def first_rank(sources, spans):
    for rank, (a, b) in enumerate(spans, 1):
        if any(a <= s <= b for s in sources):
            return rank
    return None


def score_rows(rankings):
    mids = {m['ID']: m for m in rankings['mids']}
    per_query = []
    for row in rankings['rows']:
        q = QUERIES[row['id']]
        entry = {'id': row['id'], 'family': row['family'], 'arms': {}}
        for arm in ARMS:
            data = row['arms'][arm]
            delivered, relevant = intervals(data['delivered'], mids), intervals(data['relevant'], mids)
            result = {'delivered_hit': reach(q['evidence'], delivered),
                      'rank': first_rank(q['evidence'], relevant) if q['evidence'] else None,
                      'relevant_count': len(data['relevant'])}
            for k in KS:
                result[f'recall@{k}'] = reach(q['evidence'], relevant[:k])
            if q['obsolete']:
                result['obsolete_delivered'] = reach(q['obsolete'], delivered)
                old = first_rank(q['obsolete'], relevant)
                new = result['rank']
                result['newer_ranked_first'] = new is not None and (old is None or new <= old)
            entry['arms'][arm] = result
        per_query.append(entry)
    return per_query


def aggregate(per_query):
    table = {}
    for arm in ARMS:
        table[arm] = {}
        for family in FAMILIES + ['primary']:
            rows = [r['arms'][arm] for r in per_query
                    if (r['family'] in PRIMARY if family == 'primary' else r['family'] == family)]
            present = [r for r in rows if r['delivered_hit'] is not None]
            cell = {'n': len(rows)}
            if present:
                hits = sum(r['delivered_hit'] for r in present)
                cell['delivered_hits'] = hits
                cell['delivered_rate'] = round(hits / len(present), 3)
                cell['delivered_ci'] = wilson(hits, len(present))
                for k in KS:
                    cell[f'recall@{k}'] = round(sum(r[f'recall@{k}'] for r in present) / len(present), 3)
                cell['mrr@20'] = round(sum(1 / r['rank'] if r['rank'] else 0 for r in present) / len(present), 3)
            else:
                nonempty = sum(r['relevant_count'] > 0 for r in rows)
                cell['relevant_nonempty'] = nonempty
                cell['relevant_nonempty_ci'] = wilson(nonempty, len(rows))
            if family == 'correction':
                cell['newest_delivered'] = sum(r['delivered_hit'] for r in rows)
                cell['obsolete_only_delivered'] = sum(r['obsolete_delivered'] > 0 and not r['delivered_hit'] for r in rows)
                cell['newer_ranked_first'] = sum(r['newer_ranked_first'] for r in rows)
            table[arm][family] = cell
    return table


def auc(present, absent):
    """Probability a present query's top-1 score exceeds an absent one's (ties count half)."""
    pairs = [(p > a) + 0.5 * (p == a) for p in present for a in absent]
    return round(sum(pairs) / len(pairs), 3) if pairs else None


def controls_pass(controls):
    real = controls['real_store']
    oracle = {arm: real['oracle_reach'].get(arm, 0) >= ORACLE_PASS for arm in ARMS if arm in real['oracle_reach']}
    recency = real['similarity_top2_equals_recency'] < real['queries'] / 2
    distinct = real['distinct_similarity_top1'] >= 20
    return {'oracle': oracle, 'similarity_differs_from_recency': recency, 'similarity_top1_distinct': distinct}


def verdict(table, per_query, checks, live):
    wins = sum(r['arms']['B']['delivered_hit'] == 1 and r['arms']['A']['delivered_hit'] == 0
               for r in per_query if r['family'] in PRIMARY)
    losses = sum(r['arms']['B']['delivered_hit'] == 0 and r['arms']['A']['delivered_hit'] == 1
                 for r in per_query if r['family'] in PRIMARY)
    p = sign_test(wins, losses)
    family_loss = {f: table['A'][f]['delivered_hits'] - table['B'][f]['delivered_hits'] for f in PRIMARY}
    guard = (all(v < 2 for v in family_loss.values())
             and table['B']['correction']['newest_delivered'] >= table['A']['correction']['newest_delivered'] - 1)
    required = [checks['oracle'].get('A'), checks['oracle'].get('B'), checks['similarity_differs_from_recency'],
                checks['similarity_top1_distinct'], live.get('A_inspect_works'), live.get('B_inspect_works')]
    if not all(required):
        outcome = 'undetermined'
        reason = 'a required control failed'
    elif wins > losses and p < 0.05 and guard:
        outcome, reason = 'valid', 'B reaches evidence significantly more often and no guardrail fails'
    elif (losses > wins and p < 0.05) or (wins > losses and p < 0.05 and not guard):
        outcome = 'not valid'
        reason = 'A reaches evidence significantly more often' if losses > wins else 'B wins overall but a guardrail fails'
    elif not guard:
        outcome, reason = 'not valid', 'no significant overall difference and a guardrail fails'
    else:
        outcome, reason = 'undetermined', 'no significant difference between B and A on the primary queries'
    return {'B_only': wins, 'A_only': losses, 'sign_test_p': round(p, 4), 'family_A_minus_B': family_loss,
            'guardrails_hold': guard, 'required_controls': required, 'verdict': outcome, 'reason': reason}


def score_live(live, rankings, judgments):
    mids = {m['ID']: m for m in rankings['mids']}
    cells = {}
    for key, cell in live.items():
        q = QUERIES[cell['query']]
        inspected = [t['id'] for t in cell['inspections']]
        ok = [t['id'] for t in cell['inspections'] if not str(t['result']).startswith(('invalid', 'conversation'))]
        final = re.sub(r'<think>.*?</think>', '', cell['final'] or '', flags=re.S)
        keys = all(re.search(k, final, re.I) for k in LIVE_KEYS[cell['query']]) if LIVE_KEYS[cell['query']] else None
        delivered = rankings_row(rankings, cell['query'])['arms'][cell['arm']]['delivered']
        cells[key] = {'arm': cell['arm'], 'query': cell['query'], 'evidence_delivered': bool(
            q['evidence'] and reach(q['evidence'], intervals(delivered, mids))),
            'model_calls': sum(1 for m in cell['messages'] if m['role'] == 'assistant'),
            'inspections': inspected, 'successful_inspections': len(ok),
            'drilled_to_evidence': bool(set(inspected) & set(q['evidence'])),
            'answer_keys_match': keys, 'author_judgment': judgments.get(key)}
    summary = {f'{arm}_inspect_works': any(c['successful_inspections'] for c in cells.values() if c['arm'] == arm)
               for arm in ['A', 'B']}
    return cells, summary


def rankings_row(rankings, qid):
    return next(r for r in rankings['rows'] if r['id'] == qid)


def cost(evidence, rankings):
    calls = json.loads((evidence / 'calls.json').read_text())
    groups = {}
    for c in calls:
        kind = c['cell'].split('-')[0]
        g = groups.setdefault(kind, {'calls': 0, 'input': 0, 'output': 0, 'seconds': 0.0})
        g['calls'] += 1
        g['input'] += c['charged_input']
        g['output'] += c['charged_output']
        g['seconds'] = round(g['seconds'] + c.get('seconds', 0), 3)
    total = {k: sum(g[k] for g in groups.values()) for k in ['calls', 'input', 'output']}
    total['usd'] = round((total['input'] * 0.30 + total['output'] * 1.20) / 1e6, 5)
    n = len(rankings['rows'])
    phrase = groups.get('phrase', {'calls': 0, 'input': 0, 'output': 0, 'seconds': 0})
    embed = sum(r['embed_seconds_per_text'] for r in rankings['rows']) / n
    per_turn = {
        'A': {'model_calls': 1, 'embedding_calls': 0, 'input_tokens': round(phrase['input'] / n, 1),
              'output_tokens': round(phrase['output'] / n, 1), 'model_seconds': round(phrase['seconds'] / n, 3)},
        'A0': {'model_calls': 0, 'embedding_calls': 0, 'input_tokens': 0, 'output_tokens': 0, 'model_seconds': 0},
        'B': {'model_calls': 0, 'embedding_calls': 1, 'embed_seconds': round(embed, 4)},
        'C': {'model_calls': 1, 'embedding_calls': round(sum(len(r['terms']) for r in rankings['rows']) / n, 2),
              'input_tokens': round(phrase['input'] / n, 1), 'output_tokens': round(phrase['output'] / n, 1)},
        'D': {'model_calls': 0, 'embedding_calls': 1, 'embed_seconds': round(embed, 4)},
    }
    return {'groups': groups, 'total': total, 'per_turn': per_turn}


def main(evidence):
    evidence = Path(evidence)
    rankings = json.loads((evidence / 'rankings.json').read_text())
    per_query = score_rows(rankings)
    table = aggregate(per_query)
    checks = controls_pass(rankings['controls'])
    live_path, judgments_path = evidence / 'live.json', HERE / 'judgments.json'
    judgments = json.loads(judgments_path.read_text()) if judgments_path.exists() else {}
    live_cells, live_summary = score_live(json.loads(live_path.read_text()), rankings, judgments) \
        if live_path.exists() else ({}, {})
    top = {'present': [], 'absent': []}
    for r in rankings['rows']:
        if r['family'] in PRIMARY:
            top['present'].append(r['arms']['B']['top_scores'][0])
        elif r['family'] == 'absent':
            top['absent'].append(r['arms']['B']['top_scores'][0])
    result = {'per_query': per_query, 'table': table, 'controls': checks, 'live': live_cells,
              'live_controls': live_summary, 'top1_score_auc_present_vs_absent': auc(top['present'], top['absent']),
              'verdict': verdict(table, per_query, checks, live_summary)}
    if (evidence / 'calls.json').exists():
        result['cost'] = cost(evidence, rankings)
    (evidence / 'scores.json').write_text(json.dumps(result, indent=1) + '\n')
    print(json.dumps(result['verdict'], indent=1))


if __name__ == '__main__':
    main(sys.argv[1] if len(sys.argv) > 1 else HERE / 'evidence')
