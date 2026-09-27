"""Remove each behaviour a test names, run the tests, restore it, and
record that the tests failed (evidence/mutation.txt).

    python3 research/memory-paradigms/validation/mutation.py

It covers what #900 adds: the study's window, label, trace and corpus
hooks, arms W, R and D, and score.py's rules.
"""

import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
STUDY = ROOT / "research" / "chained-recall" / "study"

GO_STUDY = ["go", "test", "./research/chained-recall/study/"]
GO_W = ["go", "test", "./research/memory-paradigms/validation/arms/full/"]
GO_R = ["go", "test", "./research/memory-paradigms/validation/arms/agent/"]
GO_D = ["go", "test", "./research/memory-paradigms/validation/arms/document/"]
PY = ["python3", "-B", "-m", "unittest", "score_test"]

# (file, text, replacement, test command, what the mutation removes)
MUTATIONS = [
    (STUDY / "live.go", "\t\twindow = w.WindowFor(setting)\n", "", GO_STUDY, "the study restoring an arm's own window"),
    (STUDY / "live.go", "\t\tres.Trace = t.LastTrace()\n", "", GO_STUDY, "the live record keeping the memory's trace"),
    (STUDY / "adapt.go", "\t\t\tlabel = it.Label\n", "", GO_STUDY, "an item's own label"),
    (STUDY / "main.go", "\tcorpus, err := LoadCorpus(*corpusDir)", "\tcorpus, err := LoadCorpus(*dir)", GO_STUDY, "reading the corpus from -corpus"),
    (HERE / "arms/full/main.go", "func (arm) WindowFor(string) int { return WindowMessages }", "func (arm) windowFor(string) int { return WindowMessages }", GO_W, "W's window"),
    (HERE / "arms/agent/recall.go", "\t\t\tok = ok && inspected[s]\n", "", GO_R, "R's citation check"),
    (HERE / "arms/agent/recall.go", "\t\tok := len(f.Sources) > 0\n", "\t\tok := true\n", GO_R, "refusing a fact without sources"),
    (HERE / "arms/agent/recall.go", "\t\t\ttools = []tool.Descriptor{resultDescriptor()}\n", "", GO_R, "the last call offering only recall_result"),
    (HERE / "arms/agent/recall.go", "\t\t\t\t\tinspected[id] = true\n", "", GO_R, "recording what the step inspected"),
    (HERE / "arms/agent/recall.go", "m.probes[query] == id {", "false {", GO_R, "naming the probe from the live session's task (Amendment 5)"),
    (HERE / "arms/document/document.go", "\tif strict && drops > quota {", "\tif false {", GO_D, "refusing a pass that drops more than a quarter"),
    (HERE / "arms/document/document.go", "\t\tif _, ok := intervals[m]; !ok {\n\t\t\treturn false\n\t\t}\n", "", GO_D, "rejecting a line citing a mid its pass was not given"),
    (HERE / "arms/document/document.go", "\tif next.Chars() > Bound {\n\t\tif strict {", "\tif false {\n\t\tif strict {", GO_D, "the bound"),
    (HERE / "arms/document/document.go", "\t\t\tnext.History = append(next.History, Version{Line: id, Op: \"drop\", Pass: pass})\n", "", GO_D, "keeping a dropped line's history"),
    (HERE / "arms/document/consolidate.go", "\t\t\tfeedback = fmt.Sprintf(\"\\n\\n(Attempt %d. The last operations were refused: %v.)\", attempt+1, err)\n", "", GO_D, "telling a refused pass why"),
    (HERE / "arms/document/consolidate.go", "\t\tif pass <= len(records) {", "\t\tif false {", GO_D, "replaying a recorded pass without a call"),
    (HERE / "arms/document/consolidate.go", "\t\tops = append(ops, *result.Ops...)\n", "\t\tops = *result.Ops\n", GO_D, "reading several document_update calls as one (Amendment 1)"),
    (HERE / "arms/document/document.go", "\t\treturn d.apply(ops, pass, intervals, false, true)\n", "\t\treturn Applied{}, err\n", GO_D, "a partial pass holding the bound against lengthening supersedes (Amendment 2)"),
    (HERE / "arms/document/consolidate.go", "\treturn ops[:maxOps], len(ops) - maxOps\n", "\treturn ops, 0\n", GO_D, "keeping an answer's first 16 operations (Amendment 3)"),
    (HERE / "score.py", "    rest = words[1] if len(words) > 1 else \"\"", "    rest = fact[\"statement\"]", PY, "leaving the first word out of support"),
    (HERE / "score.py", "\"guardrails_hold\": all(guard[f] >= -ALLOWED_LOSS[f] for f in guard)", "\"guardrails_hold\": True", PY, "the guardrails"),
    (HERE / "score.py", "compared = [n for n in per if n != BASELINE and primary_of(n)]", "compared = [n for n in per if n != BASELINE]", PY, "keeping W out of the decision"),
]


def run(cmd, cwd):
    return subprocess.run(cmd, cwd=cwd, capture_output=True, text=True).returncode


def main():
    lines = []
    for path, text, replacement, cmd, what in MUTATIONS:
        original = path.read_text()
        rel = path.relative_to(ROOT)
        if original.count(text) != 1:
            raise SystemExit(f"{rel}: the mutated text must occur once: {text!r}")
        cwd = ROOT if cmd[0] == "go" else HERE
        if run(cmd, cwd) != 0:
            raise SystemExit(f"{rel}: the tests fail before the mutation")
        try:
            path.write_text(original.replace(text, replacement))
            failed = run(cmd, cwd) != 0
        finally:
            path.write_text(original)
        lines.append(f"{'FAILS' if failed else 'SURVIVES'}  {rel}: {what}")
        print(lines[-1])
    (HERE / "evidence").mkdir(exist_ok=True)
    (HERE / "evidence" / "mutation.txt").write_text("\n".join(lines) + "\n")
    if any(line.startswith("SURVIVES") for line in lines):
        raise SystemExit("a mutation survived")


if __name__ == "__main__":
    main()
