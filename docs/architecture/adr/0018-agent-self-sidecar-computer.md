# ADR 0018: The agent is the self; the workspace is the data plane — pod-per-agent, and tools are independent servers behind a platform protocol

> Status: accepted
> Date: 2026-08-09 (revised in place 2026-08-13)

## Context

Two pressures outgrew the original single-container model, in which trust domains were separated by Unix UIDs inside one container:

- **Extensibility.** A capability joined either as Go code in the platform binary (a rebuild) or as a config-registered script tool the platform still owned. Many users must contribute capabilities — browser automation, GitHub tooling, external APIs — without forking or rebuilding the platform, and without configuring the platform and the capability's home separately.
- **Security placement.** Authority and permissions belong to the environment and Linux permissions, not to in-process trust domains inside the agent's own process. The single container accepted self-destruction inside the agent UID domain as a risk; moving execution out of the agent's container blocks it by construction.

The single-container record itself anticipated this move: "The pod/sidecar topology remains a documented hardening option for high-value deployments; the trust-domain model is unchanged, only the physical split moves." The former records for the body model, the single-container topology, the no-external-loader rule, and config-registered script tools are removed; this record absorbs their live content.

The follow-up #249/#250 work added a gRPC computer contract that bundled filesystem and process exec behind one service. The sidecar's tool-hosting role was removed and process exec was settled on the workspace: the workspace is the computer (filesystem + exec), the agent executes nothing, and the plugin concept was dropped in favor of tools-as-packages imported at compile time.

The follow-up #255 work closed the tools-as-packages loop: `tools/files`, `tools/playwright`, `tools/shell_exec` were in-process Go packages; the gRPC workspace client was the boundary tools use to reach the workspace; the tool contract was described as an agent-facing wire protocol (`ListTools`/`Call`) even though the in-process implementation short-circuited it.

The remaining pressure on the tools-as-packages shape was threefold:

- **Contribution friction.** Every contribution requires a Go import and a rebuild of the platform binary; the agent binary is no longer the right unit of evolution for capabilities.
- **Responsibility mixing.** The platform was asked to be the runtime registry, the trust gate, and the wire protocol at once — and to load user code into its own address space. Each of those is a separable concern.
- **Contract drift between docs and code.** The tool contract was documented as an external wire surface (`ListTools`/`Call`) while the implementation was an in-process Go interface; nothing matched the documented shape, and the platform could neither verify descriptors it did not fetch nor isolate behavior it did not own.

The platform is asked to specify *only the protocol*: how a tool advertises itself and how the agent calls it. Everything inside the tool — its inputs, outputs, validation, and behavior, including any external API it chooses to talk to — is the tool's own decision. This puts the responsibility split in the only place it can hold: the platform owns the protocol, the boundary, the policy, and the registry of installed tools; the tool owns its description, its validation, and its work.

## Decision

One pod per agent, holding two containers. Tools are independent servers behind one platform-owned protocol; the workspace is the data plane.

### Pod

- **The agent container is the agent's self.** It holds the gateway (the agent's ears and eyes), the reasoning loop, memory (the agent's long-term organ — a layer, never a tool), the mouth (outbound messaging), and **the tool client** (the platform's protocol client over `internal/tool` — discovery, call, validation, policy). The agent constructs nothing tool-shaped at compile time and discovers tools at runtime from its operator-supplied list. The agent executes nothing and owns no tool behavior.
- **The sidecar container is the workspace — the computer.** It serves the gRPC contract — Read, Write, List, Exec — over a loopback listener. It holds no tool servers and no `tools:` config. Tools that need files or exec call the workspace gRPC client; tools that need an external API call it directly. Process exec is the workspace's own: the executor (`internal/workspace/shell/`) runs children dropped to the configured exec UID in the workspace directory (ADR 0008), and the model reaches exec through the `shell_exec` tool, which calls the workspace's Exec RPC.

### Tool contract

A tool is an **independent server** behind one platform-owned protocol. The platform specifies only the protocol; everything else is the tool's choice.

| Concern | Owner |
|---|---|
| Wire protocol (descriptor fetch, call, error shape) | Platform |
| Installation list (which tools are present) | Operator (config) |
| Trust posture (boundary validation, permissions, secret scoping, drift pinning) | Platform |
| Self-description (name, description, input/output schema, usage instructions) | Tool |
| Input validation, output conformance | Tool (primary); Platform (boundary re-validation) |
| Behavior — external API calls, local compute, workspace calls, anything else | Tool |
| Domain security policy | Tool (ADR 0005) |

The platform exposes one protocol client — discovery fetches the tool's descriptor and schema from the tool itself; the platform pins the descriptor hash and refuses drift at startup. The tool's self-description is untrusted input; the platform validates every fetched descriptor at the boundary and quarantines any tool whose schema fails to parse or whose output violates its declared output schema. Strict schema enforcement at the model API layer (constrained decoding when the provider supports it, `parallel_tool_calls: false` because strict is incompatible with parallel calls) prevents the model from emitting arguments the schema rejects.

There is no compile-time tool registry, no runtime plugin loader, and no remote tool registry. A capability is added by installing a tool server, declared in the operator's tool list, and (for first-party tools we ship) checked in as a Go binary in this repository. The platform's `internal/tool` package is the protocol client: descriptor discovery, call dispatch, schema validation, permission policy, and drift pinning.

### Workspace contract (unchanged)

- **Permission enforcement lives at the data plane.** Workspace writes are kernel-enforced (DAC), not filtered (ADR 0008, ADR 0010 scoped to the workspace). Process exec is the workspace's own: children drop to the configured exec UID and run in the workspace directory.
- Sidecars start dedicated, one per agent. A shared sidecar serving several agents is a later step.

### What tools look like in this repository

- **First-party tools** we ship (`files`, `shell_exec`, `message_send`, `playwright`, and any new tool the architecture implies, including operator-sanctioned external APIs like `github`) are Go binaries that live under this repository, expose the platform protocol, and are installed via the operator's tool list. They are not imported by the agent binary; they are spawned (stdio) or reached (HTTP) by the platform's client.
- **Third-party tools** follow the same protocol and install the same way; nothing in their treatment distinguishes them structurally from first-party tools.

### Required tools

The platform contract tells the model to use specific tools (e.g., `message_send` for replies). Required tools are an invariant of the platform, not an implementation in the platform binary. The platform validates the required tool set at startup; a missing required tool fails closed.

## Consequences

- Self-destruction is bounded by construction: an attacker who reaches the workspace can read, write, list, and exec inside the workspace's trust domain — bounded by the exec UID's DAC and the network namespace — but the agent's self is unreachable from the sidecar.
- Adding a capability is an installation, not a rebuild. The platform binary evolves independently of the tool set; the operator chooses which tools are present.
- The workspace contract remains a wire protocol (descriptor plus call) for files and exec; the tool contract becomes an actual wire protocol (descriptor plus call) for tools. Two protocols, two audiences. The tool contract is the platform's external surface for tools; the agent binary never imports a tool's package.
- The responsibility split is structural: where today the platform must reach into every tool to validate, version, and privilege, the platform now reaches the protocol and the tool self-serves everything else. Each tool is reviewable on its own; the platform is reviewable on its own.
- Secret-bearing tools (operator-sanctioned external APIs) hold their credentials in their own process env, scoped per tool by operator config. The platform does not become a secret dispatcher (ADR 0003, ADR 0014); secrets never materialize in the execution domain (ADR 0008 scrubbed exec env is unchanged).
- `memory.db` remains in the agent container; the per-agent SQLite store and its store interface (ADR 0002) carry forward. Memory is still a layer, never a tool.
- Caller authorization stays in the orchestration layer (ADR 0015); workspace permission enforcement is at the sidecar, the action site for files.
- The deployment grows from one binary plus a directory tree to a pod with two containers, two volumes (memory, workspace), and a tool set. Existing packages re-wire by moving, not by layering: `internal/agent` and `internal/memory` stay in the agent binary; `internal/workspace` stays in the sidecar; the tool packages move from `tools/` imports to `cmd/<tool>/` binaries (or third-party equivalents); the platform's tool client replaces the in-process registry.
- The protocol is the only thing we standardize. The tool's internal shape — language, libraries, internal structure — is the tool author's choice.

## Non-goals

The workspace is explicitly not:

- **A tool host.** Tools live in independent processes outside both containers; the workspace exposes no tool registry and no tool call surface.
- **A plugin loader.** There is no runtime mechanism to register a new tool against the workspace.

The platform is explicitly not:

- **A tool behavior host.** Tools execute in their own processes; the platform neither executes their behavior nor hosts their code.
- **A secret dispatcher.** Per-tool secret scoping is operator config; the platform forwards, it does not store.

The tool contract is explicitly not:

- **A workspace.** It is the platform's external surface for tools.
- **Used by the agent to reach the workspace.** The agent constructs nothing tool-shaped; tools own the wire to the workspace.

## Rejected alternatives

- **Keep tools-as-packages (the previous shape).** The contract docs describe an external wire protocol; the code is an in-process interface; nothing matches. Contribution still requires a rebuild. Rejected by the contribution-friction and contract-drift pressure above.
- **Schema-only tools (declarative descriptors with no behavior).** A descriptor alone cannot carry behavior; thin API proxies are the only class that fits. Shell exec, browser automation, delivery-with-fallback, and any future behavior-bearing tool cannot live in such a shape. Rejected for missing the general case.
- **Custom REST contract on top of the existing `ListTools`/`Call` ADR-0018-vestige.** We would design and document our own protocol that no one else speaks; every contributor learns our bespoke API; zero ecosystem. Rejected for the same reason ADR 0014 rejected an egress proxy.
- **gRPC for the tool contract.** Protobuf gives strong typing on the wire but produces two schema systems (proto and the JSON Schema the model needs), adds a protoc toolchain to every contributor, and offers none of its strengths (streaming, binary efficiency) for small JSON tool calls. Rejected: it is the wrong layer — gRPC is correctly placed at the workspace data plane.
- **Treating the platform as a generic tool executor (OpenAPI-style declarative binding).** A platform that interprets declarative bindings becomes a workflow engine with embedded auth, response mapping, pagination, and error mapping; the executor becomes a DSL. Secrets then live in the platform to be forwarded, which violates ADR 0003's and ADR 0014's structural answer (a secret is a stolen secret). Rejected for restoring the same complexity the architecture removed.
- **Keep exec agent-native** — the agent container then carries the shell, the privilege drop, and the untrusted-code surface, and exec'd programs see workspace files only through a shared-mount path-equality trick; the workspace owning exec resolves file visibility natively (ADR 0008). The remote-shell exposure of the Exec RPC is real and is why the listener stays loopback-only until contract authentication exists (ADR 0013).
- **Let the agent import `internal/workspace` directly** — collapses the boundary: tools own the wire to the workspace. The agent constructs nothing tool-shaped; the agent binary never calls the workspace gRPC client itself.
- **Runtime tool self-registration by any user.** Would put arbitrary code next to platform secrets. Operator-installed, by config, is the only shape that preserves the trust posture.