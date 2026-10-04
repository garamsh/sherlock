# ADR 0022: The running agent reports what it loaded, and drains its in-flight turn on SIGTERM

> Status: accepted
> Date: 2026-10-04

## Context

An orchestrator rolling an agent needs two things the gateway did not give it (#931, which also resolves #476).

- **What a running process serves.** The identity, the start and the configuration revision lived in the launcher's arguments and in one log line. A launcher could not ask the process which of them it actually loaded, and the gateway had no route a probe could read without enqueuing work or holding a stream open (`deployment.md` §Health and readiness).
- **A shutdown that does not throw work away.** `SIGTERM` closed the listener first, so a sender got a refused connection it could not tell from a crash, and then cancelled the turn in flight, so a turn the model had nearly finished committed nothing.

Two constraints bound the answer. The status must not claim more than the process can know: a configuration digest would be a value computed here and compared nowhere, and a start time is not evidence that a revision was applied. And the orchestrator's grace period stays the only total bound, so a drain cannot promise exactly-once delivery.

## Decision

`GET /agents/{agent-id}/status` reports contract version 1: the agent ID, the assignment epoch, the generation, the config revision, the state, and the process start time, each from what this process loaded once initialization succeeded. The revision is the config file's top-level `revision` key, opaque and echoed, with empty meaning unknown. On `SIGTERM` the agent turns `draining`, answers new posts 503 while its listener stays open, lets the turn in flight complete with its durable writes, starts no queued turn and skips the idempotent after-turn pass, then closes the listener and memory, releasing the writer lock last.

## Consequences

- A launcher reads which identity, start and revision a process serves from the process itself. The route enqueues nothing and reads no store, so it is the probe route, behind the gateway's authentication like every other route.
- A sender posting during a drain is told `draining` rather than refused a connection, and can redeliver to the next process under the same message ID.
- The turn in flight commits instead of being cancelled, so a shutdown no longer costs a nearly finished turn. It also means a turn is bounded by the grace period alone: a turn the `SIGKILL` cuts off commits nothing, and its messages read unknown after the restart.
- Queued messages are still lost on exit, as before; the sender learns that from the lookup and redelivers. Delivery stays at-least-once.
- The status reports and does not enforce. Sherlock does not require a non-empty assignment epoch or revision, and does not compare any value it reports.

## Rejected alternatives

- **Compute a digest of the resolved configuration** — it exposes a value derived from settings that may hold paths and endpoints, and it answers a question no caller asked: the desired-config writer already names the revision it wrote, so echoing that name is the comparison it needs.
- **Close the listener first, as before, and drain behind it** — the sender cannot tell a draining pod from a crashed one, which is the ambiguity the 503 removes.
- **Persist the queue so a drain hands it to the next process** — that is the durable accept #492 leaves open, not a shutdown concern.
- **A separate health route with no agent ID in the path** — every route is agent-addressed (ADR 0016), and a route that answered for any ID would report on an agent the caller did not name.
