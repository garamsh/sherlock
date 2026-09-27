"""Remove each behaviour #888 added that a test names, run the tests,
restore it, and record that the tests failed (evidence/mutation.txt).

    python3 research/chained-recall/readtime/mutation.py

#874's and #885's mutations stay recorded in ../evidence/mutation.txt and
../confirm/evidence/mutation.txt; this file covers what #888 changed: L+G's
gate, L-T's recall and call, L+V's and L-T's instructions, the live
session's offer of memory_associate, and the corpus and scorer rules
preregistration.md fixes.
"""

import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
STUDY = HERE.parent
ROOT = HERE.parents[2]

GO_LINKS = ["go", "test", "./research/chained-recall/arms/links/"]
GO_STUDY = ["go", "test", "./research/chained-recall/study/"]
SCORE = ["python3", "-m", "unittest", "score_test"]
CORPUS = ["python3", "-m", "unittest", "corpus_test"]

# (file under chained-recall/, text, replacement, test command, what the mutation removes)
MUTATIONS = [
    ("arms/links/memory.go", "if m.graph.namesAny(h.ID, seeds) || scores[h.ID] >= gateScore {", "if true {", GO_LINKS, "L+G's gate"),
    ("arms/links/memory.go", "		linked = m.gate(linked, seeds, scores)\n", "		_ = m.gate(linked, seeds, scores)\n", GO_LINKS, "applying L+G's gate to the walk's list"),
    ("arms/links/memory.go", '"dropped": !slices.Contains(delivered, it.ID)', '"dropped": false', GO_LINKS, "L+G's trace of what it dropped"),
    ("arms/links/walk.go", "		scores[g.mids[n].ID] = score[n]\n", "", GO_LINKS, "the walk score L+G's gate reads"),
    ("arms/links/memory.go", "	return study.ShippedMemory{Conversation: m.store}.Recall(ctx, task)", "	return m.memory.Recall(ctx, task)", GO_LINKS, "L-T's shipped pre-turn recall"),
    ("arms/links/memory.go", "	return study.ShippedMemory{Conversation: m.store}.Search(ctx, query, limit)", "	return m.memory.Search(ctx, query, limit)", GO_LINKS, "L-T's shipped memory_search"),
    ("arms/links/memory.go", '	items := take(limit, map[int64]bool{}, &list{"linked", linked})', '	items := take(limit, map[int64]bool{}, &list{"linked", linked[:0]})', GO_LINKS, "L-T's walk behind memory_associate"),
    ("arms/links/main.go", "		return associating{m}, nil\n", "		return m, nil\n", GO_LINKS, "L-T's memory answering memory_associate"),
    ("arms/links/main.go", "		return OnCall\n", '		return ""\n', GO_LINKS, "L-T's instruction"),
    ("arms/links/main.go", "		return Verify\n", '		return ""\n', GO_LINKS, "L+V's instruction"),
    ("study/live.go", "		ws.associate = a\n", "		_ = a\n", GO_STUDY, "the live session's offer of memory_associate"),
    ("study/workspace.go", "	return append(slices.Clone(w.descriptors), AssociateDescriptor())", "	return w.descriptors", GO_STUDY, "listing memory_associate to the model"),
    ("study/workspace.go", '	if query == "" {\n		return nil, errors.New("query must not be empty")', '	if false {\n		return nil, errors.New("query must not be empty")', GO_STUDY, "refusing an empty memory_associate query"),
    ("readtime/score.py", "        if near <= allowed and (best is None", "        if (best is None", SCORE, "the N allowance in L+G's threshold rule"),
    ("readtime/score.py", "            if isinstance(source, int) and first <= source <= last and not t.get(\"failed\"):", "            if isinstance(source, int):", SCORE, "inspecting the walk mid that holds the fact"),
    ("readtime/score.py", '        if t["name"] in ("message_send", "file_write") and states(json.dumps(t["input"]), fact):', "        if False:", SCORE, "the order of the inspect and the first statement"),
    ("readtime/score.py", "    if not walk or other or not states(P.artifact(result), value):", "    if not walk or not states(P.artifact(result), value):", SCORE, "leaving out facts a similar mid also holds"),
    ("readtime/score.py", "    if stated and not inspected:\n        row[\"applied\"] = False", "    if stated and not inspected:\n        pass", SCORE, "enforcing L+V's check in scoring"),
    ("readtime/score.py", ' >= CALL_SHARE * summary[T]["primary"]["n"]', " >= 0", SCORE, "L-T's call control"),
    ("readtime/score.py", "        failed = sum(a[\"guard_change\"][f] < -ALLOWED_LOSS[f] for f in ALLOWED_LOSS)", "        failed = 0", SCORE, "ranking by failed guardrails"),
    ("readtime/score.py", " + [t[\"result\"] for t in result.get(\"tools\") or [] if t[\"name\"] == ASSOCIATE]", "", SCORE, "counting memory_associate results as seen"),
    ("readtime/corpus.py", "confirm.SEED = 888", "confirm.SEED = 885", CORPUS, "this corpus's seed"),
]


def run(cmd):
    cwd = HERE if cmd[0] == "python3" else ROOT
    env = {"PYTHONDONTWRITEBYTECODE": "1", "PATH": "/usr/bin:/bin:/usr/local/go/bin:" + str(Path.home() / "go" / "bin"), "HOME": str(Path.home()),
           "GOFLAGS": "-count=1", "GOCACHE": str(Path.home() / ".cache" / "go-build")}
    return subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, env=env).returncode


def main():
    lines = []
    for path, old, new, cmd, what in MUTATIONS:
        file = STUDY / path
        original = file.read_text()
        if original.count(old) != 1:
            raise SystemExit(f"{path}: the text to mutate occurs {original.count(old)} times")
        file.write_text(original.replace(old, new))
        try:
            failed = run(cmd) != 0
        finally:
            file.write_text(original)
        lines.append(f"{'FAILS' if failed else 'PASSES'}  {path}: {what}")
    if run(["go", "test", "./research/chained-recall/..."]) != 0 or run(["python3", "-m", "unittest", "score_test", "corpus_test"]) != 0:
        raise SystemExit("the unmutated tests do not pass")
    lines.append("control: every unmutated test passes")
    (HERE / "evidence").mkdir(exist_ok=True)
    (HERE / "evidence" / "mutation.txt").write_text("\n".join(lines) + "\n")
    print("\n".join(lines))


if __name__ == "__main__":
    main()
