# Mid-memory retrieval with separate similarity and recency lists

Records the preregistered comparison for issue #854 between the shipped recall and the owner's mid-memory structure over 300 synthesized mid memories. It makes no production decision.

## Question and method

Does the owner's structure deliver the evidence for a query about older conversation more often than the shipped recall? Would adopting it shrink the memory code? In the owner's structure, one size `n` sets both the rolling window and the batch. Mid memories are ranked by embedding similarity and, separately, by recency, and the two lists are merged by ID with no pre-turn model call.

- Source revision: `35e79b5` (origin/dev). The [preregistration](preregistration.md) and harness were committed as `a659ee8f4258d03360347a6e25a3a79ce890cce3` and pushed at 2026-09-25 14:40:53 UTC. The first paid call, the embedding probe, followed at 14:41:59 UTC. The [preflight manifest](evidence/preflight.json) records the SHA-256 of every input at that revision. No input changed after it; `diagnose.py` and `judgments.json` were added after collection and score no arm. One post-collection wording change in `preregistration.md` (review of #855): the embedding-endpoint sentence now names the page read; the arms, rule and budgets are unchanged.
- Runtime: Go 1.26.5. Python 3.13.5 runs the study loop. uv 0.12.13 runs `fastembed==0.7.3`.
- [`bridge.go`](bridge.go) runs production code: `sqlite.OpenConversation`, `RecordTurn`, `Accumulate`, `Search`, `RecentMid`, `Source`, `memory.ConversationRecall`, `memory.ConversationSummary`, and the `internal/agent/openai` client. The client is pointed at a local capture server, so every paid request is the exact production wire body, with only `max_tokens` added, and every response goes back through production decoding and validation.
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

## Reproduction

From the repository root:

```sh
go build -o /tmp/mid-rag-854-bridge ./docs/research/mid-memory-rag
python3 docs/research/mid-memory-rag/corpus.py            # regenerates corpus.json and queries.json byte-identically
python3 docs/research/mid-memory-rag/run.py preflight /tmp/mid-rag-854-preflight
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s docs/research/mid-memory-rag -p '*_test.py' -v
mkdir -p /tmp/mid-rag-854-rank && cp docs/research/mid-memory-rag/evidence/{synthesis,phrases}.json /tmp/mid-rag-854-rank/
python3 docs/research/mid-memory-rag/run.py rank /tmp/mid-rag-854-rank   # offline: rebuilds the store and rankings from recorded exchanges
python3 docs/research/mid-memory-rag/score.py docs/research/mid-memory-rag/evidence
python3 docs/research/mid-memory-rag/diagnose.py docs/research/mid-memory-rag/evidence
PATH=/home/sauce/go/bin:$PATH make ci
```

- `rank` needs `uv` and downloads the embedding model on first use; it writes `store.db`, `scratch/` and `rankings.json` into its directory. A re-rank on 2026-09-26 reproduced the committed rows and controls exactly, apart from the timing fields `embed_seconds_per_text` and `bridge_seconds`; `score.py` and `diagnose.py` are offline and regenerate `scores.json` and `summary-fidelity.json` from committed evidence.
- Paid phases run in the order `probe`, `synthesize`, `phrases`, `live`, each with `run.py <phase> <fresh directory>`. They need `MINIMAX_KEY` in the process environment and are a new study run with their own budget. The original run exported only `MINIMAX_KEY` from the base checkout's `.env`, with GitHub tokens unset. A phase resumes after its last recorded result.
- `make ci` runs lint, format, test and build; [verification](evidence/verification.txt) records its result for this collection.
