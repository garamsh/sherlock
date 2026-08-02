# ADR 0004: Secret-free execution domain and secret references

> Status: accepted
> Date: 2026-07-25

## Context

The agent has total authority inside its user domain (ADR 0002), including a shell tool. Any secret value present in that domain — environment variables, files, mounted credentials — must be considered exposed to whatever the agent executes. Filters (command inspection, guard models) are probabilistic and bypassable by obfuscation; absence is not. A hard requirement also exists in the other direction: programs such as the GitHub CLI need both the codebase (inside the domain) and a credential (outside it) to be useful.

## Decision

Secret values never materialize in the code-execution domain. Exposure is prevented by absence, not by filtering.

- **Secret references**: the environment carries placeholder values only, e.g. `GITHUB_TOKEN=secret://github-token`. Programs use them exactly like real values.
- **Scrubbed child environments**: child processes spawned by code-executing tools start from a minimal base (`PATH`, `HOME`, `LANG`) and never inherit the platform process's variables implicitly; operator-configured entries (script `exec.env`) are the only additions.
- **Bound resolution**: the egress tool is the only component that resolves references, and only for requests matching a policy binding of (secret, destination host, header position). References in request bodies or to unbound destinations are never substituted.
- **Response redaction**: the egress tool holds the real values and redacts any occurrence of them in response payloads, closing reflection (echo) leaks on bound destinations.
- **High-sensitivity secrets** are not exposed even as bound references; a structured tool performs the whole operation outside the domain and returns a summary.
- **Tool output schemas never include credential fields**, and secrets must not be placed into the agent's conversation context — container design cannot fix a secret the model has already seen.
- Capability misuse (using a bound credential for its intended host in an unintended way) is bounded by the credential's own scope (fine-grained, minimal permissions), not by the platform.

## Consequences

- Secret theft via the shell tool is structurally impossible for credentials managed by the platform, including prompt-injection-driven exfiltration of values.
- Programs like GitHub CLI work unmodified: the codebase lives in the domain, the credential is only a name, substitution happens on the wire.
- The egress tool becomes a hard trust anchor: it terminates TLS, holds real values, and enforces bindings — its correctness is security-critical.
- Threat model split: guard models address *accidents* (defense-in-depth, ADR 0007); structural absence addresses *adversarial* exfiltration of values.
- User-supplied secrets pasted into chat are out of scope; the data-flow rule can only govern what the platform injects.
