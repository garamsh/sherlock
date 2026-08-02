# Gateway

Governs the gateway: message ingress, routing, outbound delivery, and monitoring. Rationale: ADR 0009.

## Current decisions

- The gateway separates three flows:
  - **Ingress** — asynchronous and fire-and-forget. Channel adapters normalize inbound messages; the gateway assigns an ID, enqueues for the target environment, and acknowledges. No synchronous response couples a sender to a run.
  - **Outbound** — a `message_send` tool (channel, target, content) carries every reply. The agent decides when and to whom to respond; the sender, another person, and another agent are all the same kind of target.
  - **Monitoring** — a read-only subscription event stream (turn started, tool calls, memory writeback, turn finished or failed). Every run ends with exactly one terminal event — `turn_finished` on success, `turn_failed` with the error on failure — so subscribers can always finalize their view of a turn. Never part of the message path; typically no subscribers.
- Execution is serialized per environment. Messages arriving mid-run queue as followups; steering and interruption are deferred. Each environment's queue is bounded; a full queue applies backpressure to the sender instead of dropping messages.
- Sessions are rolling, one per environment.
- Adding a channel means adding one adapter; the rest of the system does not change.
- No authentication in this phase. The gateway serves trusted networks only and must not be exposed — an unauthenticated gateway is a remote shell into the environment.

### HTTP adapter (first channel)

- `POST /envs/{env-id}/messages` accepts a message, assigns an ID, enqueues it, and returns `202` with the ID. An unknown env-id creates the environment at enqueue time, so creation errors (e.g. an invalid ID) surface to the sender as `400`.
- `GET /envs/{env-id}/events` streams the environment's loop events (turn started, tool call, writeback, turn finished, turn failed) as server-sent events. Subscribing works with zero publishers and publishing works with zero subscribers; a subscriber that falls behind drops events rather than slowing the run.
- The MVP outbound sink writes each `message_send` message as a JSON file under `data/envs/<env-id>/outbox/`. Real channel adapters replace the sink behind the same interface later.
- The chat model is selected by the `model` config section: the scripted mock (default) or any OpenAI-compatible provider. The mock's canned script exercises the four built-in tools offline — a memory store, a `shell_exec` call, a `message_send` reply, and a writeback block; a run that exhausts the script fails, is logged, and the queue continues.
- Script tools join every environment's registry from the platform config's `tools:` section (ADR 0013): each entry names an operator-supplied executable the gateway runs as a subprocess with JSON on stdin/stdout, alongside the four built-ins. `gateway.ScriptTool` is the section's single shape from config file to registry. The entry points validate the section at startup via `Factory.ValidateTools` — a built-in or duplicate name collision, an unreadable schema, or an empty exec command fails startup with an error naming the tool, never the first message.

## Rejected alternatives

- **Synchronous request-response messaging** — couples sender latency to agent run time and breaks multi-sender input.
- **Streaming in the message path** — live output is a monitoring concern; the message model stays minimal.

## Open questions

- Authentication model — deferred.
- Mid-run steering / interruption — deferred.
- Channel adapters beyond the first HTTP/API adapter — deferred.
