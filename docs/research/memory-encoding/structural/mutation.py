"""Remove each behaviour #896 added that a test names, run the tests,
restore it, and record that the tests failed (evidence/mutation.txt).

    python3 docs/research/memory-encoding/structural/mutation.py

Earlier studies' mutations stay recorded beside their own evidence; this
file covers what #896 changed: L-ptr's pointers, L-attr's attribution,
L+chk's check, C-key's key and C-key+L-ptr's store, the study's notice
rendering, and the scorer's reach rules, margin and controls.

Amendment 1's two NC checks are mutated together: each fails wherever the
other does, since an item about the queried entity that carries an
attribution breaks both. Live, a subject equal to the queried entity is
always a seed, so the presence check already fails there; the live NC
checks are reported and have no mutation of their own.
"""

import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
RESEARCH = HERE.parents[1]
ROOT = HERE.parents[3]

GO_LINKS = ["go", "test", "./docs/research/chained-recall/arms/links/"]
GO_CONTEXT = ["go", "test", "./docs/research/memory-encoding/validation/arms/context/"]
SCORE = ["python3", "-m", "unittest", "score_test"]

LINKS = "chained-recall/arms/links/"
CONTEXT = "memory-encoding/validation/arms/context/main.go"
S = "memory-encoding/structural/score.py"

# (file under docs/research/, text, replacement, test command, what the mutation removes)
MUTATIONS = [
    (LINKS + "memory.go", "		return pointerText(via)\n", "		return it.Text\n", GO_LINKS, "L-ptr's pointer text"),
    (LINKS + "main.go", '		pointer: setting == "b4-model-ptr" || setting == keyed,', '		pointer: setting == "b4-model-ptr",', GO_LINKS, "C-key+L-ptr opened as L-ptr"),
    (LINKS + "memory.go", "		return attribution(s, seeds) + it.Text\n", "		return it.Text\n", GO_LINKS, "L-attr's attribution"),
    (LINKS + "memory.go", '	if s := m.graph.subject(it.ID); s != "" && !slices.Contains(seeds, s) {', '	if s := m.graph.subject(it.ID); s != "" {', GO_LINKS, "attributing only a subject that differs from the query's entities"),
    (LINKS + "memory.go", "			subjects[id] = m.graph.subject(id)\n", "", GO_LINKS, "L-attr's trace of subjects"),
    (LINKS + "main.go", "		shared, err = keyStore(env)\n", "		shared, err = env.SharedStore(batch)\n", GO_LINKS, "C-key+L-ptr's start from C-key's store"),
    (LINKS + "check.go", "	if len(rec.Walk) > 0 {\n", "	if false {\n", GO_LINKS, "L+chk's check call"),
    (LINKS + "check.go", "			out = out[:0]\n			for _, id := range named {\n				out = append(out, byID[id])\n			}\n", "			out = items\n", GO_LINKS, "keeping only the named items"),
    (LINKS + "check.go", "		rec.Connected = c.graph.connects(named, seeds)\n", "		rec.Connected = len(named) > 0\n", GO_LINKS, "the connectivity rule"),
    (LINKS + "check.go", '		out = []study.Item{{Level: "notice", Kind: "check", Text: noEvidence}}\n', "		out = []study.Item{}\n", GO_LINKS, "the notice"),
    (LINKS + "check.go", '	return c.ground(ctx, "search", query, items)\n', "	return items, nil\n", GO_LINKS, "the check on memory_search"),
    (LINKS + "check.go", "	return []int64{}, checkAttempts, true, nil\n", '	return nil, checkAttempts, true, errors.New("malformed")\n', GO_LINKS, "failing closed after malformed answers"),
    (LINKS + "check.go", "		named, err := parseCheck(response, midIDs(items))\n", "		named, err := parseCheck(response, []int64{})\n", GO_LINKS, "naming only delivered items"),
    ("chained-recall/study/adapt.go", '		if it.Level == "notice" {\n', "		if false {\n", GO_LINKS, "rendering the notice without a memory label"),
    (CONTEXT, "		if keepSummary {\n", "		if false {\n", GO_CONTEXT, "C-key's kept S summary"),
    (CONTEXT, "		vector, err := env.Embedder.Embed(ctx, embedText(contextLine, summary))", '		vector, err := env.Embedder.Embed(ctx, embedText("", summary))', GO_CONTEXT, "the context in the key"),
    (CONTEXT, "	if s == keyOnly {\n		store, err", "	if false {\n		store, err", GO_CONTEXT, "C-key showing no context line"),
    (S, " or pointed(it) in ends", "", SCORE, "pointer reach"),
    (S, '            row["reached"] = V.stated_in(items, ch["hops"][0]["source"], ch["answer"])', '            row["reached"] = row["reached_strict"]', SCORE, "SC value reach"),
    (S, 'prim["arm"] - prim["S"] >= MARGIN * prim["n"] and ', "", SCORE, "the admission margin"),
    (S, '"passed": bool(walk) and not bad}', '"passed": bool(walk)}', SCORE, "the offline pointer control"),
    (S, '                present += it["text"].startswith(attribution(subject, seeds))', "                present += 1", SCORE, "the offline attribution presence"),
    (S, 'n_correct += subject == normalize(g["associated"]) and it["text"]', 'n_correct += it["text"]', SCORE, "the offline attribution correctness"),
    (S, "    ok = base and nc_attributed_ok == nc_attributed and nc_queried_plain == nc_queried\n", "    ok = base\n", SCORE, "the offline NC attribution checks (Amendment 1)"),
    (S, '            equal += it["text"].strip() == summaries.get(it["first"])', "            equal += 1", SCORE, "the shown-text control"),
    (S, '"passed": differ >= RANKING_DIFFERS * len(probes)}', '"passed": True}', SCORE, "the ranking control"),
    (S, '"passed": checks > 0 and unchecked == 0}', '"passed": checks > 0}', SCORE, "the offline check control"),
    (S, '"passed": bool(lines) and bad == 0 and ', '"passed": bool(lines) and ', SCORE, "the live pointer control"),
    (S, "                    present += text.startswith(attribution(subject, seeds))", "                    present += 1", SCORE, "the live attribution control"),
    (S, "and unchecked == 0 and not short}", "and unchecked == 0}", SCORE, "the ledger's agreement with the live checks"),
    (S, '    final = [rec for rec in checks if rec["opened"] == last[rec["probe"]]]', "    final = checks", SCORE, "reading only a probe's final attempt"),
    (S, 'a["valid"] = a["significant"] and a["guardrails_hold"] and a["controls_passed"]', 'a["valid"] = a["significant"] and a["guardrails_hold"]', SCORE, "voiding an arm whose control failed"),
]


def run(cmd):
    cwd = HERE if cmd[0] == "python3" else ROOT
    env = {"PYTHONDONTWRITEBYTECODE": "1", "PATH": "/usr/bin:/bin:/usr/local/go/bin:" + str(Path.home() / "go" / "bin"), "HOME": str(Path.home()),
           "GOFLAGS": "-count=1", "GOCACHE": str(Path.home() / ".cache" / "go-build"), "GOMODCACHE": str(Path.home() / "go" / "pkg" / "mod")}
    return subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, env=env).returncode


def main():
    lines = []
    for path, old, new, cmd, what in MUTATIONS:
        file = RESEARCH / path
        original = file.read_text()
        if original.count(old) != 1:
            raise SystemExit(f"{path}: the text to mutate occurs {original.count(old)} times")
        file.write_text(original.replace(old, new))
        try:
            failed = run(cmd) != 0
        finally:
            file.write_text(original)
        lines.append(f"{'FAILS' if failed else 'PASSES'}  {path}: {what}")
    if run(GO_LINKS) != 0 or run(GO_CONTEXT) != 0 or run(SCORE) != 0:
        raise SystemExit("the unmutated tests do not pass")
    lines.append("control: every unmutated test passes")
    (HERE / "evidence").mkdir(exist_ok=True)
    (HERE / "evidence" / "mutation.txt").write_text("\n".join(lines) + "\n")
    print("\n".join(lines))


if __name__ == "__main__":
    main()
