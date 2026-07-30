# Environment

Governs the environment model: what an environment is, its topology, and its trust boundaries. Rationale: ADR 0002, ADR 0006.

## Current decisions

- The environment is the agent's body. The agent has full authority inside it; the orchestration layer manages the boundary (network, mounts, resources, lifecycle). There is no separate sandbox tier — the platform does not spawn an inner container for the agent to run commands in (ADR 0011).
- One container per environment, subdivided into trust domains by Unix UIDs:
  - **Agent UID** — runs all agent-supplied code; unprivileged, always.
  - **Memory UID** — owns `memory.db`.
  - **Runtime UID** — owns tool runtime configuration.
- Enforcement is kernel DAC (file ownership and modes), not filters. Agent code touching another UID's assets gets `EACCES`.
- A tool runtime (supervisor) manages processes and drops privileges before executing agent-supplied code.
- Boundary policy — egress, CPU/memory quotas, mount scope, lifecycle — is injected from outside at environment creation. The agent cannot change it.
- Direct network egress is blocked at the container level: the deployment image sets `NetworkMode("none")`, so no outbound path of any kind is reachable from inside an environment container today. The intended outbound path is the egress tool described in `security.md` — **not yet shipped; deferred per the open questions in `security.md`**. The wording "the only outbound path is the egress tool" is the *intended* state, not the *current* one.
- Each environment owns `data/envs/<env-id>/` (workspace + `memory.db`). The directory is the isolation unit: teardown, migration, and backup are directory operations.
- The platform itself ships as a distroless nonroot image built from the repo-root `Dockerfile` — the platform image, distinct from the environment image (the agent's body), whose definition remains an open question below. With `sandbox_exec` removed (ADR 0011), the platform no longer talks to a Docker daemon at runtime; the deployment image is the only Docker involvement.

## Rejected alternatives

- **Two-tier sandbox (PLAN §4 original reading)** — superseded by the body model (ADR 0002).
- **Container-in-container (DinD)** — requires a privileged outer container, which would then also hold the secrets; weakens the host boundary for no gain over sibling or UID separation.
- **Flat single-UID container** — no structural protection for platform assets inside the environment.

## Open questions

- UID separation mechanics (setuid supervisor): target design per ADR 0006; the MVP runs without it.
- Environment image definition and hygiene checklist (no setuid binaries, minimal capabilities): required by ADR 0006, not yet written.
