#!/usr/bin/env python3
"""Re-runs gremlins' `internal/workspace/shell` survivors against the whole suite.

gremlins gathers coverage and runs tests one package at a time, so LIVED in its
report means "this package's own tests do not kill it" and says nothing about the
rest of the suite. This reconstructs each of that package's 16 survivors from the
file, line and column its report names — gremlins prints the position of the
operator it rewrote — and runs each against the package and then the whole suite,
which is what separates a coverage-placement finding from an uncovered boundary.

Only this one package's survivors are here. The survivors in `internal/tool` and
`internal/memory` were left alone because other workers held those files in the
round this was written; the report says so rather than carrying a figure for them.
"""
from __future__ import annotations
from mutate import survey

SHELL = "./internal/workspace/shell/..."
CHILDENV = "internal/workspace/shell/childenv.go"
LIMITS = "internal/workspace/shell/limits.go"
DUMPABLE = "internal/workspace/shell/dumpable_linux.go"
PROCESS = "internal/workspace/shell/process_linux.go"

# id (the site gremlins names), file, package under test, the operator edit,
# the anchor, the replacement. Anchors carry surrounding lines wherever the
# mutated line's own text appears more than once in the file.
MUTANTS = [
    ("childenv:11", CHILDENV, SHELL,
     "CONDITIONALS_NEGATION: the PATH fallback fires when PATH is set",
     '\tif path == "" {\n', '\tif path != "" {\n'),
    ("childenv:15", CHILDENV, SHELL,
     "CONDITIONALS_NEGATION: the LANG fallback fires when LANG is set",
     '\tif lang == "" {\n', '\tif lang != "" {\n'),
    ("limits:28", LIMITS, SHELL,
     "CONDITIONALS_BOUNDARY: the shim verb is ignored at exactly two args",
     "\tif len(os.Args) < 2 || os.Args[1] != ShimCommand {\n",
     "\tif len(os.Args) <= 2 || os.Args[1] != ShimCommand {\n"),
    ("limits:48a", LIMITS, SHELL,
     "ARITHMETIC_BASE: the shim argv slice is preallocated smaller",
     "\tout := make([]string, 0, len(specs)+len(argv)+4)\n",
     "\tout := make([]string, 0, len(specs)-len(argv)+4)\n"),
    ("limits:48b", LIMITS, SHELL,
     "ARITHMETIC_BASE: the shim argv slice is preallocated smaller",
     "\tout := make([]string, 0, len(specs)+len(argv)+4)\n",
     "\tout := make([]string, 0, len(specs)+len(argv)-4)\n"),
    ("limits:65", LIMITS, SHELL,
     "CONDITIONALS_BOUNDARY: a program with one argv entry is rejected",
     "\t\t\tif len(rest) < 2 {\n", "\t\t\tif len(rest) <= 2 {\n"),
    ("dumpable:44", DUMPABLE, SHELL,
     "CONDITIONALS_NEGATION: the non-dumpable flag is never lifted for the fork",
     "\tif err == nil && dumpable == 0 {\n",
     "\tif err != nil && dumpable == 0 {\n"),
    ("process:86", PROCESS, SHELL,
     "CONDITIONALS_NEGATION: the missing-CAP_SYS_ADMIN warning inverts",
     "\tif !exec.netNS && os.Geteuid() == 0 {\n",
     "\tif !exec.netNS && os.Geteuid() != 0 {\n"),
    ("process:109", PROCESS, SHELL,
     "CONDITIONALS_NEGATION: the netns-uncapable refusal inverts",
     "\tif req.NoNetwork && euid == 0 && !e.netNS {\n",
     "\tif req.NoNetwork && euid != 0 && !e.netNS {\n"),
    ("process:116", PROCESS, SHELL,
     "CONDITIONALS_BOUNDARY: a zero timeout is accepted instead of refused",
     '\tif timeout <= 0 {\n\t\treturn Result{}, errors.New("shell: timeout is required")\n',
     '\tif timeout < 0 {\n\t\treturn Result{}, errors.New("shell: timeout is required")\n'),
    ("process:154", PROCESS, SHELL,
     "CONDITIONALS_NEGATION: the privileged netns unshare inverts",
     "\tif req.NoNetwork && euid == 0 {\n",
     "\tif req.NoNetwork && euid != 0 {\n"),
    ("process:171", PROCESS, SHELL,
     "CONDITIONALS_NEGATION: the request's stdin never reaches the child",
     "\tif req.Stdin != nil {\n", "\tif req.Stdin == nil {\n"),
    ("process:174", PROCESS, SHELL,
     "CONDITIONALS_NEGATION: the request's workdir is ignored",
     '\tif req.Workdir != "" {\n', '\tif req.Workdir == "" {\n'),
    ("process:178", PROCESS, SHELL,
     "CONDITIONALS_NEGATION: the request's env entries never reach the child",
     "\tif req.Env != nil {\n", "\tif req.Env == nil {\n"),
    ("process:201", PROCESS, SHELL,
     "CONDITIONALS_NEGATION: the userns spawn path is taken as root instead",
     "\tstart := cmd.Start\n\tif req.NoNetwork && euid != 0 {\n",
     "\tstart := cmd.Start\n\tif req.NoNetwork && euid == 0 {\n"),
    ("process:323", PROCESS, SHELL,
     "CONDITIONALS_NEGATION: netns capability is reported for the wrong euid",
     "\tif os.Geteuid() != 0 {\n\t\treturn false\n",
     "\tif os.Geteuid() == 0 {\n\t\treturn false\n"),
]

if __name__ == "__main__":
    survey(MUTANTS)
