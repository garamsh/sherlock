# Gateway

Governs the gateway: message ingress, routing, outbound delivery, and monitoring. Rationale: ADR 0009.

## Current decisions

- The gateway separates three flows:
  - **Ingress** — asynchronous and fire-and-forget. Channel adapters normalize inbound messages; the gateway assigns an ID, enqueues for the target environment, and acknowledges. No synchronous response couples a sender to a run.
  - **Outbound** — a `message_send` tool (channel, target, content) carries every reply. The agent decides when and to whom to respond; the sender, another person, and another agent are all the same kind of target.
  - **Monitoring** — a read-only subscription event stream (turn started, tool calls, memory writeback, run finished). Never part of the message path; typically no subscribers.
- Execution is serialized per environment. Messages arriving mid-run queue as followups; steering and interruption are deferred.
- Sessions are rolling, one per environment (see `agent.md`, `memory.md`).
- Adding a channel means adding one adapter; the rest of the system does not change.
- No authentication in this phase. The gateway serves trusted networks only and must not be exposed — an unauthenticated gateway is a remote shell into the environment.

## Rejected alternatives

- **Synchronous request-response messaging** — couples sender latency to agent run time and breaks multi-sender input.
- **Streaming in the message path** — live output is a monitoring concern; the message model stays minimal.

## Open questions

- Authentication model — deferred.
- Mid-run steering / interruption — deferred.
- Channel adapters beyond the first HTTP/API adapter — deferred.
