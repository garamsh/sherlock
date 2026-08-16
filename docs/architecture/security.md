# Security

Governs the security model: trust domains, secret custody, and egress. Rationale: ADR 0017, ADR 0009, ADR 0003, ADR 0005, ADR 0010, ADR 0012, ADR 0013, ADR 0014.

## Current decisions

- Two containers, two trust postures (ADR 0017):
  - **Agent container** — holds the secrets (model and channel credentials) and the memory store. Its tool client is the manifest loader and the stdio runner; the agent executes nothing and hosts no tool behavior — the untrusted-code path lives in the workspace.
  - **Sidecar (the workspace)** — serves the gRPC data plane (files and exec) and owns the one untrusted-code path: Exec children drop to the unprivileged exec UID with a scrubbed environment, rlimits, and a fresh network namespace (ADR 0008, ADR 0010). It enforces the agent's permissions at the action site with Linux users and modes.
- Tools are scripts with manifests under the operator's `tools/` directory (ADR 0017, ADR 0009). The platform owns the manifest format, the stdio call protocol, the boundary validation, the per-tool secret scoping, and the drift pin. Tools own their description, validation, behavior, and domain security policy (ADR 0005).
- **Tool trust posture.** Tools installed by the operator run as separate processes spawned by the platform's stdio runner. They execute in the operator's trust domain — the platform never runs arbitrary tool code in the agent's address space — and the tool's process environment carries only the secrets the operator scoped to that tool (the manifest's `env` field). **Free contribution, operator installation**: any user may publish a tool, but only the operator chooses which tool directories are present. Self-registration is not a supported shape.
- **Boundary posture on tool inputs and outputs.** The tool's self-description is untrusted input at the boundary. The platform re-validates fetched descriptors, refuses to load a tool whose schema fails to parse or whose descriptor hash drifts from the pinned value, and quarantines any tool whose output violates its declared output schema. Strict schema enforcement at the model API layer (constrained decoding when the provider supports it) prevents the model from emitting arguments the schema rejects.
- Secrets never materialize in the execution domain; exposure is prevented by absence, not filtering (ADR 0003). Tool output schemas never include credential fields, and secrets never enter the conversation context.
- Each tool's implementation enforces its own domain's policy; no separate security layer exists (ADR 0005). Guard models are defense-in-depth, never a boundary.
- Image hygiene stays a security property: minimal images, no unneeded setuid binaries, dropped capabilities.
- External access is tool-routed (ADR 0013 restated for the pod): the mouth (the message_send tool) and the tools the operator installs are the only outbound paths; the pod network layer is the deployment-level boundary, and shell_exec children run in a fresh, empty network namespace per call (ADR 0010).
- Callers authenticate to the gateway with mTLS (ADR 0012). Caller authorization and capability granting are the orchestration layer's concern (ADR 0014); capability permissions are enforced at the sidecar, the action site.
- Self-destruction is bounded by construction (ADR 0017): executed code can reach only the workspace, which is recreated — never the agent's self.
- A shared sidecar means multi-tenant execution — per-agent accounts on one workspace — and is deferred until the dedicated-sidecar baseline exists.

## Current state

Two processes per agent today. The agent process (`gagent agent`) holds the secrets and the memory store, and its gateway enforces mTLS per ADR 0012 — the TLS key and both config files must be owner-only, and the gateway refuses a group/other-readable key or config file at startup. It executes nothing. The sidecar (`gagent workspace`) is the data plane and owns the untrusted-code path: Exec children run through its executor (`internal/workspace/shell`) dropped to the configured exec UID in the workspace directory, with a scrubbed environment, trust-class rlimits, a per-call timeout, and a fresh, empty network namespace (privileged unshare, or unprivileged userns+netns in single-account mode); a drop the platform cannot perform fails closed with `agent_uid unavailable`, and a resource cap it cannot apply fails closed the same way — no untrusted child runs uncapped (ADR 0008, ADR 0010). In single-account mode the workspace marks itself non-dumpable, so a same-account child cannot read its `/proc/<pid>/environ` (ADR 0003). When privileged the workspace chowns the workspace directory to the exec UID, so tool writes and exec'd children share one owner.

The sidecar's gRPC listener is unauthenticated in this phase, so it serves loopback binds only — the same fail-closed posture as the gateway (ADR 0012): an unauthenticated gRPC listener carrying Exec is a remote shell into the workspace, and a non-loopback addr fails startup.

The agent's tool client (`internal/tool`) implements the platform contract: manifest loading, the stdio call protocol, schema validation, drift pinning. Required tools (the mouth, the workspace-coupling tools) are validated at startup; a missing required tool fails closed. The platform does not mediate permissions at the tool level — the operator is the gate, by directory placement.

## Rejected alternatives

- **Filters or guard models as the boundary** — probabilistic and bypassable by obfuscation; boundaries are structural only.
- **In-body secret substitution (shell wrapper, FUSE, LD_PRELOAD)** — a materialized secret is a stolen secret.
- **Shared sidecar from the start** — multi-tenant execution security before the dedicated baseline exists.
- **A runtime plugin loader for tools (any user self-registers tools at runtime).** Would put arbitrary code next to platform secrets. The contribution shape is "free to publish, operator-installed by directory placement" — that is the only shape that preserves the trust posture while letting any user contribute tools.
- **A permissions policy at the platform level.** The tool runs with the operator's process privileges; the operator is the gate. A platform-level policy adds a layer the operator already enforces by directory placement. Rejected by the owner; not in the shape.
- **Platform as secret dispatcher for tool credentials.** Rejected: a materialized secret is a stolen secret (ADR 0003, ADR 0013). Per-tool env scoping is operator config; the platform forwards.

## Open questions

- Authentication between the agent container and its sidecar (how the sidecar knows the caller is its pod's agent) — the gRPC listener ships pod-local and unauthenticated until this is decided.
- The shared-sidecar account model — with the multi-tenancy work.
- Authentication of installed tool servers and integrity attestation of fetched descriptors — operator-installed tools run with the platform's trust, so this matters when tools come from outside the operator's own build.
