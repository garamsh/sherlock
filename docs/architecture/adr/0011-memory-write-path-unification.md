# ADR 0011: Commit source and summary links together

> Status: accepted
> Date: 2026-08-02 (revised in place 2026-09-24)

## Context

The earlier decision unified model-chosen writes through the reserved `memory_write` response. Source-linked conversational memory instead needs an original visible message and summaries that cannot be orphaned by a failed maintenance step. Issue #823 changes the active write authority while preserving the terminal response transport.

## Decision

A completed turn records visible user and assistant source messages and their linked short and mid summaries in one SQLite transaction. A failure rolls back the whole turn's durable memory work; no separate processed cursor is advanced. The reserved `memory_write` call still ends the turn and carries the visible reply, but its addressed `memory_ops` do not write the legacy addressed tables in the source-linked runtime.

## Consequences

- The model cannot write a source-free memory through the active runtime.
- Old addressed rows remain readable by the legacy store API but block source-linked runtime startup until an explicit migration or export.
- The response schema still carries `memory_ops` for provider compatibility; removing that field is separate work.

## Rejected alternatives

- Writing sources and summaries in separate commits could strand a source without its search path and silently skip it on later maintenance.
