# ADR 0020: The turn drains the queue — bounded batch intake, supersede as cancellation

> Status: accepted
> Date: 2026-08-03
> Supersedes: ADR 0009's "each message is one run" semantics (the FIFO, bounded queue, and backpressure stand).

## Context

ADR 0009 made each inbound message exactly one turn. Under orchestration, inbound traffic changes shape: bursts from many sources, plus numerous notification-class messages that do not each warrant a full model turn. Per-message turns make burst latency grow linearly with queue depth. The owner's constraints: keep the structure simple (no work-board, no busy-state machinery, no control channel) and keep ADR 0019's no-intervention boundary.

## Decision

- **A turn drains the queue.** When an environment's runner starts a turn, it takes every queued message up to a small bound K (K = 8; overflow stays queued for the next turn) and renders them as one turn's input, each with its message ID and sender. A turn is still one reasoning loop with exactly one terminal event.
- **The envelope gains a sender.** `Inbound` carries the sender's identity so the agent can address replies per message through `message_send`.
- **Cancellation is a message, not a verb.** There is no cancel API. A sender retracts a request by sending another message ("disregard my previous request"); FIFO order plus same-batch coalescing means the agent usually sees the original and the retraction in one turn and never starts the work. This is ADR 0019's "the only influence is another message" stated for cancellation.

## Consequences

- Burst cost flattens: N queued messages cost ⌈N/K⌉ turns instead of N, and notification-class items share turns instead of each buying one.
- The gateway API is unchanged (POST + SSE); no new verbs, no new components, no work-item lifecycle.
- A failed turn consumes its batch the way a failed run consumed its message before; nothing is lost semantically because ADR 0016 captures every message at accept time.
- The contract tells the model a turn may carry several messages and that a later message may retract an earlier one.
- `docs/architecture/gateway.md` execution bullet and ADR 0009's per-message wording are superseded as noted above; monitoring events reference the batch's message IDs.

## Rejected alternatives

- **Work board / task objects** — a new concept family (items, states, lifecycle) for throughput the drain already provides.
- **Busy-state protocol toward the orchestrator** — queue depth plus blocking senders is already the load signal; an explicit state API may be added later as polish, not structure.
- **Queue-cancel verb (DELETE /messages/{id})** — a control channel in the message path (ADR 0019), race-prone at the drain boundary, and largely redundant once corrections coalesce into the same batch.
