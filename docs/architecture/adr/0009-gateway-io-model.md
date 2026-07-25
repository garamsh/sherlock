# ADR 0009: Gateway I/O — asynchronous ingress, tool-based outbound, separate monitoring

> Status: accepted
> Date: 2026-07-25

## Context

The agent is modeled as a person, not a task runner: one continuous rolling session rather than opencode-style per-task sessions. Input is normally fire-and-forget messages; real-time visibility into ongoing work is needed only occasionally and must not complicate the message path. Replies — to the sender, to another person, or to another agent — should be the agent's own decision. OpenClaw, the reference platform, validates this shape: a single long-lived gateway owning all channels, request/ack plus server-push event streams over one protocol, one rolling main session across a user's DM channels, per-session serialized runs with queued followups, outbound via session tools, and per-agent SQLite state.

## Decision

The gateway separates three flows: ingress, outbound, and monitoring.

- **Ingress is asynchronous.** Channel adapters normalize inbound messages; the gateway accepts each message, assigns an ID, enqueues it for the target environment, and acknowledges. No synchronous response couples a sender to an agent run.
- **One rolling session per environment.** No automatic reset and no per-task sessions; context growth drains into long-term memory per ADR 0008. The consolidation trigger "session end" is reinterpreted as environment shutdown; the threshold trigger is unchanged.
- **Execution is serialized per environment.** Messages arriving mid-run are queued as followups. Mid-run steering and interruption are deferred.
- **Outbound is a tool.** A `message_send` tool (channel, target, content) carries every reply. The agent decides when and to whom to respond; answering the sender, another person, or another agent is the same operation.
- **Monitoring is a separate subscription stream.** Agent events (turn started, tool calls, memory writeback, run finished) are published on a read-only stream that clients subscribe to only when they want live visibility. It is never part of the message path and typically has no subscribers.
- **No authentication in this phase.** The gateway serves trusted networks only; authentication is a deferred decision and the gateway must not be exposed to untrusted networks.

## Consequences

- Senders of different kinds (people, other agents, scheduled jobs) mix safely: the queue serializes them, and no sender blocks on a run.
- Reply routing is decoupled from the sender — work requested by one party can be delivered to another, and agent-to-agent messaging uses the same mechanism as human messaging.
- The agent may legitimately choose not to reply; the default reply conventions live in the agent's instructions and are covered by the end-to-end test.
- Streaming partial model output is just another monitoring event type if ever wanted; the message model does not change.
- An unauthenticated gateway is a remote shell with full authority inside the environment: the trusted-network restriction is a hard operational rule until auth lands.
