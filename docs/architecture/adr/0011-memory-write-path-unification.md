# ADR 0011: Persist sources before atomic summary checkpoints

> Status: accepted
> Date: 2026-08-02 (revised in place 2026-09-24)

## Context

The earlier decision unified model-chosen writes through the reserved `memory_write` response. Source-linked conversational memory instead needs an original visible message and summaries that cannot be orphaned by a failed maintenance step. Issue #823 changes the active write authority while preserving the terminal response transport.

## Decision

A completed response archives its visible source messages in one transaction before model synthesis. Each full cross-turn batch creates an immutable mid episode with its direct source interval. The last committed interval is the only processing checkpoint. Model calls happen outside write transactions; a checkpoint comparison refuses a stale concurrent synthesis.

A failure preserves completed sources and any earlier committed episodes. Startup and subsequent completions resume from the persisted intervals without skipping pending input. Gateway IDs identify a replay of the same completed batch; ID/content conflicts and partially overlapping batches fail explicitly. The reserved `memory_write` response still carries the reply, while its addressed `memory_ops` do not write the legacy tables.

## Consequences

- The model cannot write a source-free memory through the active runtime.
- Old addressed rows remain readable by the legacy store API but block source-linked runtime startup until an explicit migration or export.
- The response schema still carries `memory_ops` for provider compatibility; removing that field is separate work.

## Rejected alternatives

- Keeping remote synthesis inside the source transaction would lose completed messages on a model failure and hold the write lock during network latency. Persisted pending intervals and bounded pending context make separate source capture recoverable.
- An independent mutable cursor could advance without the summary it describes. Deriving progress from committed summary intervals avoids that failure state.
