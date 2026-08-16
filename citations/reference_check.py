"""Checks that every repository path and ADR a document cites actually resolves.

A document names a file, a directory, or an architecture record, and the thing is
not there. That defect arrives regularly, is invisible to the claim registers
under `docs/experiments/`, which check figures rather than paths, and is uniform
enough to find in one pass over the tree rather than per document.

What counts as a citation is deliberately narrow, because a check that reports
prose as a dead path is a check somebody turns off. Two forms are read: a
Markdown inline-code span or link target, which is how `documentation.md` §Format
says a path is written here, and the same plus bare tokens inside a `#` or `//`
comment. A token is then a repository path only when it carries a directory
separator and is either rooted — its first segment names something at the
repository root, so `tools/playwright` is a claim about this tree and `read/write`
is not — or local, resolving against the citing file's own directory or
any directory above it, which is how a record cites its sibling and how an
experiment cites a path from its own root.

Everything else is excluded on purpose. Each exclusion below costs coverage, and
the cost is stated rather than left implicit:

  - A token that is neither rooted nor local. That is what leaves `read/write`
    and `and/or` alone, and what leaves a bare `manifest.json` or `memory.db`
    alone: those name a kind of file rather than a path, and nothing in the
    syntax separates a kind from a sibling. Cost: a citation whose first segment
    is dead too, and a dead bare filename, are both missed.
  - A string literal in code. A literal is an instruction to a runtime, resolved
    against a working directory this cannot know, and often names a file the
    program is about to create. Cost: a script writing into a directory that is
    not in the tree is missed, which is the shape of #400.
  - A token whose extension is not one this repository stores. That is how a Go
    package-qualified symbol in a comment — `internal/agent/openai.DefaultTimeout`
    — stays out. Cost: a dead path with an unusual extension is missed.
  - A first segment carrying an interior dot, which is a hostname or a schema
    namespace (`gagent.local/schema/tool-input.json`) rather than a directory.
  - `docs/convention/stack-go.md`, the one convention file that illustrates with
    paths rooted in this tree's own top-level names — `internal/infra/` and
    `internal/kit/` are names it exists to forbid, `internal/user/user.go` and
    `internal/logger/` are shapes it is describing. Other convention files
    illustrate with foreign roots (`src/auth/service.ts`), which nothing above
    admits, so they are checked. Cost: 3 live citations inside `stack-go.md`
    go unchecked, naming 2 distinct paths, `internal/` and `tests/`; a dead
    reference among them is missed.

Reads tracked files only, resolves against the working tree, and touches no
network. Exits non-zero when a citation does not resolve. Takes the tree to check
as an optional argument, which is how the fixtures point it at a reconstruction.
"""
from __future__ import annotations
import os
import re
import subprocess
import sys

# Extensions this repository stores. A token ending in anything else is not a
# path here, which is what keeps Go package-qualified symbols out.
EXTENSIONS = {".md", ".go", ".py", ".yml", ".yaml", ".json", ".sh", ".mod",
              ".sum", ".proto", ".db", ".txt", ".toml", ".lock"}

# Files whose comments are read. Markdown is read whole; everything else only
# inside a comment, for the reason the docstring gives about string literals.
COMMENT_PREFIXES = {".go": "//", ".py": "#", ".sh": "#", ".yml": "#",
                    ".yaml": "#", ".mod": "//"}
COMMENT_FILENAMES = {"Dockerfile": "#", "Makefile": "#", ".dockerignore": "#",
                     ".gitignore": "#"}

# A citation may hold these characters and nothing else. Anything wider is prose,
# a glob, a placeholder, a shell expansion or a URL.
TOKEN = re.compile(r"^[A-Za-z0-9._/-]+$")

CODE_SPAN = re.compile(r"`([^`\n]+)`")
LINK_TARGET = re.compile(r"\[[^\]]*\]\(([^)\s]+)\)")
BARE_TOKEN = re.compile(r"(?<![`\w/.-])([A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)+/?)")
ADR_REFERENCE = re.compile(r"\bADR[\s-]?(\d{4})\b")

# Excluded for the reason the docstring gives.
EXCLUDED_FILE = "docs/convention/stack-go.md"


def tracked(root):
    """Every file git tracks, repository-relative."""
    out = subprocess.run(["git", "-C", root, "ls-files"],
                         capture_output=True, text=True, check=True)
    return [p for p in out.stdout.splitlines() if p]


def comment_body(path, line):
    """The comment part of one line, or None when the line carries no comment.

    The marker is found outside quotes, so a `#` inside a string and the `//` of
    a URL are not mistaken for one.
    """
    marker = COMMENT_FILENAMES.get(os.path.basename(path))
    if marker is None:
        marker = COMMENT_PREFIXES.get(os.path.splitext(path)[1])
    if marker is None:
        return None
    quote = None
    for i, char in enumerate(line):
        if quote:
            if char == quote and line[i - 1:i] != "\\":
                quote = None
        elif char in "\"'`":
            quote = char
        elif line.startswith(marker, i):
            return line[i + len(marker):]
    return None


def citations(path, text):
    """(token, line number) for every path-shaped citation in one file.

    Markdown is read whole. Every other file is read one comment at a time, and
    bare tokens count there because a comment writes `deploy/local` without
    backticks as often as with them.
    """
    markdown = path.endswith(".md")
    for number, line in enumerate(text.splitlines(), 1):
        body = line if markdown else comment_body(path, line)
        if body is None:
            continue
        for token in CODE_SPAN.findall(body) + LINK_TARGET.findall(body):
            yield token, number
        if not markdown:
            stripped = CODE_SPAN.sub(" ", body)
            for token in BARE_TOKEN.findall(stripped):
                yield token, number


def clean(token):
    """The token without the sentence punctuation a comment leaves attached."""
    return token.rstrip(".,;:")


def is_repository_path(token, citing_dir, root, top_level):
    """Whether a token claims something about this tree, and so must resolve.

    Rooted or local. The docstring states what that leaves out and what it costs.
    """
    if not TOKEN.match(token) or "//" in token or token.startswith("-"):
        return False
    body = token.rstrip("/")
    extension = os.path.splitext(body)[1].lower()
    if extension and extension not in EXTENSIONS:
        return False
    first = body.split("/")[0]
    if "." in first.lstrip("."):
        return False
    return first in top_level or resolves(body, citing_dir, root)


def bases(citing_dir):
    """The directories a citation may be written against: the file's own, each
    directory above it, and the repository root.

    An experiment record cites `harness/round4/` from a file two levels inside
    the experiment, because its own instructions start by entering the experiment
    root. That is a citation against an ancestor, not a dead path.
    """
    parts = citing_dir.split("/") if citing_dir else []
    return [os.path.join(*parts[:n]) if n else "" for n in range(len(parts), -1, -1)]


def resolves(token, citing_dir, root):
    """Whether the token names something in the tree, from any of those bases."""
    cleaned = token.rstrip("/")
    return any(os.path.exists(os.path.join(root, base, cleaned))
               for base in bases(citing_dir))


def adr_numbers(root):
    directory = os.path.join(root, "docs", "architecture", "adr")
    if not os.path.isdir(directory):
        return set()
    return {name[:4] for name in os.listdir(directory) if name[:4].isdigit()}


def check(root):
    """Every citation in the tracked tree that does not resolve."""
    top_level = {p.split("/")[0] for p in tracked(root)}
    adrs = adr_numbers(root)
    readable = {".md"} | set(COMMENT_PREFIXES)
    dead, seen = [], 0

    for path in tracked(root):
        name, extension = os.path.basename(path), os.path.splitext(path)[1]
        if extension not in readable and name not in COMMENT_FILENAMES:
            continue
        if path == EXCLUDED_FILE:
            continue
        try:
            text = open(os.path.join(root, path), encoding="utf-8").read()
        except (UnicodeDecodeError, FileNotFoundError):
            continue
        citing_dir = os.path.dirname(path)

        for raw, line in citations(path, text):
            token = clean(raw)
            if not is_repository_path(token, citing_dir, root, top_level):
                continue
            seen += 1
            if not resolves(token, citing_dir, root):
                dead.append((path, line, token, "no such path in the tree"))

        for number, source_line in enumerate(text.splitlines(), 1):
            body = source_line if path.endswith(".md") else comment_body(path, source_line)
            if body is None:
                continue
            for adr in ADR_REFERENCE.findall(body):
                seen += 1
                if adr not in adrs:
                    dead.append((path, number, f"ADR {adr}",
                                 "no such record in docs/architecture/adr/"))

    return seen, dead


def main():
    root = sys.argv[1] if len(sys.argv) > 1 else os.path.join(
        os.path.dirname(os.path.abspath(__file__)), "..")
    root = os.path.abspath(root)
    seen, dead = check(root)

    print(f"{seen} citations checked in {os.path.basename(root)}")
    if not dead:
        print("every cited path and record resolves")
        return 0
    print(f"\n{len(dead)} do not resolve:\n")
    for path, line, token, why in dead:
        print(f"  {path}:{line}: {token} — {why}")
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
