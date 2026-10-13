# ADR 0013: External egress is tool-routed — no dedicated egress proxy

> Status: accepted
> Date: 2026-08-03 (revised in place 2026-08-13; 2026-08-17; 2026-08-23; 2026-09-26; 2026-10-09)

## Context

An early design carried a deferred "egress tool/proxy": a component meant to be the only outbound path from the execution domain — terminating TLS, resolving secret references at bound destinations, and redacting responses. Later decisions absorbed its purposes. The execution boundary record (ADR 0010) network-isolates executor children on request, closing the raw-egress hole the proxy was meant to guard. The tool-shape record (ADR 0009, ADR 0017) makes tool servers — independent processes the operator installs via the platform protocol — the only operator-sanctioned egress channels that keep network by operator choice. Installed tools are trusted processes inside the agent container and inherit the agent environment whole (ADR 0009). Credentials remain absent from the separate workspace execution domain through its scrubbed Exec environment (ADR 0003); tool output must not carry secrets into conversation context.

## Decision

External access is tool-routed by design; no dedicated egress proxy or egress tool is built. The operator chooses every outbound destination and the model chooses none: replies leave through `message_send`, capabilities through the operator-installed tool servers (network-keeping by operator choice), and the reasoning loop's and memory's model and embedding traffic through the operator-configured base URLs (ADR 0004). The embeddings base URL receives each turn's task text and every mid-episode summary (ADR 0019), and it need not be the chat model's vendor. The agent process also fetches each trusted issuer's key set from the `keys-url` the operator configured for it, carrying no conversation content and no credential (ADR 0026). Executor children are network-isolated on request (`NoNetwork`, ADR 0008). Destination allowlists, response redaction, and secret-reference bindings land only when a concrete use case needs them, inside the tool that needs them (`docs/convention/simplicity.md`).

## Consequences

- "The only outbound path is the egress tool" is restated as "every outbound destination is one the operator chose" — the guarantee the proxy was built to give survives as a property of who picks the destination, not as a count of channels.
- The agent cannot reach arbitrary URLs. Granting egress is a configuration decision: a tool server added to the operator's tool list, or the model or embeddings base URL set in the operator's config (ADR 0004) — reviewed like any other configuration, not a runtime capability.
- Conversation content leaves the pod for every endpoint the operator configures: the chat endpoint receives the prompt, and the embeddings endpoint receives task text and summaries. Pointing the embeddings base URL at a local server keeps that content inside the operator's network.
- Every installed tool can read every credential in the agent environment; installing one is a trust decision, and neither a manifest nor operator configuration provides per-tool credential scope (ADR 0009, `security.md`). The separate workspace and its Exec children retain the ADR 0003 secret-free posture; whole-environment tool inheritance does not extend to that domain.

## Rejected alternatives

- **Dedicated egress proxy with TLS termination and CA provisioning** — a second network component to run and secure, built ahead of a use case; tool servers already provide sanctioned egress with less machinery.
- **Secret references (`secret://name`) resolved by a platform resolver** — adds a resolution layer and reflection-leak surface. Trusted installed tools already inherit credentials from the agent environment whole; the platform provides no secret dispatcher or per-tool credential scope (ADR 0009).
