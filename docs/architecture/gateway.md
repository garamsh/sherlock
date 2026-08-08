# Gateway

Governs the gateway: message ingress, outbound delivery, and monitoring. Rationale: ADR 0018, ADR 0007, ADR 0013, ADR 0016.

## Current decisions

- The gateway is the agent's ears and eyes; it lives in the agent container and serves that one agent (ADR 0018). It has no relationship with the sidecar.
- Routing senders to agent pods is the orchestration layer's concern (ADR 0015); the gateway terminates mTLS and nothing more.
- Three flows, unchanged in shape:
  - **Ingress** — asynchronous and fire-and-forget: accept, capture the message verbatim as an episodic `user-turn` memory (ADR 0012), enqueue, acknowledge. No synchronous response couples a sender to a turn.
  - **Outbound** — the mouth: `message_send` lives agent-side as the agent's own speech; delivery targets a console endpoint over mTLS with a file-outbox fallback, so a message is never silently lost.
  - **Monitoring** — a read-only event stream; every turn ends with exactly one terminal event (`turn_finished` or `turn_failed`). Never part of the message path.
- One queue per agent; a turn drains the queue (bound 8, sender envelopes, supersede-as-cancellation — ADR 0016). No forced stop, suspend, or steering (ADR 0015).

## Current state

One gateway binary hosts every environment in a single container: `POST /agents/{agent-id}/messages` returns 202 with an ID (an unknown agent ID creates the environment at enqueue time; the sender envelope is the verified mTLS caller identity) and `GET /agents/{agent-id}/events` streams the agent's loop events as SSE. Callers authenticate with mTLS when a `tls:` config section is set; without it the gateway serves loopback only. Outbound delivery POSTs every `message_send` to the configured console endpoint over mTLS, falling back to the per-environment file outbox (`data/envs/<id>/outbox/`) on failure. The pod split — one gateway per agent pod — is pending with the ADR 0018 migration.

## Open questions

- Channel adapters beyond HTTP — console-side, deferred.
