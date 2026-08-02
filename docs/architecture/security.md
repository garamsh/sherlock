# Security

Governs the security model: trust domains, secret custody, and egress. Rationale: ADR 0004, ADR 0007, ADR 0012, ADR 0013, ADR 0014.

## Current decisions

### Trust domains

- Every tool belongs to one of two domains, decided at design review:
  - **Code-executing tools** (`files`, `shell_exec`) run with no reachable secrets. `shell_exec` runs agent-supplied commands as sub-processes dropped to the agent UID with resource limits (ADR 0012); when the platform cannot drop privileges the tool reports `agent_uid unavailable` and the agent falls back to the typed tools.
  - **Secret-holding tools** (`memory`, `message_send`) never execute agent-supplied code and accept only schema-validated structured input.
- The "secret exposure through a shell tool" risk ADR 0007 flagged is mitigated structurally, not by absence: the shell sub-process runs under the agent UID, which owns no secrets (kernel DAC, ADR 0006).
- Two tool sources exist: Go built-ins under `internal/<domain>/` and config-registered subprocess script tools (ADR 0013). Script tools are operator-supplied executables registered at platform startup, never from agent input. Under ADR 0014 they drop to the agent UID like `shell_exec` — their stdin is agent-controlled — but keep network access: a script tool is an operator-sanctioned egress channel, while `shell_exec` children get a fresh empty network namespace. No in-process plugin or network tool loader exists (ADR 0010).
- No separate security layer exists; each tool enforces its own domain's policy.

### Secrets

- Secret values never materialize in the code-execution domain. Exposure is prevented by absence, not filtering.
- Child processes spawned by code-executing tools (`shell_exec`, script tools) start from a minimal scrubbed environment — `PATH`, `HOME`, `LANG` only. The platform process's variables (provider API keys and the like) are never inherited implicitly; a script tool's operator-configured `exec.env` entries are the explicit channel for additional variables.
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
