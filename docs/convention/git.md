# Git Conventions

How branches, commits, and PR titles are written, and what survives a merge.

## Branches

- `feat/<short-name>` — new behavior
- `fix/<short-name>` — bug fixes
- `chore/<short-name>` — tooling, config, dependencies
- `docs/<short-name>` — documentation only
- `refactor/<short-name>` — behavior-preserving restructure
- `test/<short-name>` — test-only changes

One task, one branch. Branch from `dev`, which is the default branch and where
every task's pull request lands; never commit to `dev` or `main` directly.

`main` receives `dev` in batches, through a pull request a maintainer opens when
a batch is ready. Nothing else reaches it. The pipeline is scoped to `main`
(`.github/workflows/ci.yml`), so a task branch triggers no run and a batch costs
one — which is the reason for the split, and the reason promotion is not
automated: a promotion that fires per merge puts the cost straight back.

A host namespace in front of the prefix satisfies the rule as written: `fix/dockerignore-env` and `sauce-git/fix/dockerignore-env` both carry it. What is required is the type prefix on the branch's own name, wherever the host puts that name.

## Commits

Only the PR title reaches `dev`, together with the trailer naming the squashed commits' authors, and reaches `main` unchanged when `dev` is promoted. A branch's commit messages are otherwise a working record for the reviewer reading the branch, not the permanent history.

Format: `<type>: <imperative summary>`

- Types: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`
- Summary: imperative mood, lowercase, no trailing period. `fix: reject empty tokens`, not `fixed some bugs`.
- One concern per commit. If a change needs "and" to describe it, split it.
- Omit the body unless the reason for the change is invisible in the diff. A body never restates what changed.
- The message contains only what the project put there — no line a tool appended unasked. A `Co-authored-by:` naming an author of the squashed commits is not such a line: squash drops those authors and the trailer is where they survive.
- Reversing an earlier decision mid-branch is an ordinary commit describing the new state. Earlier commits are not rewritten to hide that the decision changed.
- A point about a commit message is fixed in the commit that carries it — amend or rebase, then force-push the same branch. A later commit cannot remove text from an earlier one, so it cannot carry that fix. This is the one case where rewriting a branch commit is right.

## PRs

- Title follows the commit format: `<type>: <imperative summary>`. It becomes the commit on `dev`, and reaches `main` with the promotion, so it is the permanent record of the change.
- Body follows `.github/PULL_REQUEST_TEMPLATE.md`.
- PRs are squash-merged. Branch commits appear on neither `dev` nor `main`; do not rewrite them to be pretty.
