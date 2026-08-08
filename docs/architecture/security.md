# Security

Governs the security model: trust domains, secret custody, and egress. Rationale: ADR 0018, ADR 0003, ADR 0005, ADR 0010, ADR 0013, ADR 0015.

## Current decisions

- Two containers, two trust postures (ADR 0018):
  - **Agent container** — holds the secrets (model and channel credentials) and the memory store. No untrusted code ever runs here.
  - **Sidecar (the computer)** — runs agent-influenced code and owns the workspace. It enforces the agent's permissions at the action site with Linux users and modes; its service account holds only what its tools need.
- Secrets never materialize in the execution domain; exposure is prevented by absence, not filtering (ADR 0003). Tool output schemas never include credential fields, and secrets never enter the conversation context.
- Each tool's implementation enforces its own domain's policy; no separate security layer exists (ADR 0005). Guard models are defense-in-depth, never a boundary.
- Image hygiene stays a security property: minimal images, no unneeded setuid binaries, dropped capabilities.
- External access is tool-routed (ADR 0014 restated for the pod): the mouth (agent-side channels) and the computer's tool servers are the only outbound paths; the pod network layer replaces the in-process network namespace.
- Callers authenticate to the gateway with mTLS (ADR 0013). Caller authorization and capability granting are the orchestration layer's concern (ADR 0015); capability permissions are enforced at the sidecar, the action site.
- Self-destruction is bounded by construction (ADR 0018): executed code can reach only the computer, which is recreated — never the agent's self.
- A shared sidecar means multi-tenant execution — per-agent accounts on one computer — and is deferred until the dedicated-sidecar baseline exists.

## Current state

One container today. UID trust domains apply only when the platform binary runs privileged (setuid-root): `memory.db` is chowned to the memory UID with mode 0600, `workspace/` to the agent UID, and `shell_exec` and script-tool children drop to the agent UID — the shell additionally in a fresh empty network namespace, while script tools keep network as operator-sanctioned egress. The shipped distroless nonroot image cannot drop privileges, so those paths report `agent_uid unavailable` under it. The gateway enforces mTLS per ADR 0013, and its TLS key and the platform config must be 0600 owner-only — the gateway refuses a group/other-readable key at startup. Child processes start from a minimal scrubbed environment; a script tool's `exec.env` entries are the explicit channel for additional variables. The two-container split is pending with the ADR 0018 migration.

## Rejected alternatives

- **Filters or guard models as the boundary** — probabilistic and bypassable by obfuscation; boundaries are structural only.
- **In-body secret substitution (shell wrapper, FUSE, LD_PRELOAD)** — a materialized secret is a stolen secret.
- **Shared sidecar from the start** — multi-tenant execution security before the dedicated baseline exists.

## Open questions

- Authentication between the agent container and its sidecar (how the sidecar knows the caller is its pod's agent) — decided with the contract protocol.
- The shared-sidecar account model — with the multi-tenancy work.
