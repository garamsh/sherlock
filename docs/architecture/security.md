# Security

Governs the security model: trust domains, secret custody, and egress. Rationale: ADR 0018, ADR 0003, ADR 0005, ADR 0010, ADR 0013, ADR 0015.

## Current decisions

- Two containers, two trust postures (ADR 0018):
  - **Agent container** — holds the secrets (model and channel credentials) and the memory store. No untrusted code ever runs here; the agent's tools are its own Go packages.
  - **Sidecar (the workspace)** — serves the filesystem-only gRPC data plane. It enforces the agent's permissions at the action site with Linux users and modes; its service account holds only what its tools need.
- Secrets never materialize in the execution domain; exposure is prevented by absence, not filtering (ADR 0003). Tool output schemas never include credential fields, and secrets never enter the conversation context.
- Each tool's implementation enforces its own domain's policy; no separate security layer exists (ADR 0005). Guard models are defense-in-depth, never a boundary.
- Image hygiene stays a security property: minimal images, no unneeded setuid binaries, dropped capabilities.
- External access is tool-routed (ADR 0014 restated for the pod): the mouth (agent-side channels) and the tools the agent hosts are the only outbound paths; the pod network layer is additional to the in-process network namespace, which remains the in-tool boundary for any network-isolated children the executor spawns.
- Callers authenticate to the gateway with mTLS (ADR 0013). Caller authorization and capability granting are the orchestration layer's concern (ADR 0015); capability permissions are enforced at the sidecar, the action site.
- Self-destruction is bounded by construction (ADR 0018): executed code can reach only the workspace, which is recreated — never the agent's self.
- A shared sidecar means multi-tenant execution — per-agent accounts on one workspace — and is deferred until the dedicated-sidecar baseline exists.

## Current state

Two processes per agent today. The agent process (`gagent agent`) runs no untrusted code: the memory-UID custody of `memory.db` is dissolved (ADR 0018), and its gateway enforces mTLS per ADR 0013 — the TLS key and both config files must be owner-only, and the gateway refuses a group/other-readable key or config file at startup. The sidecar (`gagent workspace`) is the data plane: the workspace is chowned to the sidecar's own UID at startup so files the workspace writes share one owner. Children that a tool spawns through `internal/shell.Executor` drop to the configured `exec-uid` (a privileged agent chowns nothing extra — the executor's `AgentUID` matches the agent's own euid, the single-account mode); a privileged agent chowns the agent UID's tooling tree so shell children share the same owner. The agent can request `NoNetwork` on a per-call basis; the executor then drops the child into a fresh, empty network namespace. A platform that cannot drop privileges reports `agent_uid unavailable` instead of silently upgrading (ADR 0008, ADR 0010). Child processes start from a minimal scrubbed environment; a tool's `Request.Env` entries apply on top.

The sidecar's gRPC listener is unauthenticated in this phase, so it serves loopback binds only — the same fail-closed posture as the gateway (ADR 0013): an unauthenticated gRPC listener is a remote shell into the workspace, and a non-loopback addr fails startup.

## Rejected alternatives

- **Filters or guard models as the boundary** — probabilistic and bypassable by obfuscation; boundaries are structural only.
- **In-body secret substitution (shell wrapper, FUSE, LD_PRELOAD)** — a materialized secret is a stolen secret.
- **Shared sidecar from the start** — multi-tenant execution security before the dedicated baseline exists.
- **A runtime plugin loader for tools** — runtime pluggable tools complicate trust (who authored this tool? what UID does it run as?) without buying anything the compile-time import model cannot deliver.

## Open questions

- Authentication between the agent container and its sidecar (how the sidecar knows the caller is its pod's agent) — the gRPC listener ships pod-local and unauthenticated until this is decided.
- The shared-sidecar account model — with the multi-tenancy work.
