# ADR 0010: One execution boundary — agent-influenced effects under the unprivileged UID

> Status: accepted
> Date: 2026-08-02

## Context

Two paths carry agent-influenced effects: file writes through the workspace data plane (ADR 0018) and sub-processes spawned by the agent's native executor (ADR 0008). They must share one trust boundary, not drift into per-path privileges.

## Decision

All agent-influenced effects land under the unprivileged UID; only operator-sanctioned tools reach the network.

- **File writes** run under the sidecar's own UID with kernel-enforced ownership (DAC), not filters — the data plane is the action site for files (ADR 0018).
- **Executor children** carry the UID drop, scrubbed environment, and resource limits of ADR 0008. Network isolation is per-request (`NoNetwork`): a fresh, empty network namespace (a privileged unshare, or a user namespace with the agent's UID mapped to itself in single-account mode — no capability required; a host without unprivileged user namespaces fails closed). In the pod topology the pod network layer is an additional mechanism (ADR 0018).
- **Resource caps bind untrusted code only.** Agent-influenced, untrusted children carry the executor's rlimits (CPU, address space, file size, open files, NPROC in the privileged drop). Operator-chosen tool packages run without them: their bound is the deployment's container/pod resource policy (the orchestration layer owns that policy, ADR 0015/ADR 0018), not in-process rlimits. The trust class is explicit in the executor's request, never inferred from the network axis.
- When the platform cannot run a child as the configured account, the executor reports `agent_uid unavailable`; it never silently upgrades. A silent privilege upgrade is worse than an unavailable capability.

## Consequences

- One trust domain for agent-influenced effects: the configured account, kernel DAC, no ambiguity about which account an effect lands under. In single-account mode that domain is the process's own account — an explicit operator declaration, never an implicit fallback.
- In the pod topology the file side of this boundary lives inside the sidecar and the exec side in the agent container (ADR 0018).
- The Playwright example keeps working: browser automation is an operator-sanctioned egress channel — the operator chooses a tool's reach by choosing the tool.

## Rejected alternatives

- **Network-isolating operator-sanctioned tools too** — breaks the flagship use case (browser automation *is* network egress); the tool is the sanctioned channel, unlike agent-influenced exec.
- **Per-tool `uid` option in the config** — an option nobody needs yet (`simplicity.md` §No speculative or dead generality); it arrives with its first use case.
- **Silent privileged fallback** — a privilege upgrade the operator never sees; the hard-fail contract is the honest one.
