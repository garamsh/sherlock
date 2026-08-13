# Environment

Governs the environment model: what an environment is, its topology, and its trust boundaries. Rationale: ADR 0018, ADR 0008, ADR 0009, ADR 0010.

## Current decisions

- The environment is split into two layers: the workspace (the agent's data plane) and the tools (the agent's capabilities). The workspace is a dedicated sidecar container in the agent's pod; the tools are scripts the operator installs under `tools/<name>/` and the platform runs over stdio (ADR 0018, ADR 0009). One pod per agent; one workspace per agent to start.
- The sidecar is the computer: it serves the gRPC workspace contract — Read, Write, List, Exec — over a loopback listener. It holds no tool servers and no `tools:` config; its executor (`internal/workspace/shell`) runs agent-influenced processes in the workspace directory (ADR 0008). The agent executes nothing; the model reaches exec through the `shell_exec` script, which calls the workspace's Exec RPC (ADR 0018).
- Tools are scripts with manifests (ADR 0009). The platform specifies only the contract shape and the call protocol; the tool owns its description, validation, behavior, and domain security. Adding a capability is dropping a `tools/<name>/` directory: a `manifest.json` and an executable. No Go import, no protocol implementation, no sidecar config change.
- The workspace is configured from outside, like a real working volume: the workspace directory is created on first use, the listener bind address is operator-supplied, and the agent learns the listener's address from its own config (`--workspace-addr`, default `127.0.0.1:8081`).
- The workspace directory is the workspace's own real directory: the workspace serves the typed file API over it and execs children in it, so exec'd programs see workspace files natively — no shared mount and no path-equality trick. The agent holds no workspace-path setting; it only dials the workspace's address.
- Permission enforcement lives at the data plane — the action site for files and processes. Workspace writes run under the sidecar's own UID and workspace ownership is kernel-enforced (DAC), not filtered (ADR 0008, ADR 0010). A privileged workspace chowns the workspace directory to the exec UID (`exec-uid`, default 1001), so files written by tools through the gRPC contract and by exec'd children share one owner; an unprivileged workspace owns the directory itself. A platform that cannot run a child as the configured exec UID reports `agent_uid unavailable` instead of silently upgrading (ADR 0008).
- Exec children run under the workspace's trust boundary (ADR 0008, ADR 0010): dropped to the exec UID (workspace key `exec-uid`), bounded per call (`exec-timeout`, default 30s) and by rlimits, with a scrubbed environment and a fresh, empty network namespace. Single-account mode applies unchanged: exec-uid equal to the workspace's own UID needs no privilege; any other unprivileged value fails closed.
- Egress control lives at the pod network layer — the deployment-level boundary (the infrastructure layer's mechanism, not an artifact this repo ships). Process-level network isolation for exec children is the per-request netns above.
- Self-destruction is bounded by construction: files live on the workspace, and the agent's self (a separate container) is unreachable from it. A broken workspace is recreated.
- A shared workspace serving several agents — one volume with several user accounts — is a later step, not the starting shape.

## Current state

The pod split is two processes today. `gagent workspace` serves one workspace (config key, created on first use) over the gRPC contract — Read, Write, List, Exec. It owns no tools and no script-tool runtime, but it owns the executor: Exec children run in the workspace directory, dropped to the exec UID, and when privileged the workspace chowns the directory to the same UID so both writers share one owner. The shipped images make this concrete: the agent image hosts the reasoning loop, gateway, memory, and the tool client — it executes nothing and hosts no tool behavior, so it stays distroless nonroot with no shell; the sidecar image hosts the workspace gRPC server with a shell (Exec children are `sh -c` sub-processes), the uid-1001 exec account, and exactly the capabilities the executor needs — SETUID, SETGID, CHOWN, SYS_ADMIN (`deploy/local/`).

`gagent agent` holds the reasoning loop, gateway, memory, and the platform tool client (`internal/tool`). The agent's tool set is the operator's `tools/` directory — each tool is a `manifest.json` plus an executable. The platform's stdio runner spawns the executable on demand. Tools that need the workspace (files, shell_exec) hold the workspace gRPC client themselves; the agent binary never imports `internal/workspace` directly. The agent's only workspace setting is `workspace-addr` — the agent keeps no exec machinery and no workspace-path.

## Open questions

- The agent↔sidecar authentication (pod-local workspace listener today) — with the contract protocol work.
- The shared-sidecar account model — with the multi-tenancy work.
- A remote tool form (e.g., a hosted MCP server or HTTP tool) when an external integration needs one. The current tool shape is stdio-only; an external transport fields (HTTP, gRPC) lands when the first real use case arrives, not before.