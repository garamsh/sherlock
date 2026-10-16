# ADR 0019: Recall older conversation by embedding similarity and an entity walk beside recency, and let the agent search again during work

> Status: accepted
> Date: 2026-08-17 (revised in place 2026-10-05)

## Context

The prior read step resolved a question to a model-written address and widened to a subject on a miss. The running measurement in `research/recall-specificity/` found no addressed hits in 30 probes; every answer that carried the planted value came from widening. Source-linked summaries need a read that can find several topics and survive paraphrase without claiming that a candidate is an answer.

The first source-linked read asked the session model for up to four search phrases and ranked mid summaries by FTS5 lexical relevance. The replication in `research/mid-memory-rag/` (§Replication (issue #856)) compared it over 280 primary queries with ranking by embedding similarity, at three batch sizes, and with a hybrid of both.

Similarity alone does not follow a chain: a question that names one entity, whose answer lies in an episode reached only through a second entity, finds the first hop and misses the rest. `research/chained-recall/` (#874, #885) linked each episode to the entities its synthesis names and walked those links; `research/memory-encoding/attributed/` (#916) measured that walk, with its episodes attributed to their subject, against the shipped recall.

## Decision

During the act loop the agent can search again: the reserved `memory_search` call ranks mid episodes by cosine similarity to a query the model writes, with the conversation and its note in view, and returns at most five with their summaries and source intervals. The per-tool budget bounds it at six calls a turn. The contract tells the model to write a standalone query that resolves references such as "that" or "what I said earlier", and to search again with another query when the first misses.

Before the act loop, recall offers the two most recent mid episodes, then fills five more slots from two ranked lists in turn, skipping any episode already delivered; a list that runs out gives its turns to the other. No model call precedes the act loop. A full batch is four completed visible messages, so each summary covers a short interval.

- **The similar list** ranks episodes by cosine similarity between the embedding of the task text and each stored mid summary's embedding. There is no similarity threshold: an unrelated task still receives the most similar episodes, ties falling to the newest.
- **The walk's list** ranks the episodes an entity walk reaches. The synthesis call that writes an episode's summary also names the entities its batch mentions; the names are normalized (lowercased, without a possessive or leading stopwords) and linked to the episode when it commits. The walk's seeds are the stored names the task contains as whole words, each weighted by one over the number of episodes naming it. Personalized PageRank at damping 0.5 over the graph of episodes and entities ranks every episode it reaches, highest first, the newer on a tie.
- **Attribution.** An episode's subject is the first entity its synthesis named. A walk episode whose subject is none of the seeds opens with `About "<subject>"; the question names "<seed>".`, every seed named. A walk episode about a seed, and every similar episode, is shown plain.

`memory_search` fills its five results the same way from its query. There is no switch for the walk: a query naming no stored entity receives the similar list alone.

**Synthesis is asked up to three times.** The synthesis call that writes an episode and names its entities must answer with exactly one valid `memory_summary` call. The tool is offered, not forced. An answer that is not one valid call is asked again, up to three calls a batch; a model error is not. After the third unusable answer the batch fails with that answer's error and stays pending, as any failed synthesis does. Linking an episode from before entities goes through the same call.

Recent episodes remain available whatever the ranking. Pending source previews keep the unsummarized interval reachable with small rolling windows, bounded by the batch size. Each summary and preview is bounded in length and labelled as such, with an inspection reference. A candidate is not proof that a requested fact exists; original wording requires source inspection.

## Evidence and limits

- The replication delivered the evidence for 209 of 280 primary queries at batch 4 (B4), against 168 for the phrase call with FTS5. B4 led in every family: exact 64 against 60, paraphrase 52 against 31, implicit 28 against 19, correction 65 against 58. Batch 8 delivered 194 and batch 16 delivered 164, so smaller batches help similarity.
- The hybrid, both relevant lists alternated, delivered 203. It keeps the phrase call and the FTS5 path beside the embedding path, so it is not taken.
- Parity: the production recall, run over the same corpus with the recorded batch-4 syntheses and `BAAI/bge-base-en-v1.5` served over the OpenAI-compatible endpoint, delivered 212 of 280 (`research/mid-memory-rag/` §Production parity (issue #858)).
- The corpus is model-written, and its paraphrase and implicit families were selected to miss lexically, which favours similarity. A single local embedding model was measured. Neither ranking nor recency establishes truth; the agent must inspect the linked evidence.
- `TestSessionRun_SearchRanksWithTheModelsQuery` pins the search call's query and bound, and `TestConversation_SearchReturnsTheEpisodeForTheModelsQuery` its result through the store.
- `TestConversationRecall_RecentFirstThenSimilarDeduplicatedAndBounded` pins the selection, and `TestConversationRecall_RecentCorrectionAndOlderRelevantEpisode` shows a weak-overlap recent correction surviving while an older similar episode stays reachable. This is deterministic reachability, not model answer quality.
- The opt-in `SHERLOCK_EPISODIC_MEMORY_PROBE=1 go test ./tests -run '^TestEpisodicMemory_LiveProbe$' -count=1 -v` ran at `2589be1` on 2026-09-24 against MiniMax-M2, under the phrase read and batch 8: 4 HTTP calls, 4,301 input tokens, 684 output tokens, no runtime failure. It synthesized a correction batch live, retrieved its episode, inspected the correction source, and answered with the corrected name. Run again at `23f5a48` on 2026-09-28, with the entity walk and the synthesis naming entities: 3 HTTP calls, 4,358 input tokens, 682 output tokens, no runtime failure, and the same four outcomes. The probe checks the extended synthesis call live; it does not check the walk, which the parity test covers.
- **Entity walk with attribution.** #916 (PR #919, `research/memory-encoding/attributed/`) ran the walk with attribution (`b4-model-attr` of `research/chained-recall/arms/links/`) against the shipped recall, live on MiniMax-M2, on a fresh corpus. An absent query was scored by the owner's rule of 2026-09-28: an answer is acceptable when it abstains, or when it gives an associate's value and says whose it is.
  - Primary chains applied: 66 of 100 against 5 (63 to 2, exact McNemar p = 1.2e-16); implicit two-hop chains 8 against 0.
  - Every guardrail held: absent N 55 against 56, absent NC 29 against 30, K 27 against 27, KU 19 against 18, C1 30 against 27, E 20 against 21.
  - Under the strict rule, that only abstention is acceptable, it is not valid: absent N 35 against 56. It states the associate's value on 54 of 60 absent queries, naming whose value it is on 53.
  - Parity: `TestLinkedRecall_MatchesTheStudiedSettingOnItsRecordedProbes` builds #916's store through the production write path from its recorded syntheses and entities, and holds production recall on all 470 probes and `memory_search` on all 505 hop statements to the items, order and attribution openings `b4-model-attr` recorded, with no model call.
  - `TestLinked_WalksPastTheSeedsBestFirst` pins the walk's ranking on a small graph, `TestConversationRecall_FillsHistoricalSlotsFromSimilarAndTheWalkInTurn` and `TestSessionRun_SearchTakesSimilarAndWalkEpisodesInTurn` the slots and the attribution.
- **Synthesis attempts.** #937's second run, live on MiniMax-M2 through the real images at `2ad4c13`, completed two turns and created no mid episode: the synthesis call answered without `memory_summary`, on its one attempt (#946). The studies whose syntheses this design rests on gave each batch three attempts (`research/chained-recall/arms/links/synthesis.go`, `entityBatch`, at `b200295`). `TestConversationSummary_AsksAgainAfterAnAnswerWithoutTheCall`, `TestConversationSummary_AsksAgainAfterAMalformedCall`, `TestConversationSummary_DoesNotAskAgainAfterAModelError` and `TestRecordTurn_ThreeAnswersWithoutTheCallLeaveTheBatchPending` pin the attempts.
- **The call is not forced.** MiniMax documents `tool_choice` only as `none` or `auto`, read 2026-10-05: https://platform.minimax.io/docs/api-reference/text-post defines it as a string taking `none` or `auto`, https://platform.minimax.io/docs/api-reference/text-chat-anthropic says only `auto` and `none` are supported, and https://platform.minimax.io/docs/api-reference/text-openai-api, the OpenAI-compatible page the configured endpoint follows, names no `tool_choice`. A named function there is undocumented and could be refused on every synthesis, so no provider forces the call.
- **Search during work.** `research/agent-context/` tested the search call with the bounded note of ADR 0011, as candidate A, against the shipped structure.
  - Issue #862: A applied the evidence in 209 of 280 primary probes and the shipped structure in 124 (A only 86, shipped only 1). Follow-ups whose referent lay beyond the window went from 9 to 49 of 70.
  - Issue #864, on a new seed: 192 against 122 (74 to 4), and those follow-ups from 4 to 40.
  - Both of those verdicts are undetermined, because the oracle control failed. In #862 it counted a key word in a summary, which summaries reworded. In #864 it counted a top-five mid holding the evidence source; every miss was a mid that mixes the evidence with a neighbouring exchange and so ranks lower (257 of 280, 266 required, in both arms).
  - Issue #870 (PR #873) placed each evidence exchange in a batch of its own, so the oracle could pass or fail on ranking alone. Every control passed: oracle reach 277 and 279 of 280, and every stored evidence batch unmixed. A applied the evidence in 212 against 171 (42 to 1, p = 1.0e-11), with its guardrails holding, and those follow-ups went from 31 to 53 of 70. **The preregistered verdict is valid**, and it is the basis for adopting the search call.

## Consequences

- Each turn embeds the task text once, and each new mid episode is embedded once after it commits. Both use the embedder configured beside the chat model (ADR 0004); the episode's vector is stored with it (ADR 0002).
- Batch 4 costs about 1.5 times batch 8's synthesis spend per message (`research/mid-memory-rag/README.md` §Replication (issue #856), its §Cost). Naming entities adds no call; in #916 the synthesis read 388 and wrote 238 tokens per batch, against 327 and 203 without them.
- The walk makes no model call. In #916 it moved recall context from 2,859 to 3,011 characters per turn, and stored characters per source character from 0.741 to 0.823. Each recall and search reads every entity link and walks the graph in process.
- A batch whose answers are unusable costs up to three synthesis calls before it is left pending.
- Episodes stored before entities carry no links, so the walk cannot reach them until each is named. After each completed turn, off the turn path, one of them is sent through the same synthesis call over its own sources; its stored summary is kept (`memory.md` §Existing databases).
- Candidate count and preview length are bounded. Recent and pending material can be unrelated to the question and is not presented as a matching result.
- Recent context remains in the rolling session; recall is for older information that need not fit there.

## Read-policy limits

- Pre-turn recall runs once, with the raw task text. What the model later searches for, and whether it searches at all, is the model's choice: it searched in 39, 39 and 23 of 70 probes whose referent lay beyond the window in #862, #864 and #870.
- Neither similarity nor recency reliably exposes implicit needs such as writing methods or coding conventions: the best arm reached 28 of 70 implicit queries. Iterative model-directed search remains separate experimental work.
- Without a threshold, recall cannot tell the agent that nothing relevant exists; the replication's top-1 cosine separated present from absent queries only partly.
- The walk puts an associate's episode in view when the question names someone the store never linked to the answer. Attribution says whose episode it is; whether the model then answers from it is the model's choice, and the owner's rule accepts that answer when it is attributed.
- A seed must match a stored name exactly. A query that names an entity differently from the synthesis seeds nothing; the studied corpora reused each name exactly, so this failure was not measured.

## Rejected alternatives

- Resolving to the old question address retains the mismatch measured in the running system.
- Putting the entire source archive into each prompt removes the context bound.
- Keeping the phrase call and FTS5 beside similarity (the hybrid) delivered fewer queries than B4 and keeps two read paths.
- Similarity-only recall applied 5 of 100 primary chains against the walk's 66 (#916).
