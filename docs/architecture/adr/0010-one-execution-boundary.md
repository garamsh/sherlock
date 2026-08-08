# ADR 0010: One execution boundary — agent-influenced code in the unprivileged UID

> Status: accepted
> Date: 2026-08-02

## Context

Two tool paths execute agent-influenced code: `shell_exec` (agent-supplied commands, ADR 0008) and script tools (operator-supplied executables whose stdin is agent-controlled JSON, ADR 0009). They must share one trust boundary, not drift into per-path privileges.

## Decision

All agent-influenced code runs under the unprivileged agent UID; only operator-sanctioned tools reach the network.

- `shell_exec` children have no network — today a fresh, empty network namespace; in the pod topology the pod network layer is the mechanism (ADR 0018).
- Script tools drop to the agent UID through the same executor. Their network is not namespaced: a script tool is an operator-sanctioned egress channel — the operator chooses a tool's reach by choosing the tool.
- When the platform cannot drop privileges, both paths report `agent_uid unavailable`; neither silently upgrades. A silent privilege upgrade is worse than an unavailable tool.

## Consequences

- One trust domain for agent-influenced execution: agent UID, kernel DAC, no ambiguity about which UID a tool runs as.
- In the pod topology this boundary lives inside the sidecar, the agent's computer (ADR 0018).
- The Playwright example keeps working under the agent UID: browser automation needs no privilege, and its network use is the operator-sanctioned egress channel.

## Rejected alternatives

- **Network-namespace script tools too** — breaks the flagship use case (browser automation *is* network egress); the tool is the sanctioned channel, unlike the generic shell.
- **Per-tool `uid` option in the config** — an option nobody needs yet (`simplicity.md` §No speculative or dead generality); it arrives with its first use case.
- **Silent privileged fallback** — a privilege upgrade the operator never sees; the hard-fail contract is the honest one.
