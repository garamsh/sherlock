# ADR 0002: The environment is the agent's body

> Status: accepted
> Date: 2026-07-25

## Context

PLAN §4 describes the agent running "on" a Docker container and a `sandbox_exec` tool that creates and destroys execution containers — two container tiers. The project owner instead wants a single-tier model: the agent has total authority inside its environment, and the environment's boundary (network, mounts, resources, lifecycle) is managed from outside. Two risks were raised: self-destructive behavior by the agent, and secret exposure through a shell tool.

## Decision

The environment **is** the agent's body. Code the agent runs shares fate with the agent — there is no separate sandbox container tier.

- `sandbox_exec` means "execute a command inside my own environment". Its `create`/`exec`/`destroy` operations map to the environment lifecycle, not to child containers.
- Policy (egress, CPU/memory quotas, mount scope, lifecycle) is injected from outside at environment creation. The agent can call `exec` but cannot change policy.
- "Total authority" is refined: the agent has total authority within its user domain. Platform assets (memory store, tool runtime, secrets) are outside that domain but may still live inside the same container — the mechanism is defined in ADR 0006.
- Self-destruction is bounded by construction: agent code cannot reach outside its user domain (ADR 0006), and secrets never materialize inside it (ADR 0004).

## Consequences

- Simpler mental model and fewer moving parts than nested containers (no Docker-in-Docker, no host socket mount).
- Code executed by the agent can destroy anything inside its own user domain (workspace, installed packages, own processes) — accepted as the explicit ownership model.
- This supersedes the two-tier reading of PLAN §4; PLAN.md is the original directive and is not rewritten, this ADR is the record of the change.
- Physical topology (single container vs pod) is decided in ADR 0006; security ownership in ADR 0007.
