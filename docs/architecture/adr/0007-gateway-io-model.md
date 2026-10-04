# ADR 0007: Gateway I/O — asynchronous ingress, tool-based outbound, separate monitoring

> Status: accepted
> Date: 2026-07-25

## Context

The agent is modeled as a person, not a task runner: one continuous rolling session rather than per-task sessions. Input is normally fire-and-forget messages; real-time visibility into ongoing work is needed only occasionally and must not complicate the message path. Replies — to the sender, to another person, or to another agent — should be the agent's own decision. OpenClaw, the reference platform, validates this shape: a single long-lived gateway owning all channels, request/ack plus server-push event streams, one rolling main session, per-session serialized runs with queued followups, outbound via session tools, and per-agent SQLite state.

## Decision

The gateway separates three flows: ingress, outbound, and monitoring.

- **Ingress is asynchronous.** Channel adapters normalize inbound messages; the gateway accepts each message under the ID its sender gave it, or one it assigns when the sender gave none, enqueues it for the agent, and acknowledges. No synchronous response couples a sender to a turn.
- **A sender can redeliver safely.** A message ID is scoped to its sender's principal — the verified mTLS caller identity, empty over plain-HTTP loopback — and kept byte for byte. A message already queued or running in this process is not queued again; one whose turn committed its sources to memory is answered as completed and not run again; either delivered again with other content is refused. Completed means those committed sources and nothing weaker, so the 202 never means it. The sender can ask whether a message is completed, pending in this process, or unknown to it; a restart forgets pending, not completed.
- **One rolling session per agent.** No automatic reset and no per-task sessions; context growth drains into long-term memory per ADR 0006.
- **Execution is serialized per agent.** Messages arriving mid-run are queued as followups. There is no mid-run steering or interruption (ADR 0014).
- **Outbound is a tool.** A `message_send` tool (channel, target, content) carries every reply — the agent's mouth. The agent decides when and to whom to respond; answering the sender, another person, or another agent is the same operation.
- **Each reply is one outbox entry.** `message_send` writes it as `<id>.json` into `outbox/` beside the memory file. `id` is assigned once, when the entry is written, and is unique per entry. The body is `id`, `agent_id`, `assignment_epoch`, `generation`, `in_reply_to` (the IDs of the messages the turn drained), `channel`, `target` and `content`; the model supplies only the last three. `assignment_epoch` and `generation` record origin only (ADR 0016) and are empty when the process was started without them.
- **An entry is written atomically and durably, and never changed.** The write is a temporary file in `outbox/`, fsynced, renamed to `<id>.json` without replacing an existing name, then the directory fsynced, so a reader never sees a partial entry. Sherlock never deletes, rewrites or renames an entry once written. A rerun of an interrupted turn may write further entries: delivery is at least once.
- **Forwarding is the consumer's.** A consumer removes an entry only after its destination durably acknowledges it. Any current forwarder may forward an entry an earlier process wrote; nothing in an entry binds it to the process that wrote it. An entry's fields record where it came from and are not a credential, so the destination checks the current forwarder's generation and idempotency key separately from an entry's historical `assignment_epoch` and `generation`.
- **Monitoring is a separate subscription stream.** Agent events (turn started, tool calls, memory writeback, turn finished) are published on a read-only stream that clients subscribe to only when they want live visibility. It is never part of the message path and typically has no subscribers.
- **Callers authenticate with mTLS** (ADR 0012).

## Consequences

- Senders of different kinds (people, other agents, scheduled jobs) mix safely: the queue serializes them, and no sender blocks on a run.
- Reply routing is decoupled from the sender — work requested by one party can be delivered to another, and agent-to-agent messaging uses the same mechanism as human messaging.
- The agent may legitimately choose not to reply; the default reply conventions live in the agent's instructions.
- Recording is deduplicated and delivery is at least once: a turn that is interrupted or ends without committing leaves its message unknown, a sender that redelivers it runs it again, and the effects that turn already had may repeat. There is no exactly-once claim, and the message IDs on monitoring events stay a hint.
- A consumer deduplicates on its own terms: the entry `id` names one write, and a rerun turn's reply is a new write with a new `id`. Relating a reply to what it answers goes through `in_reply_to`.
- Streaming partial model output is just another monitoring event type if ever wanted; the message model does not change.
