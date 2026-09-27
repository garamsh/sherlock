"""Score the replication offline under its preregistered rule, and reproduce its power table; no network call."""
import json
import math
from pathlib import Path
import random
import sys

import score

HERE = Path(__file__).resolve().parent
OUT = HERE / 'evidence' / 'replication'
PRIMARY = ['exact', 'paraphrase', 'implicit', 'correction']
FAMILIES = PRIMARY + ['absent', 'recent']
ARMS = ['A', 'A0', 'B4', 'B8', 'B16', 'H']
DECIDED = ['B8', 'H']
DELTA = 0.10
ORACLE_PASS = 0.95
KS = [1, 5, 7, 20]


def binomial_cdf(k, n, p):
    return sum(math.comb(n, i) * p ** i * (1 - p) ** (n - i) for i in range(k + 1))


def clopper_pearson(k, n, alpha=0.05):
    """Exact interval for a binomial proportion, by bisection."""
    def bisect(accept):
        low, high = 0.0, 1.0
        for _ in range(60):
            mid = (low + high) / 2
            low, high = (mid, high) if accept(mid) else (low, mid)
        return (low + high) / 2
    lower = 0.0 if k == 0 else bisect(lambda p: 1 - binomial_cdf(k - 1, n, p) < alpha / 2)
    upper = 1.0 if k == n else bisect(lambda p: binomial_cdf(k, n, p) > alpha / 2)
    return lower, upper


def paired_interval(wins, losses, n):
    """95% interval for the paired difference in hit rate, conditional on the discordant count."""
    m = wins + losses
    if m == 0:
        return 0.0, 0.0
    lower, upper = clopper_pearson(wins, m)
    return m * (2 * lower - 1) / n, m * (2 * upper - 1) / n


def decide(wins, losses, n, guard, controls):
    low, high = paired_interval(wins, losses, n)
    if not controls:
        return 'undetermined', 'a required control failed'
    if low > 0 and guard:
        return 'valid', 'the interval excludes no gain and every guardrail holds'
    if high < DELTA:
        return 'not valid', f'the interval excludes a gain of {DELTA:.2f}'
    return 'undetermined', 'the interval admits both no gain and a gain of the detectable size'


def power(n=280, reps=3000, seed=856, psis=(0.15, 0.20, 0.25, 0.30)):
    """Simulated probability of each verdict at discordance psi and true difference d, under the rule above."""
    table = []
    for psi in psis:
        for d in [0.0, 0.05, 0.07, 0.08, 0.09, 0.10]:
            rng, counts, cache = random.Random(seed), {'valid': 0, 'not valid': 0, 'undetermined': 0}, {}
            for _ in range(reps):
                wins = losses = 0
                for _ in range(n):
                    u = rng.random()
                    if u < (psi + d) / 2:
                        wins += 1
                    elif u < psi:
                        losses += 1
                key = (wins, losses)
                if key not in cache:
                    cache[key] = decide(wins, losses, n, True, True)[0]
                counts[cache[key]] += 1
            table.append({'psi': psi, 'd': d, **{k: round(v / reps, 3) for k, v in counts.items()}})
    return table


def hit(sources, spans):
    return any(a <= s <= b for s in sources for a, b in spans)


def first_rank(sources, spans):
    for rank, (a, b) in enumerate(spans, 1):
        if any(a <= s <= b for s in sources):
            return rank
    return None


def per_query(rankings, queries):
    by_id = {q['id']: q for q in queries}
    out = []
    for row in rankings['rows']:
        q = by_id[row['id']]
        arms = {}
        for arm in ARMS:
            data = row['arms'][arm]
            result = {'hit': hit(q['evidence'], data['delivered']) if q['evidence'] else None,
                      'rank': first_rank(q['evidence'], data['relevant']) if q['evidence'] else None,
                      'relevant_count': len(data['relevant'])}
            for k in KS:
                result[f'recall@{k}'] = hit(q['evidence'], data['relevant'][:k]) if q['evidence'] else None
            if q['obsolete']:
                result['obsolete_delivered'] = hit(q['obsolete'], data['delivered'])
                old = first_rank(q['obsolete'], data['relevant'])
                result['newer_ranked_first'] = result['rank'] is not None and (old is None or result['rank'] <= old)
            if 'top_score' in data:
                result['top_score'] = data['top_score']
            arms[arm] = result
        out.append({'id': q['id'], 'family': q['family'], 'arms': arms})
    return out


def table(rows):
    out = {}
    for arm in ARMS:
        out[arm] = {}
        for family in FAMILIES + ['primary']:
            chosen = [r['arms'][arm] for r in rows if (r['family'] in PRIMARY if family == 'primary'
                                                        else r['family'] == family)]
            cell = {'n': len(chosen)}
            present = [c for c in chosen if c['hit'] is not None]
            if present:
                hits = sum(c['hit'] for c in present)
                cell.update(hits=hits, rate=round(hits / len(present), 3), ci=score.wilson(hits, len(present)),
                            mrr=round(sum(1 / c['rank'] for c in present if c['rank']) / len(present), 3))
                for k in KS:
                    cell[f'recall@{k}'] = round(sum(c[f'recall@{k}'] for c in present) / len(present), 3)
            else:
                nonempty = sum(c['relevant_count'] > 0 for c in chosen)
                cell.update(relevant_nonempty=nonempty, ci=score.wilson(nonempty, len(chosen)))
            if family == 'correction':
                cell.update(newest=sum(c['hit'] for c in chosen),
                            obsolete_only=sum(c['obsolete_delivered'] and not c['hit'] for c in chosen),
                            newer_ranked_first=sum(c['newer_ranked_first'] for c in chosen))
            out[arm][family] = cell
    return out


def controls(rankings):
    c = rankings['controls']
    oracle = {arm: c['oracle_reach'][arm] >= ORACLE_PASS * c['oracle_total'][arm] for arm in c['oracle_reach']}
    recency = {f'B{n}': c[f'B{n}_top2_equals_recency'] < c['queries'] / 2 and c[f'B{n}_distinct_top1'] >= 20
               for n in [4, 8, 16]}
    return {'oracle': oracle, 'similarity_not_recency': recency}


def compare(rows, tab, checks):
    out = {}
    for arm in ARMS[1:]:
        primary = [r for r in rows if r['family'] in PRIMARY]
        wins = sum(r['arms'][arm]['hit'] and not r['arms']['A']['hit'] for r in primary)
        losses = sum(r['arms']['A']['hit'] and not r['arms'][arm]['hit'] for r in primary)
        n = len(primary)
        low, high = paired_interval(wins, losses, n)
        family_delta = {f: tab[arm][f]['hits'] - tab['A'][f]['hits'] for f in PRIMARY}
        guard = (all(v >= -DELTA * tab['A'][f]['n'] for f, v in family_delta.items())
                 and tab[arm]['correction']['newest'] >= tab['A']['correction']['newest'] - DELTA * tab['A']['correction']['n'])
        needed = [checks['oracle'].get('A'), checks['oracle'].get(arm)]
        if arm.startswith('B'):
            needed.append(checks['similarity_not_recency'][arm])
        if arm == 'H':
            needed += [checks['oracle'].get('B8'), checks['similarity_not_recency']['B8']]
        verdict, reason = decide(wins, losses, n, guard, all(needed))
        out[arm] = {'arm_only': wins, 'A_only': losses, 'n': n, 'difference': round((wins - losses) / n, 4),
                    'interval': [round(low, 4), round(high, 4)], 'mcnemar_p': round(score.sign_test(wins, losses), 4),
                    'family_arm_minus_A': family_delta, 'guardrails_hold': guard, 'controls_pass': all(needed),
                    'verdict': verdict, 'reason': reason, 'decides': arm in DECIDED}
    return out


def cost(output, rankings):
    groups = {}
    for line in (output / 'calls.jsonl').read_text().splitlines():
        record = json.loads(line)
        if 'charged_input' not in record:
            continue
        kind = record['cell'].split('-')[0] + ('-' + record['cell'].split('-')[1] if record['cell'].startswith(
            'synthesis-') else '')
        g = groups.setdefault(kind, {'calls': 0, 'input': 0, 'output': 0, 'seconds': 0.0})
        g['calls'] += 1
        g['input'] += record['charged_input']
        g['output'] += record['charged_output']
        g['seconds'] = round(g['seconds'] + record.get('seconds', 0), 3)
    total = {k: sum(g[k] for g in groups.values()) for k in ['calls', 'input', 'output']}
    total['usd'] = round((total['input'] * 0.30 + total['output'] * 1.20) / 1e6, 5)
    # Requests stopped in flight never settled; the meter charges them at their reservation.
    starts = {}
    for line in (output / 'calls.jsonl').read_text().splitlines():
        record = json.loads(line)
        starts.setdefault(record['index'], {}).update(record)
    unsettled = [r for r in starts.values() if 'charged_input' not in r]
    total['unsettled_calls'] = len(unsettled)
    total['usd_with_unsettled_reservations'] = round(total['usd'] + sum(
        r['reserved_input'] * 0.30 + r['reserved_output'] * 1.20 for r in unsettled) / 1e6, 5)
    phrase = groups.get('phrase', {'calls': 1, 'input': 0, 'output': 0, 'seconds': 0})
    n = phrase['calls']
    embed = rankings['embedding']['seconds_per_text']
    per_phrase = {'input_tokens': round(phrase['input'] / n, 1), 'output_tokens': round(phrase['output'] / n, 1),
                  'model_seconds': round(phrase['seconds'] / n, 3)}
    per_turn = {'A': {'model_calls': 1, 'embeddings': 0, **per_phrase},
                'A0': {'model_calls': 0, 'embeddings': 0},
                'B': {'model_calls': 0, 'embeddings': 1, 'embed_seconds': round(embed, 4)},
                'H': {'model_calls': 1, 'embeddings': 1, **per_phrase, 'embed_seconds': round(embed, 4)}}
    return {'groups': groups, 'total': total, 'per_turn': per_turn}


def main(output):
    output = Path(output)
    rankings = json.loads((output / 'rankings.json').read_text())
    queries = json.loads((output / 'queries.json').read_text())
    rows = per_query(rankings, queries)
    tab = table(rows)
    checks = controls(rankings)
    present = [r['arms']['B8']['top_score'] for r in rows if r['family'] in PRIMARY]
    absent = [r['arms']['B8']['top_score'] for r in rows if r['family'] == 'absent']
    result = {'per_query': rows, 'table': tab, 'controls': checks, 'comparisons': compare(rows, tab, checks),
              'b8_top1_auc_present_vs_absent': score.auc(present, absent)}
    if (output / 'calls.jsonl').exists():
        result['cost'] = cost(output, rankings)
    (output / 'scores.json').write_text(json.dumps(result, indent=1) + '\n')
    print(json.dumps({arm: {k: v[k] for k in ['arm_only', 'A_only', 'interval', 'verdict']}
                      for arm, v in result['comparisons'].items()}, indent=1))


if __name__ == '__main__':
    if len(sys.argv) > 1 and sys.argv[1] == 'power':
        print(json.dumps(power(psis=[float(x) for x in sys.argv[2:]] or (0.15, 0.20, 0.25, 0.30)), indent=1))
    else:
        main(sys.argv[1] if len(sys.argv) > 1 else OUT)
