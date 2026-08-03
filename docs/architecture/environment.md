# Environment

Governs the environment model: what an environment is, its topology, and its trust boundaries. Rationale: ADR 0002, ADR 0006, ADR 0012.

## Current decisions

- The environment is the agent's body. The agent has full authority inside it; the orchestration layer manages the boundary (network, mounts, resources, lifecycle). There is no separate sandbox tier — the platform does not spawn an inner container for the agent to run commands in (ADR 0011).
- One container per environment, subdivided into trust domains by Unix UIDs (target design, ADR 0006):
  - **Agent UID** — runs all agent-supplied code; unprivileged, always.
  - **Memory UID** — owns `memory.db`.
  - **Runtime UID** — owns tool runtime configuration.
- Enforcement is kernel DAC (file ownership and modes), not filters. Agent code touching another UID's assets gets `EACCES`. Current state: `shell_exec` and script-tool child processes drop to the agent UID through the same executor, and only when the platform binary runs setuid-root — otherwise they report `agent_uid unavailable` (ADR 0012, ADR 0014). A privileged platform (euid 0) chowns `memory.db` to the memory UID with mode 0600 at environment creation, so agent-UID children get `EACCES`; unprivileged platforms skip the chown and own the file themselves. The memory service still opens the file as the platform process (root bypasses DAC). The runtime UID's domain is enforced via modes on the runtime material itself: the gateway TLS server key and the platform config must be mode 0600 owner-only — platform-owned and unreadable by agent-UID children — and the gateway refuses a group/other-readable server key at startup rather than chowning operator-provided files. Full UID custody of tool runtime configuration remains target design. The workspace is the agent UID's domain: a privileged platform chowns `workspace/` to the agent UID at environment creation, and the files tool drops its filesystem UID (`setfsuid`, Linux-only, privileged only) to the agent UID for workspace operations, so files-tool writes and shell-created files share one owner. Unprivileged platforms do neither and keep current behavior.
- A tool runtime (supervisor) manages processes and drops privileges before executing agent-supplied code.
- Boundary policy — egress, CPU/memory quotas, mount scope, lifecycle — is injected from outside at environment creation (target design). Current state: no container isolates the platform; `shell_exec` children run in a fresh empty network namespace (no loopback, no interfaces) on top of the agent-UID drop, while script tools keep network access as operator-sanctioned egress channels (ADR 0014, `security.md`). Resource caps on shell children are applied via rlimits.
- Each environment owns `data/envs/<env-id>/` (workspace + `memory.db`). The directory is the isolation unit: teardown, migration, and backup are directory operations. The data dir root stays platform-owned; only `workspace/` (agent UID) and `memory.db` (memory UID) change hands in a privileged deployment.
- The platform itself ships as a distroless nonroot image built from the repo-root `Dockerfile` — the platform image, distinct from the environment image (the agent's body), whose definition remains an open question below. The platform does not talk to a Docker daemon at runtime (ADR 0011); the deployment image is the only Docker involvement. Because the image runs nonroot, `shell_exec` reports `agent_uid unavailable` under it unless the deployment adds privilege.

## Rejected alternatives

- **Two-tier sandbox (the founding brief's original reading)** — superseded by the body model (ADR 0002).
- **Container-in-container (DinD)** — requires a privileged outer container, which would then also hold the secrets; weakens the host boundary for no gain over sibling or UID separation.
- **Flat single-UID container** — no structural protection for platform assets inside the environment.

## Open questions

- Runtime UID separation (tool runtime configuration custody): target design per ADR 0006. The runtime material is enforced via modes today — TLS server key and platform config at 0600, with a startup check on the key; `workspace/` and `memory.db` are the two UID domains enforced today.
- Environment image definition and hygiene checklist (no setuid binaries, minimal capabilities): required by ADR 0006, not yet written.
