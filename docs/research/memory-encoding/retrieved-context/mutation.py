"""Remove each behaviour #902 added that a test names, run the tests,
restore it, and record that the tests failed (evidence/mutation.txt).

    python3 docs/research/memory-encoding/retrieved-context/mutation.py

It covers the search at write (its exclusion, its bounds, RW's search over
its own earlier mids), both calls (RW's added sentence and context,
RW-key's schema, grounding and retry), both stores (RW's summary, RW-key's
kept summary and key-only line, a failed batch, resuming), and the
scorer's carried reach, controls, stale copies, links, margin and
decision.
"""

import os
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]

GO = ["go", "test", "-count=1", "./docs/research/memory-encoding/retrieved-context/arms/retrieved/"]
SCORE = ["python3", "-m", "unittest", "score_test"]

A = "arms/retrieved/"

# (file under this directory, text, replacement, test command, what the mutation removes)
MUTATIONS = [
    (A + "retrieve.go", "		if u.Last < first {\n", "		if true {\n", GO, "reading nothing at or after the batch"),
    (A + "retrieve.go", "	for _, i := range index[:min(k, len(index))] {\n", "	for _, i := range index {\n", GO, "the bound on what the search returns"),
    (A + "retrieve.go", "	sort.Slice(out, func(i, j int) bool { return out[i].First < out[j].First })\n", "", GO, "returning what was found in source order"),
    (A + "main.go", "		pool = append(pool, vec{ID: own.ID, First: own.First, Last: own.Last, Text: text, Vector: v})\n", "		pool = append(pool, own)\n", GO, "RW searching its own earlier mids"),
    (A + "main.go", "	rec, done := recorded[batch[0].ID]\n", "	rec, done := recorded[-1]\n", GO, "RW resuming after its recorded batches"),
    (A + "main.go", "		if rec.Failed != \"\" {\n			continue\n		}\n		if err := applyOne", "		if err := applyOne", GO, "a failed batch keeping S's mid"),
    (A + "main.go", "`UPDATE conversation_mid SET summary=?, embedding=?", "`UPDATE conversation_mid SET summary=coalesce(summary, ?), embedding=?", GO, "RW storing its own summary"),
    (A + "main.go", "		v, err := env.Embedder.Embed(ctx, keyText(summary, rec.Line))\n", "		v, err := env.Embedder.Embed(ctx, summary)\n", GO, "RW-key's line in the key"),
    (A + "main.go", '	return line + "\\n\\n" + summary\n', '	return summary + "\\n\\n" + line\n', GO, "RW-key's line before the summary in the key"),
    (A + "main.go", "`UPDATE conversation_mid SET related=?, embedding=?", "`UPDATE conversation_mid SET related=?, summary=summary||' '||related, embedding=?", GO, "RW-key never showing the line"),
    (A + "synthesis.go", "const rwPrompt = productionPrompt + retrievedSentence + ", "const rwPrompt = productionPrompt + ", GO, "RW's added sentence"),
    (A + "synthesis.go", "		messages[i] = m.Text\n", "		messages[i] = \"\"\n", GO, "sending the retrieved messages"),
    (A + "synthesis.go", "		memories[i] = memoryInput{Mid: m.ID, Summary: m.Text}\n", "		memories[i] = memoryInput{Mid: m.ID}\n", GO, "sending the retrieved mids"),
    (A + "synthesis.go", '	return fmt.Sprintf(" (Attempt %d.)", attempt)\n', '	return ""\n', GO, "varying a retried request"),
    (A + "synthesis.go", "		if !strings.Contains(seen, strings.ToLower(e)) {\n", "		if false {\n", GO, "dropping a name written nowhere it was sent"),
    (A + "synthesis.go", "		if !slices.Contains(ids, id) {\n", "		if false {\n", GO, "dropping a mid that was not retrieved"),
    (A + "synthesis.go", '	if len(*result.Entities) > maxEntities || len(*result.Mids) > maxRelated {\n', "	if false {\n", GO, "the bound on names and mids"),
    ("score.py", '        row["reached"] = all(row["carried"])\n', '        row["reached"] = row["reached_strict"]\n', SCORE, "carried reach"),
    ("score.py", '"passed": bool(recorded) and not missing and not late and', '"passed": bool(recorded) and not missing and', SCORE, "the retrieval control's exclusion"),
    ("score.py", " and not over and (n - failed)", " and (n - failed)", SCORE, "the retrieval control's bounds"),
    ("score.py", " and (n - failed) >= WELL_FORMED * n}", "}", SCORE, "the well-formed share"),
    ("score.py", '"passed": bool(recorded) and bad == 0}', '"passed": bool(recorded)}', SCORE, "the line control"),
    ("score.py", "and equal == len(items) and leaked == 0}", "and leaked == 0}", SCORE, "the shown-text control"),
    ("score.py", "and equal == len(items) and leaked == 0}", "and equal == len(items)}", SCORE, "the line never shown"),
    ("score.py", "if f > change and states(", "if states(", SCORE, "counting only mids written after the correction"),
    ("score.py", '        by_mid = any(m["first"] <= earlier <= m["last"] for m in r["mids"])\n', "        by_mid = False\n", SCORE, "a link through a mid"),
    ("score.py", '    return any(states(it["text"], hop["key"]) for it in items)\n', '    return any(states(it["text"], hop["key"].split()[-1]) for it in items)\n', SCORE, "the carried-reach matcher's both entities"),
    ("score.py", '    return any(it["first"] > change and states(it["text"], guard["obsolete"]) for it in items)\n', '    return any(states(it["text"], guard["obsolete"]) for it in items)\n', SCORE, "the stale rule's mids after the correction"),
    ("score.py", '    return any(it["first"] > change and states(it["text"], guard["obsolete"]) for it in items)\n', '    return False\n', SCORE, "the stale rule"),
    ("score.py", "        out[ch[\"family\"]][\"every_pair\"] += all(", "        out[ch[\"family\"]][\"every_pair\"] += any(", SCORE, "a chain linked only when every pair is"),
    ("score.py", 'return prim["arm"] - prim["S"] >= MARGIN * prim["n"] and ', "return ", SCORE, "the admission margin"),
    ("score.py", 'a["valid"] = a["significant"] and a["guardrails_hold"] and a["controls_passed"]', 'a["valid"] = a["significant"] and a["guardrails_hold"]', SCORE, "voiding an arm whose control failed"),
    ("score.py", "next((n for n in ORDER if", "next((n for n in reversed(ORDER) if", SCORE, "RW-key named before RW"),
]


def run(cmd):
    cwd = HERE if cmd[0] == "python3" else ROOT
    env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1")
    return subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, env=env).returncode


def main():
    if run(GO) != 0 or run(SCORE) != 0:
        raise SystemExit("the unmutated tests do not pass")
    lines = []
    for path, old, new, cmd, what in MUTATIONS:
        file = HERE / path
        original = file.read_text()
        if original.count(old) != 1:
            raise SystemExit(f"{path}: the text to mutate occurs {original.count(old)} times")
        file.write_text(original.replace(old, new))
        try:
            failed = run(cmd) != 0
        finally:
            file.write_text(original)
        lines.append(f"{'FAILS' if failed else 'PASSES'}  {path}: {what}")
    if run(GO) != 0 or run(SCORE) != 0:
        raise SystemExit("the unmutated tests do not pass after the mutations")
    lines.append("control: every unmutated test passes")
    (HERE / "evidence").mkdir(exist_ok=True)
    (HERE / "evidence" / "mutation.txt").write_text("\n".join(lines) + "\n")
    print("\n".join(lines))


if __name__ == "__main__":
    main()
