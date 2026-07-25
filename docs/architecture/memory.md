# Memory

Governs long-term memory: the store, the search, and the lifecycle. Rationale: ADR 0003, ADR 0008 (including the surveyed prior art).

## Current decisions

### Store

- Each environment owns `memory.db` (SQLite, pure-Go driver) in its data directory, owned by the memory UID. Agent code cannot open the file; access goes only through the `memory` tool.
- Five operations: `store`, `search`, `update`, `compress`, `forget`.
- Each memory carries: content, embedding, type (`episodic` | `semantic`), `importance_score`, `access_count`, `last_accessed`, `timestamp`, `source`, `superseded_by`, `valid_to`.
- The schema has an active (searchable) table and an archive table. Nothing is hard-deleted by the platform.
- A store interface hides SQLite so a remote or shared store can replace it per environment later.

### Search

- Hybrid: brute-force cosine over stored embeddings plus SQLite FTS5, combined by one ranking function weighted by relevance, recency, importance, and access.
- MVP uses a deterministic local embedder; a real embedding model plugs into the same interface.

### Lifecycle (every turn)

1. **Frame** — the agent articulates the current task in context.
2. **Recall** — the runtime searches with the task statement and injects a variable number of raw entries within a token budget, each with ID and age. Explicit "no relevant memories" when empty.
3. **Act** — accessed memories get `access_count` / `last_accessed` updates.
4. **Writeback** — every model response ends with a memory block: `keep` / `update` / `delete` per recalled memory, plus `add` for new facts.
5. **Write-time coherence** — on `add`/`update`, the runtime surfaces the most similar existing memories for judgment in the same step, so related memories stay consistent.

### Maintenance

- **Updates supersede, never overwrite** — the old row records `superseded_by` and `valid_to` and moves to the archive.
- **Capacity** — the active index is capped; eviction score is the same weighted function as ranking; evicted entries move to the archive.
- **Consolidation** — on store-size threshold or environment shutdown, episodic clusters are compressed into semantic parent memories (originals archived).
- **Context overflow** — when the conversation window fills, the oldest turns are summarized and stored as episodic memory.
- One active session per environment.

## Open questions

- Snapshot/backup of `memory.db` outside the environment — default post-MVP.
- Cross-environment shared memory — default deferred.
- Real embedding provider — deferred.
- Benchmark-based evaluation harness — deferred; retrieval and writeback events are logged meanwhile.
