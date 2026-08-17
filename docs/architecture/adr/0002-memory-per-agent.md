# ADR 0002: Per-agent memory in a single SQLite file

> Status: accepted
> Date: 2026-07-25 (revised in place 2026-08-17)

## Context

The founding plan requires long-term memory with hybrid vector+keyword search and agent-driven curation, suggesting an external vector store (pgvector/Qdrant/Chroma). The project owner wants each agent to own its memory directly, so that backup, migration, and multi-agent separation all reduce to handling one agent's data. Memory must also survive self-destructive behavior by the agent.

What this record decides is where an agent's memory lives and who owns it. What it holds is ADR 0018's, and that answer changed after this record was written: a fact is held at the address of the question it answers, so the vector table, the keyword index and the ranking that read them are gone. The file, its per-agent ownership and the interface hiding the engine do not depend on what is in it, which is why they carry forward unchanged.

## Decision

Each agent owns its memory as one file: `memory.db` (SQLite, pure-Go driver), stored with the agent's own data — in the pod topology (ADR 0017) the agent container's volume; the file is configured directly as the agent's `memory-path` (default `data/memory.db`).

- A row is an address — the subject, the question it answers and whatever restriction the sentence places on that question — and the value held there (ADR 0018). No embedding, no sentence, no keyword index, and no type field: an addressed store holds nothing a read has to score.
- One unique index on the address and its value carries every read, and the three reads a caller can make are its three prefixes: one address, one question with the restrictions under it, one subject. Holding a value once at its address is the same index.
- A write that replaces what an address holds supersedes it rather than overwriting: the old row records `superseded_by` and `valid_to` and moves to an archive table, so every change is reversible. Nothing is ever deleted immediately.
- The active index has a size cap; eviction moves low-scoring entries to the archive.
- The agent curates content; the platform owns maintenance triggers (ADR 0011).
- A store interface hides SQLite so a remote or shared store can replace it per agent later without contract changes.
- A file written under the previous schema is not migrated. It holds sentences, and computing an address from one is a model call the store does not make, so opening it fails with that stated rather than with a missing column.

## Consequences

- Backup, migration, and isolation are file operations on one agent's data.
- No external service dependency for memory in single-agent deployments.
- Memory survives agent misbehavior by construction: no executed code runs beside it in the agent container (ADR 0017).
- Volume is bounded by the index rather than by a pass over the rows: a read touches the address asked and the restrictions under it, which is above one lookup and far below a scan. The active-index cap is what caps volume, and `docs/experiments/memory-frames/` measures a decade of turns arriving tens of units a year against it.
