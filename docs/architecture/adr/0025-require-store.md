# ADR 0025: A start may require the memory store to exist, and refuses one that is absent or uninitialized

> Status: accepted
> Date: 2026-10-05

## Context

The agent creates `memory.db` on first use: the store takes its writer lock, then opens the file and writes the schema, whether or not a file was there (ADR 0002). A first start and a start on a volume that lost its store therefore look the same. Both serve, the second as a blank agent with nothing reporting that its memory is gone. A per-generation runtime replaces the process on the same volume each generation, so for that launcher a missing store is a fault, and the agent was the only party that could see it (#952, relaying Garam's #1180).

Alternatives considered:

- **Always refuse an absent store** — breaks every first start, which would then need a separate create step that nothing else requires.
- **A config-file key or `SHERLOCK_*` variable** — both outlive a single start. A deployment carrying one would refuse the start that creates the store, and the operator would have to edit the deployment between the first start and the second.
- **A marker file beside the store** — a second file to keep in step with the first, which the same volume loss removes along with it.

## Decision

`--require-store`, a flag read off the command line alone, makes the agent refuse to start when `memory.db` is absent, or present but uninitialized: empty, or holding none of Sherlock's tables. The check runs once the writer lock is held and before anything migrates or catches up, reads the file only through a read-only open, and never creates, repairs or resets anything. Without the flag a start creates the store as before.

## Consequences

- A launcher that knows a start is a replacement says so per start, and a lost store stops the process before the gateway listens, with an error naming the path and the flag.
- The first start is an ordinary launch without the flag; nothing else changes for a deployment that never passes it.
- The refusal tells absent from present, and Sherlock's schema from none. It does not tell this agent's store from another agent's: a store copied from elsewhere passes.
- The lock file is still created beside a store the check then refuses, because the lock is taken first; it refuses nothing by its presence.
- A store's directory that is absent is refused without a lock, since no lock file can sit in it.

## Signs the decision was wrong

- Launchers pass the flag on every start and handle the first one some other way, which would mean the default is what they want and the opt-in is backwards.
- A refused start is answered by deleting or recreating the file to get past it, which would mean the refusal needs a recovery path rather than a stop.
- Stores that pass the check still turn out to be the wrong ones, which would mean presence was not the property worth checking and identity is.
