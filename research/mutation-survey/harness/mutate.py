#!/usr/bin/env python3
"""Applies one mutant at a time, runs the tests, records red or green, restores.

A mutant here is an exact-string replacement in a tracked file. The runner
refuses to start on a dirty tree, because a restore it cannot trust makes every
later result worthless, and it reports the tree state at the end for the same
reason. Each mutant is run against its own package's tests first and against the
whole suite second, which is the distinction the report rests on: a mutant the
package misses and the suite catches is a coverage-placement finding, and one
both miss is an uncovered boundary.

A mutant whose anchor does not appear exactly once is reported and skipped rather
than applied, so a moved line can never silently mutate a different site.

Two knobs, both for asking the same question of a different command. Naming
mutant ids on the command line runs only those, so one boundary can be checked
without paying for twelve others. `SHERLOCK_MUTATE_SUITE_CMD` replaces the
whole-suite command, which is what a boundary reachable only under privilege
needs: `go test ./...` cannot execute it, so the mutant survives that and is
killed by the command that can. The command used is printed with the results,
because the second column means nothing without it.
"""
from __future__ import annotations
import os
import subprocess
import sys

REPO = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)),
                                    "..", "..", ".."))

SUITE_CMD = os.environ.get("SHERLOCK_MUTATE_SUITE_CMD", "go test -count=1 ./...")


def run(cmd, timeout=1800):
    return subprocess.run(cmd, shell=True, cwd=REPO, capture_output=True,
                          text=True, timeout=timeout)


def survey(mutants, only=None):
    """Runs each mutant and prints one line per result. Returns the results."""
    wanted = set(sys.argv[1:] if only is None else only)
    if wanted:
        mutants = [m for m in mutants if m[0] in wanted]
        missing = sorted(wanted - {m[0] for m in mutants})
        if missing:
            print("no such mutant: " + ", ".join(missing))
            sys.exit(1)

    dirty = run("git status --porcelain").stdout.strip()
    if dirty:
        print("REFUSING: working tree not clean:\n" + dirty)
        sys.exit(1)
    print("HEAD:", run("git rev-parse --short HEAD").stdout.strip(), flush=True)
    print("suite command:", SUITE_CMD, flush=True)

    results = []
    for mid, path, pkg, desc, old, new in mutants:
        if old is None:
            results.append((mid, desc, "NO-ANCHOR", "-"))
            print(f"{mid}: NO-ANCHOR (no safe textual anchor)", flush=True)
            continue
        full = os.path.join(REPO, path)
        with open(full) as fh:
            src = fh.read()
        if src.count(old) != 1:
            results.append((mid, desc, "ANCHOR-MISS", "-"))
            print(f"{mid}: ANCHOR-MISS ({src.count(old)} matches in {path})",
                  flush=True)
            continue
        with open(full, "w") as fh:
            fh.write(src.replace(old, new, 1))

        if run("go build ./...").returncode != 0:
            run(f"git checkout -- {path}")
            results.append((mid, desc, "NO-COMPILE", "-"))
            print(f"{mid}: NO-COMPILE (not a viable mutant)", flush=True)
            continue

        pkg_status = ("KILLED" if run(f"go test -count=1 {pkg}").returncode != 0
                      else "SURVIVED")
        suite_status = "-"
        if pkg_status == "SURVIVED":
            suite_status = ("KILLED" if run(SUITE_CMD).returncode != 0
                            else "SURVIVED")
        run(f"git checkout -- {path}")
        results.append((mid, desc, pkg_status, suite_status))
        print(f"{mid}: package={pkg_status} suite={suite_status}  ({desc})",
              flush=True)

    print("\n==== SUMMARY ====")
    print(f"{'id':<12} {'pkg tests':<11} {'full suite':<11} description")
    for mid, desc, p, s in results:
        print(f"{mid:<12} {p:<11} {s:<11} {desc}")
    viable = [r for r in results if r[2] in ("KILLED", "SURVIVED")]
    print(f"\nattempted:                          {len(results)}")
    print(f"viable:                             {len(viable)}")
    print(f"survived their own package's tests: "
          f"{len([r for r in viable if r[2] == 'SURVIVED'])}")
    print(f"survived the entire suite:          "
          f"{len([r for r in viable if r[3] == 'SURVIVED'])}")
    left = run("git status --porcelain").stdout.strip()
    print("\ntree clean after restore:", "YES" if not left else "NO -> " + left)
    return results
