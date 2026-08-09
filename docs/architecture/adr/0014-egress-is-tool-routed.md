# ADR 0014: External egress is tool-routed — no dedicated egress proxy

> Status: accepted
> Date: 2026-08-03

## Context

An early design carried a deferred "egress tool/proxy": a component meant to be the only outbound path from the execution domain — terminating TLS, resolving secret references at bound destinations, and redacting responses. Later decisions absorbed its purposes. The execution boundary record (ADR 0010) network-isolates executor children on request, closing the raw-egress hole the proxy was meant to guard. The tool-shape record (ADR 0009) makes tool packages operator-sanctioned egress channels that keep network by operator choice. Scrubbed child environments plus environment-resolved tool configuration give an explicit, operator-chosen path for secrets into trusted tools; the agent sees only structured tool output.

## Decision

External access is tool-routed by design; no dedicated egress proxy or egress tool is built. The outbound paths are: `message_send` (replies) and the tool packages the agent imports at compile time (network-keeping by operator choice) — and nothing else; executor children are network-isolated on request (`NoNetwork`, ADR 0008). Destination allowlists, response redaction, and secret-reference bindings land only when a concrete use case needs them, inside the tool that needs them (`docs/convention/simplicity.md`).

## Consequences

- "The only outbound path is the egress tool" is restated as "the only outbound paths are the imported tools."
- The agent cannot reach arbitrary URLs. Granting new egress means adding a tool package to the agent's build — a code change reviewed like any other, not a runtime capability.
- Secret-bearing tools keep secrets out of the agent's domain through environment-resolved configuration plus the scrubbed child environment: the secret reaches the trusted tool code only, and the agent sees tool output, never the environment.

## Rejected alternatives

- **Dedicated egress proxy with TLS termination and CA provisioning** — a second network component to run and secure, built ahead of a use case; tool packages already provide sanctioned egress with less machinery.
- **Secret references (`secret://name`) resolved by a platform resolver** — adds a resolution layer and reflection-leak surface; explicit env entries into a trusted tool achieve custody with no new concept.
