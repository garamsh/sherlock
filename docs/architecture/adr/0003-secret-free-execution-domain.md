# ADR 0003: Secret-free execution domain

> Status: accepted
> Date: 2026-07-25

## Context

The agent runs code (its own commands, tool executables). Any secret value present in the execution domain — environment variables, files, mounted credentials — must be considered exposed to whatever the agent executes. Filters (command inspection, guard models) are probabilistic and bypassable by obfuscation; absence is not. A hard requirement exists in the other direction: programs such as the GitHub CLI need both the workspace (inside the domain) and a credential (outside it) to be useful.

## Decision

Secret values never materialize in the code-execution domain. Exposure is prevented by absence, not by filtering.

- **Explicit channels only**: a secret reaches trusted tool code through operator-declared configuration that resolves the value from the process environment at startup, never through a config-file literal. The execution domain this governs is the workspace and the Exec children it runs: those start from a minimal scrubbed environment and inherit nothing (ADR 0008, ADR 0017 moved that path into the sidecar). It is not the agent's own environment — a tool the operator installs runs inside that one and inherits it whole, which `security.md` §Tool trust posture states and prices.
- **High-sensitivity operations stay outside the domain**: a trusted tool performs the whole operation and returns a summary.
- **Tool output schemas never include credential fields**, and secrets must not be placed into the agent's conversation context — container design cannot fix a secret the model has already seen.
- Capability misuse (using a credential in an unintended way) is bounded by the credential's own scope (fine-grained, minimal permissions), not by the platform.

## Consequences

- Secret theft from the execution domain is structurally impossible for platform-managed credentials, including prompt-injection-driven exfiltration of values.
- Threat model split: guard models address *accidents* (defense-in-depth, ADR 0005); structural absence addresses *adversarial* exfiltration of values.
- User-supplied secrets pasted into chat are out of scope; the data-flow rule can only govern what the platform injects.
