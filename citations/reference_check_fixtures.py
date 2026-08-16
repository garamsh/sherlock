"""The dead references `reference_check.py` exists to catch, and one it does not.

Four defects arrived in one day and none was catchable by the claim registers.
Each is reconstructed here as the line that carried it, in a tree shaped like the
one it lived in, so the check is shown finding it rather than asserted to. The
fourth is reconstructed too, and shown *not* being found: it sits behind a named
exclusion, and a fixture that quietly dropped it would hide the boundary.

Two more hold the convention exclusion to its width from both sides: a name
`stack-go.md` exists to forbid stays silent, and a dead path in a sibling
convention file is reported. Widening the exclusion back to the directory would
fail the second.

A check tuned until its output is empty passes every day and finds nothing. This
is what stops that: tighten a rule far enough and these stop failing.

Runs the check over throwaway trees under a temporary directory; touches neither
the repository nor the network.
"""
from __future__ import annotations
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import reference_check

# Each fixture: the defect, the file that carried it, the line verbatim, and the
# citation the check must report — or None where it is out of scope by design.
FIXTURES = [
    {
        "defect": "#384 — ADR 0001 built its Context on a plan that is not in the tree",
        "path": "docs/architecture/adr/0001-stack-and-layout.md",
        "line": "`docs/PLAN.md` §6 fixes the language as Go and the working "
                "language as English. PLAN §5.1 names a top-level structure of "
                "`gateway/`, `agent/`, `tools/`, while `docs/convention/stack-go.md` "
                "mandates Layout A (`cmd/<binary>` + `internal/<domain>`) with a "
                "banned-name list.",
        "expect": "docs/PLAN.md",
    },
    {
        "defect": "#384 — the local deployment described a tool the agent does not import",
        "path": "deploy/local/README.md",
        "line": "The agent imports the tools it hosts at compile time "
                "(`tools/files`, `tools/playwright`, `tools/shell_exec`) and they "
                "call the workspace over the shared network namespace's loopback.",
        "expect": "tools/playwright",
    },
    {
        "defect": "#388 — the Dockerfile cited a record the log does not reach",
        "path": "Dockerfile",
        "line": "# Platform images (ADR 0018): one build stage, two targets.",
        "expect": "ADR 0018",
    },
    {
        "defect": "#400 — the memory-cycle runner wrote into a directory never committed",
        "path": "docs/experiments/memory-cycle/harness/system_runner.py",
        "line": '    with open("results/system_cycle.md", "w") as f:',
        # Out of scope: a string literal, excluded because it is an instruction to
        # a runtime rather than a citation to a reader. Admitting it would need the
        # rule that also admits `data/memory.db` and `dir/manifest.json`, which are
        # not defects. The check reports nothing here and that is the boundary.
        "expect": None,
    },
    {
        "defect": "#428 — a name stack-go.md exists to forbid is not a dead path",
        "path": "docs/convention/stack-go.md",
        "line": "Never pre-emptively create `internal/infra/`, `internal/platform/`.",
        # Suppressed by the one excluded file. `internal` is a real top-level entry
        # here, so the token is admitted and the exclusion is what silences it —
        # without it in the scaffold this fixture would pass for the wrong reason.
        "expect": None,
    },
    {
        "defect": "#428 — a convention file other than stack-go.md is checked",
        "path": "docs/convention/documentation.md",
        "line": "Use file paths (`docs/PLAN.md`) instead of drawn hierarchies.",
        "expect": "docs/PLAN.md",
    },
]

# Enough of the real tree for the rooted rule to mean what it means in the
# repository: `docs` and `tools` exist, so a dead path under them is a claim.
SCAFFOLD = [
    "README.md",
    "tools/files/main.go",
    "tools/shell_exec/main.go",
    "internal/memory/ranking.go",
    "docs/convention/stack-go.md",
    "docs/experiments/memory-cycle/README.md",
    "deploy/local/docker-compose.yml",
] + [f"docs/architecture/adr/{n:04d}-record.md" for n in range(18)]


def build(root, fixture):
    """A throwaway tree holding the scaffold and the one reconstructed line."""
    for path in SCAFFOLD + [fixture["path"]]:
        full = os.path.join(root, path)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        if not os.path.exists(full):
            open(full, "w").write("placeholder\n")
    open(os.path.join(root, fixture["path"]), "w").write(fixture["line"] + "\n")
    for command in (["init", "-q"], ["add", "-A"]):
        subprocess.run(["git", "-C", root] + command, check=True,
                       capture_output=True)


def main():
    failures = []
    for fixture in FIXTURES:
        with tempfile.TemporaryDirectory() as root:
            build(root, fixture)
            _, dead = reference_check.check(root)
            reported = [token for _, _, token, _ in dead]
            expected = fixture["expect"]

            if expected is None:
                ok = not reported
                got = f"reported {reported}" if reported else "reported nothing"
            else:
                ok = expected in reported
                got = f"reported {reported}"

            print(f"{'ok  ' if ok else 'FAIL'}  {fixture['defect']}")
            print(f"        {fixture['path']}: "
                  f"{'expected ' + expected if expected else 'out of scope'}, {got}")
            if not ok:
                failures.append(fixture["defect"])

    if failures:
        print(f"\n{len(failures)} fixtures did not behave as recorded:")
        for defect in failures:
            print(f"  {defect}")
        return 1
    print("\nevery reconstructed reference behaves as recorded")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
