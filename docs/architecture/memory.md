# Memory

Governs long-term memory: the store, the search, and the lifecycle. Rationale: ADR 0017, ADR 0006, ADR 0011, ADR 0018.

## Current decisions

- Memory is the agent's long-term organ — a layer inside the agent container, never exposed as a tool (ADR 0017). The model touches memory only through the lifecycle below.
- The store is one SQLite file per agent in the agent container's volume, behind a store interface that hides SQLite so a remote or shared store can replace it later. The per-agent store carries forward from the pre-pod design; the memory-UID custody dissolves because no untrusted code runs in the agent container.
- The store is **curation-only**: only the writeback block writes to it (ADR 0006, ADR 0011). There is no platform auto-capture of inbound messages — a message that never earns a model `add` never enters long-term memory.
- Salience is platform-derived from the writeback op, never model-written: `update` ranks highest, `add` of semantic facts ranks high, `add` of episodic observations ranks mid (ADR 0006).
- **A fact is held at the address of the question it answers, and the write states whether a value replaces what is there or joins it** (ADR 0018). Identity is computed, never inferred from similarity; where a writeback must err, it errs toward join, because a wrong join is repaired by the next correct replacement at that address and a wrong replacement is repaired by nothing.
- Memory is three parts, not one (ADR 0018): a writeback that reads a sentence into an address, a store that holds an address and a value, and a read step that reads a question into an address. The store does no finding — asked in words, an addressed store answers nothing, so the read step in front of it is not an optimization.
- Forgetting is decided by use rather than by age or by the write (ADR 0018). An age-ordered rule discards the oldest fact, which is the one a decade of turns never re-asserts and a question still asks for.

The three parts above are the decided shape and not the running one: nothing in `internal/memory/` implements them yet, and #336 is the sequence that does. §Current state (as implemented) is what runs today.

## Current state (as implemented)

- **Store** — one `memory.db` per agent at the configured `memory-path`; active and archive tables; the platform never hard-deletes. The memory-UID custody is gone: no untrusted code runs in the agent container (ADR 0017).
- **Search** — hybrid brute-force cosine over embeddings plus SQLite FTS5, combined by one ranking function weighted by salience, relevance, and salience-scaled recency (Ebbinghaus decay whose half-life grows with salience; ADR 0006). The embedder sits behind `memory.Embedder`: `hashing` (deterministic, offline) or an OpenAI-compatible provider with the key read from a named environment variable.
- **Lifecycle (every turn)** — frame; recall (task-anchored hybrid search, budgeted injection with IDs and ages, explicit emptiness — the token budget is recall's only bound, so the count varies with it; the row limit belongs to `Search` and `Similar`, which take one from their caller); act (access bookkeeping); writeback (`keep` / `update` / `delete` / `add` — the single write path, ADR 0011); write-time coherence (similar memories surfaced on add/update as a system note the next model call reads).
- **Supersession on insert** — an `add` similar to an active memory at or above `SupersessionSimilarity` (`internal/memory/ranking.go`) supersedes it in place; the old row archives with `superseded_by`/`valid_to`. The decision behind it is withdrawn: ADR 0006 no longer carries the mechanism and ADR 0018 settles what replaces it. The code still does this until #336 lands, so it is listed here and nowhere above.
- **Tiered retention** — after a completed turn, high-salience memories are kept indefinitely, mid-salience retained for a bounded window, low-salience dropped to the archive (ADR 0006).
- **Maintenance** — updates supersede (never overwrite); deletes retire to the archive; capacity eviction uses the ranking score; consolidation on threshold (100 active episodic, evaluated after a completed turn) or shutdown compresses episodic clusters into chunked semantic parents (content-preserving; model-assisted with a deterministic fallback); context overflow summarizes oldest history into an episodic memory (model-generated, so it obeys the curation rule).
- Memory is never a tool: the model touches memory only through the lifecycle — recall injection covers reads, the writeback block is the single write path (ADR 0011, ADR 0017).

## Open questions

- Snapshot/backup of the memory file outside the pod — default post-MVP.
- Cross-agent shared memory — deferred.
- Benchmark-based evaluation harness — the read path is measured against a planted corpus with known ground truth under both embedders, and the supersession threshold against the pair set that refuted it (`docs/experiments/memory-retrieval/`); the shipped store is measured over a decade of turns beside sixteen other structures (`docs/experiments/memory-frames/`), including what a real model extracts from a message, what it declares about an assertion, and what address it reads a question onto. No benchmark runs against a live agent's store, and no run measures whether a model writes back at all — that is `docs/experiments/memory-cycle/`'s subject.
- What a writeback does with a declaration it cannot trust (ADR 0018). Taking a model's declarations and always replacing are a genuine trade, and which failure is cheaper is a property of what the agent is for rather than of anything measured.
- What the read step costs resolving into a question-shaped address space. It was measured resolving into the topic space, because that is the space it ran in (ADR 0018).
- What the read step returns when a question reaches more addresses than fit. An addressed store has no ordering over the addresses under a subject and no budget to answer inside; where an ordering would have to live is the read step (ADR 0018).