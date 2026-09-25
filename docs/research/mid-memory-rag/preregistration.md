# Mid-memory retrieval preregistration

Fixes the arms, corpus, queries, metrics, budgets and decision rule for issue #854 before any paid call.

## Question

Does the owner's mid-memory structure (one size `n` for window and batch, mid memories ranked by embedding similarity and separately by recency, merged by ID, no pre-turn model call) deliver the evidence for an older-conversation query more often than the shipped recall, over at least 300 mid memories? And would adopting it shrink the memory code?

## Corpus and queries

- [`corpus.py`](corpus.py) generates [`corpus.json`](corpus.json) and [`queries.json`](queries.json) from seed 854, with no model call. Frozen SHA-256, recorded before any ranking was computed on any store:
  - `corpus.json` `e609d6fc283203a21e071484a6bb499186146466cb2ebbbd00662309ae171209`
  - `queries.json` `cf4a7e8e7d270d0b0160837f5110cd2f64b1db99c2d576050bdf63f849fe6c9e`
  - `corpus.py` `36c3175b4fcbd468a99fd756b058f60e07145ed9cfe7507bb4e7f99b97f8f6a3`
  - None of the three changes after this commit.
- 300 episodes of 8 alternating user/assistant messages: 2,400 sources, IDs 1–2400. Episode `e` holds sources `8(e−1)+1 … 8e`, which is exactly the batch the production store turns into mid `e`. 298 episodes are templated filler over 15 domains with shared attributes and entity pools, so each planted fact has same-domain and same-attribute hard negatives. The two newest episodes are authored whole.
- 55 queries, each with its evidence source IDs:

| Family | n | Evidence |
|---|---:|---|
| exact | 10 | query reuses the evidence's own words |
| paraphrase | 10 | same fact, different wording |
| implicit | 10 | a rule or preference the task needs but never names; preflight proves production FTS finds no shared token |
| correction | 10 | an older assertion, and a newer one at least 40 episodes later that must be reached |
| absent | 10 | the fact was never stated; a near-miss about the same entity exists |
| recent | 5 | vague reference to the two newest episodes |

- Planted evidence replaces one user message in an episode chosen by seed from episodes 1–296, with the following assistant message a content-free acknowledgement. No evidence sits in the two newest mids except the recent family.

## Summaries

- Every mid summary is synthesized live by the production summarizer (`internal/agent/memory/summary.go`, `ConversationSummary`) through the production OpenAI-compatible client (`internal/agent/openai`) against MiniMax-M2. [`bridge.go`](bridge.go) points that client at a local capture server: the exact production request body is recorded, sent by [`run.py`](run.py) to `https://api.minimax.io/v1/chat/completions` with `max_tokens` added and nothing else changed, and the provider's body is fed back through production decoding and validation.
- The store is then built by production `RecordTurn` and `Accumulate`, with each batch replayed from its recorded exchange; the bridge refuses a batch whose request differs from the recorded one.
- A synthesis the production summarizer rejects is retried at most twice for that episode; retries are recorded. A third rejection stops collection.
- Summaries are measured as synthesized. None is edited, and summary fidelity counts against every summary-based arm equally.

## Arms

All arms present at most 7 mids: the 2 most recent (`RecentMid(2)`) first, then relevant candidates, deduplicated by ID and capped at 7. That is the production merge in `internal/agent/memory/conversation.go`; only what fills the relevant list differs.

| Arm | Relevant list | Pre-turn model call |
|---|---|---|
| A, shipped | production `ConversationRecall` with the production phrase call, FTS5 over summaries | 1 (MiniMax-M2) |
| A0, added | production `ConversationRecall` with no model: task text into FTS5 | 0 |
| B, owner | cosine similarity of the raw query embedding against each mid summary embedding | 0 |
| C | B, with A's recorded phrases embedded and each mid scored by its maximum cosine over them; production fallback to task text | 1 (shared with A) |
| D, added | B, with each mid embedded from its 8 source messages instead of its summary | 0 |

- A0 isolates the phrase call from the lexical index; D shows whether the synthesized summary is needed for similarity retrieval.
- A and C use one recorded phrase call per query, made once. The relevant-list depth for recall@k beyond the delivered 7 is production `Search` (A, A0) or the cosine order (B, C, D) to depth 20.
- Embedding model. The first paid call probes `POST https://api.minimax.io/v1/embeddings` with model `embo-01`, one text. MiniMax's API reference lists no embedding endpoint (checked 2026-09-25). If the probe returns vectors with `base_resp.status_code` 0, embo-01 becomes the primary embedder for B, C and D, and the local model below is reported as an added arm; otherwise the probe's status and body are recorded and the local model is primary. Local model: `BAAI/bge-base-en-v1.5` (768 dimensions) through `fastembed==0.7.3` under `uv`, with the model card's query instruction prefixed to queries and nothing prefixed to passages. It is a real open retrieval embedder, not TF-IDF.

## Controls

Run on the synthesized store, before scoring. Preflight runs the same checks on a stand-in store whose summaries are the raw episode text.

- **Oracle reach**, 20 mids sampled with seed 8540: A must deliver the target when the phrase call returns the target's summary; A0 when the task text is the summary; B and C must rank the target first when the query is its summary; D when the query is its source text. An arm passes at 19 of 20.
- **Similarity is not recency**: B's top-2 equals the recency top-2 in fewer than half of the 55 queries, and B's top-1 takes at least 20 distinct mids.
- **Implicit queries are implicit**: production `Search` over a one-episode store holding only the evidence finds nothing for the query, and finds the evidence for its own text.
- **Scorer can fail**: [`score_test.py`](score_test.py) shows a ranking that omits the evidence scores zero and the verdict turns with the paired counts.
- **Drill-down path works**: in the live run, at least one `memory_inspect` call per arm returns a source.

## Metrics

Per arm and per family, with the per-query table committed:

- Delivered hit: an evidence source lies inside a delivered mid's inclusive interval (source-level). Wilson 95% interval.
- Source-level recall@1/3/5/7/10/20 and MRR@20 over the relevant list alone.
- Correction: newest delivered; obsolete delivered without the newest; newer ranked no lower than older.
- Absent: fraction with a non-empty relevant list. An unthresholded similarity list is non-empty by construction, so this is descriptive; the AUC of B's top-1 cosine for present against absent queries says whether a threshold could separate them.
- Cost per turn: model calls, embedding calls, input and output tokens, HTTP seconds for the phrase call, and embedding seconds per text (model load excluded).

## Live drill-down

Arms A and B, queries `exact-01`, `paraphrase-01`, `implicit-01`, `correction-01`, `absent-01`, `recent-01`: 12 cells, each at most 4 calls of MiniMax-M2 at `max_tokens` 1536, tool `memory_inspect` (production descriptor, production `Source`). The system message is the `## Source-linked memory` section of `internal/agent/instructions.go`, read at run time, plus one line saying to answer in the final message. The user message is the production `frameContext` rendering of that arm's delivered set. A's phrase call is the recorded one; no retries.

Recorded per cell: whether the evidence was delivered, whether an evidence source was inspected, and whether the final answer meets the key: `4471`; `22` with the half-past-three time; code indented with tabs; `19` as current; an explicit statement that the bike-shed code is unavailable with no invented code; `sage green`. The author judges absent and correction answers, and any answer the key cannot decide, in `judgments.json` after collection. The live run measures whether the agent drills into linked sources; with one cell per query it does not rank arms.

## Decision rule

Primary set: the 40 exact, paraphrase, implicit and correction queries (correction scored on the newer source). Compare B with A on delivered hit, paired by query: `B_only` and `A_only` discordant counts, exact two-sided sign test.

Guardrails: in no primary family does B deliver 2 or more fewer hits than A, and B's correction-newest count is at least A's minus 1.

1. **Undetermined** if a required control fails: oracle reach for A or B, either similarity-versus-recency check, or the drill-down path for A or B. The failing control is named.
2. **Valid** if `B_only > A_only` with p < 0.05 and both guardrails hold.
3. **Not valid** if `A_only > B_only` with p < 0.05, or if a guardrail fails.
4. **Undetermined** otherwise: no significant difference. The cost difference is reported beside it and does not change the verdict.

C, A0 and D are reported and do not enter the verdict. If an added or secondary arm's oracle fails, its results are reported as uninterpretable. When the verdict is valid, the report adds a structural sketch with estimated net line change per file; a structure that is valid and grows the code is reported as such.

## Budgets

- Chat: at most 470 HTTP calls, 1,600,000 input tokens, 1,200,000 output tokens, USD 1.80 at MiniMax-M2's standard USD 0.30/M input and USD 1.20/M output ([pricing](https://platform.minimax.io/docs/guides/pricing-paygo), checked 2026-09-25). Planned use: 300 syntheses (up to 600 retries in principle, stopped by the ceilings first), 55 phrase calls, at most 48 live calls.
- `max_tokens`: synthesis 2048, phrase 1024, live 1536. Before each request the meter reserves the body's UTF-8 byte count plus 1,024 as an input-token upper bound and the full `max_tokens`, and refuses a request that would pass any ceiling. Unknown usage is charged at its reservation.
- The embedding probe reserves 64 input tokens. If embo-01 is used, its usage is recorded and counted against the same input ceiling at the MiniMax-M2 input rate. The issue's USD 2.00 total is not exceeded.
- No paid synthesis, phrase or live call is repeated except the synthesis retries above. A transport failure stops the phase; a restart resumes after the last recorded result without re-sending completed calls.

## What was run before this commit

- `run.py preflight` (no network) and a stand-in dry run of `rank` and `score.py` on a store whose summaries are raw episode text, with phrases equal to the query text. Both ran after the corpus and queries were frozen at the hashes above; neither changed them. The dry run's outputs are committed as stand-in evidence and are not the study's result.
- One wording fix before freezing: preflight's implicit-overlap check failed on three queries sharing `to`, `my` and `me` with their evidence, and those three queries were reworded.
