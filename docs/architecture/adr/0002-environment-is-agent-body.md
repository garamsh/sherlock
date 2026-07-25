# ADR 0002: The environment is the agent's body

> Status: accepted
> Date: 2026-07-25

## Context

PLAN §4 describes the agent running "on" a Docker container and a `sandbox_exec` tool that creates and destroys execution containers — two container tiers. The project owner instead wants a single-tier model: the agent has total authority inside its environment, and the environment's boundary (network, mounts, resources, lifecycle) is managed from outside. Two risks were raised: self-destructive behavior by the agent, and secret exposure through a shell tool.

## Decision

The environment (a container) **is** the agent's body. Code the agent runs shares fate with the agent — there is no separate sandbox container tier.

- `sandbox_exec` means "execute a command inside my own environment". Its `create`/`exec`/`destroy` operations map to the environment lifecycle, not to child containers.
- The execution backend is an implementation detail of the tool: MVP runs the agent as a local process with a Docker container as its body; a future deployment may run the agent loop inside the container itself. The tool interface does not change.
- Policy (egress allowlist, CPU/memory quotas, mount scope, TTL) is injected by the orchestration side at environment creation. The agent can call `exec` but cannot change policy.
- Self-destruction is handled by isolation plus recovery, not prevention: the blast radius is one environment, the environment is re-creatable from its definition, and memory is protected per ADR 0003.

## Consequences

- Simpler mental model and fewer moving parts than nested containers (no Docker-in-Docker, no host socket mount).
- Code executed by the agent can read or kill anything in the environment, including the agent itself — accepted as the explicit ownership model; recovery is the mitigation.
- This supersedes the two-tier reading of PLAN §4; PLAN.md is the original directive and is not rewritten, this ADR is the record of the change.
- Open: memory snapshot/backup cadence is undecided (default: post-MVP, see ADR 0003). Open: egress allowlist enforcement mechanism (default: MVP ships default-deny networking, allowlist defined in the interface only).
