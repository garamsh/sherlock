# ADR 0014: One execution boundary — shell network isolation, script tools in the agent UID

> Status: proposed
> Date: 2026-08-02

## Context

Two tool paths execute agent-influenced code with inconsistent boundaries:

- `shell_exec` (ADR 0012) drops to the agent UID when the platform is privileged, but its children inherit the platform's network — ADR 0012's "the child has no network access" carried the container-era intent over, and the sub-process design never implemented it.
- Script tools (ADR 0013) run as the platform UID with no privilege drop and full network. Their executables are operator-supplied, but their stdin is agent-controlled JSON: a parser bug or a confused-deputy (e.g. a browser tool fetching `file://` or internal addresses) acts with platform privileges and platform network.

`environment.md` and `security.md` record both gaps as open questions.

## Decision

All agent-influenced code runs under the agent UID; only operator-sanctioned tools reach the network.

- `shell_exec` children are created in a fresh, empty network namespace (`CLONE_NEWNET`) before the UID drop, implementing ADR 0012's original intent. No loopback, no interfaces: the child has no network at all.
- Script tools drop to the agent UID through the same setuid fork+exec mechanism (`internal/shell/`). Their network is **not** namespaced: a script tool is an operator-sanctioned egress channel — external access is tool-routed by principle (`security.md`), and the operator chooses a tool's reach by choosing the tool.
- When the platform cannot drop privileges, both paths report `agent_uid unavailable`; neither silently runs as the platform UID. Consequence: script tools stop working in unprivileged dev environments until the platform is installed setuid-root — the same contract `shell_exec` already has. A silent privilege upgrade is worse than an unavailable tool.

## Consequences

- One trust domain for agent-influenced execution: agent UID, kernel DAC, no ambiguity about which UID a tool runs as.
- `internal/tool/script/` execs through the `shell.Executor` instead of `exec.CommandContext` directly; the executor gains the network-namespace step (Linux build tag, no-op on other platforms).
- The Playwright example keeps working under the agent UID: browser automation needs no privilege, and its network use is the operator-sanctioned egress channel.
- The `environment.md`/`security.md` open questions on network isolation and script-tool privilege close; the memory/runtime UID split stays open (it concerns platform assets, not tool execution).

## Rejected alternatives

- **Network-namespace script tools too** — breaks the flagship use case (browser automation *is* network egress); the tool is the sanctioned channel, unlike the generic shell.
- **Per-tool `uid` option in the config** — an option nobody needs yet (`simplicity.md` §No speculative or dead generality); it arrives with its first use case.
- **Silent platform-UID fallback for script tools** — a privilege upgrade the operator never sees; the `shell_exec` hard-fail contract is the honest one.
