"""Remove each behaviour a test names, run the tests, restore it, and
record that the tests failed (evidence/mutation.txt).

    python3 docs/research/memory-encoding/validation/mutation.py

It covers F's recall and synthesis and the rules corpus.py and score.py
fix for #890. C and P record their own.
"""

import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[3]

GO_F = ["go", "test", "./docs/research/memory-encoding/validation/arms/facts/"]
PY = ["python3", "-B", "-m", "unittest"]

# (file under validation/, text, replacement, test command, what the mutation removes)
MUTATIONS = [
    ("arms/facts/memory.go", "profile := m.profile()", "profile := []study.Item(nil)", GO_F, "F's profile block"),
    ("arms/facts/memory.go", "similar, err := m.similar(ctx, task, delivered)", "similar, err := []study.Item(nil), error(nil)", GO_F, "F's similar facts"),
    ("arms/facts/memory.go", '\t\tif f.Kind != "user" {\n\t\t\tcontinue\n\t\t}\n', "", GO_F, "the profile's user filter"),
    ("arms/facts/memory.go", "\t\tif used+n > profileChars {\n\t\t\tbreak\n\t\t}\n", "", GO_F, "the profile's character bound"),
    ("arms/facts/memory.go", "\t\tif !delivered[f.ID] {", "\t\tif true {", GO_F, "leaving profile facts out of the similar list"),
    ("arms/facts/synthesis.go", "\tif result.Facts == nil {", "\tif false {", GO_F, "refusing a call without facts"),
    ("arms/facts/synthesis.go", "\treturn fmt.Sprintf(\" (Attempt %d.)\", attempt)", '\treturn ""', GO_F, "varying a retried request past the proxy's cache"),
    ("corpus.py", "and not ((s - kind) & second)", "", PY + ["corpus_test"], "keeping the subject out of the situated evidence"),
    ("corpus.py", "needed = raw[1:] if family in", "needed = raw[5:] if family in", PY + ["corpus_test"], "checking the sources a family needs"),
    ("score.py", "\"reached\": all(h[1] for h in hops)", "\"reached\": all(h[0] for h in hops)", PY + ["score_test"], "requiring the evidence to state its values"),
    ("score.py", '"guardrails_hold": all(guard[f] >= -ALLOWED_LOSS[f] for f in guard)', '"guardrails_hold": True', PY + ["score_test"], "the guardrails"),
    ("score.py", "return min(valid, key=lambda n: NET_LINES[n])", "return valid[-1]", PY + ["score_test"], "taking the smallest valid arm"),
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
