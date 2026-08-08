# ADR 0006: Memory lifecycle — recall, act, writeback

> Status: accepted
> Date: 2026-07-25

## Context

The founding plan requires human-like memory: memories are refreshed, compressed, and forgotten, with the agent as the decision-maker. A pull-only design (memory reachable only through deliberate search) suffers silent recall failure — the agent cannot know what it does not know, so it never thinks to search. Prior art was surveyed and converges on the same mechanisms: self-editing memory (MemGPT, arXiv:2310.08560), an operation vocabulary of ADD/UPDATE/DELETE/NOOP (Mem0, arXiv:2504.19413), write-time updates of related memories (A-MEM, arXiv:2502.12110; Mem0), temporal invalidation instead of overwrite (Zep, arXiv:2501.13956), and retrieval scoring from recency, relevance, and importance with threshold-triggered reflection (Generative Agents, arXiv:2304.03442). Known risks: recall quality bottlenecks, noisy stores degrading retrieval over time, and related memories becoming inconsistent when only the recalled subset is updated.

## Decision

Memory and context are separate stores connected by an automatic loop. Context is organized around the current task; long-term memory flows in and out of it each turn.

1. **Frame** — at each user request the agent articulates the current task in context. The task statement anchors what follows.
2. **Recall** — the runtime retrieves memories using the task statement as the hybrid-search query (ADR 0002): a variable number of raw entries within a token budget, each shown with its ID and age. When nothing relevant exists, the absence is stated explicitly.
3. **Act** — the agent executes with the composed context. Accessed memories have `access_count` and `last_accessed` updated automatically.
4. **Writeback** — every model response ends with a structured memory block: for each recalled memory `keep` / `update` / `delete`, plus `add` for new facts worth persisting.
5. **Write-time coherence** — when the writeback contains `add` or `update`, the runtime retrieves the most similar existing memories and presents them for judgment as a system note the next model call reads (the same turn when it continues, otherwise the next turn's context), so related memories are updated together instead of drifting into contradiction.

Update and eviction semantics:

- **Updates supersede, never overwrite.** An update creates a new version; the old row records `superseded_by` and `valid_to` and moves to the archive table. History is preserved and rollback stays possible.
- **Capacity is bounded.** The active (searchable) index has a size cap; eviction score uses the same weighted function as retrieval ranking (importance × recency × access). Eviction moves entries to the archive — nothing is hard-deleted by the platform.
- **Consolidation** runs on shutdown or when the store crosses a threshold: clusters of episodic memories are compressed into semantic parent memories (originals archived), promoting detail into durable knowledge.
- **Context overflow** drains into memory: when the conversation window fills, the oldest turns are summarized and stored as episodic memory.
- One active session per agent; concurrent sessions are out of scope.

## Consequences

- Recall no longer depends on the agent remembering to search; writeback keeps the store fresh every turn at the cost of a small structured block appended to each response.
- Consistency of related memories is handled at write time, when the contradicting information is present — matching the point where prior systems (A-MEM, Mem0) apply the same fix.
- Supersede chains plus the archive make every destructive operation reversible; "forgetting" is a search-visibility change, not data loss.
- The full loop runs on the mock model and local embedder (ADR 0004), so it is covered by the end-to-end test.
- Deferred: explicit memory graphs or link schemas (measured benefit is small — Mem0's graph variant gains ~2%), a real embedding provider, and a benchmark-based evaluation harness. Retrieval and writeback events are logged so tuning decisions can be made from data later.
- Parameters (token budget, similarity thresholds, capacity cap, decay) are tunable constants, not architecture.
