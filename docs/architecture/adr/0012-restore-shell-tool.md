# ADR 0012: Restore the `shell_exec` tool — sub-process in agent_uid

> Status: accepted
> Date: 2026-07-30
> Supersedes: ADR 0011's outcome (no shell_exec tool). The *principle* in ADR 0011 ("no separate sandbox container tier") stands and is restated below.

## Context

ADR 0011 removed `sandbox_exec` and argued the agent doesn't need a generic shell — three typed tools (`memory`, `files`, `message_send`) are sufficient. After shipping, real workloads surfaced that the shell is the right answer for several general-purpose tasks:

- "Run the project's test suite and report which tests fail."
- "Find all .go files larger than 100KB and list them."
- "Compute a value with awk / jq / sed without the model having to do it inline."
- "Run a one-off shell pipeline on a workspace file."

The previous decision (PR #101) was right that the platform does not need a *separate container tier* for the shell — that would be the two-tier body/head split that ADR 0002 explicitly rejected. But removing the shell_exec tool entirely was an over-correction: the right answer is to give the agent a shell in the **same container**, with the existing UID trust domain providing the safety boundary. ADR 0006 already specifies the mechanism: "the supervisor forks, drops privileges via `setuid`, then execs."

## Decision

Add a `shell_exec` tool. It runs the agent-supplied command as a sub-process of the platform with the existing UID-based isolation from ADR 0006.

The tool is named **`shell_exec`**, not `sandbox_exec`. The previous name implied a separate security boundary (a "sandbox"); the new design uses the boundary the container already provides (kernel DAC, no network, dropped privileges). The honest name is the one that says what the tool does.

Mechanism:

- The platform binary is setuid-root (or runs under a supervisor that has the privilege to switch UID).
- When the agent calls `shell_exec`, the platform forks, sets the child to `agent_uid` (the same UID the `files` tool already runs under per ADR 0006), drops Linux capabilities, applies an optional chroot to the workspace, applies resource limits, and execs the command.
- The child has no network access (the deployment image already sets `NetworkMode("none")`).
- The child cannot read `memory.db` (owned by `memory_uid`; the agent_uid's DAC is denied).
- The child cannot modify the platform binary or its config (owned by `runtime_uid`; same).
- The child has a configurable timeout; on expiry the platform sends SIGKILL and the result carries `timed_out: true`.

Threat model (honest):

- The shell can read everything under the workspace (this is its purpose).
- The shell can encode workspace contents into timing channels, file-name patterns, or any other covert channel that survives process boundaries. Two containers don't fix this either.
- The shell **cannot** read platform secrets (ADR 0004), modify the platform binary, or make network requests. This is enforced by kernel DAC and the container network policy.
- "Friction layer, not absolute wall" — same framing ADR 0007 used for guard models.

What the shell is NOT:

- Not a general-purpose programming environment. The agent should still prefer typed tools for typed work; the shell is for cases where the typed surface doesn't yet cover a need.
- Not the right answer for every "compute" question. "What is 6 times 7?" is a question the model can answer directly. The shell is for "I need to run this on the workspace, not in my head."

## Consequences

- Four built-in tools: `memory`, `files`, `message_send`, `shell_exec`. The shell is the only one that runs arbitrary commands; the other three remain typed and narrow.
- A new `internal/shell/` package (the previous `internal/sandbox/` package was deleted in PR #101) contains the sub-process runtime. The previous Docker-container runtime is **not** reinstated — there is no `sandbox.NewDockerRuntime`.
- The CLI config gains `--exec-timeout` (the only sandbox-related config that was removed in PR #101 and is needed again). The other three (`--image`, `--cpu-limit`, `--memory-limit`) stay gone — they were for the Docker-spawned container, which this design does not use. CPU and memory limits for the sub-process are set via `setrlimit` at process spawn; they are not configurable from the CLI in this iteration.
- The platform binary's `setuid` bit is required at install time. If the binary is not installed setuid-root, the supervisor (separate process or container init) must run as root. If neither applies, the `shell_exec` tool returns an error at startup (`agent_uid unavailable`) and the agent falls back to the three typed tools.
- `cmd/gagent/mock_model.go`'s canned scenario updates to include a `shell_exec` call (e.g. `command: 'echo 42'`) so the offline test path exercises the tool.
- `internal/agent/instructions.go::Contract` updates: add a `shell_exec` line in the tools section; drop the prior implication that no shell exists.
- The OpenAI-subset guard still applies to the new tool's schema.
- ADR 0011 (sandbox_exec removal) is superseded. The principle it expressed — "no separate container tier" — stands and is restated here. The *outcome* (no shell_exec tool) is reversed.

## Rejected alternatives

- **Two-container sandbox** — strong isolation, but reintroduces Docker at runtime, adds IPC, contradicts ADR 0002.
- **Typed compute tools (`jq_query`, `python_eval`)** — narrow but every new use case needs a new tool; the shell is the general answer.
- **Keep no shell** — simplest, but the user-visible workload that needs a shell is real.
- **Sandbox via a separate binary** — same setuid mechanism, but splits the platform across two processes; no benefit over fork+exec.

## What this ADR does NOT change

- **ADR 0001** — tool implementations under `internal/<domain>/`; `internal/shell/` joins as the fourth.
- **ADR 0002** — the body model stands. The shell is in the body, not a head.
- **ADR 0004** — secret-free execution domain. The shell cannot see platform secrets.
- **ADR 0006** — UID trust domain. The shell uses the existing mechanism; this ADR is the application of ADR 0006 to the shell, not a new isolation system.
- **ADR 0007** — tools own security, trust-domain invariant. The shell is `TrustCodeExecuting` (it runs agent-supplied commands).
- **ADR 0008, 0009, 0010** — unchanged.

## Open questions

- Optional hardening (chroot, seccomp, more rlimits) — listed in the rejected alternative section as "rejected for MVP scope" but the `internal/shell/` design should leave room to add them without a contract change. The Executor interface returns `Result` and is the only thing the `Tool` calls; an `Executor` implementation that adds chroot/seccomp is a swap, not a rewrite.
- Whether to expose a `workspace` flag (default true) that toggles the optional chroot. Defer until a use case needs it.
