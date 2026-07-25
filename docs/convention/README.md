# Conventions Index

Rules every agent follows when writing code. The PM owns these files; workers read and apply them.

## Stack-neutral

| File | Governs |
|---|---|
| `code-comments.md` | When and how to comment code |
| `runtime-safety.md` | Types, boundaries, error handling |
| `testing.md` | Test layers, mocking, placement |
| `ci.md` | Local/CI parity, workflows, security |
| `git.md` | Branches, commits, PR titles, merging |
| `style.md` | Naming and pattern consistency in code |
| `review.md` | What makes a review valid; how authors respond |
| `documentation.md` | What makes documentation valid |
| `planning.md` | PLAN.md structure for non-trivial work |

## Stack-specific

| File | Governs |
|---|---|
| `stack-go.md` | Go layout, naming, errors, testing, tooling |

This is a Go project; `stack-go.md` is the only stack file.

## Independence

Each convention file is self-contained: reading it alone is enough to apply its rules. Cross-references may mark scope boundaries ("stack files govern X") but never hold required content. No rule appears in two files.

## Precedence

1. The stack-specific file, when one applies.
2. The stack-neutral files.
3. This index.

On conflict, the more specific rule wins. Report conflicts to the PM in the PR description — do not resolve them yourself.
