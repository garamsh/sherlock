# ADR 0007: Gateway I/O — asynchronous ingress, tool-based outbound, separate monitoring

> Status: accepted
> Date: 2026-07-25

## Context

The agent is modeled as a person, not a task runner: one continuous rolling session rather than per-task sessions. Input is normally fire-and-forget messages; real-time visibility into ongoing work is needed only occasionally and must not complicate the message path. Replies — to the sender, to another person, or to another agent — should be the agent's own decision. OpenClaw, the reference platform, validates this shape: a single long-lived gateway owning all channels, request/ack plus server-push event streams, one rolling main session, per-session serialized runs with queued followups, outbound via session tools, and per-agent SQLite state.

## Decision

The gateway separates three flows: ingress, outbound, and monitoring.

- **Ingress is asynchronous.** Channel adapters normalize inbound messages; the gateway accepts each message, assigns an ID, enqueues it for the agent, and acknowledges. No synchronous response couples a sender to a turn.
- **One rolling session per agent.** No automatic reset and no per-task sessions; context growth drains into long-term memory per ADR 0006.
- **Execution is serialized per agent.** Messages arriving mid-run are queued as followups. There is no mid-run steering or interruption (ADR 0015).
- **Outbound is a tool.** A `message_send` tool (channel, target, content) carries every reply — the agent's mouth. The agent decides when and to whom to respond; answering the sender, another person, or another agent is the same operation.
- **Monitoring is a separate subscription stream.** Agent events (turn started, tool calls, memory writeback, turn finished) are published on a read-only stream that clients subscribe to only when they want live visibility. It is never part of the message path and typically has no subscribers.
- **Callers authenticate with mTLS** (ADR 0013).

## Consequences

- Senders of different kinds (people, other agents, scheduled jobs) mix safely: the queue serializes them, and no sender blocks on a run.
- Reply routing is decoupled from the sender — work requested by one party can be delivered to another, and agent-to-agent messaging uses the same mechanism as human messaging.
- The agent may legitimately choose not to reply; the default reply conventions live in the agent's instructions.
- Streaming partial model output is just another monitoring event type if ever wanted; the message model does not change.
