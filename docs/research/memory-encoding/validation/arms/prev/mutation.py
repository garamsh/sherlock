"""Remove each mechanism P adds, run the tests, restore it, and record that
the tests failed (evidence/mutation.txt beside this file).

    python3 docs/research/memory-encoding/validation/arms/prev/mutation.py
"""

import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[5]

GO = ["go", "test", "-count=1", "./docs/research/memory-encoding/validation/arms/prev/"]
PY = ["python3", "-B", "-m", "unittest", "controls_test"]

# (file beside this one, text, replacement, test command, what the mutation removes)
MUTATIONS = [
    ("memory.go", "\t\tif best >= 0 {\n", "\t\tif false {\n", GO, "the thread list's turn"),
    ("memory.go", "(best < 0 || r < best)", "(best < 0 || r > best)", GO, "taking the linked neighbour the ranking puts first"),
    ("main.go", "\t\tout[linked] = append(out[linked], mid)\n", "", GO, "following a link from the earlier mid to the later"),
    ("main.go", "\t\tout[mid] = append(out[mid], linked)\n", "", GO, "following a link from the later mid to the earlier"),
    ("main.go", "given = mids[at-1 : at]", "given = mids[at : at+1]", GO, "giving b4-prev the previous mid"),
    ("main.go", "if h.ID < own && len(given) < candidates {", "if h.ID != own && len(given) < candidates {", GO, "giving b4-k5 only earlier mids"),
    ("synthesis.go", "\t\tif !slices.ContainsFunc(given, func(e earlier) bool { return e.ID == id }) {", "\t\tif false {", GO, "refusing an ID that was not given"),
    ("synthesis.go", "err != nil || continues == nil {", "err != nil {", GO, "refusing a null continues at b4-prev"),
    ("synthesis.go", "err != nil || ids == nil {", "err != nil {", GO, "refusing a null continues at b4-k5"),
    ("synthesis.go", "\treturn fmt.Sprintf(\" (Attempt %d.)\", attempt)", '\treturn ""', GO, "varying a retried request past the proxy's cache"),
    ("synthesis.go", "\tif malformed == linkAttempts {", "\tif false {", GO, "recording a batch every attempt left malformed"),
    ("synthesis.go", "\t\tif !slices.Equal(b.Given, ids(r.given)) {", "\t\tif false {", GO, "refusing a recording given other mids"),
    ("controls.py", "f[\"linked\"] += x in later[\"links\"]", "f[\"linked\"] += x in later[\"given\"]", PY, "counting a pair linked only when the model chose it"),
]


def run(cmd, cwd):
    return subprocess.run(cmd, cwd=cwd, capture_output=True, text=True).returncode


def main():
    lines = []
    for rel, text, replacement, cmd, what in MUTATIONS:
        path = HERE / rel
        original = path.read_text()
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
