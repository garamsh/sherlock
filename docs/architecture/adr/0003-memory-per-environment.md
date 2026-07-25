# ADR 0003: Per-environment memory in a single SQLite file

> Status: accepted
> Date: 2026-07-25

## Context

PLAN §3 requires a `memory` tool with five operations (store, search, update, compress, forget), hybrid vector+keyword search, and agent-driven memory management. It suggests an external vector store (pgvector/Qdrant/Chroma). The project owner wants each agent to own its memory directly, with the environment as the isolation unit, so that environment teardown, migration, and multi-agent separation all reduce to handling one environment. Memory must also survive self-destructive behavior inside the agent's user domain (ADR 0002).

## Decision

Each environment owns its memory as one file: `memory.db` (SQLite, pure-Go driver) inside the environment's data directory.

- The file is owned by a dedicated memory UID; agent code (agent UID) cannot open it — access goes only through the `memory` tool interface (ADR 0006, ADR 0007).
- Vector search: embeddings stored in a table, cosine similarity computed in memory (brute force). MVP uses a deterministic local embedder so the full pipeline runs with no external API; a real embedding model plugs in later behind the same interface.
- Keyword search: SQLite FTS5.
- Ranking: one hybrid scoring function combining vector and FTS5 scores, per PLAN §3. Re-ranking is out of scope.
- `compress` summarizes detail memories into a parent memory and moves originals to an archive table — never immediate deletion.
- Each memory carries the metadata of PLAN §3 plus a type field (`episodic` | `semantic`); the lifecycle that transitions between types is defined in ADR 0008.
- The agent is the sole decision-maker for store/compress/forget; no separate memory-manager component exists.
- A store interface hides SQLite so a remote/shared store can replace it per environment later without tool or agent changes.

## Consequences

- Environment isolation, backup, and migration are file operations on the environment directory — aligned with ADR 0002 and ADR 0006.
- No external service dependency for memory in MVP or in single-agent deployments.
- Memory survives agent self-destruction by file ownership, not by physical separation.
- Brute-force vector search caps comfortable memory volume at tens of thousands of entries per environment; acceptable for MVP, and the store interface is the escape hatch.
- Open: snapshot/backup of `memory.db` outside the environment is undecided (default: post-MVP). Open: cross-environment shared memory for multi-agent scenarios (default: deferred; env-scoped only).
