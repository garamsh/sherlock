"""Remove each behaviour #916 added that a test names, run the tests,
restore it, and record that the tests failed (evidence/mutation.txt).

    python3 research/memory-encoding/attributed/mutation.py

The arms' code is #896's and unchanged; its mutations stay recorded in
../structural/evidence/mutation.txt. This file covers the matcher, the
audit's pass mark and this study's scorer.
"""

import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
TESTS = ["python3", "-m", "unittest", "matcher_test", "score_test"]
M, A, S = "matcher.py", "audit.py", "score.py"

# (file in this directory, text, replacement, what the mutation removes)
MUTATIONS = [
    (M, '"owner": declined or (attributed and not asserted),', '"owner": declined or not asserted,', "the attribution requirement"),
    (M, "            if subject_clause(c, value, entity, associate):\n", "            if False:\n", "the subject rule"),
    (M, "    return not (you and you.start() < e.start())\n", "    return True\n", "the address exception"),
    (M, "                if first is not None and i > first and HEDGE.search(c):\n", "                if False:\n", "the hedge exception"),
    (M, "                if first is not None and i > first and HEDGE.search(c):\n", "                if HEDGE.search(c):\n", "the hedge exception's order condition"),
    (M, "    if e is None or v is None or e.start() > v:\n", "    if e is None or v is None:\n", "the entity before the value"),
    (M, "    if associate.search(clause):\n        return False\n", "", "a clause naming the associate is not the entity's"),
    (M, "    return [c.strip() for c in CLAUSE_BREAK.split(sentence) if c.strip()]\n", "    return [sentence]\n", "the clause split"),
    (M, ' + r"(?!\\s+(?-i:[A-Z]))"', "", "the given name alone only outside another full name"),
    (M, "        if words[0] not in shared:\n            alts.append(re.escape(words[0]) + r", "        if True:\n            alts.append(re.escape(words[0]) + r", "a shared word naming neither"),
    (M, 'phrase_pattern(NC_ASSOCIATE[guard["domain"]] + NC_ANY)', "phrase_pattern(NC_ANY)", "NC's roles"),
    (M, " and not asserted and not tied)}", " and not asserted)}", "reading B's tie"),
    (M, "    declined = abstained or bool(DECLINE.search(reply))\n", "    declined = abstained\n", "Amendment 1's declining words"),
    (M, "(associate.search(s) or points_back(ss, i, associate))", "associate.search(s)", "Amendment 1's sentence pointing back"),
    (M, "            alts.append(misspelt(words[-1]))\n", "            alts.append(re.escape(words[-1]))\n", "Amendment 1's misspelt surname"),
    (A, ' and false_accept <= FALSE_ACCEPT * n,', ",", "the audit's false-accept mark"),
    (S, '"applied": j["owner"]})', '"applied": j["strict"]})', "N and NC scored by the owner's rule"),
    (S, 'return prim["arm"] - prim["S"] >= MARGIN * prim["n"] and ', "return ", "the admission margin"),
    (S, '"passed": bool(unseeded) and equal == len(unseeded)}', '"passed": bool(unseeded)}', "the unseeded probes' equality"),
    (S, '("mids", "SELECT id, source_first, source_last, summary, embedding FROM', '("mids", "SELECT id, source_first, source_last, summary FROM', "the vectors in the store digest"),
]


def run():
    env = {"PYTHONDONTWRITEBYTECODE": "1", "PATH": "/usr/bin:/bin", "HOME": str(Path.home())}
    return subprocess.run(TESTS, cwd=HERE, capture_output=True, text=True, env=env).returncode


def main():
    lines = []
    for path, old, new, what in MUTATIONS:
        file = HERE / path
        original = file.read_text()
        if original.count(old) != 1:
            raise SystemExit(f"{path}: the text to mutate occurs {original.count(old)} times")
        file.write_text(original.replace(old, new))
        try:
            failed = run() != 0
        finally:
            file.write_text(original)
        lines.append(f"{'FAILS' if failed else 'PASSES'}  {path}: {what}")
    if run() != 0:
        raise SystemExit("the unmutated tests do not pass")
    lines.append("control: every unmutated test passes")
    (HERE / "evidence").mkdir(exist_ok=True)
    (HERE / "evidence" / "mutation.txt").write_text("\n".join(lines) + "\n")
    print("\n".join(lines))


if __name__ == "__main__":
    main()
