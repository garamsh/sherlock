# ADR 0006: Single-container environment with UID trust domains

> Status: accepted
> Date: 2026-07-25

## Context

ADR 0002 defines the environment as the agent's body but not its physical topology. Candidates: (a) one flat container where everything runs together; (b) container-in-container, an outer container holding tools with the agent in an inner container; (c) a pod of sibling containers (agent, memory, egress proxy as sidecars); (d) one container subdivided by Unix user domains. All container options share the host kernel, so kernel attack surface is identical; they differ in bootstrap cost, resource use, operational complexity, and isolation granularity. Option (b) requires a privileged outer container — which would then also hold the secrets, the worst possible placement — and is rejected. Option (c) maximizes isolation but multiplies orchestration cost.

## Decision

An environment is a **single container subdivided into trust domains by Unix UIDs**.

- A tool runtime (supervisor) manages the environment's processes.
- Tools that execute agent-supplied code (shell, file operations) always run under the unprivileged **agent UID**: the supervisor forks, drops privileges via `setuid`, then execs. Agent code never runs as root.
- Platform assets are owned by dedicated UIDs with restrictive modes: the memory store file by a memory UID (ADR 0003), tool runtime configuration by its own UID. Agent code gets `EACCES` from the kernel — enforcement is DAC, not a filter.
- Self-destruction is bounded: agent code can only delete what the agent UID owns (its workspace).
- Image hygiene is security-critical: no setuid binaries, no writable system paths, minimal capabilities.
- Direct egress for the agent UID is blocked at the host/netfilter level (UID owner match); the only outbound path is the egress tool (ADR 0004).
- The pod/sidecar topology remains a documented hardening option for high-value deployments; the trust-domain model is unchanged, only the physical split moves.

## Consequences

- Fast environment bootstrap and low resource use compared to multi-container topologies.
- "Total authority" means total authority within the agent UID domain: workspace, packages, own processes. The agent cannot damage its own platform runtime — an accepted refinement of ADR 0002.
- Isolation strength depends on image hygiene (setuid removal, capability drops); a checklist must accompany the environment image definition.
- Minor shared channels remain (process visibility via `ps`, `/dev/shm`); accepted for the default topology and closed by the pod option if ever needed.
