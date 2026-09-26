"""Remove each behaviour a test names, run the tests, restore it, and record
that the tests failed (evidence/mutation.txt).

    python3 docs/research/chained-recall/mutation.py
"""

import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]

# (file, text, replacement, test command, what the mutation removes)
MUTATIONS = [
    ("study/store.go", 'text = bearerToken.ReplaceAllString(text, "[redacted credential]")', "_ = bearerToken",
     ["go", "test", "./docs/research/chained-recall/study/"], "the copied bearer-token redaction"),
    ("study/shipped.go", "shippedRecent  = 2", "shippedRecent  = 3",
     ["go", "test", "./docs/research/chained-recall/study/"], "the shipped recall's two recent mids"),
    ("study/adapt.go", "out = append(out, agent.RecalledMemory{ID: fmt.Sprintf(\"source:%d\"", "_ = append(out, agent.RecalledMemory{ID: fmt.Sprintf(\"source:%d\"",
     ["go", "test", "./docs/research/chained-recall/study/"], "the pending previews after an arm's items"),
    ("study/adapt.go", 'summary = it.Level + ": " + summary', 'summary = "" + summary',
     ["go", "test", "./docs/research/chained-recall/study/"], "the level label on a search result that is not a mid"),
    ("study/live.go", "Recaller:           recaller{memory: memory},", "Recaller:           nil,",
     ["go", "test", "./docs/research/chained-recall/study/"], "the arm's recall in the live session"),
    ("study/live.go", 'ego = agent.DefaultEgo + "\\n\\n" + extra', "ego = \"\"",
     ["go", "test", "./docs/research/chained-recall/study/"], "the arm's instruction in the live session"),
    ("harness/quota.go", 'return bytes.Contains(body, []byte("2056"))', 'return bytes.Contains(body, []byte(""))',
     ["go", "test", "./docs/research/chained-recall/harness/"], "the usage-limit code test"),
    ("harness/quota.go", "\t\t\tg.closed = false\n", "",
     ["go", "test", "./docs/research/chained-recall/harness/"], "reopening after a successful probe"),
    ("harness/proxy.go", "utf8.RuneCountInString(args.Summary) <= 2000", "utf8.RuneCountInString(args.Summary) >= 0",
     ["go", "test", "./docs/research/chained-recall/harness/"], "the summary length bound on what the proxy caches"),
    ("score.py", 'return bool(holding), any(states(it["text"], hop["key"]) for it in holding)', 'return bool(holding), bool(holding)',
     ["python3", "-m", "unittest", "score_test"], "the entity check of a reached hop"),
    ("score.py", 'return states(text, check["value"])', "return True",
     ["python3", "-m", "unittest", "score_test"], "the answer check of the live stage"),
    ("corpus.py", "if new & query or any(new & hops[j] for j in range(i)):", "if False:",
     ["python3", "-m", "unittest", "corpus_test"], "the rule that later hop words stay out of the query"),
]


def run(cmd):
    cwd = HERE if cmd[0] == "python3" else ROOT
    env = {"PYTHONDONTWRITEBYTECODE": "1", "PATH": "/usr/bin:/bin:/usr/local/go/bin:" + str(Path.home() / "go" / "bin"), "HOME": str(Path.home())}
    return subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, env={**env, "GOFLAGS": "-count=1"}).returncode


def main():
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
    if run(["go", "test", "./docs/research/chained-recall/..."]) != 0 or run(["python3", "-m", "unittest", "score_test", "corpus_test"]) != 0:
        raise SystemExit("the unmutated tests do not pass")
    lines.append("control: every unmutated test passes")
    (HERE / "evidence" / "mutation.txt").write_text("\n".join(lines) + "\n")
    print("\n".join(lines))


if __name__ == "__main__":
    main()
