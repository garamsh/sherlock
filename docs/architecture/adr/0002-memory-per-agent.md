# ADR 0002: Per-agent memory in a single SQLite file

> Status: accepted
> Date: 2026-07-25

## Context

The founding plan requires long-term memory with hybrid vector+keyword search and agent-driven curation, suggesting an external vector store (pgvector/Qdrant/Chroma). The project owner wants each agent to own its memory directly, so that backup, migration, and multi-agent separation all reduce to handling one agent's data. Memory must also survive self-destructive behavior by the agent.

## Decision

Each agent owns its memory as one file: `memory.db` (SQLite, pure-Go driver), stored with the agent's own data — in the pod topology (ADR 0017) the agent container's volume; the file is configured directly as the agent's `memory-path` (default `data/memory.db`).

- Vector search: embeddings stored in a table, cosine similarity computed in memory (brute force). A deterministic local embedder is the default so the pipeline runs with no external API; a real embedding model plugs in behind the same interface.
- Keyword search: SQLite FTS5.
- Ranking: one hybrid scoring function combining vector and FTS5 scores.
- `compress` summarizes detail memories into a parent memory and moves originals to an archive table — never immediate deletion.
- Updates supersede rather than overwrite: the old row records `superseded_by` and `valid_to` and moves to the archive, so every change is reversible.
- The searchable index has a size cap; eviction moves low-scoring entries to the archive.
- Each memory carries a type field (`episodic` | `semantic`); the lifecycle that transitions between types is defined in ADR 0006.
- The agent curates content (what to keep, correct, retract); the platform owns maintenance triggers (ADR 0011).
- A store interface hides SQLite so a remote or shared store can replace it per agent later without contract changes.

## Consequences

- Backup, migration, and isolation are file operations on one agent's data.
- No external service dependency for memory in single-agent deployments.
- Memory survives agent misbehavior by construction: no executed code runs beside it in the agent container (ADR 0017).
- Brute-force vector search caps comfortable memory volume at tens of thousands of entries per agent; the store interface is the escape hatch.
