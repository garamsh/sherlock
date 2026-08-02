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
- The `embedder` config section selects the embedder behind `memory.Embedder`: `hashing` (default, deterministic, fully offline) or `openai` — an OpenAI-compatible embeddings provider (`internal/memory/openai/`) reading base URL, model, and timeout from config and the API key from the environment variable named by `api-key-env`, never from the config file (ADR 0004). A missing key fails platform startup when the real provider is selected.

### Lifecycle (every turn)

1. **Frame** — the agent articulates the current task in context.
2. **Recall** — the runtime searches with the task statement and injects a variable number of raw entries within a token budget, each with ID and age. Explicit "no relevant memories" when empty.
3. **Act** — accessed memories get `access_count` / `last_accessed` updates.
4. **Writeback** — every model response ends with a memory block: `keep` / `update` / `delete` per recalled memory, plus `add` for new facts.
5. **Write-time coherence** — on `add`/`update`, the runtime surfaces the most similar existing memories for judgment in the same step, so related memories stay consistent.

### Maintenance

- **Updates supersede, never overwrite** — the old row records `superseded_by` and `valid_to` and moves to the archive.
- **Delete retires, never erases** — the writeback `delete` op goes through `memory.Service.Delete`: the row moves to the archive with `valid_to` set and `superseded_by` left empty (retirement, not supersession). Delete is not idempotent; a missing or already-archived ID is `ErrNotFound`.
- **Capacity** — the active index is capped; eviction score is the same weighted function as ranking; evicted entries move to the archive.
- **Consolidation** — on store-size threshold or environment shutdown, episodic clusters are compressed into semantic parent memories (originals archived).
- **Context overflow** — `internal/gateway/` wires a message-count cap on every rolling session; crossing it summarizes the oldest history into an episodic memory and retains the newest context.
- One active session per environment.

## Open questions

- Snapshot/backup of `memory.db` outside the environment — default post-MVP.
- Cross-environment shared memory — default deferred.
- Benchmark-based evaluation harness — deferred; retrieval and writeback events are logged meanwhile.
