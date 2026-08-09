# Environment

Governs the environment model: what an environment is, its topology, and its trust boundaries. Rationale: ADR 0018, ADR 0008, ADR 0010.

## Current decisions

- The environment is split into two layers: the workspace (the agent's data plane) and the tools (the agent's capabilities). The workspace is a dedicated sidecar container in the agent's pod; the tools are agent-side Go packages. One pod per agent; one workspace per agent to start (ADR 0018).
- The sidecar serves only the filesystem-only gRPC workspace contract — Read, Write, List. It holds no tool servers, no process executor, and no `tools:` config: it is a pure data plane. Process exec is the agent's native capability (`os/exec`), not a workspace call; the plugin concept is dropped — tools are Go packages the agent imports at compile time.
- The workspace is configured from outside, like a real working volume: the workspace directory is created on first use and the agent learns its location from the sidecar config. Adding a tool is a Go-package change in the agent build, not a sidecar config change.
- Permission enforcement lives at the data plane — the action site for files. Agent-influenced file writes run under an unprivileged UID, and workspace ownership is kernel-enforced (DAC), not filtered (ADR 0008, ADR 0010 relocated into the sidecar). A platform that cannot drop privileges reports unavailability instead of silently upgrading.
- Process exec inherits the agent process's UID — operator scope governs the trust posture of the agent process itself.
- Egress control lives at two layers: the pod network layer is the additional deployment-level boundary (the infrastructure layer's mechanism, not an artifact this repo ships), and the agent's process-level controls are the in-process boundary for any network-isolated children the agent may launch.
- Self-destruction is bounded by construction: files live on the workspace, and the agent's self (a separate container) is unreachable from it. A broken workspace is recreated.
- A shared workspace serving several agents — one volume with several user accounts — is a later step, not the starting shape.

## Current state

The pod split is two processes today. `gagent sidecar` serves one workspace (config key, created on first use) over the workspace gRPC contract — Read, Write, List. It owns no tools, no exec backend, and no script-tool runtime. Workspace writes drop to the configured `exec-uid` (default 1001): a privileged sidecar switches to it and the workspace is chowned to it; with `exec-uid` equal to the sidecar's own UID the run is single-account — no privilege is needed. Any other unprivileged `exec-uid` fails closed with `agent_uid unavailable`. The shipped images make this concrete: the agent image hosts the reasoning loop, gateway, memory, native process exec, and tools (Go packages); the sidecar image hosts the workspace gRPC server with no setuid binaries, run with exactly CAP_SETUID/SETGID/CHOWN/SYS_ADMIN so the workspace chown operates (`deploy/local/`).

`gagent agent` holds the reasoning loop, gateway, memory, native process exec, and the tools the agent imports. The `data/envs/<id>/` pairing is gone; the agent configures only its memory file (`memory-path`) and uses the workspace gRPC client for files. Tools that need files use the same client.

## Open questions

- The agent↔sidecar authentication (pod-local workspace listener today) — with the contract protocol work.
- The tool move from the sidecar's built-in tool set into `tools/` as agent-side packages — a follow-up refactor that builds on this record.