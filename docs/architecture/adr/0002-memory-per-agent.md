# ADR 0002: Per-agent memory in a single SQLite file

> Status: accepted
> Date: 2026-07-25 (revised in place 2026-09-26)

## Context

An agent needs durable memory that stays with its own data. The earlier addressed schema and the source-linked schema both use the agent container's volume, and a restart must recover records without another service. Issue #823 changed what the file holds, not who owns it.

## Decision

Each agent owns one configured `memory.db` SQLite file. The source archive, mid episodes, and each episode's embedding share it. The store interface keeps SQLite behind the memory boundary. Completed sources commit before synthesis; each mid episode and its processing interval commit atomically (ADR 0011). The old addressed tables remain in existing files; the source-linked runtime refuses a file with active or archived addressed rows instead of deleting or treating them as source-backed evidence. Populated extractive source-linked schemas likewise require an explicit operator decision; `memory.md` records the fresh-path procedure that preserves the old archive.

## Consequences

- A single volume backup contains conversation sources and their search links.
- A populated addressed store requires explicit export or migration before this runtime starts. The old rows stay untouched on refusal.
- Similarity ranking reads the stored embeddings in process, so no external search service or vector index is needed. Only the embedding of new text needs the configured endpoint (ADR 0004).
