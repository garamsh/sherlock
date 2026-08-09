# Environment

Governs the environment model: what an environment is, its topology, and its trust boundaries. Rationale: ADR 0018, ADR 0008, ADR 0010.

## Current decisions

- The environment is split into two layers: the workspace (the agent's data plane) and the tools (the agent's capabilities). The workspace is a dedicated sidecar container in the agent's pod; the tools are agent-side Go packages the agent imports at compile time. One pod per agent; one workspace per agent to start (ADR 0018).
- The sidecar serves only the filesystem-only gRPC workspace contract — Read, Write, List — over a loopback listener. It holds no tool servers, no process executor, and no `tools:` config: it is a pure data plane. Process exec is the agent's native capability (ADR 0018), surfaced as the `shell_exec` tool over `internal/shell`. The plugin concept is dropped — tools are Go packages the agent imports.
- The workspace is configured from outside, like a real working volume: the workspace directory is created on first use, the listener bind address is operator-supplied, and the agent learns the listener's address from its own config (`--workspace-addr`, default `127.0.0.1:8081`). Adding a tool is a Go-package change in the agent build, not a sidecar config change.
- The workspace directory is a real directory shared by both processes: the workspace serves the typed file API over it, and the agent's exec children chdir into it directly (`--workspace-path`, default `data/workspace`, matching the workspace's own default). Locally both processes use the same path; in a pod a shared volume mounts at the same path in both containers.
- Permission enforcement lives at the data plane — the action site for files. Workspace writes run under the sidecar's own UID and workspace ownership is kernel-enforced (DAC), not filtered (ADR 0008, ADR 0010). A privileged workspace chowns the workspace directory to the agent's exec UID (`exec-uid`, default 1001), so files written by tools through the gRPC contract and by exec'd children share one owner; an unprivileged workspace owns the directory itself. A platform that cannot run a child as the configured exec UID reports `agent_uid unavailable` instead of silently upgrading (ADR 0008).
- Exec children run under the agent's trust boundary (ADR 0008, ADR 0010): dropped to the exec UID (`--exec-uid`), bounded per call (`--exec-timeout`, default 30s) and by rlimits, with a scrubbed environment and a fresh, empty network namespace. Single-account mode applies unchanged: exec-uid equal to the agent's own UID needs no privilege; any other unprivileged value fails closed.
- Egress control lives at the pod network layer — the deployment-level boundary (the infrastructure layer's mechanism, not an artifact this repo ships). Process-level network isolation for exec children is the per-request netns above.
- Self-destruction is bounded by construction: files live on the workspace, and the agent's self (a separate container) is unreachable from it. A broken workspace is recreated.
- A shared workspace serving several agents — one volume with several user accounts — is a later step, not the starting shape.

## Current state

The pod split is two processes today. `gagent workspace` serves one workspace (config key, created on first use) over the filesystem-only gRPC contract — Read, Write, List. It owns no tools, no exec backend, and no script-tool runtime. The workspace spawns no children; when privileged it chowns the workspace directory to the exec UID so both writers share one owner. The shipped images make this concrete: the agent image hosts the reasoning loop, gateway, memory, tools (Go packages), and the native exec backend — it carries a shell and runs privileged enough to setuid, since shell_exec children are `sh -c` sub-processes dropped to the exec UID; the sidecar image hosts the workspace gRPC server with no setuid binaries and drops all capabilities except CAP_CHOWN (`deploy/local/`).

`gagent agent` holds the reasoning loop, gateway, memory, and the tools the agent imports at compile time. The compile-time registry is the unit: `cmd/gagent/agent.go` imports `tools/files`, `tools/playwright`, and `tools/shell_exec`, dials the workspace gRPC client with the configured endpoint, and constructs the tools. shell_exec runs over the agent's native executor (`internal/shell`) with the configured `exec-uid`, `exec-timeout`, and `workspace-path`; its children chdir into the shared workspace directory and see the files the workspace serves. The agent binary never imports `internal/workspace` directly — the gRPC client wrapper (`internal/workspace/client`) is the boundary the issue specified, and tools own the wire.

## Open questions

- The agent↔sidecar authentication (pod-local workspace listener today) — with the contract protocol work.
- The shared-sidecar account model — with the multi-tenancy work.
