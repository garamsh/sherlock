# ADR 0008: Memory lifecycle — task-anchored recall, compose, act, update, consolidate

> Status: accepted
> Date: 2026-07-25

## Context

PLAN §3 requires human-like memory: memories are refreshed, compressed, and forgotten, with the agent as the decision-maker. A pull-only design (memory reachable only through the search tool) suffers silent recall failure — the agent cannot know what it does not know, so it never thinks to search. Raw automatic injection of search results pollutes the context. The project owner wants a human-shaped flow: recognize the task first, recall memories cued by that task, organize thoughts (compress), then act — and let acting update memory, with compression applied proactively so the memory count stays bounded.

## Decision

Memory and context are separate stores connected by an automatic, task-anchored loop. Context is organized around the current task; long-term memory flows in and out of it.

1. **Frame** — at each user request the agent first articulates the current task in context. The task statement anchors everything that follows.
2. **Recall** — the runtime automatically retrieves memories using the task statement as the hybrid-search query (ADR 0003). A bounded set of candidates with their IDs is produced. Deeper deliberate recall remains available through the search tool.
3. **Compose** — recalled memories and the current task are composed into the working context. If the recalled set exceeds a token budget, it is compressed into an LLM-summarized brief before injection; raw bulk injection never happens.
4. **Act** — the agent executes with the composed context. Accessed memories have `access_count` and `last_accessed` updated automatically.
5. **Update** — at turn or task end the agent emits an explicit memory decision: store, update (referencing an existing memory ID), or none. When a new memory closely matches existing ones, the agent is steered to update or merge instead of storing a duplicate, keeping the memory count bounded at write time.
6. **Consolidate** — on session end or when the store crosses a size threshold, the agent runs reflection: clusters of episodic memories are compressed into semantic parent memories (originals archived per ADR 0003) and low-value memories (`importance` × time decay × `access_count`) are forgotten.

Compression therefore happens at three points: compose-time (transient, context only), store-time (persistent merge), and consolidate-time (persistent episodic-to-semantic promotion). Memories carry a type field (`episodic` | `semantic`); consolidation is the transition event between them.

Context overflow is handled by the same pipeline: when the conversation window fills, the oldest turns are summarized and stored as episodic memory, so context pressure drains into long-term memory instead of being dropped.

## Consequences

- Recall no longer depends on the agent remembering to search; context stays task-focused instead of accumulating raw dumps.
- Memory count is bounded proactively (store-time merge) rather than only by periodic cleanup.
- Cost: one search per task frame (near-zero with the local embedder); compose-time compression adds an LLM call only when over budget.
- Task boundary equals a user request; sub-tasks within a request reuse the same frame. Parameters (k, token budget, thresholds, decay) are tunable constants, not architecture.
- The full loop is exercisable with the mock model (ADR 0005), so it is covered by the MVP end-to-end test.
