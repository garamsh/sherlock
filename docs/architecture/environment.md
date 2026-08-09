# Environment

Governs the environment model: what an environment is, its topology, and its trust boundaries. Rationale: ADR 0018, ADR 0008, ADR 0010.

## Current decisions

- The environment is split into two layers: the workspace (the agent's data plane) and the tools (the agent's capabilities). The workspace is a dedicated sidecar container in the agent's pod; the tools are agent-side Go packages the agent imports at compile time. One pod per agent; one workspace per agent to start (ADR 0018).
- The sidecar serves only the filesystem-only gRPC workspace contract — Read, Write, List — over a loopback listener. It holds no tool servers, no process executor, and no `tools:` config: it is a pure data plane. Process exec is the agent's native capability (`os/exec` via `internal/shell`); the plugin concept is dropped — tools are Go packages the agent imports.
- The workspace is configured from outside, like a real working volume: the workspace directory is created on first use, the listener bind address is operator-supplied, and the agent learns the listener's address from its own config (`--workspace-addr`, default `127.0.0.1:8081`). Adding a tool is a Go-package change in the agent build, not a sidecar config change.
- Permission enforcement lives at the data plane — the action site for files. Workspace writes run under the sidecar's own UID and workspace ownership is kernel-enforced (DAC), not filtered (ADR 0008, ADR 0010). A platform that cannot drop privileges reports unavailability instead of silently upgrading (the sidecar today reports `agent_uid unavailable` only when it tries to run a child — a workspace that runs no children, like the new shape, needs no drop).
- Process exec inherits the agent process's UID — operator scope governs the trust posture of the agent process itself. Tools that need exec import `internal/shell` and call `Executor.Run`; the network isolation the executor applies (`NoNetwork`) is per-request and only relevant when a tool asks for it.
- Egress control lives at two layers: the pod network layer is the additional deployment-level boundary (the infrastructure layer's mechanism, not an artifact this repo ships), and the agent's process-level controls are the in-process boundary for any network-isolated children a tool may launch.
- Self-destruction is bounded by construction: files live on the workspace, and the agent's self (a separate container) is unreachable from it. A broken workspace is recreated.
- A shared workspace serving several agents — one volume with several user accounts — is a later step, not the starting shape.

## Current state

The pod split is two processes today. `gagent workspace` serves one workspace (config key, created on first use) over the filesystem-only gRPC contract — Read, Write, List. It owns no tools, no exec backend, and no script-tool runtime. Workspace writes run as the sidecar's own UID (no children spawn, so no privilege drop is needed in the typical run); a privileged sidecar chowns the workspace directory to that UID once at startup. The shipped images make this concrete: the agent image hosts the reasoning loop, gateway, memory, native process exec, and tools (Go packages); the sidecar image hosts the workspace gRPC server with no setuid binaries, run with exactly CAP_SETUID/SETGID/CHOWN/SYS_ADMIN so the workspace chown operates (`deploy/local/`).

`gagent agent` holds the reasoning loop, gateway, memory, native process exec, and the tools the agent imports at compile time. The compile-time registry is the unit: `cmd/gagent/agent.go` imports `tools/files` and `tools/playwright`, dials the workspace gRPC client with the configured endpoint, and constructs the tools with that client. The agent binary never imports `internal/workspace` directly — the gRPC client wrapper (`internal/workspace/client`) is the boundary the issue specified, and tools own the wire. The agent's config exposes a single `--workspace-addr` flag (and `workspace-addr:` config key); there is no longer a `tool-servers:` config or a tool discovery step at startup.

## Open questions

- The agent↔sidecar authentication (pod-local workspace listener today) — with the contract protocol work.
- The shared-sidecar account model — with the multi-tenancy work.
