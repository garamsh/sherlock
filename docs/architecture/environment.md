# Environment

Governs the environment model: what an environment is, its topology, and its trust boundaries. Rationale: ADR 0018, ADR 0008, ADR 0010.

## Current decisions

- The environment is split into two layers: the workspace (the agent's data plane) and the tools (the agent's capabilities). The workspace is a dedicated sidecar container in the agent's pod; the tools are agent-side Go packages the agent imports at compile time. One pod per agent; one workspace per agent to start (ADR 0018).
- The sidecar is the computer: it serves the gRPC workspace contract — Read, Write, List, Exec — over a loopback listener. It holds no tool servers and no `tools:` config; its executor (`internal/workspace/shell`) runs agent-influenced processes in the workspace directory (ADR 0008). The agent executes nothing; the model reaches exec through the `shell_exec` tool, which calls the workspace's Exec RPC (ADR 0018). The plugin concept is dropped — tools are Go packages the agent imports.
- The workspace is configured from outside, like a real working volume: the workspace directory is created on first use, the listener bind address is operator-supplied, and the agent learns the listener's address from its own config (`--workspace-addr`, default `127.0.0.1:8081`). Adding a tool is a Go-package change in the agent build, not a sidecar config change.
- The workspace directory is the workspace's own real directory: the workspace serves the typed file API over it and execs children in it, so exec'd programs see workspace files natively — no shared mount and no path-equality trick. The agent holds no workspace-path setting; it only dials the workspace's address.
- Permission enforcement lives at the data plane — the action site for files and processes. Workspace writes run under the sidecar's own UID and workspace ownership is kernel-enforced (DAC), not filtered (ADR 0008, ADR 0010). A privileged workspace chowns the workspace directory to the exec UID (`exec-uid`, default 1001), so files written by tools through the gRPC contract and by exec'd children share one owner; an unprivileged workspace owns the directory itself. A platform that cannot run a child as the configured exec UID reports `agent_uid unavailable` instead of silently upgrading (ADR 0008).
- Exec children run under the workspace's trust boundary (ADR 0008, ADR 0010): dropped to the exec UID (workspace key `exec-uid`), bounded per call (`exec-timeout`, default 30s) and by rlimits, with a scrubbed environment and a fresh, empty network namespace. Single-account mode applies unchanged: exec-uid equal to the workspace's own UID needs no privilege; any other unprivileged value fails closed.
- Egress control lives at the pod network layer — the deployment-level boundary (the infrastructure layer's mechanism, not an artifact this repo ships). Process-level network isolation for exec children is the per-request netns above.
- Self-destruction is bounded by construction: files live on the workspace, and the agent's self (a separate container) is unreachable from it. A broken workspace is recreated.
- A shared workspace serving several agents — one volume with several user accounts — is a later step, not the starting shape.

## Current state

The pod split is two processes today. `gagent workspace` serves one workspace (config key, created on first use) over the gRPC contract — Read, Write, List, Exec. It owns no tools and no script-tool runtime, but it owns the executor: Exec children run in the workspace directory, dropped to the exec UID, and when privileged the workspace chowns the directory to the same UID so both writers share one owner. The shipped images make this concrete: the agent image hosts the reasoning loop, gateway, memory, and tools (Go packages) — it executes nothing, so it stays distroless nonroot with no shell; the sidecar image hosts the workspace gRPC server with a shell (Exec children are `sh -c` sub-processes), the uid-1001 exec account, and exactly the capabilities the executor needs — SETUID, SETGID, CHOWN, SYS_ADMIN (`deploy/local/`).

`gagent agent` holds the reasoning loop, gateway, memory, and the tools the agent imports at compile time. The compile-time registry is the unit: `cmd/gagent/agent.go` imports `tools/files`, `tools/playwright`, and `tools/shell_exec`, dials the workspace gRPC client with the configured endpoint, and constructs the tools. shell_exec calls the workspace's Exec RPC; the command runs in the workspace directory and sees the files the workspace serves. The agent's only workspace setting is `workspace-addr` — the agent keeps no exec machinery and no workspace-path. The agent binary never imports `internal/workspace` directly — the gRPC client wrapper (`internal/workspace/client`) is the boundary the issue specified, and tools own the wire.

## Open questions

- The agent↔sidecar authentication (pod-local workspace listener today) — with the contract protocol work.
- The shared-sidecar account model — with the multi-tenancy work.
