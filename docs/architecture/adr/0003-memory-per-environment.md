# ADR 0003: Per-environment memory in a single SQLite file

> Status: accepted
> Date: 2026-07-25

## Context

PLAN §3 requires a `memory` tool with five operations (store, search, update, compress, forget), hybrid vector+keyword search, and agent-driven memory management. It suggests an external vector store (pgvector/Qdrant/Chroma). The project owner wants each agent to own its memory directly, with the environment as the isolation unit, so that environment teardown, migration, and multi-agent separation all reduce to handling one directory.

## Decision

Each environment owns its memory as one file: `memory.db` (SQLite, pure-Go driver) inside the environment's data directory.

- Vector search: embeddings stored in a table, cosine similarity computed in memory (brute force). MVP uses a deterministic local embedder so the full pipeline runs with no external API; a real embedding model plugs in later behind the same interface.
- Keyword search: SQLite FTS5.
- Ranking: one hybrid scoring function combining vector and FTS5 scores, per PLAN §3. Re-ranking is out of scope.
- `compress` summarizes detail memories into a parent memory and moves originals to an archive table — never immediate deletion.
- The agent is the sole decision-maker for store/compress/forget; no separate memory-manager component exists.
- A store interface hides SQLite so a remote/shared store (e.g. Qdrant) can replace it per environment later without tool or agent changes.

## Consequences

- Environment isolation, backup, and migration are file operations on the environment directory — aligned with ADR 0002.
- No external service dependency for memory in MVP or in single-agent deployments.
- Brute-force vector search caps comfortable memory volume at tens of thousands of entries per environment; acceptable for MVP, and the store interface is the escape hatch.
- Open: snapshot/backup of `memory.db` outside the environment (self-destruction recovery per ADR 0002) is undecided (default: post-MVP).
