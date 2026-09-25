# Mid-memory retrieval with separate similarity and recency lists

Records the preregistered comparison for issue #854 between the shipped recall and the owner's mid-memory structure over 300 synthesized mid memories, its powered replication for issue #856 (§Replication), which turns #854's undetermined verdict into a decided one, and the production parity check for issue #858 (§Production parity). The study itself makes no production decision; ADR 0019 records the one #858 took.

The Go bridge `bridge.go` that both collections ran was deleted by #858, which removed the phrase call and FTS5 search it called. Its last working revision is `b898867`; `git log -- docs/research/mid-memory-rag/bridge.go` reaches it. Every evidence file and figure below is unchanged, and §Reproduction separates what still runs at head from what needs that revision.

## Question and method

Does the owner's structure deliver the evidence for a query about older conversation more often than the shipped recall? Would adopting it shrink the memory code? In the owner's structure, one size `n` sets both the rolling window and the batch. Mid memories are ranked by embedding similarity and, separately, by recency, and the two lists are merged by ID with no pre-turn model call.

- Source revision: `35e79b5` (origin/dev). The [preregistration](preregistration.md) and harness were committed as `a659ee8f4258d03360347a6e25a3a79ce890cce3` and pushed at 2026-09-25 14:40:53 UTC. The first paid call, the embedding probe, followed at 14:41:59 UTC. The [preflight manifest](evidence/preflight.json) records the SHA-256 of every input at that revision. No input changed after it; `diagnose.py` and `judgments.json` were added after collection and score no arm. One post-collection wording change in `preregistration.md` (review of #855): the embedding-endpoint sentence now names the page read; the arms, rule and budgets are unchanged.
- Runtime: Go 1.26.5. Python 3.13.5 runs the study loop. uv 0.12.13 runs `fastembed==0.7.3`.
- `bridge.go` ran production code: `sqlite.OpenConversation`, `RecordTurn`, `Accumulate`, `Search`, `RecentMid`, `Source`, `memory.ConversationRecall`, `memory.ConversationSummary`, and the `internal/agent/openai` client. The client is pointed at a local capture server, so every paid request is the exact production wire body, with only `max_tokens` added, and every response goes back through production decoding and validation.
- Chat model: `MiniMax-M2` at `https://api.minimax.io/v1/chat/completions`, requested and returned. It is a provider alias with no immutable weight revision.
- Embedding model: `BAAI/bge-base-en-v1.5` through fastembed, which serves the quantized ONNX export `qdrant/bge-base-en-v1.5-onnx-q`, snapshot `199291fdd1aa89faf9c20b722dc72ad5e17aa0d0`, `model_optimized.onnx` SHA-256 `4e556722bc4f65716c544c8a931f1e90fb3f866e5741fd93a96f051d673339c7`. It is local and free. The model card's retrieval instruction is prefixed to queries. It is primary because the preregistered [MiniMax embedding probe](evidence/embedding-probe.json) (`embo-01`, one text) got HTTP 200 with `base_resp.status_code` 1002, `rate limit exceeded(RPM)`, and no vectors. That was the key's first embedding request, and MiniMax's API overview (https://platform.minimax.io/docs/api-reference/api-overview, read 2026-09-25) lists no embedding endpoint.
- Corpus: [`corpus.py`](corpus.py) generates 300 episodes of 8 messages (2,400 sources) from seed 854. The first 298 are templated filler over 15 domains with shared attributes; the newest two are authored whole. The production summarizer synthesized all 300 mids live from their sources, each on its first attempt ([synthesis](evidence/synthesis.json)). A post-hoc check ([`diagnose.py`](diagnose.py), [output](evidence/summary-fidelity.json)) found that all 45 evidence summaries keep their planted value, so no miss below is a synthesis omission.
- Queries: 55 queries in six families, listed in [queries.json](queries.json): exact, paraphrase, implicit, correction, absent (10 each), and recent (5). Each carries its evidence source IDs. Preflight confirmed with production FTS that no implicit query shares a searchable token with its evidence.

| Arm | Relevant list | Pre-turn model call |
|---|---|---|
| A, shipped | production `ConversationRecall`: phrase call, FTS5 over summaries | 1 |
| A0, added | production `ConversationRecall` without a model: task text into FTS5 | 0 |
| B, owner | cosine of the raw query embedding against summary embeddings | 0 |
| C | B, scored by maximum cosine over A's recorded phrases | 1 (A's) |
| D, added | B, embedding each mid's 8 source messages instead of its summary | 0 |

Every arm delivers at most 7 mids through the production merge: the 2 newest (`RecentMid(2)`) first, then the relevant list, deduplicated by ID. Only the relevant list differs.

## Controls

[Rankings](evidence/rankings.json) hold the controls; [preflight](evidence/preflight.json) holds the same checks on a stand-in store.

| Control | Required | Result |
|---|---|---|
| Oracle reach, 20 sampled mids (A, A0: delivered; B, C, D: ranked first) | 19/20 | A 20, A0 20, B 20, C 20, D 20 |
| B top-2 equals the recency top-2 | fewer than 28 of 55 | 0 |
| Distinct B top-1 mids over 55 queries | at least 20 | 46 |
| Implicit queries share no FTS token with their evidence; evidence matches itself | 10/10 | 10/10 |
| Live `memory_inspect` returns a source, per arm | at least 1 | A 14, B 19 |
| Tests fail with the behaviour removed | each | [mutation output](evidence/mutation.txt): the reservation guard, interval credit, merge order and significance check each break a test |

Every arm is exercised and working. A null result below is therefore a measurement, not a harness defect.

## Results

[Scores](evidence/scores.json) hold every per-query value. Delivered hit means an evidence source lies inside a delivered mid's source interval (source-level). Intervals are Wilson 95%.

| Family | A | A0 | B | C | D |
|---|---|---|---|---|---|
| exact (10) | 10 [0.72, 1.00] | 10 | 10 [0.72, 1.00] | 9 | 10 |
| paraphrase (10) | 6 [0.31, 0.83] | 0 | 6 [0.31, 0.83] | 7 | 4 |
| implicit (10) | 3 [0.11, 0.60] | 1 | 2 [0.06, 0.51] | 2 | 1 |
| correction, newer source (10) | 10 [0.72, 1.00] | 10 | 9 [0.60, 0.98] | 9 | 8 |
| **primary (40)** | **29 [0.57, 0.84]** | **21 [0.38, 0.67]** | **27 [0.52, 0.80]** | **27** | **23** |
| recent (5) | 5 | 5 | 5 | 5 | 5 |

Recall@k and MRR@20 over the relevant list alone, primary 40 queries:

| Arm | recall@1 | recall@5 | recall@20 | MRR@20 |
|---|---:|---:|---:|---:|
| A | 0.450 | 0.725 | 0.825 | 0.580 |
| A0 | 0.275 | 0.525 | 0.600 | 0.402 |
| B | 0.475 | 0.675 | 0.800 | 0.565 |
| C | 0.375 | 0.675 | 0.850 | 0.509 |
| D | 0.425 | 0.575 | 0.700 | 0.499 |

- **B against A.** B and A disagree on six primary queries. Only A delivers `correction-02`, `paraphrase-05`, `implicit-01` and `implicit-10`; only B delivers `paraphrase-09` and `implicit-08`. The exact two-sided sign test gives p = 0.69. For `paraphrase-09`, A's phrase call returned no tool call and recall fell back to the task text.
- **The phrase call is what makes FTS competitive.** A0 (FTS with no phrase call) delivers 21 of 40, against A's 29 and B's 27. B matches A without the phrase call; replacing lexical search with embedding recovers roughly what the phrase call adds.
- **Beyond the delivered cut.** B ranks paraphrase evidence within its top 20 for 9 of 10 queries, against A's 6, but only 6 fall inside the five relevant slots. This is a description, not the preregistered endpoint.
- **Implicit needs stay unsolved.** Seven of the ten implicit queries are missed by both A and B. No arm reaches more than 3 of 10.
- **Summaries beat raw sources for similarity.** D, which embeds the 8 source messages, trails B, which embeds the summary, by 4 primary queries. So similarity retrieval would not remove the synthesis call.
- **Corrections.**

  | Arm | Newest delivered | Obsolete without newest | Newer ranked no lower than older |
  |---|---:|---:|---:|
  | A | 10 | 0 | 6 |
  | B | 9 | 1 | 7 |
  | C | 9 | 1 | 6 |
  | D | 8 | 1 | 6 |

  B misses `correction-02` ("it's actually 3381" never names a phone).
- **Absent queries.** Every arm returns a non-empty relevant list for all 10 [0.72, 1.00]: FTS always matched a token, and an unthresholded similarity list is non-empty by construction. For B, the AUC of top-1 cosine between present and absent queries is 0.765, so a threshold would separate them only partly.

### Cost per turn

[Spend](evidence/calls.json) is 391 HTTP calls, 164,927 input and 113,242 output tokens, USD 0.185 at USD 0.30/M input and USD 1.20/M output (priced from usage, not an invoice). By phase: synthesis 300 calls, USD 0.147; phrase 55 calls, USD 0.018; live 35 calls, USD 0.020; one embedding probe, 64 tokens reserved.

| Arm | Model calls | Embedding calls | Input tokens | Output tokens | Latency |
|---|---:|---:|---:|---:|---|
| A | 1 | 0 | 225.7 | 221.8 | 3.54 s HTTP per phrase call (mean) |
| A0 | 0 | 0 | 0 | 0 | local FTS only |
| B | 0 | 1 | 0 | 0 | 0.013 s local embedding per text, model load excluded |
| C | 1 | 3.62 | 225.7 | 221.8 | phrase call plus embeddings |
| D | 0 | 1 | 0 | 0 | as B |

Adopting B would also add one embedding per new mid at synthesis time.

### Live drill-down

There were 12 cells: arms A and B on one query per family, at most 4 MiniMax-M2 calls each, with the production `memory_inspect` tool. [Raw cells](evidence/live.json) hold every message; [author judgments](judgments.json) record the verdict per cell.

| Query | A | B |
|---|---|---|
| exact-01 | inspected evidence, correct | inspected evidence, correct |
| paraphrase-01 | reached evidence on the last call; no answer | evidence delivered, not reached; no answer |
| implicit-01 | inspected the rule; script indented with tabs | rule not delivered; spaces |
| correction-01 | evidence delivered, not reached; no answer | reached the correction; last call spent elsewhere; no answer |
| absent-01 | gave the storage-unit code as the bike-shed code | inspected the storage-unit source; same fabrication |
| recent-01 | reached the decision; no answer | reached the decision; no answer |

- Both arms drill into linked sources: A in 5 of 6 cells, B in 6 of 6.
- In 6 of the 12 cells the model inspected one source ID per call, walking an 8-message interval in order, and used up the call bound before answering. That is a limit of the study's 4-call loop combined with the one-ID inspection tool, and it binds A and B alike.
- Both absent cells answered with the near-miss fact. Neither arm's retrieval prevents that; it is an answer-level failure.
- With one cell per query, the live run shows whether drill-down happens. It does not rank the arms.

## Verdict

**Undetermined**, under the preregistered rule. Every required control passes, and both guardrails hold: no family loses 2 or more, and correction-newest is 9 against 10. But B and A differ on only 6 of the 40 primary queries (2 to B, 4 to A, p = 0.69). At this size the study detects no difference. It cannot claim that the owner's structure retrieves the right older conversation more often, and it cannot claim that it retrieves less often.

What the measurement does establish:

- Similarity over summaries matches the shipped recall at zero model calls per turn. The shipped path spends about 3.5 s and 448 tokens on its phrase call.
- Without that call, the shipped FTS index drops to 21 of 40.
- Neither structure solves implicit needs.

### Code-size estimate

The rule asks for a structural sketch only when the verdict is valid. The issue's second question, whether adoption would shrink the code, is answered here as an estimate from reading the files at `35e79b5`; nothing was implemented.

| File | Change | Net lines (estimate) |
|---|---|---:|
| `internal/agent/memory/conversation.go` | delete the phrase call and its parse | −22 |
| `internal/agent/conversation.go` | delete `SearchTokens` and the stop words (only FTS and the phrase fallback use them) | −17 |
| `internal/memory/sqlite/conversation.go` | FTS5 table and trigger replaced by an embedding column; `Search` becomes cosine over stored vectors | +5 to +15 |
| `internal/memory/sqlite/accumulate.go` | fixed `8` (two sites) becomes the shared `n`; embed each new mid after synthesis | +10 to +15 |
| new embedder interface and client (for example under `internal/agent/`) | the repository has no embedder today | +80 to +120 |
| `internal/config/`, `cmd/sherlock/`, `internal/gateway/runtime.go` | embedding endpoint, model and key settings, plus wiring | +25 to +45 |
| existing databases | backfill vectors for stored mids, or refuse them, per `memory.md` §Existing databases | +20 to +40 |

Estimated net: about +100 to +190 production lines before tests. The phrase-call constant, FTS index and fixed batch constant go away, and an embedding provider arrives with its configuration and migration. The recent and relevant constants (2 and 5) stay: the owner's structure keeps two lists, so it still needs two bounds. The deployment has no embedding endpoint: this key's MiniMax probe was refused, so adoption would also need a provider or a local model server that `docs/architecture/deployment.md` does not describe. The structure grows the code.

## Limits

- 40 primary queries give low power. A true difference of a few queries in 40 is invisible, and the Wilson intervals for A and B overlap almost entirely.
- The corpus is templated. Filler episodes repeat sentence frames, and planted facts replace one filler message, so their topic can shift abruptly within an episode. Real conversation may favour either arm differently.
- There is one synthesis per episode, one phrase call per query and one live cell per query-arm pair, with no variance estimate. The provider alias and server conditions are uncontrolled. Production sends no `max_tokens`; the study added it.
- The embedding model is a quantized local model. A hosted embedder (for example `text-embedding-3-small`, which `memory-retrieval` measured) might rank differently. No other provider was used, as the issue required.
- The delivered budget is 7 mids, 5 of them relevant, for every arm. Other bounds would change both arms' hit counts.
- The live loop is study-local: a reduced system prompt, 4 calls and no retries. It is not the shipped session, and its no-answer cells reflect that bound.
- A stand-in dry run (summaries equal to raw episode text, phrases equal to the query text) ran before the preregistration commit, to crash-test the pipeline after the corpus was frozen. Its outputs are kept in [evidence/standin](evidence/standin/) and are not results. It delivered A 21 and B 23 of 40.

Current responsibilities and the open read-policy question stay in [memory architecture](../../architecture/memory.md) and [ADR 0019](../../architecture/adr/0019-memory-read-step.md).

## Replication (issue #856)

The #854 sections above are unchanged, and their evidence files are byte-identical. This section records the powered replication preregistered in [replication-preregistration.md](replication-preregistration.md). Its evidence is under [evidence/replication](evidence/replication/).

### Method

- **Revisions.** Source revision `2b27bac` (origin/dev, #855 merged). The preregistration was committed and pushed as `45c0d91` before the run's first paid call, the embedding probe. Four amendments were committed before any response they govern was used:
  - `e731c8e` added a supplementary design round;
  - `cb05702` narrowed the leak rule and added paraphrase supply;
  - `4d26d7c` applied the leak test to filler;
  - `a587211` (17:48:06Z) raised the ceiling from USD 2.90 to 2.99, reordered the n = 16 synthesis, and fixed the phrase decode.

  `a587211` does not precede all n = 16 synthesis. Four n = 16 requests, for batches 1 to 4, started at 17:40:34Z and were stopped in flight with no recorded response. They are charged at reservation (USD 0.0166). The same batches were re-requested at 17:48:20Z, and only those responses are used.

  The corpus was frozen in `d1e7fab` before any synthesis. No ranking and no score of the replication corpus existed before any of the four amendments. The amendments were prompted only by counts from design and assembly, and by spend.

  After all collection ended, the harness commit `5b827a7` (18:10:36Z) changed how the n = 8 store is replayed: the batch is handed over redacted, as production stores it (the Synthesis bullet below). Two `rank` runs of the replication corpus came before that commit:
  - The first, before the change, stopped in the store build at batch 56, when the replay check refused the unredacted batch. It wrote no `rankings.json` and no score.
  - The second ran the working tree that `5b827a7` then committed, unchanged in `replication.py`. It wrote the committed `rankings.json` at 18:07:18Z, and `replication_score.py` then produced the primary counts reported below.

  So the only ranking and score of the replication corpus are the ones reported; there is no earlier count to set beside them. `5b827a7`'s other changes are in `replication_score.py` and add only the cost fields and the power-at-ψ option. The #857 review's independent `rank` at `0e19b36` reproduced the committed `rankings.json`, timing fields aside.
- **Runtime and models.** Go 1.26.5, Python 3.13.5, uv 0.12.13 with `fastembed==0.7.3`. Chat model `MiniMax-M2` for design, conversations, synthesis and phrase calls. Embedder `BAAI/bge-base-en-v1.5`, as in #854. The [embo-01 probe](evidence/replication/embedding-probe.json) again got `base_resp.status_code` 1002, `rate limit exceeded(RPM)`, with no vectors.
- **Corpus.** The model designed 880 items, of which 506 passed the item checks, and 240 filler topics. It then wrote 839 chats in their own topics, with the evidence sentence said by the user inside a longer message. 79 chats were rejected three times, all for wrong length.
  - [`replication.py assemble`](replication.py) selected, deterministically, 70 each of exact, paraphrase, implicit and correction, plus 30 absent. 18 items were banned for restating another selected item. 115 filler chats and one authored closing chat fill a 4,800-message stream of 496 chats ([assembly](evidence/replication/assembly.json), [corpus](evidence/replication/corpus.json), [queries](evidence/replication/queries.json)).
  - Filler shares no sentence frame: every chat is written separately. Evidence is every message of the item's chat that carries its key. 181 queries have one evidence message and 99 have two to six.
  - Paraphrase and implicit queries miss every evidence message under production `Search`, a check applied to the final chat messages as well as to the designed sentence.
- **Synthesis.** The production summarizer synthesized every batch at `n` = 4 (1,200 mids), 8 (600) and 16 (300); none failed. 11 n = 4 batches needed a second attempt.
  - The store redacts credential patterns before synthesis, but the recorded synthesis requests were built from unredacted text. Eight messages match those patterns (four Wi-Fi passwords that are evidence, and four false positives such as "secret is letting").
  - So for up to 8 batches per `n`, the summarizer saw text production would have redacted. Every arm then reads the summaries as the production store keeps them: redacted and trimmed. The bridge verifies the study's copy of the three redaction patterns against the production store on all 600 n = 8 batches.
- **Arms.** A, A0, B4, B8, B16 and H, all delivering at most 7 mids through the production merge. H alternates A's first 7 FTS candidates with B8's first 7 cosine candidates, A first.
- **Phrase calls.** 314, one per query, shared by A and H. 57 returned no tool call and one had truncated arguments. Production recall answered those 58 from the task text.

### Controls

All required controls pass ([rankings](evidence/replication/rankings.json) `controls`).

| Control | Required | Result |
|---|---|---|
| Oracle reach on all 280 primary evidence mids, each arm's own path | ≥ 266 | A 280, A0 280, B4 280, B8 280, B16 280, H 280 |
| B top-2 equals the recency top-2, of 314 queries | < 157 | B4 0, B8 0, B16 0 |
| Distinct B top-1 mids | ≥ 20 | B4 285, B8 246, B16 187 |
| Paraphrase and implicit queries lexically miss their evidence | all selected | 140 of 140, by construction of selection |
| Tests fail with the behaviour removed | each | [mutation output](evidence/replication/mutation.txt): 8 of 8 mutations break a test |

### Results

Delivered hits, with Wilson 95% intervals, from [scores](evidence/replication/scores.json) (per-query values in `per_query`):

| Family (n) | A | A0 | B4 | B8 | B16 | H |
|---|---|---|---|---|---|---|
| exact (70) | 60 [0.76, 0.92] | 62 | 64 | 54 [0.66, 0.85] | 50 | 65 [0.84, 0.97] |
| paraphrase (70) | 31 [0.33, 0.56] | 9 | 52 | 48 [0.57, 0.78] | 37 | 49 [0.59, 0.80] |
| implicit (70) | 19 [0.18, 0.39] | 8 | 28 | 30 [0.32, 0.55] | 22 | 24 [0.24, 0.46] |
| correction, newer (70) | 58 [0.72, 0.90] | 61 | 65 | 62 [0.79, 0.94] | 55 | 65 [0.84, 0.97] |
| **primary (280)** | **168 [0.54, 0.66]** | **140** | **209 [0.69, 0.79]** | **194 [0.64, 0.74]** | **164** | **203 [0.67, 0.77]** |
| recent (4) | 4 | 4 | 4 | 4 | 4 | 4 |

Paired against A on the 280 primary queries. The interval is the preregistered conditional exact interval for the difference in hit rate.

| Arm | Arm only | A only | Difference | 95% interval | McNemar p | Guardrails | Outcome |
|---|---:|---:|---:|---|---:|---|---|
| **B8** | 59 | 33 | +0.093 | [+0.023, +0.157] | 0.009 | hold (exact −6 of −7 allowed) | **valid** |
| **H** | 39 | 4 | +0.125 | [+0.086, +0.146] | < 0.001 | hold | **valid** |
| B4 | 61 | 20 | +0.146 | [+0.084, +0.198] | < 0.001 | hold | valid (secondary) |
| B16 | 48 | 52 | −0.014 | [−0.086, +0.059] | 0.76 | exact −10 fails | not valid (secondary) |
| A0 | 8 | 36 | −0.100 | [−0.131, −0.054] | < 0.001 | fails | not valid (secondary) |

Relevant list alone, primary 280:

| Arm | recall@1 | recall@5 | recall@20 | MRR@20 |
|---|---:|---:|---:|---:|
| A | 0.386 | 0.596 | 0.764 | 0.485 |
| A0 | 0.329 | 0.496 | 0.607 | 0.406 |
| B4 | 0.486 | 0.746 | 0.871 | 0.609 |
| B8 | 0.407 | 0.693 | 0.861 | 0.537 |
| B16 | 0.329 | 0.586 | 0.818 | 0.446 |
| H | 0.386 | 0.725 | 0.907 | 0.545 |

- **Where B gains and loses.** B8's gain comes from paraphrase (+17) and implicit (+11), the two families built to miss lexically. It loses 6 exact queries to FTS. H keeps A's exact and correction strength and adds most of B's paraphrase gain, and it loses a query to A in only 4 cases.
- **Batch size.** Smaller batches help similarity: B4 209, B8 194, B16 164. A 16-message summary dilutes the embedding enough that B16 no longer beats A.
- **Corrections.**

  | Arm | Newest delivered | Obsolete without newest | Newer ranked no lower than older |
  |---|---:|---:|---:|
  | A | 58 | 7 | 41 |
  | B8 | 62 | 6 | 43 |
  | H | 65 | 2 | 46 |

- **Absent queries.** Every arm returns a non-empty relevant list for all 30. B8's top-1 cosine separates present from absent queries with AUC 0.588, so a similarity threshold would not tell them apart here.
- **Power at the observed disagreement.** B8 and A disagree on 92 of 280 queries (ψ = 0.33), above the preregistered 0.30 bound. At ψ = 0.33 the rule still reaches "valid" for a true 10-point gain with probability 0.817 ([observed-ψ power](evidence/replication/power-observed.json)). For H, ψ = 0.154, and a 7-point gain is detected with probability 0.837.

### Cost

- **Spend.** [Calls](evidence/replication/calls.jsonl): 3,692 HTTP calls, 1,647,973 input and 1,978,922 output tokens settled, USD 2.869. With the four n = 16 requests stopped in flight charged at their reservation, the total is USD 2.886, under the issue's USD 3.00. By phase, in USD:

  | Phase | Calls | USD |
  |---|---:|---:|
  | design | 100 | 0.178 |
  | conversations | 1,161 | 1.274 |
  | synthesis, n = 4 | 1,212 | 0.617 |
  | synthesis, n = 8 | 600 | 0.402 |
  | synthesis, n = 16 | 300 | 0.268 |
  | phrase | 314 | 0.131 |
  | embedding probe | 1 | 0.000 |

- **Per turn.**
  - A: one model call, 224 input and 291 output tokens, 4.4 s mean HTTP.
  - B: no model call and one local embedding, 0.141 s per text (batch mean, model load excluded).
  - H: both.
- **Per 1,000 messages.** Synthesis costs USD 0.129 at n = 4, 0.084 at n = 8 and 0.056 at n = 16. So B4's extra retrieval costs about 1.5 times B8's synthesis spend.

### Verdict

- **B8: valid.** The owner's structure at the shipped batch size delivers the evidence more often than the shipped recall: +9.3 points, interval [+2.3, +15.7]. It gives up 6 exact-wording queries of 70 to the lexical index.
- **H: valid.** The hybrid delivers more often than the shipped recall: +12.5 points, interval [+8.6, +14.6]. It is the only arm that loses almost nothing to A (4 queries).
- Both verdicts rest on a model-written corpus whose paraphrase and implicit families are selected to miss lexically (§Replication limits). The per-family table is the part that transfers.

### Code-size estimate, revised for the valid arms

This is read from the files at `2b27bac`, as in #854 §Code-size estimate. Nothing is implemented.

| Change | B8 (net lines) | H (net lines) |
|---|---:|---:|
| Delete the phrase call (`internal/agent/memory/conversation.go`) | −22 | 0 (kept) |
| Delete `SearchTokens` and its stop words (`internal/agent/conversation.go`) | −17 | 0 (kept) |
| Store: embedding column and cosine search (`internal/memory/sqlite/conversation.go`) | +5 to +15 (replaces FTS5) | +15 to +30 (beside FTS5) |
| Embed each new mid after synthesis (`internal/memory/sqlite/accumulate.go`) | +10 to +15 | +10 to +15 |
| Alternating merge of the two relevant lists (`internal/agent/memory/conversation.go`) | 0 | +10 to +15 |
| New embedder interface and client | +80 to +120 | +80 to +120 |
| Embedding settings and wiring (`internal/config/`, `cmd/sherlock/`, `internal/gateway/runtime.go`) | +25 to +45 | +25 to +45 |
| Vectors for existing databases (`memory.md` §Existing databases) | +20 to +40 | +20 to +40 |
| **Net production lines, before tests** | **+100 to +190** | **+160 to +265** |

- Both valid structures grow the code. H grows it most, because it keeps every part of the shipped read path and adds the embedding path beside it.
- B4 would also replace the fixed batch constant `8` with the shared `n` = 4. That is no net line change, but about 1.5 times the synthesis spend per message.
- Neither has a deployed embedding provider: this key's MiniMax probe was refused again. Adoption needs a provider or a local model server that `docs/architecture/deployment.md` does not describe.

### Replication limits

- **One model wrote everything.** MiniMax-M2 wrote the items, the chats, the summaries and A's phrases. Its phrasing may suit its own rewrites, which favours A and H, or the embedder, which favours B, in ways this run cannot separate.
- **Paraphrase and implicit favour similarity by construction.** They are selected to miss production FTS on the evidence message, as the issue asks. So they favour similarity over lexical search and make up half the primary set. On exact and correction, B8 is 116 against A's 118. The primary verdicts weight the four families equally.
- **Items are simple.** Designed items are single facts with short keys. Real conversation spreads facts over several messages and restates them.
- **No variance estimate.** There is one synthesis per batch at each `n`, one phrase call per query and one corpus.
- **Redaction.** Up to 8 batches per `n` were synthesized from unredacted text that production would have redacted before synthesis.
- **Summaries cap at 2,000 characters.** At n = 16 the cap may have compressed facts; that is part of what B16 measures.
- **The embedder is local and quantized, as in #854.** H's rule (alternate, A first) is one of several possible fusions, and no other was measured.
- **Amendments.** All four were committed before the data they govern existed. They still widened selection after the first design and assembly counts were known.

## Production parity (issue #858)

Does the shipped recall, as #858 built it, reproduce the replication's B4 result: at least 200 of the 280 primary queries at batch 4? It delivers 212. Evidence is under [evidence/production-parity](evidence/production-parity/).

### Method

- **Revision.** The harness and the production code it runs are at `cfc5606`, the implementation commit of #858 on the branch of PR #859. Go 1.26.5.
- **Harness.** [`parity/main.go`](parity/main.go) calls only production code for the store and recall: `sqlite.OpenConversation`, `RecordTurn`, `memory.ConversationSummary`, `memory.ConversationRecall`, the `internal/agent/openai` chat client, and the `internal/agent/openai` embedder.
  - It records the replication's 4,800 messages one per `RecordTurn`, so the store's own batching forms the 1,200 batches of four.
  - Each synthesis request goes through the chat client to a local replay server. The server answers with the recorded n = 4 response for the next batch ([synthesis-n4.jsonl](evidence/replication/synthesis-n4.jsonl)), and the production summarizer decodes it. No paid call was made.
  - The store embeds each summary through the embedder, and recall embeds each query's raw text. There is no instruction prefix.
  - A query is a delivered hit when an evidence source lies inside a delivered mid's interval, the replication's rule.
- **Embedding server.** Hugging Face text-embeddings-inference 1.9.4 on CPU, image `ghcr.io/huggingface/text-embeddings-inference@sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d`, serving `BAAI/bge-base-en-v1.5` at revision `a5beb1e3e68b9ab74eb54cfd186867f64f240e1a`. It reports float32 weights and CLS pooling ([server info](evidence/production-parity/tei-info.json)). The only network traffic was the image and model download.

### Controls

[Scores](evidence/production-parity/scores.json) `run` holds these.

- The store built 1,200 mids from 1,200 replayed synthesis calls. Each mid covers exactly one recorded batch of four, or the harness stops.
- Two stored summaries differ from the recorded text. Those are the store's credential redaction, as §Replication Synthesis describes.
- A second run at `cfc5606`, and a third at `68f294c` (both PR #859) after a refactor of the store's row closing, wrote byte-identical [rankings](evidence/production-parity/rankings.json).

### Results

| Family (n) | Production | B4 | Both | Production only | B4 only |
|---|---:|---:|---:|---:|---:|
| exact (70) | 63 | 64 | 63 | 0 | 1 |
| paraphrase (70) | 56 | 52 | 52 | 4 | 0 |
| implicit (70) | 27 | 28 | 25 | 2 | 3 |
| correction, newer (70) | 66 | 65 | 65 | 1 | 0 |
| **primary (280)** | **212** | **209** | **205** | **7** | **4** |
| recent (4) | 4 | 4 | 4 | 0 | 0 |

- The production recall meets the criterion: 212 of 280, against B4's 209.
- The two disagree on 11 queries. They differ in three ways: B4 prefixed queries with the model card's retrieval instruction, used fastembed's quantized ONNX export, and broke similarity ties oldest first. Production embeds the raw text with float32 weights and breaks ties newest first. Which of these moves which query was not measured.
- Building the store took 260 s, including 1,200 embedding calls. Recall took 0.079 s per query on this CPU, including one embedding call and the in-process ranking over 1,200 stored vectors.

### Limits

- The corpus, its selection toward lexical misses, and the recorded syntheses are the replication's, so §Replication limits apply unchanged.
- One embedding server, one model and one CPU host were measured. A hosted embedder was not.
- The run replays synthesis rather than calling a model. So it measures retrieval over fixed summaries, not the live synthesis at batch 4.

### Reproduction

From the repository root at `cfc5606` (PR #859) or later, with Docker:

```sh
docker run -d --name tei-858 -p 127.0.0.1:8090:80 \
  ghcr.io/huggingface/text-embeddings-inference@sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d \
  --model-id BAAI/bge-base-en-v1.5 --revision a5beb1e3e68b9ab74eb54cfd186867f64f240e1a
curl -s 127.0.0.1:8090/info                         # once it answers, the model is loaded
go run ./docs/research/mid-memory-rag/parity        # writes evidence/production-parity/{rankings,scores}.json
docker rm -f tei-858
```

The timing fields in `scores.json` vary between runs; every other field and `rankings.json` do not.

## Reproduction

At head, from the repository root, these run without the bridge and leave the committed files unchanged:

```sh
python3 docs/research/mid-memory-rag/corpus.py            # regenerates corpus.json and queries.json byte-identically
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s docs/research/mid-memory-rag -p '*_test.py' -v
python3 docs/research/mid-memory-rag/score.py docs/research/mid-memory-rag/evidence
python3 docs/research/mid-memory-rag/diagnose.py docs/research/mid-memory-rag/evidence
python3 docs/research/mid-memory-rag/replication_score.py                  # offline; regenerates replication/scores.json
python3 docs/research/mid-memory-rag/replication_score.py power            # the preregistered power table
```

Everything that calls the bridge runs from a checkout of `b898867`:

```sh
go build -o /tmp/mid-rag-854-bridge ./docs/research/mid-memory-rag
python3 docs/research/mid-memory-rag/run.py preflight /tmp/mid-rag-854-preflight
mkdir -p /tmp/mid-rag-854-rank && cp docs/research/mid-memory-rag/evidence/{synthesis,phrases}.json /tmp/mid-rag-854-rank/
python3 docs/research/mid-memory-rag/run.py rank /tmp/mid-rag-854-rank   # offline: rebuilds the store and rankings from recorded exchanges
PATH=/home/sauce/go/bin:$PATH make ci
```

- `rank` needs `uv` and downloads the embedding model on first use; it writes `store.db`, `scratch/` and `rankings.json` into its directory. A re-rank on 2026-09-26 reproduced the committed rows and controls exactly, apart from the timing fields `embed_seconds_per_text` and `bridge_seconds`; `score.py` and `diagnose.py` are offline and regenerate `scores.json` and `summary-fidelity.json` from committed evidence.
- Paid phases run in the order `probe`, `synthesize`, `phrases`, `live`, each with `run.py <phase> <fresh directory>`. They need `MINIMAX_KEY` in the process environment and are a new study run with their own budget. The original run exported only `MINIMAX_KEY` from the base checkout's `.env`, with GitHub tokens unset. A phase resumes after its last recorded result.
- `make ci` runs lint, format, test and build; [verification](evidence/verification.txt) records its result for this collection.

Replication, from a checkout of `b898867`, since `rank` calls the bridge:

```sh
go build -o /tmp/mid-rag-854-bridge ./docs/research/mid-memory-rag
python3 docs/research/mid-memory-rag/replication.py rank                   # offline; rebuilds the store and rankings.json
```

- `rank` rebuilds `store.db`, `scratch/` and `rankings.json` from the recorded synthesis and phrase exchanges, under `evidence/replication/`; neither `store.db` nor `scratch/` is committed. It needs `uv` and downloads the embedding model on first use.
- `assemble` is also offline, and also runs at `b898867`, because its lexical checks call the bridge. It regenerates `corpus.json` and `queries.json` from `design.jsonl` and `conversations.jsonl`, and caches the item checks in `items.json`.
- [Verification](evidence/replication/verification.txt) records `make ci`, the unit tests, the byte-identity check of the #854 evidence and the credential search.
- The paid phases run in the order `probe`, `design`, `converse`, `synthesize` and `phrases`, as `replication.py <phase>`, and need `MINIMAX_KEY` in the process environment. Each resumes after its last recorded result, so re-running one against the committed evidence sends nothing new. A fresh run needs a fresh output directory and its own budget.
