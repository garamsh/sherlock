# ADR 0019: Recall older conversation by embedding similarity beside recency, and let the agent search again during work

> Status: accepted
> Date: 2026-08-17 (revised in place 2026-09-26)

## Context

The prior read step resolved a question to a model-written address and widened to a subject on a miss. The running measurement in `research/recall-specificity/` found no addressed hits in 30 probes; every answer that carried the planted value came from widening. Source-linked summaries need a read that can find several topics and survive paraphrase without claiming that a candidate is an answer.

The first source-linked read asked the session model for up to four search phrases and ranked mid summaries by FTS5 lexical relevance. The replication in `research/mid-memory-rag/` (§Replication, issue #856) compared it over 280 primary queries with ranking by embedding similarity, at three batch sizes, and with a hybrid of both.

## Decision

During the act loop the agent can search again: the reserved `memory_search` call ranks mid episodes by cosine similarity to a query the model writes, with the conversation and its note in view, and returns at most five with their summaries and source intervals. The per-tool budget bounds it at six calls a turn. The contract tells the model to write a standalone query that resolves references such as "that" or "what I said earlier", and to search again with another query when the first misses.

Before the act loop, recall offers the two most recent mid episodes and up to five more ranked by cosine similarity between the embedding of the task text and each stored mid summary's embedding, deduplicated by ID, recent first. No model call precedes the act loop. A full batch is four completed visible messages, so each summary covers a short interval. There is no similarity threshold: an unrelated task still receives the most similar episodes, ties falling to the newest.

Recent episodes remain available whatever the ranking. Pending source previews keep the unsummarized interval reachable with small rolling windows, bounded by the batch size. Each summary and preview is bounded in length and labelled as such, with an inspection reference. A candidate is not proof that a requested fact exists; original wording requires source inspection.

## Evidence and limits

- The replication delivered the evidence for 209 of 280 primary queries at batch 4 (B4), against 168 for the phrase call with FTS5. B4 led in every family: exact 64 against 60, paraphrase 52 against 31, implicit 28 against 19, correction 65 against 58. Batch 8 delivered 194 and batch 16 delivered 164, so smaller batches help similarity.
- The hybrid, both relevant lists alternated, delivered 203. It keeps the phrase call and the FTS5 path beside the embedding path, so it is not taken.
- Parity: the production recall, run over the same corpus with the recorded batch-4 syntheses and `BAAI/bge-base-en-v1.5` served over the OpenAI-compatible endpoint, delivered 212 of 280 (`research/mid-memory-rag/` §Production parity).
- The corpus is model-written, and its paraphrase and implicit families were selected to miss lexically, which favours similarity. A single local embedding model was measured. Neither ranking nor recency establishes truth; the agent must inspect the linked evidence.
- `TestSessionRun_SearchRanksWithTheModelsQuery` pins the search call's query and bound, and `TestConversation_SearchReturnsTheEpisodeForTheModelsQuery` its result through the store.
- `TestConversationRecall_RecentFirstThenSimilarDeduplicatedAndBounded` pins the selection, and `TestConversationRecall_RecentCorrectionAndOlderRelevantEpisode` shows a weak-overlap recent correction surviving while an older similar episode stays reachable. This is deterministic reachability, not model answer quality.
- The opt-in `SHERLOCK_EPISODIC_MEMORY_PROBE=1 go test ./tests -run '^TestEpisodicMemory_LiveProbe$' -count=1 -v` ran at `2589be1` on 2026-09-24 against MiniMax-M2, under the phrase read and batch 8: 4 HTTP calls, 4,301 input tokens, 684 output tokens, no runtime failure. It synthesized a correction batch live, retrieved its episode, inspected the correction source, and answered with the corrected name. It has not been run since this revision.
- **Search during work.** `research/agent-context/` tested the search call with the bounded note of ADR 0011, as candidate A, against the shipped structure.
  - Issue #862: A applied the evidence in 209 of 280 primary probes and the shipped structure in 124 (A only 86, shipped only 1). Follow-ups whose referent lay beyond the window went from 9 to 49 of 70.
  - Issue #864, on a new seed: 192 against 122 (74 to 4), and those follow-ups from 4 to 40.
  - Both of those verdicts are undetermined, because the oracle control failed. In #862 it counted a key word in a summary, which summaries reworded. In #864 it counted a top-five mid holding the evidence source; every miss was a mid that mixes the evidence with a neighbouring exchange and so ranks lower (257 of 280, 266 required, in both arms).
  - Issue #870 (PR #873) placed each evidence exchange in a batch of its own, so the oracle could pass or fail on ranking alone. Every control passed: oracle reach 277 and 279 of 280, and every stored evidence batch unmixed. A applied the evidence in 212 against 171 (42 to 1, p = 1.0e-11), with its guardrails holding, and those follow-ups went from 31 to 53 of 70. **The preregistered verdict is valid**, and it is the basis for adopting the search call.

## Consequences

- Each turn embeds the task text once, and each new mid episode is embedded once after it commits. Both use the embedder configured beside the chat model (ADR 0004); the episode's vector is stored with it (ADR 0002).
- Batch 4 costs about 1.5 times batch 8's synthesis spend per message (replication §Cost).
- Candidate count and preview length are bounded. Recent and pending material can be unrelated to the question and is not presented as a matching result.
- Recent context remains in the rolling session; recall is for older information that need not fit there.

## Read-policy limits

- Pre-turn recall runs once, with the raw task text. What the model later searches for, and whether it searches at all, is the model's choice: it searched in 39, 39 and 23 of 70 probes whose referent lay beyond the window in #862, #864 and #870.
- Neither similarity nor recency reliably exposes implicit needs such as writing methods or coding conventions: the best arm reached 28 of 70 implicit queries. Iterative model-directed search remains separate experimental work.
- Without a threshold, recall cannot tell the agent that nothing relevant exists; the replication's top-1 cosine separated present from absent queries only partly.

## Rejected alternatives

- Resolving to the old question address retains the mismatch measured in the running system.
- Putting the entire source archive into each prompt removes the context bound.
- Keeping the phrase call and FTS5 beside similarity (the hybrid) delivered fewer queries than B4 and keeps two read paths.
