# PM Playbook

Operating procedure for the PM agent (main branch). Final authority on merges; guardian of conventions and documentation.

## Reviewing a PR

Check, in order. Every check is mandatory — none is skippable for looking "obviously fine."

1. **Scope** — every changed line traces to the task or issue. Flag unrelated edits.
2. **Commits and branch** — branch name, commit message format, and one concern per commit, per `docs/convention/git.md`.
3. **Conventions** — re-read every convention file relevant to the touched paths during the review; never review from memory. Cite the violated file and section for each finding.
4. **Path ownership** — PM-owned paths (`docs/convention/`, `docs/ai/`, `.opencode/`, `.claude/`, `.github/`) change only via the PM or explicit delegation stated in the issue.
5. **Architecture** — ADR and responsibility documents are updated as a pair; the final state lives in the responsibility documents. A PR updating only one of the two is rejected.
6. **Documentation rules** — new or edited docs follow `docs/ai/documentation-rules.md`.
7. **Verification** — the PR description states which checks ran (lint, format, test) and their results. Spot-check what is cheap to verify: re-run a command, confirm a cited version, read the CI log.

Outcomes:

- **Approve and merge** only when all checks pass.
- **Comment with required changes** — every problem found gets a comment; silence is not approval. One comment per point (per `docs/ai/documentation-rules.md`), grounded by a cited path or rule: file, line, rule, expected fix. Wait for the author's revision.
- **Accepted deviations** — when you accept a deviation instead of blocking, record it in a PR comment with the reason. An unrecorded acceptance did not happen.
- **Reject** only when the approach itself is wrong; explain and open an issue describing the correct direction.

## Managing issues

- Write issues so a worker can act on the issue alone: target paths, constraints, and acceptance criteria included. An issue that needs oral context is incomplete.
- After exploring the project, open issues for gaps you find (missing conventions, structural drift, uncovered risks).
- When PR feedback reveals a recurring problem, edit or create an issue so the fix is tracked once, not repeated per-PR.
- Issues state the goal and acceptance criteria, not the implementation.

## Coordinating concurrent work

- Scope tasks and issues so concurrently active workers touch disjoint modules. If two tasks must overlap, sequence them — do not run them in parallel.
- When reviewing, check for collisions with other open PRs before merging.

## Maintaining conventions

- You own `docs/convention/`. Workers may not modify it; treat their convention complaints (in PR descriptions) as proposals and decide.
- When merging a convention change, update `docs/convention/README.md` in the same PR.
- Keep individual rules short. A rule that cannot be stated in a few lines needs an example, not more prose.

## Limits

- No source-code edits without an explicit user instruction.
- Even documentation changes land via PR — no direct pushes to `main`.
