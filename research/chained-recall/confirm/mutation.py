"""Remove each behaviour #885 added that a test names, run the tests,
restore it, and record that the tests failed (evidence/mutation.txt).

    python3 research/chained-recall/confirm/mutation.py

#874's own mutations stay recorded in ../evidence/mutation.txt; this file
covers what #885 changed: L+A's label and instruction, the setting's
instruction in the live session, the study-directory lookup, the proxy's
entity check, and the corpus and scorer rules preregistration.md fixes.
"""

import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
STUDY = HERE.parent
ROOT = HERE.parents[2]

GO_LINKS = ["go", "test", "./research/chained-recall/arms/links/"]
GO_STUDY = ["go", "test", "./research/chained-recall/study/"]
GO_HARNESS = ["go", "test", "./research/chained-recall/harness/"]

# (file under chained-recall/, text, replacement, test command, what the mutation removes)
MUTATIONS = [
    ("arms/links/memory.go", 'items[i].Kind = "associated"', '_ = "associated"', GO_LINKS, "L+A's associated kind on walk-delivered mids"),
    ("arms/links/memory.go", "items[i].Text = associatedLabel(via[it.ID]) + it.Text", "_ = associatedLabel", GO_LINKS, "L+A's label naming the entity"),
    ("arms/links/walk.go", "if best < 0 || score[e] > score[best] {", "if best < 0 {", GO_LINKS, "the entity a walk mid was reached through"),
    ("arms/links/main.go", "\t\treturn Abstention\n", '\t\treturn ""\n', GO_LINKS, "L+A's instruction"),
    ("study/live.go", "extra = si.InstructionsFor(setting)", "_ = si", GO_STUDY, "the setting's instruction in the live session"),
    ("study/env.go", 'if _, err := os.Stat(filepath.Join(parent, "mid-memory-rag")); err == nil {', "if false {", GO_STUDY, "finding research from a corpus below the study"),
    ("harness/proxy.go", "if json.Unmarshal(args.Entities, &names) != nil || names == nil {", "if false {", GO_HARNESS, "refusing malformed entities before caching"),
    ("confirm/score.py", '"guardrails_hold": all(guard[f] >= -ALLOWED_LOSS[f] for f in guard)', '"guardrails_hold": True', ["python3", "-m", "unittest", "score_test"], "the guardrails"),
    ("confirm/score.py", 'return bool(lines) and all("] (associated): " + LABEL in ln for ln in lines)', "return bool(lines)", ["python3", "-m", "unittest", "score_test"], "the label control"),
    ("confirm/score.py", "return min(valid, key=lambda n: NET_LINES[n]) if valid else None", "return valid[-1] if valid else None", ["python3", "-m", "unittest", "score_test"], "taking the smallest valid arm"),
    ("confirm/corpus.py", "and not (x & query) and v <= value and not (v & (query | join))", "and not (x & query) and v <= value", ["python3", "-m", "unittest", "corpus_test"], "keeping F's value out of the query and the joining exchange"),
    ("confirm/corpus.py", "not (e & value) and", "", ["python3", "-m", "unittest", "corpus_test"], "keeping E out of F's episode"),
    ("confirm/corpus.py", '    return {r["id"] for r in check["chains"] if any(x <= check["summary"]["top"] for x in r["raw"][1:])}', "    return set()", ["python3", "-m", "unittest", "corpus_test"], "leaving leaking chains out of the selection"),
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
