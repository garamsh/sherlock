# ADR 0008: Agent-influenced sub-processes run dropped to the unprivileged UID

> Status: accepted
> Date: 2026-07-30

## Context

The agent needs general-purpose exec for real workloads: "run the project's test suite and report which tests fail", "find all files larger than a size and list them", "compute a value with awk / jq / sed instead of inline in the model", "run a one-off pipeline on a workspace file". Typed tools cover typed needs; native exec covers the rest. Process exec is the agent's own capability — not a tool, not a workspace call (ADR 0018) — so the trust boundary for agent-influenced sub-processes is decided here, once.

## Decision

When the agent's native executor (`internal/shell`, ADR 0018) spawns a child for agent-influenced work, the child runs dropped to the unprivileged agent UID. The file side of the same boundary stands inside the sidecar: workspace writes run under the sidecar's own UID, kernel-enforced (ADR 0018).

Mechanism:

- The executor forks, sets the child to the agent UID, drops Linux capabilities, applies resource limits, and execs the command.
- Network isolation is per-request (`NoNetwork`): the child lands in a fresh, empty network namespace (a privileged unshare, or an unprivileged user+network namespace in single-account mode, ADR 0010); in the pod topology the pod network layer is an additional mechanism.
- The child cannot reach the agent's memory or the platform's runtime material — kernel DAC either way. In single-account mode this one axis intentionally does not hold — the child shares the agent's account; the remaining axes (scrubbed environment, resource limits, optional network isolation) are unchanged.
- The child has a configurable timeout; on expiry it is SIGKILLed and the result carries `timed_out: true`.
- When the platform cannot run the child as the configured account, the executor reports `agent_uid unavailable` instead of silently upgrading.

Threat model (honest):

- A child can read everything under the workspace (this is its purpose) and can leak workspace contents through covert channels; containers do not fix that.
- A child **cannot** read secrets (ADR 0003), modify the tool runtime, or make network requests unless the operator sanctioned egress — enforced structurally.

What native exec is NOT:

- Not a tool — the model never sees exec as a tool call; the agent's own code paths invoke the executor directly (ADR 0018).
- Not a general-purpose programming environment exposed to the model — typed tools first; exec is for what the typed surface does not cover.

## Consequences

- The `internal/shell/` package holds the executor; the Executor interface is the only thing callers use, so added hardening (chroot, seccomp, more rlimits) is a swap, not a rewrite.
- Two deployment modes run real workloads. The privileged drop: an agent process with UID-switch privilege (setuid-root binary, supervisor, or container capabilities) runs children as the configured agent UID. The single-account mode: the agent UID set to the process's own UID needs no privilege at all — the operator declares the account explicitly; there is no implicit fallback to the current UID. An unprivileged process with any other agent UID reports `agent_uid unavailable`.

## Rejected alternatives

- **Typed compute tools only (`jq_query`, `python_eval`)** — narrow but every new use case needs a new tool; native exec is the general answer.
- **Keep no exec capability** — simplest, but the workloads that need exec are real.
- **Exec as a workspace RPC** — process exec is the agent's own; a remote exec RPC is an unauthenticated remote shell and a layering violation (ADR 0018).
