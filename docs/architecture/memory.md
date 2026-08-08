# Memory

Governs long-term memory: the store, the search, and the lifecycle. Rationale: ADR 0018, ADR 0006, ADR 0011, ADR 0012.

## Current decisions

- Memory is the agent's long-term organ — a layer inside the agent container, never exposed as a tool (ADR 0018). The model touches memory only through the lifecycle below.
- The store is one SQLite file per agent in the agent container's volume, behind a store interface that hides SQLite so a remote or shared store can replace it later. The per-agent store carries forward from the pre-pod design; the memory-UID custody dissolves because no untrusted code runs in the agent container.
- The store, search, and lifecycle mechanics are unchanged by the pivot and are restated below as implemented.

## Current state (as implemented)

- **Store** — one `memory.db` per agent at the configured `memory-path`; active and archive tables; the platform never hard-deletes. The memory-UID custody is gone: no untrusted code runs in the agent container (ADR 0018).
- **Search** — hybrid brute-force cosine over embeddings plus SQLite FTS5, combined by one ranking function weighted by relevance, recency, importance, and access. The embedder sits behind `memory.Embedder`: `hashing` (deterministic, offline) or an OpenAI-compatible provider with the key read from a named environment variable.
- **Lifecycle (every turn)** — frame; recall (task-anchored hybrid search, budgeted injection with IDs and ages, explicit emptiness); act (access bookkeeping); writeback (`keep` / `update` / `delete` / `add` — the single write path, ADR 0011); write-time coherence (similar memories surfaced on add/update).
- **Maintenance** — updates supersede (never overwrite); deletes retire to the archive; capacity eviction uses the ranking score; consolidation on threshold or shutdown compresses episodic clusters into chunked semantic parents (content-preserving; model-assisted with a deterministic fallback); context overflow summarizes oldest history into an episodic memory.
- **Auto-capture** — every inbound message is stored verbatim as an episodic `user-turn` memory with no model involvement (ADR 0012).
- Memory is never a tool: the model touches memory only through the lifecycle — recall injection covers reads, the writeback block is the single write path (ADR 0011, ADR 0018).

## Open questions

- Snapshot/backup of the memory file outside the pod — default post-MVP.
- Cross-agent shared memory — deferred.
- Benchmark-based evaluation harness — deferred; retrieval and writeback events are logged meanwhile.
