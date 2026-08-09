# ADR 0018: The agent is the self; the workspace is the data plane — pod-per-agent

> Status: accepted
> Date: 2026-08-09

## Context

Two pressures outgrew the original single-container model, in which trust domains were separated by Unix UIDs inside one container:

- **Extensibility.** A capability joined either as Go code in the platform binary (a rebuild) or as a config-registered script tool the platform still owned. Many users must contribute capabilities — browser automation, GitHub tooling, external APIs — without forking or rebuilding the platform, and without configuring the platform and the capability's home separately.
- **Security placement.** Authority and permissions belong to the environment and Linux permissions, not to in-process trust domains inside the agent's own process. The single container accepted self-destruction inside the agent UID domain as a risk; moving execution out of the agent's container blocks it by construction.

The single-container record itself anticipated this move: "The pod/sidecar topology remains a documented hardening option for high-value deployments; the trust-domain model is unchanged, only the physical split moves." The former records for the body model, the single-container topology, the no-external-loader rule, and config-registered script tools are removed; this record absorbs their live content.

The follow-up #249/#250 work added a gRPC computer contract that bundled filesystem and process exec behind one service. A structural correction narrows the contract to the filesystem alone and removes the sidecar's tool-hosting role:

- The workspace is the OS API the tools use, and it is **filesystem only** — Read, Write, List. Process exec is the agent's native capability (`os/exec` via `internal/shell`), not a workspace call. The Exec RPC and the shell-executor dependency are removed from the contract.
- The plugin concept is dropped — tools are agent-side Go packages the agent imports at compile time. The `tools:` sidecar config section, the script-tool subprocess adapter, and `tools/playwright/` as a config-registered tool are superseded by the tools-as-packages model. Multiple users collaborate by adding or removing tool packages in the codebase, not by hot-loading.

The follow-up #255 work closed the loop on tools-as-packages: `tools/files/` is the canonical agent-side files tool, `tools/playwright/` is a Go package (no subprocess wrapping), `internal/tool/script/` is removed, the sidecar's `tools:` config is gone, and the agent's tool loader is a compile-time registry. The gRPC workspace client is the boundary tools use to reach the workspace; the agent binary never imports `internal/workspace` directly.

The architecture has two surfaces with two distinct audiences:

- **Tool contract** (HTTP, agent-facing, the wire protocol `internal/tool` speaks): the agent's hand. `ListTools` / `Call`. The agent and any reachable tool server speak it; the contract is the external (agent-facing) surface.
- **Workspace contract** (gRPC, filesystem-only, the data plane): the OS API tools compose. Tools are the only callers in the destination shape; the agent uses the workspace gRPC client when a tool it hosts needs to read, write, or list files.

## Decision

One pod per agent, holding two containers.

- **The agent container is the agent's self.** It holds the gateway (the agent's ears and eyes), the reasoning loop, memory (the agent's long-term organ — a layer, never a tool), the mouth (outbound messaging), **native process exec** (`os/exec` via `internal/shell`), and **tools** (Go packages imported at compile time from `tools/`). The agent is the only place a capability runs as the agent's own code. The agent constructs tools at startup with the workspace gRPC client it dials from a single `--workspace-addr` config; tools own the wire to the workspace, not the agent.
- **The sidecar container is the workspace.** It serves the filesystem-only gRPC contract — Read, Write, List — over a loopback listener. It holds no tool servers, no process executor, and no `tools:` config: it is a pure data plane. Tools that need files call the workspace gRPC client; tools that need an external API call it directly. There is no `shell_exec` tool — the agent execs natively.
- **A tool is an agent-side Go package** — descriptor, implementation, imported at compile time. There is no runtime plugin loader and no remote tool registry: adding a capability is a Go-package change in the agent's build. The compile-time registry is the unit: `cmd/gagent/agent.go` imports the tool packages it needs (today: `tools/files`, `tools/playwright`) and registers the constructed `tool.Tool` values alongside the in-process mouth.
- **Permission enforcement lives at the data plane.** Workspace writes are kernel-enforced (DAC), not filtered (ADR 0008, ADR 0010 relocated into the sidecar). Process exec is the agent's own and inherits the agent process's UID — operator scope governs the trust posture of the agent process itself.
- Sidecars start dedicated, one per agent. A shared sidecar serving several agents — one workspace with several user accounts — is a later step.

### Non-goals

The workspace is explicitly not:

- **A tool host.** Tools live in the agent. The workspace exposes no tool registry and no tool call surface.
- **A process executor.** The Exec RPC is removed; `shell.Executor` is no longer a workspace backend. Process exec is the agent's native capability.
- **A plugin loader.** There is no runtime mechanism to register a new tool against the workspace; tools are Go packages compiled into the agent binary.

The tool contract is explicitly not:

- **A workspace.** It is the agent's external surface for tool calls — kept as the agent-facing contract. No doc claims the tool contract is gRPC.
- **Used by the agent to reach the workspace.** The agent imports tools; tools own the wire to the workspace. The gRPC workspace client is not part of the agent's surface.

## Consequences

- Self-destruction is bounded by construction at the data plane: an attacker who reaches the workspace can only read, write, or list files inside it — the agent's self is unreachable from the sidecar.
- Adding a capability is a code change in the agent build: a new tool package under `tools/`, imported by the agent's binary. The agent and the capability move together.
- The workspace contract is a wire protocol (descriptor plus call) for files; the tool contract remains a wire protocol for tools. Two protocols, two audiences.
- `memory.db` moves into the agent container; the per-agent SQLite store and its store interface (ADR 0002) carry forward. The memory-UID custody concern dissolves — the agent container hosts memory and the agent's own process exec.
- The UID trust-domain mechanism is narrowed to the data plane: agent-influenced file writes run under the sidecar's own UID, and ownership is kernel-enforced (ADR 0008, ADR 0010 stand inside the sidecar, scoped to the workspace). Egress control moves to the pod network layer; the in-process network namespace is no longer the workspace boundary — it remains the in-tool boundary for tools that ask for a network-isolated child via `shell.Request.NoNetwork`.
- Caller authorization stays in the orchestration layer (ADR 0015); workspace permission enforcement is at the sidecar, the action site for files.
- The gateway serves its own pod's agent only; routing senders to agent pods is the orchestration layer's concern. Turn-drains-queue semantics (ADR 0016) are unchanged per agent.
- The deployment grows from one binary plus a directory tree to a pod with two containers and two volumes (memory, workspace). Existing packages re-wire: `internal/agent` and `internal/memory` into the agent binary, which also gains tool packages under `tools/`; `internal/files` is the workspace's filesystem backend, wrapped by the gRPC server in `internal/workspace`; `internal/workspace/client` is the typed client wrapper tools import; the tool contract, `internal/tool`, stays as the agent's external surface; `internal/shell` is the agent's native exec backend.

## Rejected alternatives

- **Keep the sidecar as a generic computer** — bundles process exec, files, and tool hosting under one label; the new structure clarifies that the sidecar is the data plane and the agent hosts tools and exec.
- **Move the filesystem into the agent** — collapses the data plane into the agent process; the sidecar's job as a separately permissioned workspace volume dissolves.
- **Keep the plugin / script-tool mechanism** — runtime pluggable tools complicate trust (who authored this tool? what UID does it run as?) without buying anything the compile-time import model cannot deliver.
- **Keep the Exec RPC in the workspace** — process exec is the agent's own; a remote exec RPC is an unauthenticated remote shell (ADR 0013) and a layering violation.
- **Let the agent import `internal/workspace` directly** — collapses the boundary the issue specified: tools own the wire to the workspace. The agent constructs tools with a gRPC client it dials from config; the agent binary never calls the workspace gRPC client itself.
