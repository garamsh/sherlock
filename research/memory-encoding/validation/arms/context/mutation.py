"""Remove each behaviour of arm C a test names, run the tests, restore it,
and record that the tests failed (evidence/mutation.txt).

    python3 docs/research/memory-encoding/validation/arms/context/mutation.py
"""

import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[5]

GO = ["go", "test", "-count=1", "./docs/research/memory-encoding/validation/arms/context/"]
PY = ["python3", "-B", "-m", "unittest", "controls_test"]

# (file beside this one, text, replacement, test command, what the mutation removes)
MUTATIONS = [
    ("synthesis.go", "messages[max(i-window, 0):i]", "messages[i:i]", GO, "the messages before the batch"),
    ("synthesis.go", "n > maxContext", "n > 1<<20", GO, "the context's character bound"),
    ("synthesis.go", "\tif result.Context == nil {", "\tif false {", GO, "refusing a call without a context"),
    ("synthesis.go", "\treturn fmt.Sprintf(\" (Attempt %d.)\", attempt)", '\treturn ""', GO, "varying a retried request past the proxy's cache"),
    ("main.go", "env.Embedder.Embed(ctx, embedText(contextLine, summary))", "env.Embedder.Embed(ctx, summary)", GO, "embedding the context"),
    ("main.go", 'return contextLine + "\\n\\n" + summary', 'return summary + "\\n\\n" + contextLine', GO, "the context before the summary"),
    ("main.go", "`UPDATE conversation_mid SET summary=?, context=?,", "`UPDATE conversation_mid SET summary=summary||substr(?,1,0), context=?,", GO, "storing C's summary"),
    ("memory.go", 'items[i].Text = strings.TrimSpace(it.Text) + "\\n" + contextLabel + c', "items[i].Text = it.Text", GO, "showing the context on recall"),
    ("controls.py", 'S.states(batch_of(c["hops"][0]["source"])["context"], c["subject"])', "True", PY, "the SC subject control"),
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
