# ADR 0019: Recall older conversation by embedding similarity beside recency

> Status: accepted
> Date: 2026-08-17 (revised in place 2026-09-26)

## Context

The prior read step resolved a question to a model-written address and widened to a subject on a miss. The running measurement in `docs/research/recall-specificity/` found no addressed hits in 30 probes; every answer that carried the planted value came from widening. Source-linked summaries need a read that can find several topics and survive paraphrase without claiming that a candidate is an answer.

The first source-linked read asked the session model for up to four search phrases and ranked mid summaries by FTS5 lexical relevance. The replication in `docs/research/mid-memory-rag/` (§Replication, issue #856) compared it over 280 primary queries with ranking by embedding similarity, at three batch sizes, and with a hybrid of both.

## Decision

Recall offers the two most recent mid episodes and up to five more ranked by cosine similarity between the embedding of the task text and each stored mid summary's embedding, deduplicated by ID, recent first. No model call precedes the act loop. A full batch is four completed visible messages, so each summary covers a short interval. There is no similarity threshold: an unrelated task still receives the most similar episodes, ties falling to the newest.

Recent episodes remain available whatever the ranking. Pending source previews keep the unsummarized interval reachable with small rolling windows, bounded by the batch size. Each summary and preview is bounded in length and labelled as such, with an inspection reference. A candidate is not proof that a requested fact exists; original wording requires source inspection.

## Evidence and limits

- The replication delivered the evidence for 209 of 280 primary queries at batch 4 (B4), against 168 for the phrase call with FTS5. B4 led in every family: exact 64 against 60, paraphrase 52 against 31, implicit 28 against 19, correction 65 against 58. Batch 8 delivered 194 and batch 16 delivered 164, so smaller batches help similarity.
- The hybrid, both relevant lists alternated, delivered 203. It keeps the phrase call and the FTS5 path beside the embedding path, so it is not taken.
- Parity: the production recall, run over the same corpus with the recorded batch-4 syntheses and `BAAI/bge-base-en-v1.5` served over the OpenAI-compatible endpoint, delivered 212 of 280 (`docs/research/mid-memory-rag/` §Production parity).
- The corpus is model-written, and its paraphrase and implicit families were selected to miss lexically, which favours similarity. A single local embedding model was measured. Neither ranking nor recency establishes truth; the agent must inspect the linked evidence.
- `TestConversationRecall_RecentFirstThenSimilarDeduplicatedAndBounded` pins the selection, and `TestConversationRecall_RecentCorrectionAndOlderRelevantEpisode` shows a weak-overlap recent correction surviving while an older similar episode stays reachable. This is deterministic reachability, not model answer quality.
- The opt-in `SHERLOCK_EPISODIC_MEMORY_PROBE=1 go test ./tests -run '^TestEpisodicMemory_LiveProbe$' -count=1 -v` ran at `2589be1` on 2026-09-24 against MiniMax-M2, under the phrase read and batch 8: 4 HTTP calls, 4,301 input tokens, 684 output tokens, no runtime failure. It synthesized a correction batch live, retrieved its episode, inspected the correction source, and answered with the corrected name. It has not been run since this revision.

## Consequences

- Each turn embeds the task text once, and each new mid episode is embedded once after it commits. Both use the embedder configured beside the chat model (ADR 0004); the episode's vector is stored with it (ADR 0002).
- Batch 4 costs about 1.5 times batch 8's synthesis spend per message (replication §Cost).
- Candidate count and preview length are bounded. Recent and pending material can be unrelated to the question and is not presented as a matching result.
- Recent context remains in the rolling session; recall is for older information that need not fit there.

## Read-policy limits

- Recall runs once before the act loop. The act loop can inspect sources but cannot issue another memory search.
- Neither similarity nor recency reliably exposes implicit needs such as writing methods or coding conventions: the best arm reached 28 of 70 implicit queries. Iterative model-directed search remains separate experimental work.
- Without a threshold, recall cannot tell the agent that nothing relevant exists; the replication's top-1 cosine separated present from absent queries only partly.

## Rejected alternatives

- Resolving to the old question address retains the mismatch measured in the running system.
- Putting the entire source archive into each prompt removes the context bound.
- Keeping the phrase call and FTS5 beside similarity (the hybrid) delivered fewer queries than B4 and keeps two read paths.
