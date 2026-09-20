# Conventions Index

Rules every agent follows when writing code, tests, commits, and documents. The PM owns these files; workers read and apply them.

## Contents
- Scope
- Stack-neutral
- Stack-specific
- Independence
- Precedence

## Scope

These rules bind what the project writes. Committed output a generator produced is not that — the project owns the input and the decision to commit it, not the output's internals.

Where a tool decides an artifact's shape, that shape is an interface and not prose: a comment a tool writes into or parses, a template a forge copies into every issue and pull request opened from it, a file a runtime includes. What such an artifact says is the project's and is bound by these rules. What it must open with, and the order its parts come in, is the tool's.

What makes anything generated is that regenerating it changes nothing. What nobody can regenerate is authored, whatever its header says. The test runs on what a generator writes and not on the file holding it, so one file can be generated in one part and authored in the rest.

## Stack-neutral

| File | Governs |
|---|---|
| `code-comments.md` | When and how to comment code |
| `runtime-safety.md` | Types, trust boundaries, error handling |
| `testing.md` | Test layers, mocking, placement |
| `ci.md` | Local/CI parity, pipeline authoring, security |
| `git.md` | Branches, commits, PR titles, merging |
| `style.md` | Naming and pattern consistency in code |
| `simplicity.md` | Whether to add or to change the shape; when an abstraction earns its place |
| `review.md` | What a review must carry to be valid, and how an author responds |
| `documentation.md` | What makes a document or a GitHub artifact valid, review comments included |

Every project keeps all of these. They are the floor, and bootstrap prunes only the table below.

Selection is not once. A change that alters the project's stack carries the selection with it — the file that now matches added, the one that no longer does dropped, in the same pull request. A worker whose change triggers a selection does not make it.

## Stack-specific

These apply only when the project uses that stack. The bootstrap process keeps the relevant ones and deletes the rest, removing the rows below for the files it deletes.

| File | Governs |
|---|---|
| `stack-container.md` | Container images the project builds |
| `stack-go.md` | Go modules and services |

A stack file states the concrete form of what a stack-neutral file governs: the test client and file placement behind `testing.md`, the comment syntax behind `code-comments.md`, the commands behind an entry-point name in `ci.md`. It never restates the rule itself. This table and the one above are where that split is recorded — the files do not point at each other.

## Independence

Each convention file is self-contained: reading it alone is enough to apply its rules.

- **One file, one territory.** No artifact is governed by two files. Where two files could both decide a case, one of them is holding the wrong rule.
- **A rule appears once** — inside a file as much as across files. A section that restates earlier rules in the negative is a second site to keep in sync, not a summary.
- **Two files' rules may share a reason.** That is not duplication. Each states its own reason; neither points at the other for it.
- **A convention file does not send the reader to another convention file.** Where one file's territory ends and the next begins is recorded in the tables above, not inside the files themselves.

## Precedence

1. The stack-specific file, when one applies.
2. The stack-neutral files.
3. This index.

On conflict, the more specific rule wins. Report conflicts to the PM in the PR description — do not resolve them yourself.

§Scope is not a tier in this ladder. It bounds what every tier reaches, so no rule at any rank reaches what §Scope places outside what the project writes.
