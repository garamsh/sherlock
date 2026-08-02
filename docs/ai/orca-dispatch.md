# Orca Dispatch Guide

How the PM creates orca worktrees and dispatches role-bound agents into them. Orca gives each task an isolated worktree, a persistent terminal the PM can watch, and a link to its issue.

## Contents

- When to use
- Prerequisites
- Binding the role
- Running unattended
- Dispatching a worker
- Watching a worker
- Reviving an idle worker
- After the PR
- Parallel work
- Anti-patterns
- Limits

## When to use

Use orca to run a worker on an issue as a persistent, observable process rather than an ephemeral in-session helper. The worktree survives the PM session, stays visible in the Orca app, and is tied to its issue. Prefer it whenever the PM assigns implementation work (see `pm-guide.md` §Managing issues).

Dispatch is gated on a human. The PM opens and refines the issue on its own, but creates no worktree and launches no worker until a human confirms the issue. This gate keeps a human in control of what work actually starts.

## Prerequisites

- The orca CLI is installed and the project is registered as an orca repo (`orca repo list`). On Linux the executable may be `orca-ide` — bare `orca` can resolve to the GNOME screen reader; check `which orca` first.
- The agent tool the worker will run is installed (`which claude`, `which opencode`). Only installed tools can be launched.
- `--help` output is the authoritative command schema; orca commands evolve and an unknown subcommand falls through to launching the desktop app. Consult it rather than trusting this guide's examples verbatim.

## Binding the role

Orca's own `--agent <id>` selects the *tool* (`claude`, `opencode`, `codex`, `cursor`, `claude-teams`), not the project role. Launching with it alone produces a role-less agent. Bind the role with the tool's own `--agent <role>` flag, which loads the matching role definition.

| Tool | Role-bound launch command | Role definition |
|---|---|---|
| Claude Code | `claude --agent <role>` | `.claude/agents/<role>.md` |
| opencode | `opencode --agent <role>` | `.opencode/agents/<role>.md` |

Roles are `pm`, `worker`, `qa`. For Claude, `--settings '{"outputStyle":"<Name>"}'` (names live in `.claude/output-styles/`) is an equivalent way to lock the same role. A launched agent that reports the wrong role — or none — was not bound correctly; fix the flag before sending work.

## Running unattended

A dispatched worker runs with no human at its terminal, so launch it headless: one command that carries the task, binds the worker role, bypasses permission prompts, runs to completion, and exits. Do not launch an interactive session and then send the prompt as a second step — the extra step is fragile and is the usual cause of a worker that never starts.

Orca has no autonomy or headless flag of its own; its `--agent` only selects the tool. All of it comes from the tool's own flags, passed through `orca terminal create --command`:

| Tool | Headless, role-bound, autonomous launch |
|---|---|
| Claude Code | `claude --agent worker --permission-mode bypassPermissions -p '<prompt>'` |
| opencode | `opencode run --agent worker --auto '<prompt>'` |

Keep `<prompt>` free of single quotes (the whole command is already single-quoted inside `--command`). Permission bypass is acceptable only because the worker runs in an isolated worktree on a task branch, never on `main`.

## Dispatching a worker

1. Open the issue with goal and acceptance criteria (`pm-guide.md` §Managing issues). The worker acts from the issue alone.
2. Wait for a human to confirm the issue. Do not create a worktree or launch a worker before confirmation.
3. Create the worktree in orca, linked to the confirmed issue and based on `main`:
   `orca worktree create --repo name:<repo> --name <slug> --base-branch main --issue <N> --json`.
   Pass `--repo` explicitly (`name:<repo>` or `id:<id>`); do not pass orca's `--agent` here (it selects the tool, not the role — see §Binding the role). Read the worktree path from the result.
4. Launch through a wrapper script, not an inline command. `terminal create --command` passes through a shell, and an inline command with quotes, parentheses, or backticks arrives mangled — the classic dispatch that never starts. A single-token `--command` survives every layer:
   - write `/tmp/opencode/dispatch-<N>-prompt.txt` with the prompt (keep it short — name the issue and point to the guides; the worker reads the issue itself), then
   - write `/tmp/opencode/dispatch-<N>.sh`:
     `#!/bin/sh` / `cd <worktree-path>` / `exec opencode run --agent worker --auto "$(cat /tmp/opencode/dispatch-<N>-prompt.txt)"`
   - `orca terminal create --worktree path:<worktree-path> --command "sh /tmp/opencode/dispatch-<N>.sh" --json` — capture the handle.
5. Verify the dispatch by reading the terminal (§Watching a worker): the agent is working, not idling at an empty prompt. "Worktree created" is not "work started."

The worker reads the issue, works on its branch, opens a PR, and exits; it does not create another branch or worktree.

Fallbacks, in order, when step 4 fails:

- **Orca-native one-shot**: `orca worktree create --name <task> --no-parent --agent <id> --prompt "<brief>" --json` in step 3 launches the tool with the brief in the worktree's first terminal (`result.startupTerminal.handle` is the agent handle). The brief must be self-contained — task, constraints, paths. Remember this agent is role-less unless the tool was configured otherwise; use it only when role binding does not matter.
- **Detached spawn** (last resort): when orca terminal tracking itself is unusable, spawn the tool detached in the worktree — e.g. Python `subprocess.Popen(..., start_new_session=True)` with stdout to a log file — and poll the log and `gh pr list`. The worker is then invisible to the Orca app: the worktree shows no agent and its status never updates, so it looks idle. Remove the worktree the moment the PR merges; never leave a detached-spawn worktree behind.

## Watching a worker

- `orca terminal wait --terminal <handle> --for exit` — block until the headless worker finishes. Its exit ends the dispatch.
- `orca terminal read --terminal <handle> [--cursor <n>]` — read output, including the final PR URL. A headless worker prints little until it finishes; pass the previous `nextCursor` for incremental reads. Read the raw text output, not `--json`, when you just want to eyeball progress.
- To steer a worker instead of running it to completion, launch it interactively (omit `-p`) and drive it with `orca terminal send --terminal <handle> --text "<message>" [--interrupt] --enter`.

## Reviving an idle worker

1. `terminal read` — confirm whether the prompt text appears in the terminal.
2. Before any `terminal send`, wait for `orca terminal wait --terminal <handle> --for tui-idle` — input sent before the TUI is ready is lost.
3. Prompt missing or agent waiting at an empty prompt: `terminal send --terminal <handle> --text "<brief>" --enter`.
4. Never create a second terminal or worktree to "retry" — fix the delivery in the existing handle. If the handle reports `terminal_handle_stale`, reacquire with `terminal list --worktree <selector>`.

## After the PR

Review and merge per `pm-guide.md`. Branch hygiene extends to orca: after merging, remove the worktree with `orca worktree rm --worktree path:<worktree-path>` and delete the task branch. Never remove a worktree whose worker is still running or whose PR is open (`pm-guide.md` §Branch hygiene). A worktree listed as in-progress whose terminal shows an exited process is a stale dispatch, not active work — remove it.

## Parallel work

For a task that benefits from parallel decomposition, `orca claude-teams` runs Claude Code with native team panes (must be started inside an orca terminal). Keep concurrently active workers on disjoint modules (`pm-guide.md` §Coordinating concurrent work).

## Anti-patterns

- Bare `worktree create` then `terminal create --command <agent>` without verifying the terminal shows the agent working — leaves a fallback shell and invites lost input.
- `terminal send` before the TUI is ready — input is lost; wait for `tui-idle` first.
- Sending to both an old and a replacement handle — one handle at a time.
- Creating a worktree per attempt when delivery failed — diagnose the existing one.

## Limits

- Do not hardcode absolute worktree paths in procedures; discover them with `orca worktree list` and address worktrees by selector (`name:`, `path:`, `id:`, `active`).
- A worker launched this way is still a worker: it obeys `worker-guide.md` and may not merge, manage issues, or edit `docs/convention/`, `docs/ai/`, or agent configuration.
