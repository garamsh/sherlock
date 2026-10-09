# ADR 0008: Agent-influenced sub-processes run dropped to the unprivileged UID

> Status: accepted
> Date: 2026-07-30 (revised in place 2026-08-13; 2026-08-16; 2026-08-17; 2026-10-09; 2026-10-10)

## Context

The agent needs general-purpose exec for real workloads: "run the project's test suite and report which tests fail", "find all files larger than a size and list them", "compute a value with awk / jq / sed instead of inline in the model", "run a one-off pipeline on a workspace file". Typed tools cover typed needs; exec covers the rest. The workspace is the computer (ADR 0017): it serves the filesystem AND runs agent-influenced processes, so the trust boundary for those sub-processes is decided here, once, at the workspace that owns the executor.

## Decision

When the workspace's executor (`internal/workspace/shell`, ADR 0017) spawns a child for agent-influenced work, the child runs dropped to the unprivileged exec UID. The file side of the same boundary stands in the same process: workspace writes and exec'd children share the workspace directory's ownership story.

The model-facing surface is a tool: `shell_exec` is an independent tool server (ADR 0017, ADR 0009) that wraps the workspace's Exec RPC. The model reaches exec through the same tool-call channel as every other capability. The agent itself executes nothing — the tool calls the workspace over the gRPC client. Exec children work in the workspace directory by default, so an exec'd program sees the same files the file tools sees natively — the workspace execs children in its own directory and no shared-mount trick is needed.

Mechanism:

- The executor forks, sets the child to the exec UID, drops Linux capabilities, applies resource limits, and execs the command.
- Network isolation is per-request (`NoNetwork`): the child lands in a fresh, empty network namespace (a privileged unshare, or an unprivileged user+network namespace in single-account mode, ADR 0010); in the pod topology the pod network layer is an additional mechanism.
- The child can neither read nor write the agent's volume — `memory.db`, its sidecars and `outbox/` — and what holds that in full is the mount: the agent's volume is not mounted into the workspace container, so no child reaches it under any account. The distinct exec UID is required as well, and on a volume both containers mount it stops reads as well as writes: the agent creates `memory.db`, its sidecars and the writer lock `0600` and `outbox/` `0700`, and tightens an existing store to those modes at startup (#998). What it leaves visible there is names: the directory holding `memory.db` stays `0755`, because it may also hold the workspace directory, which an Exec child must traverse. Platform runtime material is out of reach by kernel DAC.
- The child has a configurable timeout; on expiry its whole process group is SIGKILLed and the result carries `timed_out: true` — a normal outcome crossing the wire, not an RPC error. The group, not the child alone: the child is the shim, which execs a shell, which execs the command, and each inherits the pipe the executor drains, so killing only the first leaves the caller waiting for the last.
- That kill crosses the drop, so it needs a capability the drop does not: signalling a process that owns neither of the executor's uids requires `CAP_KILL`, and root is no substitute once the capability has left the bounding set. **The claim above holds only where the workspace holds `CAP_KILL`** — single-account mode excepted, where the child owns the executor's own uid. A privileged executor without it reports `agent_uid unavailable` per call rather than running a command whose deadline it could not enforce (#612).
- When the platform cannot run the child as the configured account, the executor reports `agent_uid unavailable` instead of silently upgrading.
- When the platform cannot apply one of the child's resource limits, the executor fails the call instead of running the child uncapped. The same holds for the one bound the executor owns rather than the child: a deadline it could not enforce fails the call.

Threat model (honest):

- A child can read everything under the workspace (this is its purpose) and can leak workspace contents through covert channels; containers do not fix that.
- A child **cannot** read secrets (ADR 0003), modify the tool runtime, or make network requests unless the operator sanctioned egress — enforced structurally.

What exec is NOT:

- Not hosted in the agent — the agent process holds no exec machinery; the `shell_exec` tool is a client of the workspace contract.
- Not a general-purpose programming environment exposed to the model — typed tools first; exec is for what the typed surface does not cover.

## Consequences

- The `internal/workspace/shell/` package holds the executor; the Executor interface is the only thing the workspace server uses, so added hardening (chroot, seccomp, more rlimits) is a swap, not a rewrite. `shell_exec` is an independent tool server (ADR 0017) that calls the workspace's Exec RPC; it knows only the workspace gRPC client.
- One deployment shape is supported: the privileged drop, a workspace process with UID-switch privilege (setuid-root binary, supervisor, or container capabilities) running children as a configured exec UID that is not its own. The executor also runs children in single-account mode — the exec UID set to the process's own UID, needing no privilege, declared explicitly with no implicit fallback to the current UID — and that shape is not supported: the child runs as the account the workspace runs as, nothing but a mount separates it from what that account can reach, and on `garam-dev` that account owned the agent's store, which an Exec child then read and appended to (#966). Holding the invariant under a shared account would need a second filesystem boundary and conditions to document, for a shape whose only merit is skipping the privileged drop. An unprivileged process with any other exec UID reports `agent_uid unavailable`.
- The privileged drop needs a capability set, not just privilege: `SETUID`/`SETGID` for the credential drop, `CHOWN` for the workspace-directory handover, `SYS_ADMIN` for the netns unshare, and `KILL` for the deadline. `deployment.md` §Capabilities holds the operator-facing table and what dropping each one costs.
- Exec children share the workspace directory's ownership story: a privileged workspace chowns the workspace directory to the exec UID, keeps the directory's write bit for its own group, and gives every file the gRPC contract writes to the same UID — so files written by tools through the gRPC contract and by exec'd children share one owner, without the workspace holding a capability over every file in its container.
- The unauthenticated Exec listener is a remote shell into the workspace, so the loopback-only fail-closed posture (ADR 0012) is what stands until contract authentication exists.

## Rejected alternatives

- **Typed compute tools only (`jq_query`, `python_eval`)** — narrow but every new use case needs a new tool; exec is the general answer.
- **Keep no exec capability** — simplest, but the workloads that need exec are real.
- **Exec in the agent process** — splits the computer in two: the agent would need the workspace's files at an identical local path (a shared-mount trick) for exec'd programs to see them, and the agent container would carry the shell, the privileges, and the untrusted-code surface the pod split exists to keep out of it (ADR 0017).
