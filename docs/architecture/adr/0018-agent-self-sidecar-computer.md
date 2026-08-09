# ADR 0018: The agent is the self; the sidecar is the computer — pod-per-agent

> Status: accepted
> Date: 2026-08-09

## Context

Two pressures outgrew the original single-container model, in which trust domains were separated by Unix UIDs inside one container:

- **Extensibility.** A capability joined either as Go code in the platform binary (a rebuild) or as a config-registered script tool the platform still owned. Many users must contribute capabilities — browser automation, GitHub tooling, external APIs — without forking or rebuilding the platform, and without configuring the platform and the capability's home separately.
- **Security placement.** Authority and permissions belong to the environment and Linux permissions, not to in-process trust domains inside the agent's own process. The single container accepted self-destruction inside the agent UID domain as a risk; moving execution out of the agent's container blocks it by construction.

The single-container record itself anticipated this move: "The pod/sidecar topology remains a documented hardening option for high-value deployments; the trust-domain model is unchanged, only the physical split moves." The former records for the body model, the single-container topology, the no-external-loader rule, and config-registered script tools are removed; this record absorbs their live content.

## Decision

One pod per agent, holding two containers.

- **The agent container is the agent's self.** It holds the gateway (the agent's ears and eyes), the reasoning loop, memory (the agent's long-term organ — a layer, never a tool), and the mouth (outbound messaging). The agent executes no code in its own container.
- **The sidecar container is the agent's computer.** It composes a dedicated environment (workspace and process execution) and an I/O layer (tool servers), and enforces the agent's permissions with Linux users and modes at the action site. It is configured from outside like a real computer: installing a capability means configuring the computer, never the agent.
- **A tool is a contract with the outside** — descriptor, implementation, transport. The outside is anything reachable through the contract: the sidecar, another container, an external API. A tool is defined once, where its implementation lives (ADR 0009); the agent discovers descriptors at runtime instead of holding a static registry. Tools are not the sidecar's exclusive property: the mouth's implementation lives in the agent, computer capabilities live in the sidecar, external APIs may live elsewhere.
- Sidecars start dedicated, one per agent. A shared sidecar serving several agents — one computer with several user accounts — is a later step.

## Consequences

- Self-destruction is bounded by construction: executed code can reach only the computer, which is recreated; the agent's self is unreachable from it.
- Adding a capability touches exactly one place — the container where its implementation runs. The agent learns of it through discovery; there is no dual configuration.
- The tool contract becomes a wire protocol (descriptor listing plus call) between containers — a deliberate reversal of the former no-external-loader rule, adopted for extensibility. Discovery and versioning are part of the protocol from the start.
- `memory.db` moves into the agent container; the per-agent SQLite store and its store interface (ADR 0002) carry forward. The memory-UID custody concern dissolves — no untrusted code runs in the agent container.
- The UID trust-domain mechanism relocates into the sidecar: agent-influenced execution drops to an unprivileged UID, and ownership is kernel-enforced (ADR 0008, ADR 0010 stand inside the sidecar). Egress control moves to the pod network layer; the in-process network namespace is no longer the mechanism.
- Caller authorization stays in the orchestration layer (ADR 0015); capability permissions are enforced at the sidecar, the action site.
- The gateway serves its own pod's agent only; routing senders to agent pods is the orchestration layer's concern. Turn-drains-queue semantics (ADR 0016) are unchanged per agent.
- The deployment grows from one binary plus a directory tree to a pod with two containers and two volumes (memory, workspace). Existing packages mostly survive, re-wired: `internal/agent` and `internal/memory` into the agent binary; `internal/shell` and `internal/files` into the sidecar; `internal/tool` into the contract definition plus its client and server.
- The code migration and the deployment artifacts have landed: the repo-root `Dockerfile` (agent/sidecar targets), `deploy/local/`, and `deploy/k8s/`.

## Rejected alternatives

- **Keep the single container, harden UID domains further** — leaves multi-user extensibility unsolved and keeps self-destruction an accepted risk rather than a construction.
- **Shared sidecars from the start** — one computer with several user accounts is the hard multi-tenancy problem; dedicated first, share later.
- **Static tool registration in both containers** — every capability would need configuration in two places; discovery removes the dual configuration.
