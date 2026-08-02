# Environment

Governs the environment model: what an environment is, its topology, and its trust boundaries. Rationale: ADR 0002, ADR 0006, ADR 0012.

## Current decisions

- The environment is the agent's body. The agent has full authority inside it; the orchestration layer manages the boundary (network, mounts, resources, lifecycle). There is no separate sandbox tier — the platform does not spawn an inner container for the agent to run commands in (ADR 0011).
- One container per environment, subdivided into trust domains by Unix UIDs (target design, ADR 0006):
  - **Agent UID** — runs all agent-supplied code; unprivileged, always.
  - **Memory UID** — owns `memory.db`.
  - **Runtime UID** — owns tool runtime configuration.
- Enforcement is kernel DAC (file ownership and modes), not filters. Agent code touching another UID's assets gets `EACCES`. Current state: only the `shell_exec` sub-process drops to the agent UID, and only when the platform binary runs setuid-root — otherwise the tool reports `agent_uid unavailable` (ADR 0012). `memory.db` is still opened by the platform process; memory/runtime UID separation is not yet enforced.
- A tool runtime (supervisor) manages processes and drops privileges before executing agent-supplied code.
- Boundary policy — egress, CPU/memory quotas, mount scope, lifecycle — is injected from outside at environment creation (target design). Current state: no container isolates the platform; under ADR 0014 (proposed) `shell_exec` children run in a fresh empty network namespace (no loopback, no interfaces) on top of the agent-UID drop, while script tools keep network access as operator-sanctioned egress channels (`security.md`). Resource caps on shell children are applied via rlimits.
- Each environment owns `data/envs/<env-id>/` (workspace + `memory.db`). The directory is the isolation unit: teardown, migration, and backup are directory operations.
- The platform itself ships as a distroless nonroot image built from the repo-root `Dockerfile` — the platform image, distinct from the environment image (the agent's body), whose definition remains an open question below. The platform does not talk to a Docker daemon at runtime (ADR 0011); the deployment image is the only Docker involvement. Because the image runs nonroot, `shell_exec` reports `agent_uid unavailable` under it unless the deployment adds privilege.

## Rejected alternatives

- **Two-tier sandbox (the founding brief's original reading)** — superseded by the body model (ADR 0002).
- **Container-in-container (DinD)** — requires a privileged outer container, which would then also hold the secrets; weakens the host boundary for no gain over sibling or UID separation.
- **Flat single-UID container** — no structural protection for platform assets inside the environment.

## Open questions

- Memory/runtime UID separation (setuid supervisor beyond the `shell_exec` path): target design per ADR 0006; only the shell sub-process drops privileges today.
- Environment image definition and hygiene checklist (no setuid binaries, minimal capabilities): required by ADR 0006, not yet written.
