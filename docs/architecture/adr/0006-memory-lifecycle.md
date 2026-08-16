# ADR 0006: Memory lifecycle — curation-only, salience-tiered retention, supersession

> Status: accepted
> Date: 2026-07-25 (revised in place 2026-08-14)

## Context

The founding plan requires human-like memory: memories are refreshed, compressed, and forgotten, with the agent as the decision-maker. The original revision introduced platform auto-capture of every inbound message to make "remember X" reliable with a small chat model. A structured experiment (`/tmp/opencode/memory-experiment`, 32 candidate architectures × four workloads: small, 10-year-horizon, eight-category diverse, 60k-item large-scale) measured what that decision actually does: auto-capture stores every turn at uniform low importance, and on every metric that matters — recall@5, precision@5, anti-pollution, memory density (recall per KB), scale stability — the architectures that auto-capture lose to the architectures that store only curated content, by 25–50× on density and up to 2× on recall. The safety net was itself the pollution.

The same experiment identified the architecture that holds up on every workload: curation-only storage, a salience tier on every stored memory, salience-scaled Ebbinghaus decay, tiered retention, and supersession on insert. This record revises the lifecycle to that shape. The prior art survey stands (MemGPT self-editing, Mem0's ADD/UPDATE/DELETE vocabulary, A-MEM/Mem0 write-time updates, Zep temporal invalidation, Generative Agents scoring) — the experiment adds the empirical ranking of the mechanisms.

## Decision

Memory and context are separate stores connected by an automatic loop. Context is organized around the current task; long-term memory flows in and out of it each turn.

1. **Frame** — at each user request the agent articulates the current task in context. The task statement anchors what follows.
2. **Recall** — the runtime retrieves memories using the task statement as the hybrid-search query (ADR 0002): a variable number of raw entries within a token budget, each shown with its ID and age. When nothing relevant exists, the absence is stated explicitly.
3. **Act** — the agent executes with the composed context. Accessed memories have `access_count` and `last_accessed` updated automatically.
4. **Writeback** — every model response ends with a structured memory block: for each recalled memory `keep` / `update` / `delete`, plus `add` for new facts worth persisting.
5. **Write-time coherence** — when the writeback contains `add` or `update`, the runtime retrieves the most similar existing memories and presents them for judgment as a system note the next model call reads, so related memories are updated together instead of drifting into contradiction.

Storage discipline (new):

- **Curation-only.** Only the writeback block writes to the store. No platform auto-capture of inbound messages: no path stores a message at accept time. A message that never earns a model `add` never enters long-term memory.
- **Salience is platform-derived, never model-written.** The contract does not ask the model for a score (the `importance_score` exclusion stands). The platform derives salience from the writeback op: `update` (a correction) is highest, `add` of a semantic fact is high, `add` of an episodic observation is mid. Salience is set at insert time and used by scoring and retention below.
- **Retrieval scoring.** Score = salience + relevance + recency, where recency is an Ebbinghaus decay whose half-life scales with salience (`half-life = 24h × (1 + salience)`): high-salience memories fade slowly, low-salience memories fade fast. Relevance stays the hybrid cosine + FTS5 blend (ADR 0002).
- **Supersession on insert.** An `add` whose content is highly similar to an existing active memory supersedes it in place: the new row takes over, the old row moves to the archive with `superseded_by` and `valid_to` set. The newest version of a fact is the version that lives. The threshold is `SupersessionSimilarity` in `internal/memory/ranking.go`, calibrated there against the embedder in use. **The mechanism is contradicted by measurement**: `docs/experiments/memory-creation/` finds that facts which must both survive score higher than real corrections, at both embedding dimensions, so the highest-accuracy threshold is the one that supersedes nothing. Removal rather than retuning is tracked in #334; this decision stands until that lands, because the code still does it.
- **Tiered retention.** After a completed turn, the runtime drops active memories by tier: high salience (≥ 0.6) is kept indefinitely; mid salience is retained for a bounded window (~100 turns); low salience is dropped promptly. Nothing is hard-deleted — drops move to the archive.
- **Eviction and consolidation carry forward.** The active-index capacity cap and threshold-triggered consolidation (ADR 0002, ADR 0011) operate unchanged over the curated store; retention makes the cap rarely binding.
- **Context overflow** still drains into memory: when the conversation window fills, the oldest turns are summarized by the model into an episodic memory. This is model-generated content, not platform capture, so it obeys the same curation rule.

Update semantics are unchanged: updates supersede rather than overwrite; every change is reversible through the archive.

## Consequences

- The store holds only what the model judged worth keeping. Retrieval is precise because the noise floor is empty; density is 25–50× the auto-capture baseline (experiment rounds 1–5).
- The model's failure mode inverts: a model that never writes back (issue #302) leaves memory empty, and that emptiness is honest — there is no safety net fabricating a store out of traffic. Fixing model compliance is a model/prompt concern, not a storage concern.
- Supersession keeps the latest version of a fact authoritative; stale recall drops (experiment: 0.17 vs 0.50 for non-superseding architectures).
- Salience-scaled decay means a 10-year-old preference and a yesterday conversation coexist correctly: the preference fades slowly, the chatter fades fast.
- ADR 0015's turn-drain guarantee rests on the terminal-event signal and the sender-resends philosophy, not on a memory backstop.
- Parameters (token budget, similarity threshold, tier boundaries, half-life base) are tunable constants, not architecture.

## Rejected alternatives

- **Auto-capture at uniform importance.** Measured to be the root of recall pollution; the safety net fabricates a store out of traffic and drowns curated content (experiment rounds 1–5).
- **Full-array sync each turn** (the model receives the store and returns it) — O(store size) tokens both ways per turn, omission becomes deletion, and LLMs cannot reproduce long lists verbatim. The writeback delta is the same idea scoped to the recalled subset, with ops as the diff language.
- **Automatic per-turn consolidation** — token cost and unpredictable recall degradation; threshold triggers are the prior-art standard.
- **Reflection summaries as the default write path** — the experiment shows reflection maximizes raw recall but loses the exact keywords fact lookups need; it remains available as consolidation output, not as the per-turn mechanism.