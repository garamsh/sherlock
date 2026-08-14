# Gateway

Governs the gateway: message ingress, outbound delivery, and monitoring. Rationale: ADR 0017, ADR 0007, ADR 0012, ADR 0015.

## Current decisions

- The gateway is the agent's ears and eyes; it lives in the agent container and serves that one agent (ADR 0017). It has no relationship with the sidecar.
- Routing senders to agent pods is the orchestration layer's concern (ADR 0014); the gateway terminates mTLS and nothing more.
- Three flows, unchanged in shape:
  - **Ingress** — asynchronous and fire-and-forget: accept, enqueue, acknowledge. No synchronous response couples a sender to a turn. Inbound messages are not stored as searchable memories; the writeback is the only write path (ADR 0006, ADR 0011).
  - **Outbound** — the mouth: `message_send` lives agent-side as the agent's own speech; delivery targets a console endpoint over mTLS with a file-outbox fallback, so a message is never silently lost.
  - **Monitoring** — a read-only event stream; every turn ends with exactly one terminal event (`turn_finished` or `turn_failed`). Never part of the message path.
- One queue per agent; a turn drains the queue (bound 8, sender envelopes, supersede-as-cancellation — ADR 0015). No forced stop, suspend, or steering (ADR 0014).

## Current state

`gagent agent` runs one agent per process: the agent ID is fixed at startup (`--agent-id`), the HTTP paths keep the `/agents/{agent-id}/` shape, and a request naming any other ID gets 404. `POST /agents/{agent-id}/messages` returns 202 with an ID (the sender envelope is the verified mTLS caller identity) and `GET /agents/{agent-id}/events` streams the agent's loop events as SSE. Callers authenticate with mTLS when a `tls:` config section is set; without it the gateway serves loopback only. Outbound delivery POSTs every `message_send` to the configured console endpoint over mTLS, falling back to the file outbox (`outbox/` next to the configured `memory-path`) on failure. `gagent agent --message` runs one turn against the same tool list (the operator-installed servers) instead of serving.

## Open questions

- Channel adapters beyond HTTP — console-side, deferred.
