# Environment

Governs the environment model: what an environment is, its topology, and its trust boundaries. Rationale: ADR 0018, ADR 0008, ADR 0010.

## Current decisions

- The environment is the agent's computer: a dedicated sidecar container in the agent's pod. One pod per agent; one sidecar per agent to start (ADR 0018).
- The sidecar composes two things: a dedicated environment — the workspace (a persistent volume) and process execution — and an I/O layer, the tool servers that expose the computer's capabilities.
- The computer is configured from outside, like a real computer: adding a capability (browser automation, GitHub tooling) means installing it on the computer and declaring its tool. The agent changes nothing; it discovers the new capability at runtime.
- Permission enforcement lives at the computer — the action site. Agent-influenced execution runs under an unprivileged UID, and workspace and data ownership is kernel-enforced (DAC), not filtered (ADR 0008, ADR 0010 relocated into the sidecar). A platform that cannot drop privileges reports unavailability instead of silently upgrading.
- Egress control lives at two layers: the in-process network namespace is the in-computer boundary (`shell_exec` children get a fresh, empty netns; ADR 0010), and the pod network layer (NetworkPolicy) is the additional deployment-level boundary.
- Self-destruction is bounded by construction: executed code lives on the computer, and the agent's self (a separate container) is unreachable from it. A broken computer is recreated.
- A shared sidecar serving several agents — one computer with several user accounts — is a later step, not the starting shape.

## Current state

The pod split is two processes today. `gagent sidecar` serves one workspace (config key, created on first use), `shell_exec`, and the script tools declared in its own config over the tool contract. Children run as the configured `exec-uid` (default 1001): a privileged sidecar switches to it — the workspace is chowned and the files tool drops its filesystem UID, `shell_exec` and script-tool children drop to it, the shell additionally in a fresh empty network namespace. With `exec-uid` equal to the sidecar's own UID the run is a single-account computer: no privilege is needed and the shell's network isolation comes from an unprivileged user+network namespace. Any other unprivileged `exec-uid` fails closed with `agent_uid unavailable`. The shipped images make this concrete: the agent image is distroless static (no untrusted code runs there), and the sidecar image is a minimal glibc base with no setuid binaries, run with exactly CAP_SETUID/SETGID/CHOWN/SYS_ADMIN so the drop operates (`deploy/local/`, `deploy/k8s/`).

`gagent agent` holds no environment layout: the `data/envs/<id>/` pairing is gone, the agent configures only its memory file (`memory-path`), and every computer capability reaches it as a discovered remote tool over the contract.

## Open questions

- The agent↔sidecar authentication (pod-local listener today) — with the contract protocol work.
- The sidecar configuration format (base image plus tool and policy declaration) — the script-tool shape is the seed.
