# Gateway

Governs the gateway: message ingress, routing, outbound delivery, and monitoring. Rationale: ADR 0009, ADR 0017.

## Current decisions

- The gateway separates three flows:
  - **Ingress** — asynchronous and fire-and-forget. Channel adapters normalize inbound messages; the gateway assigns an ID, captures the message verbatim as an episodic `user-turn` memory at accept time (ADR 0016 — the capture commits before the ack, so a failed run or a restart before the run starts never loses or duplicates it; a capture failure rejects the message), enqueues for the target environment, and acknowledges. No synchronous response couples a sender to a run.
  - **Outbound** — a `message_send` tool (channel, target, content) carries every reply. The agent decides when and to whom to respond; the sender, another person, and another agent are all the same kind of target.
  - **Monitoring** — a read-only subscription event stream (turn started, tool calls, memory writeback, turn finished or failed). Every run ends with exactly one terminal event — `turn_finished` on success, `turn_failed` with the error on failure — so subscribers can always finalize their view of a turn. Never part of the message path; typically no subscribers.
- Execution is serialized per environment. Messages arriving mid-run queue as followups; when a turn starts it **drains the queue** — every queued message up to a small bound (8), each rendered with its ID and sender, becomes one turn's input with exactly one terminal event (ADR 0020). Overflow stays queued for the next turn. There is no forced stop, suspend, or steering (ADR 0019), and no cancel verb: a sender retracts a request by sending another message, which FIFO order and same-batch coalescing deliver before the work starts. Each environment's queue is bounded; a full queue applies backpressure to the sender instead of dropping messages.
- Sessions are rolling, one per environment.
- Adding a channel means adding one adapter; the rest of the system does not change.
- Callers authenticate with mTLS, terminated in the platform binary (ADR 0017): a `tls:` config section carries the server certificate/key and the client CA path, and the gateway requires and verifies client certificates against it. Without `tls:` the gateway serves loopback binds only; a non-loopback bind without TLS fails startup. An authenticated caller has full access in this phase. Certificate issuance and CA operation are operator/orchestration-layer scope.

### HTTP adapter (first channel)

- `POST /envs/{env-id}/messages` accepts a message, assigns an ID, enqueues it, and returns `202` with the ID. An unknown env-id creates the environment at enqueue time, so creation errors (e.g. an invalid ID) surface to the sender as `400`.
- `GET /envs/{env-id}/events` streams the environment's loop events (turn started, tool call, writeback, turn finished, turn failed) as server-sent events. Subscribing works with zero publishers and publishing works with zero subscribers; a subscriber that falls behind drops events rather than slowing the run.
- The MVP outbound sink writes each `message_send` message as a JSON file under `data/envs/<env-id>/outbox/`. Real channel adapters replace the sink behind the same interface later.
- The chat model is selected by the `model` config section: the scripted mock (default) or any OpenAI-compatible provider. The mock's canned script exercises the four built-in tools offline — a memory search, a `shell_exec` call, a `message_send` reply, and a writeback block; a run that exhausts the script fails, is logged, and the queue continues.
- Script tools join every environment's registry from the platform config's `tools:` section (ADR 0013): each entry names an operator-supplied executable the gateway runs as a subprocess dropped to the agent UID (ADR 0014) with JSON on stdin/stdout, alongside the four built-ins. `gateway.ScriptTool` is the section's single shape from config file to registry. The entry points validate the section at startup via `Factory.ValidateTools` — a built-in or duplicate name collision, an unreadable schema, or an empty exec command fails startup with an error naming the tool, never the first message.

## Rejected alternatives

- **Synchronous request-response messaging** — couples sender latency to agent run time and breaks multi-sender input.
- **Streaming in the message path** — live output is a monitoring concern; the message model stays minimal.

## Open questions

- Channel adapters beyond the first HTTP/API adapter — deferred.
