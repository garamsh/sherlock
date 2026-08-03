# Security

Governs the security model: trust domains, secret custody, and egress. Rationale: ADR 0004, ADR 0007, ADR 0012, ADR 0013, ADR 0014, ADR 0018.

## Current decisions

### Trust domains

- Every tool belongs to one of two domains, decided at design review:
  - **Code-executing tools** (`files`, `shell_exec`) run with no reachable secrets. `shell_exec` runs agent-supplied commands as sub-processes dropped to the agent UID with resource limits (ADR 0012); when the platform cannot drop privileges the tool reports `agent_uid unavailable` and the agent falls back to the typed tools.
  - **Secret-holding tools** (`memory`, `message_send`) never execute agent-supplied code and accept only schema-validated structured input.
- The "secret exposure through a shell tool" risk ADR 0007 flagged is mitigated structurally, not by absence: the shell sub-process runs under the agent UID, which owns no secrets (kernel DAC, ADR 0006).
- Two tool sources exist: Go built-ins under `internal/<domain>/` and config-registered subprocess script tools (ADR 0013). Script tools are operator-supplied executables registered at platform startup, never from agent input. They drop to the agent UID through the same executor as `shell_exec` (ADR 0014) — their stdin is agent-controlled — but keep network access: a script tool is an operator-sanctioned egress channel, while `shell_exec` children run in a fresh empty network namespace. No in-process plugin or network tool loader exists (ADR 0010).
- No separate security layer exists; each tool enforces its own domain's policy.

### Secrets

- Secret values never materialize in the code-execution domain. Exposure is prevented by absence, not filtering.
- Child processes spawned by code-executing tools (`shell_exec`, script tools) start from a minimal scrubbed environment — `PATH`, `HOME`, `LANG` only. The platform process's variables (provider API keys and the like) are never inherited implicitly; a script tool's operator-configured `exec.env` entries are the explicit channel for additional variables.
- An external API that needs a secret is reached through an operator-registered script tool: the secret enters the trusted tool process via `exec.env`, and the agent sees only the tool's structured output (ADR 0018). High-sensitivity operations stay outside the domain the same way — a trusted tool performs the operation and returns a summary.
- Tool output schemas never include credential fields, and secrets are never placed into the agent's conversation context.
- Capability misuse is bounded by the credential's own scope (fine-grained, minimal permissions).

### Egress

- External access is tool-routed by design; no dedicated egress proxy exists (ADR 0018). The outbound paths are `message_send` and operator-registered script tools — and nothing else: `shell_exec` children run in a fresh empty network namespace. Destination allowlists and response redaction land inside the tool that needs them, when a use case needs them.

### Guard models

- Command risk classification and output secret scanning are adopted as defense-in-depth layers. They are not boundaries: boundaries are structural only (file ownership, secret absence, network policy).

### Gateway authentication

- Callers authenticate with mTLS terminated in the platform binary (ADR 0017); the platform holds only public caller material (the client CA) plus its own server key outside the agent's execution domain — no shared caller secret enters any config. Authorization is flat: there are no per-env ACLs in the platform — granting agents their capabilities is the orchestration layer's concern (ADR 0019).
- Runtime material is owner-only (ADR 0006's runtime-UID domain in practical form): the gateway TLS server key and the platform config must be mode 0600 so agent-UID children cannot read them. The gateway fails startup on a group/other-readable server key, naming the path and the 0600 requirement, instead of mutating operator-provided files.

## Rejected alternatives

- **In-body secret substitution (shell wrapper, FUSE, LD_PRELOAD)** — the agent is root-adjacent in its domain; any secret materialized for use is materialized for theft.
- **Filters or guard models as the boundary** — probabilistic and bypassable by obfuscation.
- **Secrets as plain environment variables** — readable by any code in the domain.

## Open questions

- Runtime UID separation (tool runtime configuration custody) — target design per ADR 0006; the runtime material is enforced via modes today (TLS server key checked at startup, config documented at 0600).
