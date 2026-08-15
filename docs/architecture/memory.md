# Memory

Governs long-term memory: the store, the search, and the lifecycle. Rationale: ADR 0017, ADR 0006, ADR 0011.

## Current decisions

- Memory is the agent's long-term organ — a layer inside the agent container, never exposed as a tool (ADR 0017). The model touches memory only through the lifecycle below.
- The store is one SQLite file per agent in the agent container's volume, behind a store interface that hides SQLite so a remote or shared store can replace it later. The per-agent store carries forward from the pre-pod design; the memory-UID custody dissolves because no untrusted code runs in the agent container.
- The store is **curation-only**: only the writeback block writes to it (ADR 0006, ADR 0011). There is no platform auto-capture of inbound messages — a message that never earns a model `add` never enters long-term memory.
- Salience is platform-derived from the writeback op, never model-written: `update` ranks highest, `add` of semantic facts ranks high, `add` of episodic observations ranks mid (ADR 0006).

## Current state (as implemented)

- **Store** — one `memory.db` per agent at the configured `memory-path`; active and archive tables; the platform never hard-deletes. The memory-UID custody is gone: no untrusted code runs in the agent container (ADR 0017).
- **Search** — hybrid brute-force cosine over embeddings plus SQLite FTS5, combined by one ranking function weighted by salience, relevance, and salience-scaled recency (Ebbinghaus decay whose half-life grows with salience; ADR 0006). The embedder sits behind `memory.Embedder`: `hashing` (deterministic, offline) or an OpenAI-compatible provider with the key read from a named environment variable.
- **Lifecycle (every turn)** — frame; recall (task-anchored hybrid search, budgeted injection with IDs and ages, explicit emptiness); act (access bookkeeping); writeback (`keep` / `update` / `delete` / `add` — the single write path, ADR 0011); write-time coherence (similar memories surfaced on add/update as a system note the next model call reads).
- **Supersession on insert** — an `add` similar to an active memory at or above `SupersessionSimilarity` (`internal/memory/ranking.go`) supersedes it in place; the old row archives with `superseded_by`/`valid_to` (ADR 0006, which carries the measurement against the mechanism and the issue tracking its removal).
- **Tiered retention** — after a completed turn, high-salience memories are kept indefinitely, mid-salience retained for a bounded window, low-salience dropped to the archive (ADR 0006).
- **Maintenance** — updates supersede (never overwrite); deletes retire to the archive; capacity eviction uses the ranking score; consolidation on threshold (100 active episodic, evaluated after a completed turn) or shutdown compresses episodic clusters into chunked semantic parents (content-preserving; model-assisted with a deterministic fallback); context overflow summarizes oldest history into an episodic memory (model-generated, so it obeys the curation rule).
- Memory is never a tool: the model touches memory only through the lifecycle — recall injection covers reads, the writeback block is the single write path (ADR 0011, ADR 0017).

## Open questions

- Snapshot/backup of the memory file outside the pod — default post-MVP.
- Cross-agent shared memory — deferred.
- Benchmark-based evaluation harness — deferred; retrieval and writeback events are logged meanwhile.