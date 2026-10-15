# ADR 0017: The agent is the self; the workspace is the data plane — pod-per-agent, and tools are scripts with manifests

> Status: accepted
> Date: 2026-08-09 (revised in place 2026-08-13; 2026-08-14; 2026-08-17; 2026-09-25)

## Contents
- Context
- Decision
- Consequences
- Non-goals
- Rejected alternatives

## Context

Two pressures outgrew the original single-container model, in which trust domains were separated by Unix UIDs inside one container:

- **Extensibility.** A capability joined either as Go code in the platform binary (a rebuild) or as a config-registered script tool the platform still owned. Many users must contribute capabilities — browser automation, GitHub tooling, external APIs — without forking or rebuilding the platform, and without configuring the platform and the capability's home separately.
- **Security placement.** Authority and permissions belong to the environment and Linux permissions, not to in-process trust domains inside the agent's own process. The single container accepted self-destruction inside the agent UID domain as a risk; moving execution out of the agent's container blocks it by construction.

The single-container record itself anticipated this move: "The pod/sidecar topology remains a documented hardening option for high-value deployments; the trust-domain model is unchanged, only the physical split moves." The former records for the body model, the single-container topology, the no-external-loader rule, and config-registered script tools are removed; this record absorbs their live content.

The follow-up #249/#250 work added a gRPC computer contract that bundled filesystem and process exec behind one service. The sidecar's tool-hosting role was removed and process exec was settled on the workspace: the workspace is the computer (filesystem + exec), the agent kept no exec path, and the plugin concept was dropped in favor of tools-as-packages imported at compile time.

The remaining pressure on the tools-as-packages shape was threefold:

- **Contribution friction.** Every contribution requires a Go import and a rebuild of the platform binary; the agent binary is no longer the right unit of evolution for capabilities.
- **Responsibility mixing.** The platform was asked to be the runtime registry, the trust gate, and the wire protocol at once — and to load user code into its own address space. Each of those is a separable concern.
- **Contract drift between docs and code.** The tool contract was documented as an external wire surface (`ListTools`/`Call`) while the implementation was an in-process Go interface; nothing matched the documented shape, and the platform could neither verify descriptors it did not fetch nor isolate behavior it did not own.

The platform is asked to specify *only the protocol*: how a tool advertises itself and how the agent calls it. Everything inside the tool — its inputs, outputs, validation, and behavior, including any external API it chooses to talk to — is the tool's own decision. This puts the responsibility split in the only place it can hold: the platform owns the protocol, the boundary, the policy, and the registry of installed tools; the tool owns its description, its validation, and its work.

## Decision

One pod per agent, holding two containers. Tools are scripts with manifests behind one platform-owned contract; the workspace is the data plane.

### Pod

- **The agent container is the agent's self.** It holds the gateway (the agent's ears and eyes), the reasoning loop, memory (the agent's long-term organ — a layer, never a tool), the mouth (outbound messaging), and **the tool client** (the platform's protocol client over `internal/tool` — discovery, call, validation, policy). The agent constructs nothing tool-shaped at compile time and discovers tools at runtime from its operator-supplied list. The agent owns no tool behavior: a tool's behavior lives in its executable, which the tool client spawns as a separate process per call, never in the agent's own process. The agent has no Exec path — it runs no command the model supplies; that path is the workspace's.
- **The sidecar container is the workspace — the computer.** It serves the gRPC contract — Read, Write, List, Exec — over a loopback listener. It holds no tool servers and no `tools:` config. Tools that need files or exec call the workspace gRPC client; tools that need an external API call it directly. Process exec is the workspace's own: the executor (`internal/workspace/shell/`) runs children dropped to the configured exec UID in the workspace directory (ADR 0008), and the model reaches exec through the `shell_exec` tool, which calls the workspace's Exec RPC.
- **Two images, one per container.** An image is a set of files, and everything running in the container inherits them, so what an image ships is the reach available inside it. The agent's image is the one holding the secrets and the memory store, so it ships the platform binary and the operator-installed tool executables it spawns, and no shell and no interpreter. The workspace's image ships the shell its Exec path runs. The tool executables the agent image carries are versioned with the agent commit and live under `/opt/sherlock/tools/<name>/` in the image; a third image (`sherlock/tools`) is not built, not pushed, and not consumed (ADR 0021).

### Tool contract

A tool is a **script with a manifest**: a `manifest.json` that declares the contract and an executable the platform spawns per call. The platform specifies only the manifest format and the stdio call shape; everything else is the tool's choice.

| Concern | Owner |
|---|---|
| Contract shape (manifest schema and the call protocol) | Platform |
| Installation list (which tools are present, and the manifest hash accepted for each) | Operator (config) |
| Boundary validation (manifest parse, input/output schema check, drift pin) | Platform |
| Self-description (name, description, input/output schema, executable) | Tool author (manifest) |
| Input validation, output conformance | Tool (primary); Platform (boundary re-validation) |
| Behavior — external API calls, local compute, workspace calls, anything else | Tool |
| Domain security policy | Tool (ADR 0005) |

The platform owns the call contract — manifest format and the single stdio invocation shape — and the boundary posture (validation, drift pin, required-tool check). The tool author owns the contract surface (the manifest) and the implementation. The platform discovers the tool by reading the manifest at startup, hashes it, and refuses to load a tool whose hash drifts from the operator's pin or that the operator's pin set does not name: the installation list is the operator's, and so is the record of what each manifest said when they accepted it. The tool's self-description is untrusted input at the boundary; the platform validates every fetched descriptor at the boundary, refuses to load a tool whose schema fails to parse, and fails the call of a tool whose output violates its declared output schema — dropping the output rather than letting it reach the model, and leaving the tool installed (ADR 0009 invariant 4). Strict schema enforcement at the model API layer (constrained decoding when the provider supports it, `parallel_tool_calls: false` because strict is incompatible with parallel calls) prevents the model from emitting arguments the schema rejects.

There is no compile-time tool registry, no runtime plugin loader, no wire protocol, and no remote tool registry. A capability is added by dropping a `tools/<name>/` directory containing a `manifest.json` and an executable. The platform's `internal/tool` package is the manifest loader, the stdio runner, the registry, and the drift pinner.

### Workspace contract (unchanged)

- **Permission enforcement lives at the data plane.** Workspace writes are kernel-enforced (DAC), not filtered (ADR 0008, ADR 0010 scoped to the workspace). Process exec is the workspace's own: children drop to the configured exec UID and run in the workspace directory.
- Sidecars start dedicated, one per agent. A shared sidecar serving several agents is a later step.

### What tools look like in this repository

- **First-party tools** we ship (`file_read`, `file_write`, `file_list`, `shell_exec`, `message_send`, `github_push`, `github_open_issue`, `github_open_pull_request`, `github_comment`, and any new tool the architecture implies) live as `tools/<name>/` directories under this repository: each contains `manifest.json` and an executable that implements the manifest's contract. The language is the tool author's choice. The platform's stdio runner spawns the executable on demand.
- **Third-party tools** follow the same shape; nothing in the platform's treatment distinguishes them structurally from first-party tools.

### Required tools

The platform contract tells the model to use specific tools (e.g., `message_send` for replies). Required tools are an invariant of the platform, not an implementation in the platform binary. The platform validates the required tool set at startup; a missing required tool fails closed.

## Consequences

- Self-destruction is bounded by construction: an attacker who reaches the workspace can read, write, list, and exec inside the workspace's trust domain — bounded by the exec UID's DAC and the network namespace — but the agent's self is unreachable from the sidecar.
- Adding a capability is an installation, not a rebuild. The platform binary evolves independently of the tool set; the operator chooses which tools are present.
- The workspace contract remains a wire protocol (descriptor plus call) for files and exec; the tool contract is a manifest plus one stdio invocation per call, not a wire protocol. Two contracts, two audiences. The tool contract is the platform's external surface for tools; the agent binary never imports a tool's package.
- The responsibility split is structural: where today the platform must reach into every tool to validate, version, and privilege, the platform now reaches the protocol and the tool self-serves everything else. Each tool is reviewable on its own; the platform is reviewable on its own.
- Installed tools inherit the agent environment whole, so every tool can read every credential the agent holds (ADR 0009). Neither manifests nor operator configuration provide per-tool credential scope, and the platform is no secret dispatcher. The separate workspace execution domain remains secret-free (ADR 0003); its scrubbed Exec environment is unchanged (ADR 0008).
- `memory.db` remains in the agent container; the per-agent SQLite store and its store interface (ADR 0002) carry forward. Memory is still a layer, never a tool.
- Caller authorization stays in the orchestration layer (ADR 0014); workspace permission enforcement is at the sidecar, the action site for files.
- The deployment grows from one binary plus a directory tree to a pod with two containers, two volumes (memory, workspace), and a tool set. Existing packages re-wire by moving, not by layering: `internal/agent` and `internal/memory` stay in the agent binary; `internal/workspace` stays in the sidecar; the tool packages move from `tools/` imports to `tools/<name>/` manifests (or third-party equivalents); the platform's script-tool runtime replaces the in-process registry.
- The protocol is the only thing we standardize. The tool's internal shape — language, libraries, internal structure — is the tool author's choice.

## Non-goals

The workspace is explicitly not:

- **A tool host.** Tools run as processes the agent container's tool client spawns; the workspace exposes no tool registry and no tool call surface.
- **A plugin loader.** There is no runtime mechanism to register a new tool against the workspace.

The platform is explicitly not:

- **A tool behavior host.** Tools execute in their own processes; the platform neither executes their behavior nor hosts their code.
- **A secret dispatcher.** The platform forwards the agent environment whole to installed tools; it provides no per-tool credential scope. A credential a tool needs belongs in that shared environment (ADR 0009).

The tool contract is explicitly not:

- **A workspace.** It is the platform's external surface for tools.
- **Used by the agent to reach the workspace.** The agent constructs nothing tool-shaped; tools own the wire to the workspace.

## Rejected alternatives

- **Keep tools-as-packages (the original shape).** The contract docs described an external wire protocol; the code was an in-process interface; nothing matched. Contribution still required a rebuild. Rejected by the contribution-friction and contract-drift pressure above.
- **Tools as protocol servers (the previous revised shape).** MCP-style tools made the tool author write transport code (JSON-RPC framing, request IDs, capabilities, error codes) on top of their domain logic. The transport cost was borne by every contributor, and the platform's protocol client grew a corresponding protocol stack. The owner's preferred shape is the manifest: the contract is declarative, the transport is owned by the platform. The previous PRs (#280, #290, #292, #294, #295, #306, #307) are superseded by this revision once the corresponding code lands, as is #308, an issue rather than a PR, filed against the schema that shape shipped. #293 is not among them: it moved `internal/files` to `internal/workspace/files`, which is where the package sits today.
- **Schema-only tools (declarative descriptors with no behavior).** A descriptor alone cannot carry behavior; thin API proxies are the only class that fits. The script form keeps the descriptor in the manifest and the behavior in the executable; the two halves stay separate.
- **gRPC for the tool contract.** Protobuf gives strong typing on the wire but produces two schema systems (proto and the JSON Schema the model needs), adds a protoc toolchain to every contributor, and offers none of its strengths (streaming, binary efficiency) for small JSON tool calls. Rejected: it is the wrong layer — gRPC is correctly placed at the workspace data plane.
- **HTTP/REST for the tool contract.** The contract would be a custom HTTP shape (`POST /tools/<name>/call`); the model still needs JSON Schema. Two transports for one contract. The stdio shape (one JSON exchange per invocation) is simpler.
- **Treating the platform as a generic tool executor (OpenAPI-style declarative binding).** A platform that interprets declarative bindings becomes a workflow engine with embedded auth, response mapping, pagination, and error mapping; the executor becomes a DSL. Rejected for restoring the same complexity the architecture removed and making the platform a secret dispatcher. The accepted shape instead lets trusted installed tools inherit the agent environment whole (ADR 0009); ADR 0003 keeps credentials out of the separate workspace execution domain.
- **A permissions policy at the platform level.** The tool runs with the operator's process privileges; the operator is the gate, by directory placement. A platform-level policy is a layer the operator already enforces. Rejected by the owner; not in the shape.
- **Keep exec agent-native** — the agent container then carries the shell, the privilege drop, and the untrusted-code surface, and exec'd programs see workspace files only through a shared-mount path-equality trick; the workspace owning exec resolves file visibility natively (ADR 0008). The remote-shell exposure of the Exec RPC is real and is why the listener stays loopback-only until contract authentication exists (ADR 0012).
- **Let the agent import `internal/workspace` directly** — collapses the boundary: tools own the wire to the workspace. The agent constructs nothing tool-shaped; the agent binary never calls the workspace gRPC client itself.
- **Runtime tool self-registration by any user.** Would put arbitrary code next to platform secrets. Operator-installed, by directory placement, is the only shape that preserves the trust posture.