# Environment

Governs the environment model: what an environment is, its topology, and its trust boundaries. Rationale: ADR 0018, ADR 0008, ADR 0010.

## Current decisions

- The environment is the agent's computer: a dedicated sidecar container in the agent's pod. One pod per agent; one sidecar per agent to start (ADR 0018).
- The sidecar composes two things: a dedicated environment — the workspace (a persistent volume) and process execution — and an I/O layer, the tool servers that expose the computer's capabilities.
- The computer is configured from outside, like a real computer: adding a capability (browser automation, GitHub tooling) means installing it on the computer and declaring its tool. The agent changes nothing; it discovers the new capability at runtime.
- Permission enforcement lives at the computer — the action site. Agent-influenced execution runs under an unprivileged UID, and workspace and data ownership is kernel-enforced (DAC), not filtered (ADR 0008, ADR 0010 relocated into the sidecar). A platform that cannot drop privileges reports unavailability instead of silently upgrading.
- Egress control is a pod network-layer concern; the in-process network-namespace mechanism is retired.
- Self-destruction is bounded by construction: executed code lives on the computer, and the agent's self (a separate container) is unreachable from it. A broken computer is recreated.
- A shared sidecar serving several agents — one computer with several user accounts — is a later step, not the starting shape.

## Current state

The platform is a single binary in a single container hosting `data/envs/<agent-id>/` (`workspace/` + `memory.db`); the directory is the isolation unit (teardown, migration, backup are directory operations). UID trust domains apply only when the binary runs privileged (setuid-root): `workspace/` is chowned to the agent UID and the files tool drops its filesystem UID to it, while `shell_exec` and script-tool children drop to the agent UID, the shell additionally in a fresh empty network namespace. The shipped distroless nonroot image cannot drop privileges, so those paths report `agent_uid unavailable` under it.

The first migration step exists alongside it: `gagent sidecar` serves one workspace (config key, created on first use), `shell_exec`, and the script tools declared in its own config over the tool contract, with the same UID trust-domain behavior. The agent still consumes the in-process tools; rewiring it to the contract client is the next step.

## Open questions

- The agent↔sidecar authentication (pod-local listener today) — with the contract protocol work.
- The sidecar configuration format (base image plus tool and policy declaration) — the script-tool shape is the seed.
