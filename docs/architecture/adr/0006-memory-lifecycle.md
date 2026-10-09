# ADR 0006: Preserve recent context and source-linked older conversation

> Status: accepted
> Date: 2026-07-25 (revised in place 2026-09-26; 2026-10-10)

## Context

The addressed-only lifecycle dropped old conversation when the rolling context compacted. A model's curated writeback did not preserve the original user wording, so later exact-detail questions had no source to inspect. Issue #823 chose a bounded recent window together with durable linked summaries of older visible conversation.

## Decision

- Keep the newest session messages up to the configured context bound. Restore recent visible user and assistant messages from the store after restart.
- Commit each completed inbound user message with its gateway ID and visible assistant outputs as source records before synthesis. Synthesis failure leaves sources durable for retry at the next completion. System instructions, tool results, hidden reasoning, and credentials do not become memory sources.
- Use the rolling recent-message window as short memory. A separate short-summary tier adds no independent responsibility.
- Synthesize each full four-message cross-turn source batch into one immutable mid episode, with direct links to its original message interval. Episodes are independent: the model receives the batch, not every previous mid summary.
- Preserve partial batches durably and offer pending source previews alongside recalled episodes so compaction cannot hide the interval before a checkpoint.
- Before the act loop, recall the two most recent mid episodes and up to five more most similar to the task (ADR 0019), alongside up to four pending source previews. The reserved memory boundary inspects only original source messages by ID when detail matters.

## Consequences


- Compaction removes old messages from the current prompt but not from the durable source archive.
- The archive and summaries have one SQLite owner. There is no separate long-term memory writer competing with source capture.
- Cross-turn grouping amortizes model cost. A correction enters an episode when its batch fills and remains visible as pending source evidence sooner.
- Summary fidelity and answer grounding require measurement; deterministic model fixtures cannot establish them. The offline prototype in issue #821 measured reachability on hand-authored summaries, not model answer quality.

## Rejected alternatives

- Keeping every source message in the rolling prompt would remove the context bound.
- Recursively summarizing every prior mid state would make episodes less specific to retrieval and propagate earlier synthesis errors.
- Using the old addressed writeback as another active authority would let an unlinked value compete with inspectable source evidence.
