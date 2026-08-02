# Security

Governs the security model: trust domains, secret custody, and egress. Rationale: ADR 0004, ADR 0007, ADR 0012, ADR 0013.

## Current decisions

### Trust domains

- Every tool belongs to one of two domains, decided at design review:
  - **Code-executing tools** (`files`, `shell_exec`) run with no reachable secrets. `shell_exec` runs agent-supplied commands as sub-processes dropped to the agent UID with resource limits (ADR 0012).
  - **Secret-holding tools** (`memory`, `message_send`) never execute agent-supplied code and accept only schema-validated structured input.
- The "secret exposure through a shell tool" risk ADR 0007 flagged is mitigated structurally, not by absence: the shell sub-process runs under the agent UID, which owns no secrets (kernel DAC, ADR 0006).
- Two tool sources exist: Go built-ins under `internal/<domain>/` and config-registered subprocess script tools (ADR 0013). Script tools are operator-supplied trusted executables; they run as the platform UID with no privilege drop and are registered at platform startup, never from agent input. No in-process plugin or network tool loader exists (ADR 0010).
- No separate security layer exists; each tool enforces its own domain's policy.

### Secrets

- Secret values never materialize in the code-execution domain. Exposure is prevented by absence, not filtering.
- The environment carries placeholder references only, e.g. `GITHUB_TOKEN=secret://github-token`, injected as ordinary environment variables.
- References resolve only at the egress tool, only for requests matching a (secret, destination host, header position) binding. References in request bodies or to unbound destinations are never substituted.
- The egress tool redacts any occurrence of real values in response payloads, closing reflection leaks.
- High-sensitivity secrets skip references entirely; a structured tool performs the operation outside the domain and returns a summary.
- Tool output schemas never include credential fields, and secrets are never placed into the agent's conversation context.
- Capability misuse is bounded by the credential's own scope (fine-grained, minimal permissions).

### Egress

- External access is tool-routed by principle. Raw egress from the agent UID exists only through the egress tool, which enforces the domain allowlist and secret-reference bindings.

### Guard models

- Command risk classification and output secret scanning are adopted as defense-in-depth layers. They are not boundaries: boundaries are structural only (file ownership, secret absence, network policy).

## Rejected alternatives

- **In-body secret substitution (shell wrapper, FUSE, LD_PRELOAD)** — the agent is root-adjacent in its domain; any secret materialized for use is materialized for theft.
- **Filters or guard models as the boundary** — probabilistic and bypassable by obfuscation.
- **Secrets as plain environment variables** — readable by any code in the domain.

## Open questions

- Egress proxy implementation timing (TLS termination, CA provisioning) — MVP ships tool-routed access; raw egress proxy follows.
- Gateway authentication — deferred.
- Privilege drop for script tools — they currently run as the platform UID (ADR 0013); dropping them to the agent UID like `shell_exec` is deferred until a use case needs it.
