# ADR 0004: Secret-free environment

> Status: accepted
> Date: 2026-07-25

## Context

The agent has total authority inside its environment (ADR 0002), including a shell tool. Any secret present inside the environment — environment variables, files, mounted credentials — must be considered exposed to whatever the agent executes. Preventing exposure by restricting the agent contradicts the ownership model.

## Decision

Secrets never enter the environment. Exposure is prevented by absence, not by restriction.

- Credentials (API keys, tokens) live outside the environment: in tool implementations and gateway configuration.
- The agent reaches external services only through tools; the tool implementation attaches credentials. Neither the agent nor its shell ever sees them.
- Egress policy is enforced outside the environment (per ADR 0002), which also bounds exfiltration.
- If credentialed egress from inside the environment is ever required, a proxy broker outside the environment injects credentials per request; plaintext secrets still never enter.
- Data-flow rule: secrets must not be placed into the agent's conversation context. Container design cannot fix a secret the model has already seen.

## Consequences

- Secret exposure via the shell tool is structurally impossible for credentials managed by the platform.
- Every external integration must be expressed as a tool — consistent with PLAN §1 ("tools compose services").
- User-supplied secrets pasted into chat are out of scope; the data-flow rule can only govern what the platform injects.
