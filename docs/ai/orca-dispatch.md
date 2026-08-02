# Orca Dispatch Guide

How the PM dispatches workers into orca worktrees. Orca gives each task an isolated worktree, a persistent terminal the PM can watch, and a link to its issue. Applies only when the environment provides the orca CLI. On Linux the executable may be `orca-ide` — bare `orca` can resolve to the GNOME screen reader; check `which orca` first.

## Contents

- Never bypass Orca tracking
- Binding the role
- Lightweight mode
- Supervised mode
- Recovery
- Follow-up rounds
- Tracking and completion
- Anti-patterns
- Limits

## Never bypass Orca tracking

All agent processes are launched through the orca CLI. No `nohup`, detached spawns, or background subprocesses — an agent orca cannot see reports no status, appears as an idle worktree, and its death is invisible.

## Binding the role

Orca's `--agent <id>` selects the *tool* (`claude`, `opencode`, `codex`, `cursor`, `claude-teams`), not the project role. In the modes below the role travels in the brief: "Work GitHub issue #N as the worker — follow `docs/ai/worker-guide.md` and `docs/convention/`." A worker that reports the wrong role — or none — was not briefed correctly; fix the brief, not the worktree.

## Lightweight mode (verified)

For independent tasks where the PR is the completion signal.

```bash
orca worktree create --name <task> --no-parent \
  --agent <id> --prompt "<self-contained brief>" --json
# capture agentTerminalHandle (or startupTerminal.handle)
orca terminal wait --terminal <handle> --for tui-idle --timeout-ms 60000 --json
orca terminal read --terminal <handle> --json
```

- Always pass `--agent` and `--prompt` together — an agent without a prompt idles forever.
- The brief is self-contained: issue number, role instructions, constraints. The worker reads the issue itself; keep the brief short.
- **A dispatch is not done until `terminal read` shows the agent working.** "Worktree created" is not "work started."

## Supervised mode (verified)

For work the PM must track to completion. Uses orca orchestration: a Run inbox where workers report `worker_done`.

```bash
orca orchestration run-create --objective "<objective>" --json
orca orchestration task-create --spec "<self-contained spec>" --json
orca worktree create --name <task> --no-parent --agent <id> --json
orca terminal wait --terminal <handle> --for tui-idle --timeout-ms 60000 --json
orca orchestration dispatch --task <task_id> --to <handle> --inject --json
# completion arrives in the Run inbox:
orca orchestration check --wait --types worker_done,escalation,question --timeout-ms 900000 --json
```

- The readiness wait before `dispatch --inject` is mandatory. Dispatching before the TUI is idle silently drops the injected preamble — the worker then sits at an empty prompt while the dispatch claims `input_accepted`.
- Prefer this low-level sequence over one-shot `worker-start`: worker-start was observed racing agent readiness (opencode) and losing the injected input. If you use worker-start anyway, verify input arrival with `worker-read` immediately.
- `check --wait` timing out is a checkpoint, not a failure — coding tasks routinely run 15–60 minutes; keep waiting.

## Recovery

- **Idle worker after dispatch**: `worker-read --dispatch <id>` / `terminal read`. Preamble missing → re-run `dispatch --task <id> --to <handle> --inject` (after a tui-idle wait). Do not create a second worktree or terminal.
- **Manual prompt instead of inject**: a worker prompted by plain `terminal send` cannot settle the dispatch — its `worker_done` is rejected (`dispatch_capability_invalid`). The report still lands in the Run inbox, but the task stays unsettled. If you must prompt manually, treat the work as lightweight-mode and close the task with `task-update`.
- **Stale handle**: reacquire with `terminal list --worktree <selector>`. One handle at a time; never dual-send.

## Follow-up rounds

Work is iterative: worker completes → PM reviews → PM comments on the issue or PR → worker picks up the comment and continues. Design for rounds, not one-shots.

- **Keep the worker alive between rounds.** Do not remove the worktree or close the agent terminal until the task is fully done — the live session carries context.
- **Supervised mode**: create a new task and re-inject into the same terminal: `task-create --spec "..." --json`, then `dispatch --task <new_id> --to <same_handle> --inject`. Each round gets a fresh dispatch with valid `worker_done` authority.
- **Lightweight mode**: `terminal send --terminal <same_handle> --text "<follow-up>" --enter` — same session, but untracked.
- **Instructions live on GitHub, not in the terminal.** The PM's direction for each round is an issue/PR comment (`gh issue comment <n>`); the follow-up prompt just points at it ("read the latest comment on issue #N and apply it"). If the session dies, a fresh worker can pick up from the comment thread with zero lost context.

## Tracking and completion

- PM sweep on every wake: `orca orchestration check --json` (unread Run mail) + `orca worktree ps --json` (stuck or in-review worktrees).
- Worker-side status reporting (card updates, `worker_done`): `worker-guide.md` §Reporting status.
- After the worker's PR is merged: `orca worktree rm --worktree path:<worktree-path>` and delete the task branch. A worktree listed as in-progress whose terminal shows an exited process is a stale dispatch, not active work — remove it.

## Anti-patterns

- Detached/background agent spawns outside Orca (see §Never bypass Orca tracking).
- Bare `worktree create` then `terminal create --command <agent>` — leaves a fallback shell and invites lost input; use `--agent`.
- `terminal send` or `dispatch --inject` before the TUI is idle — input is lost silently.
- Retrying a failed dispatch by creating a new worktree instead of re-injecting into the existing one.

## Limits

- Do not hardcode absolute worktree paths in procedures; discover them with `orca worktree list` and address worktrees by selector (`name:`, `path:`, `id:`, `active`).
- A worker launched this way is still a worker: it obeys `worker-guide.md` and may not merge, manage issues, or edit `docs/convention/`, `docs/ai/`, or agent configuration.
