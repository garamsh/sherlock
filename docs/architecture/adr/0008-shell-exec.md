# ADR 0008: The `shell_exec` tool — sub-process in the unprivileged UID

> Status: accepted
> Date: 2026-07-30

## Context

The agent needs a general-purpose shell for real workloads: "run the project's test suite and report which tests fail", "find all files larger than a size and list them", "compute a value with awk / jq / sed instead of inline in the model", "run a one-off pipeline on a workspace file". Typed tools cover typed needs; the shell covers the rest.

## Decision

A `shell_exec` tool runs the agent-supplied command as a sub-process dropped to the unprivileged agent UID. In the pod topology (ADR 0018) it runs in the sidecar — the agent's computer; today it runs as a sub-process of the single platform binary.

Mechanism:

- The executor forks, sets the child to the agent UID, drops Linux capabilities, applies resource limits, and execs the command.
- The child has no network access — via a fresh, empty network namespace (a privileged unshare, or an unprivileged user+network namespace in single-account mode, ADR 0010); in the pod topology additionally via the pod network layer.
- The child cannot reach the agent's memory or the platform's runtime material — a different container in the pod topology, a different UID in the privileged drop; kernel DAC either way. In single-account mode this one axis intentionally does not hold — the child shares the sidecar's account; the remaining axes (no network, scrubbed environment, resource limits) are unchanged.
- The child has a configurable timeout; on expiry it is SIGKILLed and the result carries `timed_out: true`.
- When the platform can run the child as the configured account neither way, the tool reports `agent_uid unavailable` instead of silently upgrading.

Threat model (honest):

- The shell can read everything under the workspace (this is its purpose) and can leak workspace contents through covert channels; containers do not fix that.
- The shell **cannot** read secrets (ADR 0003), modify the tool runtime, or make network requests — enforced structurally.

What the shell is NOT:

- Not a general-purpose programming environment — typed tools first; the shell is for what the typed surface does not cover.
- Not for questions the model can answer directly.

## Consequences

- The shell is the only tool that runs arbitrary commands; the others remain typed and narrow.
- The `internal/shell/` package holds the executor; the Executor interface is the only thing the tool calls, so added hardening (chroot, seccomp, more rlimits) is a swap, not a rewrite.
- Two deployment modes run real workloads. The privileged drop: a sidecar with UID-switch privilege (setuid-root binary, supervisor, or container capabilities) runs children as the configured `exec-uid`. The single-account computer: `exec-uid` set to the sidecar's own UID needs no privilege at all — the operator declares the account explicitly; there is no implicit fallback to the current UID. An unprivileged sidecar with any other `exec-uid` reports `agent_uid unavailable`.

## Rejected alternatives

- **Typed compute tools (`jq_query`, `python_eval`)** — narrow but every new use case needs a new tool; the shell is the general answer.
- **Keep no shell** — simplest, but the workloads that need a shell are real.
- **A platform-spawned sandbox container** — the sidecar (ADR 0018) is a peer environment container, not a runtime-spawned sandbox tier; nothing here reintroduces one.
