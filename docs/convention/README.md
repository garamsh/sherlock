# Conventions Index

Rules every agent follows when writing code. The PM owns these files; workers read and apply them.

## Stack-neutral

| File | Governs |
|---|---|
| `code-comments.md` | When and how to comment code |
| `runtime-safety.md` | Types, boundaries, error handling |
| `testing.md` | Test layers, mocking, placement |
| `ci.md` | Local/CI parity, workflows, security |
| `git.md` | Branches, commits, PR titles |
| `planning.md` | PLAN.md structure for non-trivial work |

## Stack-specific

| File | Governs |
|---|---|
| `stack-go.md` | Go layout, naming, errors, testing, tooling |

This is a Go project; `stack-go.md` is the only stack file. It takes precedence over the stack-neutral files on conflict.

## Precedence

1. The stack-specific file, when one applies.
2. The stack-neutral files.
3. This index.

On conflict, the more specific rule wins. Report conflicts to the PM in the PR description — do not resolve them yourself.
