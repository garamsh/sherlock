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

Two processes per agent today. The agent process (`gagent agent`) runs no untrusted code: the memory-UID custody of `memory.db` is dissolved (ADR 0018), and its gateway enforces mTLS per ADR 0013 — the TLS key and the agent config must be 0600 owner-only, and the gateway refuses a group/other-readable key at startup. The sidecar (`gagent sidecar`) owns the UID trust domains: children run as the configured `exec-uid` — a privileged sidecar chowns `workspace/` to it and drops `shell_exec` and script-tool children to it, the shell additionally in a fresh empty network namespace, while script tools keep network as operator-sanctioned egress. An `exec-uid` equal to the sidecar's own UID is the explicitly declared single-account mode: the DAC axis between child and sidecar intentionally does not hold, the network isolation holds through an unprivileged user+network namespace, and any other unprivileged `exec-uid` reports `agent_uid unavailable`. Child processes start from a minimal scrubbed environment; a script tool's `exec.env` entries are the explicit channel for additional variables, with a bare `KEY` passthrough form that resolves from the sidecar's environment at spawn — the form secrets must take, since the value then never sits in the config file. In single-account mode the custody story is: passthrough for every secret (a same-account child can read the config file) plus a non-dumpable sidecar (a same-account child cannot read its `/proc/<pid>/environ`). One residual is documented, not fixed: a tool child's own environ is raceable by same-account processes while the child runs, and the dumpable flag lifts for the fork window of each user-namespace spawn (the child's map writes require it) — closing either structurally is the privileged drop's job (children run as a different UID); closing them unprivileged needs a pid-namespace init helper, which is out of proportion. The sidecar's contract listener is unauthenticated in this phase, so it serves loopback binds only — the gateway's fail-closed posture (ADR 0013) applied to the computer: a non-loopback bind fails startup.

## Rejected alternatives

- **Filters or guard models as the boundary** — probabilistic and bypassable by obfuscation; boundaries are structural only.
- **In-body secret substitution (shell wrapper, FUSE, LD_PRELOAD)** — a materialized secret is a stolen secret.
- **Shared sidecar from the start** — multi-tenant execution security before the dedicated baseline exists.

## Open questions

- Authentication between the agent container and its sidecar (how the sidecar knows the caller is its pod's agent) — the contract listener ships pod-local and unauthenticated until this is decided.
- The shared-sidecar account model — with the multi-tenancy work.
