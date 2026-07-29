# Environment

Governs the environment model: what an environment is, its topology, and its trust boundaries. Rationale: ADR 0002, ADR 0006.

## Current decisions

- The environment is the agent's body. Code the agent runs shares fate with the agent; there is no separate sandbox tier. `sandbox_exec` means "execute inside my own environment."
- One container per environment, subdivided into trust domains by Unix UIDs:
  - **Agent UID** — runs all agent-supplied code; unprivileged, always.
  - **Memory UID** — owns `memory.db`.
  - **Runtime UID** — owns tool runtime configuration.
- Enforcement is kernel DAC (file ownership and modes), not filters. Agent code touching another UID's assets gets `EACCES`.
- A tool runtime (supervisor) manages processes and drops privileges before executing agent-supplied code.
- Boundary policy — egress, CPU/memory quotas, mount scope, lifecycle — is injected from outside at environment creation. The agent cannot change it.
- Direct network egress for the agent UID is blocked; the only outbound path is the egress tool (see `security.md`).
- Each environment owns `data/envs/<env-id>/` (workspace + `memory.db`). The directory is the isolation unit: teardown, migration, and backup are directory operations.
- MVP container boundary: the environment container mounts only `workspace/` (never `memory.db`), has no network egress, and gets its CPU/memory quotas from a policy resolved at startup and injected at creation (`cpu-limit` / `memory-limit`; see the README config table). Quotas default to bounded values and cannot be disabled — an out-of-range quota fails startup, so no environment container runs unlimited. UID separation is not yet enforced (see open questions).
- The pod/sidecar topology is a documented hardening option for high-value deployments; the trust-domain model is unchanged.
- The platform itself ships as a distroless nonroot image built from the repo-root `Dockerfile` — the platform image, distinct from the environment image (the agent's body), whose definition remains an open question below.

## Rejected alternatives

- **Two-tier sandbox (PLAN §4 original reading)** — superseded by the body model (ADR 0002).
- **Container-in-container (DinD)** — requires a privileged outer container, which would then also hold the secrets; weakens the host boundary for no gain over sibling or UID separation.
- **Flat single-UID container** — no structural protection for platform assets inside the environment.

## Open questions

- UID separation mechanics (setuid supervisor): target design per ADR 0006; the MVP runs without it.
- Environment image definition and hygiene checklist (no setuid binaries, minimal capabilities): required by ADR 0006, not yet written.
