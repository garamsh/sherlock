# Mutation Survey

What the test suite fails to notice when the behaviour a test names is deleted, measured on `internal/tool/`, `internal/workspace/shell/` and `internal/memory/`.

## Contents
- What a survivor means
- What ran, and against which tree
- The instrument had the defect it was sent to find
- Counts
- The three deletions the whole suite misses
- The 46, triaged
- What was filed and what was not
- Does this need repeating
- Reproducing

## What a survivor means

A surviving mutant is not a bug. It is a test whose failure was the only signal anyone had, and which does not fail. **A boundary whose failure produces no test failure is a boundary nobody is checking.** It may still work; nothing here says it does not. What this record says is that the suite would not notice if it stopped.

The instance that makes it concrete: **deleting the ADR 0008 privilege drop leaves `go test ./...` green.** The drop is the assignment that makes an Exec child run as the unprivileged exec UID instead of root — the mechanism `docs/architecture/security.md` §Current decisions rests on for the whole untrusted-code path. Tests naming it exist. They skip on a non-root runner, and no run this repository performs is privileged, so they have never executed. That is worse than a missing test: anyone asking whether the drop is tested finds a test named for it and stops.

The counts below are the survey. This paragraph is the finding.

## What ran, and against which tree

- **Tool: gremlins `v0.6.0`**, installed with `go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0`. That is its current release, published 2025-12-06 and read from the project's own releases rather than recalled, as `docs/convention/ci.md` §Verify a pinned dependency before adopting it requires. The version is the install ref, not the binary's self-report: `go install` stamps no version into it and `gremlins --version` prints `dev`.
- It loads and mutates this module: the module declares `go 1.25`, so the toolchain builds gremlins itself under go1.25.13, and its `golang.org/x/tools v0.39.0` parses sources this repository's go1.26.5 toolchain compiles. The precedent from #513 — a released Go tool built against an older toolchain failing to load a go1.26 module — does not repeat.
- **`go-mutesting` was rejected without running it**, on its own documentation: last release v1.2 in June 2021, `go.mod` declaring `go 1.10`. That is the profile #513 describes.
- **Hand deletions** for the shape gremlins cannot express. Its operators rewrite an expression; deleting a whole guard is a different edit, and it is the one a security boundary fails as — the shape #586 reports from the garam PM's OAuth CSRF case.
- **Trees.** The gremlins run and the 13 hand deletions ran at `949ac99`. The two whole-suite survivors in `internal/workspace/shell` were re-run at `b4093e3`, two commits later, and both still survive; neither commit touches that package. That package's 16 gremlins survivors were re-run against the whole suite at `b4093e3` plus the commit adding this harness, which changes no Go code.
- Nothing was fixed, no test was changed, and nothing was added to `make ci`. Every mutant was restored before the next, and the harness refuses to start on a tree it cannot restore into.

## The instrument had the defect it was sent to find

gremlins derives each mutant's timeout from the baseline test duration. These three packages' test binaries run in 0.04 to 0.15 s at `b4093e3`, so the derived budget was smaller than the compile step and nearly every mutant was recorded as timed out without a test ever running. The first run's report, in `results/gremlins-mistimed.txt`:

| Package | Killed | Survived | Timed out | Reported efficacy |
|---|---|---|---|---|
| `internal/tool` | 0 | 0 | 90 | 0.00% |
| `internal/workspace/shell` | 38 | 0 | 23 | 100.00% |
| `internal/memory` | 63 | 3 | 14 | 95.45% |

Ninety timeouts cannot occur in the 1.86 seconds that run took, and a package cannot be perfectly tested with 23 of its mutants never executed. This is a well-formed report on a path nothing ran — the same defect the survey was commissioned to find, produced by the instrument, and the 100.00% is the direction that would have been believed.

`--timeout-coefficient 200` fixes it. **Anyone repeating this must pass that flag, and must read a high timed-out count as instrument failure rather than as a result.**

## Counts

Automated, at `949ac99`, from `results/gremlins.txt`:

| Package | Killed | Survived | Not covered | Examined |
|---|---|---|---|---|
| `internal/tool` | 77 | 13 | 32 | 122 |
| `internal/workspace/shell` | 45 | 16 | 12 | 73 |
| `internal/memory` | 63 | 17 | 3 | 83 |
| Total | 185 | 46 | 47 | 278 |

**What "survived" measures here.** gremlins gathers coverage and runs tests one package at a time, so a survivor in its report means *this package's own tests do not kill it* and says nothing about the rest of the suite. Two independent confirmations: `internal/tool/registry.go` has 21 mutants reported as not covered, 0 killed and 0 survived, because the package has no test file for the registry at all — while the integration suite under `tests/` does reach it; and 4 of the 16 survivors in `internal/workspace/shell`, re-run against the whole suite, are killed by it. **The 46 is a per-package figure. It is not a count of uncovered behaviour.**

Hand deletions, from `results/hand-mutants.txt`: **13 attempted, 11 viable** (one does not compile once its comparison is gone, one has no safe textual anchor), **6 survive their own package's tests, 3 survive the entire suite.**

## The three deletions the whole suite missed

**One of the three is closed.** The duplicate-name deletion (`M10`) is killed as of #605; the two privileged-path deletions (`M4`, `M6`) still survive and are #604's. The figures above are the survey as it ran and are not restated here — this section carries each deletion's current status instead.

Each is its own defect with its own cause. The exact edits are in `harness/hand_mutants.py`, which is the verbatim record — it applies each deletion by exact-string anchor and reports a miss rather than mutating a moved site, so it cannot drift out of agreement with the code the way a transcription in prose would.

### The ADR 0008 privilege drop — `internal/workspace/shell/process_linux.go:145-153`

Deleting the `if euid == 0` block whose body assigns `attr.Credential` leaves the whole suite green. Harness id `M4`; re-confirmed at `b4093e3`.

What the tests assert instead:

- `process_test.go:217` asserts the child's own `id -u` equals the test process's uid. In single-account mode the drop block is never entered, so the assertion holds identically with the block deleted — it asserts that a child ran under the account it already had.
- `process_test.go:87` asserts the refusal error surfaces when the drop cannot be performed, not that it happens when it can.
- The tests that would exercise the drop sit behind `requireRoot` (`process_test.go:42`), which skips its callers when the euid is not 0. Exactly ten tests skip in this package on a non-root run at `b4093e3` and every one of them is a `requireRoot` caller; `go test` prints `ok` for the package either way, so the skips are invisible without `-v`. `.github/workflows/ci.yml` runs `ubuntu-latest` with no `container:` and no `sudo`, so no run this repository performs is privileged. Three of the ten name behaviour a survivor in the table below removes: `TestProcessExecutor_DefaultTimeoutRejectsZero`, `TestProcessExecutor_RunsInWorkspace` and `TestProcessExecutor_RequestEnvAppliesOnTop`.

### ADR 0010 network isolation, privileged path — `internal/workspace/shell/process_linux.go:154-159`

Deleting the `attr.Unshareflags` block survives the whole suite for the same reason. Harness id `M6`; re-confirmed at `b4093e3`.

The contrast is the useful part: deleting the **unprivileged** path of the same feature (`M12`) is killed by this package's own tests. One feature, two paths, one checked — and the unchecked one is the path the deployment runs, where the workspace container is root and drops to uid 1001 (`deploy/local/`).

### Duplicate tool names — `internal/tool/registry.go:139-141`

Deleting the duplicate-name rejection survived both the package's tests and the suite. Harness id `M10`. Nothing anywhere asserted it: the guard's message had one occurrence in the tree, the `return` statement itself.

**Closed by #605.** `internal/tool/registry_test.go` installs two tool directories whose manifests claim one name and asserts the refusal by its message. `M10` reports `package=KILLED` in `results/hand-mutants-after-605.txt` — killed by the package that defines the guard, not by the integration suite.

Distinct from the drift-pin deletions (`M1`, `M2b`, `M3`), which survive `internal/tool`'s own tests and are killed by the integration suite. The pin is guarded, just not where it is defined; the duplicate-name guard is guarded nowhere.

## The 46, triaged

Only `internal/workspace/shell`'s 16 were re-run against the whole suite. The 13 in `internal/tool` and the 17 in `internal/memory` sit in files other workers held in the round this record was written, so they carry a reading of the site and no second measurement — named as a gap rather than filled with a figure this record cannot source.

`internal/workspace/shell`, all 16 re-run at the harness commit on `b4093e3` (`results/survivor-retest.txt`):

| Cause | Sites | Whole suite |
|---|---|---|
| The guard is checked by the integration suite, not by the package that defines it | `dumpable_linux.go:44`, `process_linux.go:109`, `:174`, `:201` | killed |
| Inside `if euid == 0`, which no run this repository performs enters | `process_linux.go:86`, `:154`, `:323` | survives |
| Behind `requireRoot`, so the test naming the behaviour skips | `process_linux.go:116`, `:178` | survives |
| No test asserted it at all: nothing anywhere checked that `Request.Stdin` reaches the child — closed by #606 | `process_linux.go:171` | killed by the package since #606 |
| The test asserts presence and non-emptiness, so the fallback value passes too | `childenv.go:11`, `:15` | survives |
| Unobservable: the mutant rewrites the capacity argument of a zero-length `make` | `limits.go:48` (two mutants) | survives |
| An untested boundary on an argument shape no caller builds: the shim verb with no arguments, and a program whose `Args` is non-nil and empty, which the Exec RPC cannot produce (`internal/workspace/server.go:66` passes `req.GetArgs()`, nil when the field is empty) | `limits.go:28`, `:65` | survives |

So of this package's 16: **4 are covered elsewhere, 2 are equivalent mutants no test can kill, 2 sit on a boundary no shipped caller reaches, and 8 are genuine gaps** — 5 of the 8, plus both surviving hand deletions, from the single cause that the privileged half of the executor's tests never executes. One of the 8, `process_linux.go:171`, is closed by #606; it was the only one of them whose cause was an absent test rather than a test that never runs, which is why it could be closed without touching how the suite is invoked.

`internal/tool`'s 13, by inspection only:

- **9 in `internal/tool/tooltest/`**, a test-helper package. A mutant there weakens a guard other packages' tests call; it is not shipped behaviour.
- **4 in `internal/tool/manifest.go`**, all on the two `if i > 0` comma conditions of the manifest canonicalizer. Each changes the canonical bytes and therefore every manifest hash, which is observable only against a literal expected hash — and no test pins one. The consequence is that a change to the canonicalizer silently invalidates every operator's pin set.

`internal/memory`'s 17, by inspection only:

- **7 in `internal/memory/ranking.go`**, every one of them, with 0 killed in that file: no test pins the eviction score's ordering.
- **6 in `internal/memory/store.go`** — four on the eviction sort's comparisons, two on a `make` capacity argument, the shape shown unkillable above.
- **3 in `internal/memory/retain.go`**, all boundary mutants on the retention thresholds: the tests use values away from the boundary.
- **1 in `internal/memory/sqlite/sqlite.go`**, where the mutant drops the archived IDs a supersession returns.

## What was filed and what was not

Filed, one per cause, each carrying the mutant and the green run:

- #604 — the privileged half of `internal/workspace/shell`'s tests never executes: the ADR 0008 drop, the ADR 0010 unshare, and five gremlins survivors in the same region. Open.
- #605 — duplicate tool names are accepted and nothing catches it. **Closed**; `M10` is killed by `internal/tool`'s own tests.
- #606 — no test asserts `Request.Stdin` reaches the child. **Closed**; `process:171` is killed by `internal/workspace/shell`'s own tests.

The two closed ones were the survey's only findings whose cause was an absent assertion. #604's is that a test which names the behaviour never runs, which is why it takes a change to how the suite is invoked rather than a test.

Not filed, with the reason:

- **The two `make`-capacity mutants.** Equivalent: the slice's length and contents are unchanged for every argv the executor builds. An equivalent mutant is not a test gap.
- **The two `limits.go` boundaries.** Neither shape reaches the executor from the Exec RPC, which is the only caller that supplies `Args`. A test for an input no caller can send buys nothing.
- **The 4 killed by the integration suite.** The behaviour is checked; where the check lives is a placement question, not a hole.
- **`internal/memory`'s 17.** Not re-run, and the domain is being replaced — #586's own constraint is that a mutant list against code being replaced is wasted.
- **`internal/tool`'s 13.** Not re-run. The `tooltest` 9 are not shipped behaviour; the canonicalizer 4 are real and belong with whoever next touches the pin set.
- **`childenv.go:11` and `:15`.** Genuine, and each is one assertion rather than an unchecked boundary: `TestChildEnv_MinimalBasics` asserts the child env holds exactly `HOME`, `LANG` and `PATH` with non-empty values, which the built-in fallback satisfies as well as the inherited value does. Listed here so the next reader has them; raising them as issues is the PM's call.

The 47 not-covered mutants are a different statement from the 46 and are not triaged here: 21 of them are in `internal/tool/registry.go`, which the integration suite does reach, so they record where coverage lives rather than that the code is unguarded.

## Does this need repeating

Yes, with a caveat that decides the shape of any repeat. 46 survivors and 47 uncovered mutants out of 278 is not a tail, and the corrected automated run took 34 seconds across the three packages, so cost is not the obstacle.

The caveat is that gremlins measures something adjacent to the question. Its operators perturb expressions; all three whole-suite survivors came from hand deletion. A gate built on it would have reported efficacy between 73% and 86% for these packages while the privilege drop stayed deletable. gremlins is a useful screen and not a sufficient one, and its per-package scope means a survivor it reports needs a second run against the whole suite before it is a finding. Whether it becomes a `make ci` gate is the separate decision #586 reserves.

## Reproducing

```
go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0
gremlins unleash --timeout-coefficient 200 ./internal/tool
gremlins unleash --timeout-coefficient 200 ./internal/workspace/shell
gremlins unleash --timeout-coefficient 200 ./internal/memory
cd docs/experiments/mutation-survey/harness
python3 hand_mutants.py
python3 survivor_retest.py
```

Both harness scripts rewrite tracked files under `internal/` and restore each with `git checkout --` before the next, so they refuse to start on a dirty tree and print the tree state at the end. `harness/mutate.py` holds the runner both use; the mutants themselves are the tables in the other two files. A mutant whose anchor is absent, or present more than once, is reported and skipped — so a moved line surfaces as a miss instead of mutating a different site.

Results are the four files under `results/`: `gremlins.txt` and `gremlins-mistimed.txt` are the corrected and the mis-timed automated runs, `hand-mutants.txt` is the deletion rounds, and `survivor-retest.txt` is `internal/workspace/shell`'s 16 survivors against the whole suite.

The figures above are pinned to the trees named in §What ran, and a line number in a file that has moved since names the site in the tree that was measured. `internal/workspace/shell/` has not been touched since `949ac99`, which is what lets that package's sites and its 16 re-tested survivors stand as written. `internal/tool/manifest.go`, `internal/tool/registry.go`, `internal/tool/schema.go` and `internal/memory/retain.go` have all changed since (#595, #599), so the sites in them are named by their guard rather than by a line wherever the guard is what matters. Every deletion the harness applies is anchored on the code's own text, so a moved guard surfaces as a reported miss and never as a mutation of the wrong site.
