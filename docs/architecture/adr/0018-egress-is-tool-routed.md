# ADR 0018: External egress is tool-routed — no dedicated egress proxy

> Status: accepted
> Date: 2026-08-03

## Context

`security.md` carried a deferred "egress tool/proxy": a component meant to be the only outbound path from the agent UID — terminating TLS, resolving `secret://` references at bound destinations, and redacting responses. Three later decisions absorbed its purposes. ADR 0014 removed network access from `shell_exec` children entirely, closing the raw-egress hole the proxy was meant to guard. ADR 0013/0014 made script tools operator-sanctioned egress channels that keep network by operator choice. #150 (scrubbed child environments) plus the script `exec.env` channel gives an explicit, operator-chosen path for secrets into trusted tool processes; the agent sees only structured tool output.

## Decision

External access is tool-routed by design; no dedicated egress proxy or egress tool is built. The outbound paths are: `message_send` (replies), operator-registered script tools (network-keeping, `exec.env` for secrets) — and nothing else; `shell_exec` has no network. Destination allowlists, response redaction, and secret-reference bindings land only when a concrete use case needs them, inside the tool that needs them (`docs/convention/simplicity.md`).

## Consequences

- The `security.md` egress open questions close. "The only outbound path is the egress tool" is restated as "the only outbound paths are the registered tools."
- The agent cannot reach arbitrary URLs. Granting new egress means registering a tool — a config change reviewed like any other, not a runtime capability.
- Secret-bearing tools keep secrets out of the agent's domain through `exec.env` plus the scrubbed child environment: the secret reaches the trusted tool process only, and the agent sees tool output, never the environment.

## Rejected alternatives

- **Dedicated egress proxy with TLS termination and CA provisioning** — a second network component to run and secure, built ahead of a use case; script tools already provide sanctioned egress with less machinery.
- **Secret references (`secret://name`) resolved by a platform resolver** — adds a resolution layer and reflection-leak surface; `exec.env` into a trusted tool achieves custody with no new concept.
