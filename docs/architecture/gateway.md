# Gateway

Governs the gateway: message ingress, outbound delivery, and monitoring. Rationale: ADR 0017, ADR 0007, ADR 0012, ADR 0015.

## Current decisions

- The gateway is the agent's ears and eyes; it lives in the agent container and serves that one agent (ADR 0017). It has no relationship with the sidecar.
- Routing senders to agent pods is the orchestration layer's concern (ADR 0014); the gateway terminates mTLS and nothing more.
- Three flows, unchanged in shape:
  - **Ingress** — asynchronous and fire-and-forget: accept, enqueue, acknowledge. No synchronous response couples a sender to a turn. Ingress itself stores nothing: no path writes a message to memory at accept time, and the writeback is the model's only way to write (ADR 0006, ADR 0011). The platform writes on its own where ADR 0011 puts platform mechanics — threshold-triggered and never model-invoked — which on this flow means the context-overflow summary of turns leaving the session's history (ADR 0006).
  - **Outbound** — the mouth: `message_send` lives agent-side as the agent's own speech (ADR 0007: outbound is a tool). Delivery is the tool writing one JSON file per message into the outbox directory it is given as `GAGENT_OUTBOX_DIR`; the gateway names that directory and reads it back, and nothing forwards it onward. There is no push, so there is nothing to fall back from: a message is durable once the file is written, and a reader of that directory has everything the agent sent. Each file names its sender in `agent_id` (ADR 0016), which the tool takes from its own environment rather than from the call — so an aggregated outbox stays attributable and the model never states who it is.
  - **Monitoring** — a read-only event stream; every turn emits exactly one terminal event (`turn_finished` or `turn_failed`). Never part of the message path.
- One queue per agent; a turn drains the queue (bound 8, sender envelopes, supersede-as-cancellation — ADR 0015). No forced stop, suspend, or steering (ADR 0014).
- **What a sender can and cannot learn about a turn.** This is a contract, not an implementation accident: a 202 that means "accepted" and a 202 that means "received" are different promises, and the difference cannot be seen from outside.
  - The 202 says the message is queued in this process. It does not say a turn ran, or that one will; whether acceptance survives a restart is a separate question (#492).
  - The monitoring stream is the only channel carrying a turn's outcome, and it is best-effort. A subscriber that falls behind its buffer loses events while it is still connected, with nothing in the stream marking the gap, and a subscriber that connects after posting sees nothing emitted before it subscribed — the stream has no replay.
  - So an event's presence is information and its absence is not. A missing `turn_failed` does not separate a turn that succeeded from one whose event was dropped or one that never started, and a live connection is not evidence that the stream is complete.
  - A subscriber that is keeping up sees one terminal event per turn, stamped with the message IDs that turn drained (ADR 0015). A failed turn reaches no other channel: the runner logs it and takes the next batch.
  - An event that does arrive carries the outcome: `turn_finished` the turn's visible content, `turn_failed` the error. An addressed reply to the sender is a separate act by the agent — `message_send` on the outbound flow above.

## Current state

`gagent agent` runs one agent per process: the agent ID is fixed at startup (`--agent-id`), the HTTP paths keep the `/agents/{agent-id}/` shape, and a request naming any other ID gets 404. `POST /agents/{agent-id}/messages` returns 202 with an ID (the sender envelope is the verified mTLS caller identity) and `GET /agents/{agent-id}/events` streams the agent's loop events as SSE. Fan-out is per-subscriber buffered (`SubscriberBuffer`, 32 events) and never blocks the run, so a subscriber that reads too slowly loses events rather than slowing the turn — the stream is lossy by construction, not only under failure. Callers authenticate with mTLS when a `tls:` config section is set; without it the gateway serves loopback only. Outbound delivery is the `message_send` tool writing a JSON file into `outbox/` next to the configured `memory-path`: the gateway computes that path once (`DirFor`), hands it to the tool in its environment, and reads the directory back to report what a one-shot turn delivered. Delivery has no network hop and no queue behind it, so it neither retries nor reports a failure the tool did not already return. `gagent agent --message` runs one turn against the same tool list (the operator-installed servers) instead of serving, and exits non-zero if a tool call in that turn failed. The two paths report a failed tool call differently because they have different readers: a served turn hands it to the model, which gets another iteration to react, and the turn still ends `turn_finished`; a one-shot run has exited by the time anyone looks, so its exit status is the only reader left. Neither path branches on a mode or on the configured provider.

## Open questions

- Channel adapters beyond HTTP — deferred. Nothing delivers outside the outbox today, so an adapter is new work rather than a second path alongside an existing one.
