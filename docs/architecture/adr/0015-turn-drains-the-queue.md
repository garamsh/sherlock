# ADR 0015: The turn drains the queue — bounded batch intake, supersede as cancellation

> Status: accepted
> Date: 2026-08-03

## Context

Early on, each inbound message was exactly one turn. Under orchestration, inbound traffic changes shape: bursts from many sources, plus numerous notification-class messages that do not each warrant a full model turn. Per-message turns make burst latency grow linearly with queue depth. The owner's constraints: keep the structure simple (no work-board, no busy-state machinery, no control channel) and keep the no-intervention boundary (ADR 0014).

## Decision

- **A turn drains the queue.** When the runner starts a turn, it takes every queued message up to a small bound K (K = 8; overflow stays queued for the next turn) and renders them as one turn's input, each with its message ID and sender. A turn is still one reasoning loop with exactly one terminal event.
- **The envelope gains a sender.** `Inbound` carries the sender's identity so the agent can address replies per message through `message_send`.
- **Cancellation is a message, not a verb.** There is no cancel API. A sender retracts a request by sending another message ("disregard my previous request"); FIFO order plus same-batch coalescing means the agent usually sees the original and the retraction in one turn and never starts the work. This is the "only influence is another message" boundary (ADR 0014) stated for cancellation.

## Consequences

- Burst cost flattens: N queued messages cost ⌈N/K⌉ turns instead of N, and notification-class items share turns instead of each buying one.
- The gateway API is unchanged (POST + SSE); no new verbs, no new components, no work-item lifecycle.
- A failed turn consumes its batch the way a failed run consumed its message before. The batch is not silently lost to the sender: the terminal event (`turn_failed`) signals the failure on the monitoring stream, and the only influence is another message (ADR 0014) — a sender retries by sending again.
- The contract tells the model a turn may carry several messages and that a later message may retract an earlier one.
- The queue is bounded; a full queue applies backpressure to the sender instead of dropping messages. Monitoring events reference the batch's message IDs.

## Rejected alternatives

- **Work board / task objects** — a new concept family (items, states, lifecycle) for throughput the drain already provides.
- **Busy-state protocol toward the orchestrator** — queue depth plus blocking senders is already the load signal; an explicit state API may be added later as polish, not structure.
- **Queue-cancel verb (DELETE /messages/{id})** — a control channel in the message path (ADR 0014), race-prone at the drain boundary, and largely redundant once corrections coalesce into the same batch.
