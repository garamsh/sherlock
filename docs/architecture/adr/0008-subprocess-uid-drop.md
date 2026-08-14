# ADR 0008: Agent-influenced sub-processes run dropped to the unprivileged UID

> Status: accepted
> Date: 2026-07-30 (revised in place 2026-08-13)

## Context

The agent needs general-purpose exec for real workloads: "run the project's test suite and report which tests fail", "find all files larger than a size and list them", "compute a value with awk / jq / sed instead of inline in the model", "run a one-off pipeline on a workspace file". Typed tools cover typed needs; exec covers the rest. The workspace is the computer (ADR 0017): it serves the filesystem AND runs agent-influenced processes, so the trust boundary for those sub-processes is decided here, once, at the workspace that owns the executor.

## Decision

When the workspace's executor (`internal/workspace/shell`, ADR 0017) spawns a child for agent-influenced work, the child runs dropped to the unprivileged exec UID. The file side of the same boundary stands in the same process: workspace writes and exec'd children share the workspace directory's ownership story.

The model-facing surface is a tool: `shell_exec` is an independent tool server (ADR 0017, ADR 0009) that wraps the workspace's Exec RPC. The model reaches exec through the same tool-call channel as every other capability. The agent itself executes nothing — the tool calls the workspace over the gRPC client. Exec children work in the workspace directory by default, so an exec'd program sees the same files the files tool sees natively — the workspace execs children in its own directory and no shared-mount trick is needed.

Mechanism:

- The executor forks, sets the child to the exec UID, drops Linux capabilities, applies resource limits, and execs the command.
- Network isolation is per-request (`NoNetwork`): the child lands in a fresh, empty network namespace (a privileged unshare, or an unprivileged user+network namespace in single-account mode, ADR 0010); in the pod topology the pod network layer is an additional mechanism.
- The child cannot reach the agent's memory or the platform's runtime material — kernel DAC either way. In single-account mode this one axis intentionally does not hold — the child shares the workspace's account; the remaining axes (scrubbed environment, resource limits, optional network isolation) are unchanged.
- The child has a configurable timeout; on expiry it is SIGKILLed and the result carries `timed_out: true` — a normal outcome crossing the wire, not an RPC error.
- When the platform cannot run the child as the configured account, the executor reports `agent_uid unavailable` instead of silently upgrading.

Threat model (honest):

- A child can read everything under the workspace (this is its purpose) and can leak workspace contents through covert channels; containers do not fix that.
- A child **cannot** read secrets (ADR 0003), modify the tool runtime, or make network requests unless the operator sanctioned egress — enforced structurally.

What exec is NOT:

- Not hosted in the agent — the agent process holds no exec machinery; the `shell_exec` tool is a client of the workspace contract.
- Not a general-purpose programming environment exposed to the model — typed tools first; exec is for what the typed surface does not cover.

## Consequences

- The `internal/workspace/shell/` package holds the executor; the Executor interface is the only thing the workspace server uses, so added hardening (chroot, seccomp, more rlimits) is a swap, not a rewrite. `shell_exec` is an independent tool server (ADR 0017) that calls the workspace's Exec RPC; it knows only the workspace gRPC client.
- Two deployment modes run real workloads. The privileged drop: a workspace process with UID-switch privilege (setuid-root binary, supervisor, or container capabilities) runs children as the configured exec UID. The single-account mode: the exec UID set to the process's own UID needs no privilege at all — the operator declares the account explicitly; there is no implicit fallback to the current UID. An unprivileged process with any other exec UID reports `agent_uid unavailable`.
- Exec children share the workspace directory's ownership story: a privileged workspace chowns the workspace directory to the exec UID, so files written by tools through the gRPC contract and by exec'd children share one owner.
- The unauthenticated Exec listener is a remote shell into the workspace, so the loopback-only fail-closed posture (ADR 0012) is what stands until contract authentication exists.

## Rejected alternatives

- **Typed compute tools only (`jq_query`, `python_eval`)** — narrow but every new use case needs a new tool; exec is the general answer.
- **Keep no exec capability** — simplest, but the workloads that need exec are real.
- **Exec in the agent process** — splits the computer in two: the agent would need the workspace's files at an identical local path (a shared-mount trick) for exec'd programs to see them, and the agent container would carry the shell, the privileges, and the untrusted-code surface the pod split exists to keep out of it (ADR 0017).
