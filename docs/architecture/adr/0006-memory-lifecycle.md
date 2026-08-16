# ADR 0006: Memory lifecycle — curation-only, salience-tiered retention

> Status: accepted
> Date: 2026-07-25 (revised in place 2026-08-14; 2026-08-17)

## Context

The founding plan requires human-like memory: memories are refreshed, compressed, and forgotten, with the agent as the decision-maker. The original revision introduced platform auto-capture of every inbound message to make "remember X" reliable with a small chat model. A structured experiment (`docs/experiments/memory-architecture/`, 38 candidate architectures × four workloads: small, 10-year-horizon, eight-category diverse, 60k-item large-scale) measured what that decision actually does: auto-capture stores every turn at uniform low importance, and on every metric that matters — recall@5, precision@5, anti-pollution, memory density (recall per KB), scale stability — the architectures that auto-capture lose to the architectures that store only curated content. Against auto-capture itself there is no ratio to quote: A1 and A2 return 0.00 recall@5 on every run they appear in, bar 0.03 for A1 on the diverse workload. Against hybrid RAG reading the same auto-captured store, the curated architectures recall 1.1× to 2.7× as much, and their recall per KB is 12.5× to 523.0× higher across the runs where both were measured — a spread that moves with the workload and with which curated architecture is asked, so no single figure stands for it and the record carries the per-workload numbers behind it. The safety net was itself the pollution.

The same experiment identified the architecture that holds up on every workload: curation-only storage, a salience tier on every stored memory, salience-scaled Ebbinghaus decay, and tiered retention. This record revises the lifecycle to that shape. The prior art survey stands (MemGPT self-editing, Mem0's ADD/UPDATE/DELETE vocabulary, A-MEM/Mem0 write-time updates, Zep temporal invalidation, Generative Agents scoring) — the experiment adds the empirical ranking of the mechanisms.

That experiment also produced a mechanism this record no longer carries: supersession on insert, where an `add` whose content was highly similar to an active memory superseded it in place. No threshold separates a correction from two facts that must both survive — on all three embedders measured, the correction family's median similarity sits below the coexisting family's minimum, so the highest-accuracy threshold is the one that supersedes nothing (`docs/experiments/memory-creation/`, `docs/experiments/memory-retrieval/`). Removal rather than retuning was tracked in #334, and it waited on a replacement: ADR 0018 is that replacement, and it needs no threshold, because a fact is held at the address of the question it answers and identity is computed rather than inferred. The code still supersedes until #336's sequence lands.

## Decision

Memory and context are separate stores connected by an automatic loop. Context is organized around the current task; long-term memory flows in and out of it each turn.

1. **Frame** — at each user request the agent articulates the current task in context. The task statement anchors what follows.
2. **Recall** — the runtime retrieves memories using the task statement as the hybrid-search query (ADR 0002): a variable number of raw entries within a token budget, each shown with its ID and age. When nothing relevant exists, the absence is stated explicitly.
3. **Act** — the agent executes with the composed context. Accessed memories have `access_count` and `last_accessed` updated automatically.
4. **Writeback** — every model response is a document the model API's response schema enforces: the visible reply, and the list of facts the turn states. The list is a field of the response, never a block the model may omit, and an empty list is an answer it states. Each entry is an address, a value, and — where the sentence adds a value that leaves the question alone — an operation (ADR 0018).
5. **Write-time coherence** — when the writeback states a fact, the runtime retrieves the most similar existing memories and presents them for judgment as a system note the next model call reads, so related memories are corrected together instead of drifting into contradiction.

Storage discipline (new):

- **Curation-only.** Only the writeback writes to the store. No platform auto-capture of inbound messages: no path stores a message at accept time. A message the model never states as a fact never enters long-term memory.
- **Salience is platform-derived, never model-written.** The contract does not ask the model for a score (the `importance_score` exclusion stands). The platform derives salience from the operation the write states: a replacement (a correction) is highest, a fact added beside what is held is high, an episodic observation — which only the context-overflow summary now produces — is mid. Salience is set at insert time and used by scoring and retention below.
- **Retrieval scoring.** Score = salience + relevance + recency, where recency is an Ebbinghaus decay whose half-life scales with salience (`half-life = 24h × (1 + salience)`): high-salience memories fade slowly, low-salience memories fade fast. Relevance stays the hybrid cosine + FTS5 blend (ADR 0002).
- **Identity is not inferred from similarity.** No write path decides that one memory supersedes another by how alike their contents are. Which memory a write is for is settled by the address it computes, not by a threshold over a similarity score (ADR 0018).
- **Tiered retention.** After a completed turn, the runtime drops active memories by tier: high salience (≥ 0.6) is kept indefinitely; mid salience is retained for a bounded window (~100 turns); low salience is dropped promptly. Nothing is hard-deleted — drops move to the archive.
- **Eviction and consolidation carry forward.** The active-index capacity cap and threshold-triggered consolidation (ADR 0002, ADR 0011) operate unchanged over the curated store; retention makes the cap rarely binding.
- **Context overflow** still drains into memory: when the conversation window fills, the oldest turns are summarized by the model into an episodic memory. This is model-generated content, not platform capture, so it obeys the same curation rule.

Update semantics are unchanged: updates supersede rather than overwrite; every change is reversible through the archive.

## Consequences

- The store holds only what the model judged worth keeping, so the noise floor is empty by construction, and recall per KB is 12.5× to 523.0× that of hybrid RAG reading an auto-captured store, depending on the workload and the architecture (`docs/experiments/memory-architecture/`). Against auto-capture's own retrieval there is no ratio: it returns nothing. **Precision does not follow from that emptiness**: measured against a store holding nothing but curated facts, the shipped read path still returns mostly non-answers in its top ten, under either embedder (`docs/experiments/memory-retrieval/` §What the numbers say). Curation removes one cause of imprecision; what the read path returns is its own to earn, and #411 tracks that.
- The model's failure mode inverts: a model that never writes back (issue #302) leaves memory empty, and that emptiness is honest — there is no safety net fabricating a store out of traffic. The response schema removes the shape half of that failure — a response has no hole to leave the memory field in — and leaves the content half, which is the model's willingness to state what it read. That residual is guided by the contract text and observable on the writeback event stream, where an empty list on a fact-rich turn is visible.
- Nothing in this lifecycle keeps the latest version of a fact authoritative. Curation decides what enters the store and retention decides what leaves it; which of two memories about one fact is current is a question this record no longer answers, and ADR 0018 is where it is answered.
- Salience-scaled decay means a 10-year-old preference and a yesterday conversation coexist correctly: the preference fades slowly, the chatter fades fast.
- ADR 0015's turn-drain guarantee rests on the terminal-event signal and the sender-resends philosophy, not on a memory backstop.
- Parameters (token budget, tier boundaries, half-life base) are tunable constants, not architecture.

## Rejected alternatives

- **Auto-capture at uniform importance.** Measured to be the root of recall pollution; the safety net fabricates a store out of traffic and drowns curated content (`docs/experiments/memory-architecture/`).
- **Full-array sync each turn** (the model receives the store and returns it) — O(store size) tokens both ways per turn, omission becomes deletion, and LLMs cannot reproduce long lists verbatim. The writeback states only what the turn asserted, and the address it computes is what says where each value lands.
- **A writeback the model may omit** — the fenced block the contract asked for and nothing enforced. Measured at 0.00 context-recall with 0 of 15 responses emitting one voluntarily; shape-enforcement is the change that moves it, at no extra call (`docs/experiments/memory-cycle/`).
- **Automatic per-turn consolidation** — token cost and unpredictable recall degradation; threshold triggers are the prior-art standard.
- **Reflection summaries as the default write path** — the experiment shows reflection maximizes raw recall but loses the exact keywords fact lookups need; it remains available as consolidation output, not as the per-turn mechanism.