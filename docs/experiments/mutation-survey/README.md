# Mutation Survey

What the test suite fails to notice when the behaviour a test names is deleted, measured on `internal/tool/`, `internal/workspace/shell/` and `internal/memory/`.

## Contents
- What a survivor means
- What ran, and against which tree
- The instrument had the defect it was sent to find
- Counts
- The three deletions the whole suite missed
- The 46, triaged
- What was filed and what was not
- Does this need repeating
- Reproducing

## What a survivor means

A surviving mutant is not a bug. It is a test whose failure was the only signal anyone had, and which does not fail. **A boundary whose failure produces no test failure is a boundary nobody is checking.** It may still work; nothing here says it does not. What this record says is that the suite would not notice if it stopped.

The instance that makes it concrete: **deleting the ADR 0008 privilege drop left `go test ./...` green.** The drop is the assignment that makes an Exec child run as the unprivileged exec UID instead of root — the mechanism `docs/architecture/security.md` §Current decisions rests on for the whole untrusted-code path. Tests naming it existed. They skipped on a non-root runner, no run this repository performed was privileged, and so they had never executed. That is worse than a missing test: anyone asking whether the drop is tested found a test named for it and stopped.

The counts below are the survey. This paragraph is the finding.

**Closed by #604**, whose evidence is in `results/privileged-check.txt` and whose findings are folded into the sections below. `go test ./...` still cannot see either boundary and still reports both mutants as survivors — an unprivileged process cannot execute the branch they live in. What changed is that there is now a second command, `make test-privileged`, which a privileged CI job invokes and which both deletions turn red. Where a figure below is pinned to `go test ./...` it is still what that command reports; the privileged command's answer is named alongside it.

## What ran, and against which tree

- **Tool: gremlins `v0.6.0`**, installed with `go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0`. That is its current release, published 2025-12-06 and read from the project's own releases rather than recalled, as `docs/convention/ci.md` §Verify a pinned dependency before adopting it requires. The version is the install ref, not the binary's self-report: `go install` stamps no version into it and `gremlins --version` prints `dev`.
- It loads and mutates this module: the module declares `go 1.25`, so the toolchain builds gremlins itself under go1.25.13, and its `golang.org/x/tools v0.39.0` parses sources this repository's go1.26.5 toolchain compiles. The failure `docs/convention/stack-go.md` §9 names — a tool whose own `go` directive is older than the module's yielding a binary that refuses to load its packages — does not repeat here.
- **`go-mutesting` was rejected without running it**, on its own documentation: last release v1.2 in June 2021, `go.mod` declaring `go 1.10`. That is the profile `stack-go.md` §9 names.
- **Hand deletions** for the shape gremlins cannot express. Its operators rewrite an expression; deleting a whole guard is a different edit, and it is the one a security boundary fails as — the shape #586 reports from the garam PM's OAuth CSRF case.
- **Trees.** The gremlins run and the 13 hand deletions ran at `949ac99`. The two whole-suite survivors in `internal/workspace/shell` were re-run at `b4093e3`, two commits later, and both still survive; neither commit touches that package. That package's 16 gremlins survivors were re-run against the whole suite at `b4093e3` plus the commit adding this harness, which changes no Go code.
- **#604's trees.** Everything the privileged command reports was run on this branch's code commits over `4e69889`, in the digest-pinned image the workflow's privileged job names, as root with CAP_SYS_ADMIN. The pipeline was failing account-wide while that work happened (#603), so none of it is a CI run and none of it is reported as one.
- **Line numbers.** Every `file.go:NN` below names the line a site sat on at the tree it was measured at, which is how `results/survivor-retest.txt` and the other result files record it — each names its tree at its head. None is maintained against later edits and several have since moved, so a site is found by what it is rather than by its number: `harness/hand_mutants.py` carries every hand deletion as an exact-string anchor and reports a miss rather than mutating a site that moved.
- Nothing was fixed by the survey, no test was changed by it, and nothing was added to `make ci`. Every mutant was restored before the next, and the harness refuses to start on a tree it cannot restore into. #604 later changed tests and added an entry point — that is the issue the survey opened, not the survey.

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

**What "survived" measures here.** gremlins gathers coverage and runs tests one package at a time, so a survivor in its report means *this package's own tests do not kill it* and says nothing about the rest of the suite. Two independent confirmations: `internal/tool/registry.go` has 21 mutants reported as not covered, 0 killed and 0 survived, because the package has no test file for the registry at all — while the integration suite under `tests/` does reach it; and 4 of the 16 survivors in `internal/workspace/shell`, re-run against the whole suite, are killed by it — 6 more are killed once the command is the privileged one #604 added. **The 46 is a per-package figure. It is not a count of uncovered behaviour.**

Hand deletions, from `results/hand-mutants.txt`: **13 attempted, 11 viable** (one does not compile once its comparison is gone, one has no safe textual anchor), **6 survive their own package's tests, 3 survive the entire suite.**

## The three deletions the whole suite missed

**All three are closed.** The duplicate-name deletion (`M10`) is killed as of #605; the two privileged-path deletions (`M4`, `M6`) as of #604 — but by a different command. `go test ./...` still reports those two as survivors and always will: an unprivileged process cannot execute the branch they live in. Their kill lives in `make test-privileged`, which a privileged CI job invokes. The figures above are the survey as it ran and are not restated here — this section carries each deletion's current status instead.

Each is its own defect with its own cause. The exact edits are in `harness/hand_mutants.py`, which is the verbatim record — it applies each deletion by exact-string anchor and reports a miss rather than mutating a moved site, so it cannot drift out of agreement with the code the way a transcription in prose would.

### The ADR 0008 privilege drop — the `attr.Credential` assignment in `internal/workspace/shell/process_linux.go`

Deleting the `if euid == 0` block whose body assigns `attr.Credential` leaves `go test ./...` green, and turns `make test-privileged` red. Harness id `M4`; re-confirmed at `b4093e3` before #604 and killed by the privileged command after it.

The assertion that kills it did not exist and was added by #604: `TestProcessExecutor_PrivilegedDropRunsAsExecUID` reads the child's own `id -u` and `id -g` under a privileged executor, where the two accounts are different numbers.

What the tests asserted instead, which is the finding this section records:

- `TestProcessExecutor_SingleAccountRunsAsOwnUID` asserts the child's own `id -u` equals the test process's uid. In single-account mode the drop block is never entered, so the assertion holds identically with the block deleted — it asserts that a child ran under the account it already had.
- `TestProcessExecutor_AgentUIDUnavailable` asserts the refusal error surfaces when the drop cannot be performed, not that it happens when it can.
- The tests that would exercise the drop sit behind `requireRoot` (`process_test.go`), which skips its callers when the euid is not 0. Exactly ten tests skipped in this package on a non-root run at `b4093e3` and every one of them was a `requireRoot` caller; `go test` prints `ok` for the package either way, so the skips are invisible without `-v`. `.github/workflows/ci.yml` ran `ubuntu-latest` with no `container:` and no `sudo`, so no run this repository performed was privileged. Three of the ten name behaviour a survivor in the table below removes: `TestProcessExecutor_DefaultTimeoutRejectsZero`, `TestProcessExecutor_RunsInWorkspace` and `TestProcessExecutor_RequestEnvAppliesOnTop`.
- The family had also never run anywhere, so nobody had seen that it could not: under root, four of its cases failed for reasons that have nothing to do with the boundary and one production defect surfaced. #604 records what each was.

### ADR 0010 network isolation, privileged path — the `attr.Unshareflags` assignment in `internal/workspace/shell/process_linux.go`

Deleting the `attr.Unshareflags` block survives `go test ./...` for the same reason, and turns `make test-privileged` red. Harness id `M6`; re-confirmed at `b4093e3` before #604 and killed by the privileged command after it.

Here the assertion existed: the netns cases in `internal/workspace/shell/netns_linux_test.go` dial out of the namespace and observe its interfaces. Neither could run — one execs a probe the dropped account could not reach, the other read a `/sys/class/net` that reports the namespace of whoever mounted sysfs rather than the child's. #604 fixed both.

The contrast is the useful part: deleting the **unprivileged** path of the same feature (`M12`) is killed by this package's own tests. One feature, two paths, one checked — and the unchecked one was the path the deployment runs, where the workspace container is root and drops to uid 1001 (`deploy/local/`).

### Duplicate tool names — the duplicate-name rejection in `internal/tool/registry.go`

Deleting the duplicate-name rejection survived both the package's tests and the suite. Harness id `M10`. Nothing anywhere asserted it: the guard's message had one occurrence in the tree, the `return` statement itself.

**Closed by #605.** `internal/tool/registry_test.go` installs two tool directories whose manifests claim one name and asserts the refusal by its message. `M10` reports `package=KILLED` in `results/hand-mutants-after-605.txt` — killed by the package that defines the guard, not by the integration suite.

Distinct from the drift-pin deletions (`M1`, `M2b`, `M3`), which survive `internal/tool`'s own tests and are killed by the integration suite. The pin is guarded, just not where it is defined; the duplicate-name guard is guarded nowhere.

## The 46, triaged

Only `internal/workspace/shell`'s 16 were re-run. The 13 in `internal/tool` and the 17 in `internal/memory` sit in files other workers held in the round this record was written, so they carry a reading of the site and no second measurement — named as a gap rather than filled with a figure this record cannot source.

`internal/workspace/shell`, all 16 against both commands: `go test ./...` at the harness commit on `b4093e3` (`results/survivor-retest.txt`), and `make test-privileged` after #604 (`results/privileged-retest.txt`).

| Cause | Sites | `go test ./...` | `make test-privileged` |
|---|---|---|---|
| Checked by the integration suite and by the privileged family both | `process_linux.go:109`, `:174` | killed | killed |
| Checked by the integration suite only: the unprivileged userns path, which a privileged run does not take | `dumpable_linux.go:44`, `process_linux.go:201` | killed | survives |
| No test asserted it at all: nothing anywhere checked that `Request.Stdin` reaches the child — closed by #606, and by the package that defines it | `process_linux.go:171` | killed | not reached |
| Inside `if euid == 0`, which only a privileged run enters | `process_linux.go:154`, `:323` | survives | killed |
| Behind `requireRoot`, so the test naming the behaviour skipped until a privileged run existed | `process_linux.go:178` | survives | killed |
| Killed by the environment rather than by the privilege: `LANG` is unset in the privileged job's image, so the mutant leaves it empty and the minimal-basics case fails. On a host where `LANG` is set the same mutant survives both commands | `childenv.go:15` | survives | killed |
| The test asserts presence and non-emptiness, so the fallback value passes too | `childenv.go:11` | survives | survives |
| Log-only: the mutant inverts which euid gets a warning | `process_linux.go:86` | survives | survives |
| The case runs under privilege now and the mutant still survives it: with the boundary loosened a zero timeout yields an immediately-expired context, so `TestProcessExecutor_DefaultTimeoutRejectsZero` still gets the error it asserts. It asserts *an* error, not the refusal | `process_linux.go:116` | survives | survives |
| Unobservable: the mutant rewrites the capacity argument of a zero-length `make` | `limits.go:48` (two mutants) | survives | survives |
| An untested boundary on an argument shape no caller builds: the shim verb with no arguments, and a program whose `Args` is non-nil and empty, which the Exec RPC cannot produce (`internal/workspace/server/server.go` passes `req.GetArgs()`, nil when the field is empty) | `limits.go:28`, `:65` | survives | survives |

So of this package's 16: **9 are killed by one command or the other, 2 are equivalent mutants no test can kill, 2 sit on a boundary no shipped caller reaches, and 3 are gaps that remain** — one asserting presence where the value is what matters, one on a warning nothing reads, and one, `:116`, asserting *an* error where the refusal is the point. That last is the sharpest of the three and it only became visible once its case ran.

The two commands are not nested. Four of the kills need the integration suite, four need privilege, two are in both, and one needed neither — so **neither command alone measures this package**, which is the shape of the finding one level up from the one this survey started with.

`internal/tool`'s 13, by inspection only:

- **9 in `internal/tool/tooltest/`**, a test-helper package. A mutant there weakens a guard other packages' tests call; it is not shipped behaviour.
- **4 in `internal/tool/manifest.go`**, all on the two `if i > 0` comma conditions of the manifest canonicalizer. Each changes the canonical bytes and therefore every manifest hash, which is observable only against a literal expected hash — and no test pins one. The consequence is that a change to the canonicalizer silently invalidates every operator's pin set.

`internal/memory`'s 17, by inspection only:

- **7 in `internal/memory/ranking.go`**, every one of them, with 0 killed in that file: no test pinned the eviction score's ordering. `e1ff9cf` has since deleted the file, and eviction is ordered by last read instead.
- **6 in `internal/memory/store.go`** — four on the eviction sort's comparisons, two on a `make` capacity argument, the shape shown unkillable above.
- **3 in `internal/memory/retain.go`**, all boundary mutants on the retention thresholds: the tests use values away from the boundary.
- **1 in `internal/memory/sqlite/sqlite.go`**, where the mutant drops the archived IDs a supersession returns.

## What was filed and what was not

Filed, one per cause, each carrying the mutant and the green run:

- #604 — the privileged half of `internal/workspace/shell`'s tests never executed: the ADR 0008 drop, the ADR 0010 unshare, and five gremlins survivors in the same region. **Closed**; `M4` and `M6` are killed by `make test-privileged`. It found that the family had never run anywhere, not merely that CI did not run it: four of its cases failed under root for reasons unrelated to the boundary, and one production defect — a timeout that signalled the child alone and left the caller waiting out the whole command — surfaced with them.
- #605 — duplicate tool names are accepted and nothing catches it. **Closed**; `M10` is killed by `internal/tool`'s own tests.
- #606 — no test asserts `Request.Stdin` reaches the child. **Closed**; `process:171` is killed by `internal/workspace/shell`'s own tests.

Two of the three had an absent assertion for their cause, and a test closed them. #604's cause was a test that named the behaviour and never ran, so closing it took a second command and everything that had been hiding behind never being run.

Not filed, with the reason:

- **The two `make`-capacity mutants.** Equivalent: the slice's length and contents are unchanged for every argv the executor builds. An equivalent mutant is not a test gap.
- **The two `limits.go` boundaries.** Neither shape reaches the executor from the Exec RPC, which is the only caller that supplies `Args`. A test for an input no caller can send buys nothing.
- **The 4 killed by the integration suite and the 4 killed by the privileged command.** The behaviour is checked; where the check lives is a placement question, not a hole.
- **`internal/memory`'s 17.** Not re-run, and the domain is being replaced — #586's own constraint is that a mutant list against code being replaced is wasted.
- **`internal/tool`'s 13.** Not re-run. The `tooltest` 9 are not shipped behaviour; the canonicalizer 4 are real and belong with whoever next touches the pin set.
- **`childenv.go:11`, and `process_linux.go:86` and `:116`.** Each is one assertion rather than an unchecked boundary. `TestChildEnv_MinimalBasics` asserts the child env holds exactly `HOME`, `LANG` and `PATH` with non-empty values, which the built-in fallback satisfies as well as the inherited value does — `:15` is the same assertion and is killed only where `LANG` happens to be unset. `:86` is a warning nothing reads. `:116` is the sharpest of the three now that its case runs: `TestProcessExecutor_DefaultTimeoutRejectsZero` asserts an error rather than the refusal, and both the boundary and its mutant produce one. Listed here so the next reader has them; raising them as issues is the PM's call.

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

The privileged command's answers come from the same two scripts with the suite
command replaced, and mutant ids narrow the run to the boundary in question:

```
GAGENT_MUTATE_SUITE_CMD='<a privileged make test-privileged>' \
  python3 hand_mutants.py M4 M6
```

Since #620 `make test-privileged` narrows itself to the capability set the deployment grants the workspace, read from `deploy/local/docker-compose.yml`, so a boundary cannot be checked under more privilege than it ships with. Every privileged figure in this record was taken before that and under a wider set — the runtime's default plus `SYS_ADMIN` — which is what the runs named in §What ran say.

`make test-privileged` needs root and CAP_SYS_ADMIN. The privileged job gets
both from its container; a contributor with neither cannot run it, and the
target says so rather than running a family that would skip. `results/` holds
the command each figure came from.

Both harness scripts rewrite tracked files under `internal/` and restore each with `git checkout --` before the next, so they refuse to start on a dirty tree and print the tree state at the end. `harness/mutate.py` holds the runner both use; the mutants themselves are the tables in the other two files. A mutant whose anchor is absent, or present more than once, is reported and skipped — so a moved line surfaces as a miss instead of mutating a different site.

Results are the six files under `results/`: `gremlins.txt` and `gremlins-mistimed.txt` are the corrected and the mis-timed automated runs, `hand-mutants.txt` is the deletion rounds, `survivor-retest.txt` is `internal/workspace/shell`'s 16 survivors against the whole suite, and `privileged-check.txt` and `privileged-retest.txt` are the two boundaries and those same 16 against the privileged command.

The figures above are pinned to the trees named in §What ran, and a line number in a file that has moved since names the site in the tree that was measured. `internal/workspace/shell/` was untouched from `949ac99` until #604, which is what lets the survey's sites in it stand as written; #604's own figures were taken after its changes, so the two sets are each pinned to their own tree. `internal/tool/manifest.go`, `internal/tool/registry.go`, `internal/tool/schema.go` and `internal/memory/retain.go` have all changed since the survey (#595, #599), so the sites in them are named by their guard rather than by a line wherever the guard is what matters. Every deletion the harness applies is anchored on the code's own text, so a moved guard surfaces as a reported miss and never as a mutation of the wrong site.
