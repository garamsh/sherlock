"""Post-hoc diagnostic, not preregistered: does each evidence episode's synthesized summary keep the planted value?"""
import json
from pathlib import Path
import re
import sys

HERE = Path(__file__).resolve().parent
QUERIES = json.loads((HERE / 'queries.json').read_text())
# One pattern per planted value, written after collection to classify misses; it scores no arm.
KEYS = {
    'exact-01': r'4471', 'exact-02': r'6:40', 'exact-03': r'BW-2291', 'exact-04': r'rye',
    'exact-05': r'quillon-deploy-2', 'exact-06': r'4:15', 'exact-07': r'Salt Road', 'exact-08': r'boiler',
    'exact-09': r'27B', 'exact-10': r'120',
    'paraphrase-01': r'orthodont', 'paraphrase-02': r'2,?400', 'paraphrase-03': r'380', 'paraphrase-04': r'Lisbon',
    'paraphrase-05': r'kilo|kg', 'paraphrase-06': r'reservoir|running|run', 'paraphrase-07': r'612',
    'paraphrase-08': r'passport', 'paraphrase-09': r'caster', 'paraphrase-10': r'receipt',
    'implicit-01': r'tab', 'implicit-02': r'vegetarian|gluten', 'implicit-03': r'Warm regards|exclamation',
    'implicit-04': r'seasick|boat|ferr', 'implicit-05': r'200', 'implicit-06': r'parameteri',
    'implicit-07': r'colou?r-?blind|blue', 'implicit-08': r'large print|16', 'implicit-09': r'five|5 bullet',
    'implicit-10': r'penicillin',
    'correction-01': r'19', 'correction-02': r'3381', 'correction-03': r'Luca', 'correction-04': r'3:25',
    'correction-05': r'Audit-FY26-Final', 'correction-06': r'10 ?mg', 'correction-07': r'52,?500',
    'correction-08': r'Cedar-Home', 'correction-09': r'22', 'correction-10': r'Pinecrest',
    'recent-01': r'sage', 'recent-02': r'brass', 'recent-03': r'PCN-88213', 'recent-04': r'Monday',
    'recent-05': r'two weeks|chase',
}


def main(evidence):
    synthesis = json.loads((Path(evidence) / 'synthesis.json').read_text())
    out = {}
    for q in QUERIES:
        if not q['evidence']:
            continue
        summary = synthesis[(q['evidence'][0] - 1) // 8]['summary']
        out[q['id']] = bool(re.search(KEYS[q['id']], summary, re.I))
    (Path(evidence) / 'summary-fidelity.json').write_text(json.dumps(out, indent=1) + '\n')
    print(sum(out.values()), 'of', len(out), 'evidence summaries keep the planted value;',
          'missing:', [k for k, v in out.items() if not v])


if __name__ == '__main__':
    main(sys.argv[1] if len(sys.argv) > 1 else HERE / 'evidence')
