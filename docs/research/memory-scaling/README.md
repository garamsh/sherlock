# Recall as the memory store grows

Records issue #901's study: how evidence delivery holds up as the conversation store grows with other conversations, and which way of storing and retrieving keeps it highest for the least stored text, at a fixed context budget per turn.

**The reranker arm (R) was not run.** Its server was killed for memory three times (§Cost), so R has no result. Holm adjustments run over the arms that did run: six budgeted arms for degradation and five against B4, where the preregistration named seven and six.

- **The largest scale stresses retrieval.** At 3.67× the replication's messages, the shipped recall (S4) delivers 201 of 280 primary queries, against 212 at 1×. It gains none and loses 11, p = 0.001. So the null condition does not apply, and arms are compared at 3.67×.
- **Keys over compressed values (K) is the arm the rule names.** At 3.67× it delivers 197 against budgeted B4's 203. The paired interval is [−0.042, +0.008], inside the preregistered −0.05 bound. It stores 0.28 characters per source character against B4's 0.68, 59% less text.
- **Every other arm loses.**
  - B8, B16, the aged tier (T) and the hybrid (X) each deliver significantly less than B4 at 3.67×.
  - The reranker (R) was not run; its server could not load within the memory the host could spare.
- **K's result depends on how recency is delivered.** At 1×, K's two newest units are B16 values, which take 1,591 of the 3,384-character budget, and K delivers 168. With B4-sized recency (exploratory) it delivers 202 against B4's 211.
- **The owner's drill-down (Amendment 1) holds delivery but stores the most.** Delivering raw messages from the top 5 mids by message score (DR-rel) gives 206 at 1× and 199 at 3.67×. Neither differs significantly from B4. It keeps every raw message beside the mids, so it stores 1.68 characters per source character. Ordering by newest mid (DR-new) loses to B4 at both scales.
- **Recommendation.** K merits the live answer run, with recency delivered at B4 size. The shipped recall keeps its lead until that run says otherwise.

## Contents
- Question
- Method
- Controls
- Results
- Decision
- Secondary and exploratory readings
- Amendment 1: mid-to-message drill-down
- The distractor pool
- Cost
- Limits
- Reproduction

## Question

Every measured store so far is about 1M source characters, and batch 4 stores 0.705 to 0.731 characters per source character (`../memory-paradigms/README.md`). The context delivered per turn is bounded, and the store is not. So the long-term risk is that retrieval precision falls as distractors accumulate while the store grows linearly. The study asks two things:
- whether delivery falls for the shipped recall as the store grows;
- which storage and retrieval arm keeps delivery highest per stored character.

## Method

[preregistration.md](preregistration.md) fixed everything below before any ranking. It was committed and pushed as `9c9bd5f`, and the harness as `9fa003c`. Both are based on `dev` at `8218189`.

- **Replication.** The #856 replication (`../mid-memory-rag/evidence/replication/`): 4,800 messages, 280 primary queries, and the recorded syntheses at batch 4, 8 and 16. Summaries are read as the production store keeps them, redacted and trimmed.
- **Distractors.** Four recorded batch-4 synthesis streams of other conversations: #874, #885, #888 and #890. They are placed newer than the replication, so the queried memory is the old one. Each arm stores the same distractors at batch 4; only the replication part varies by arm.
- **Scales.** Each step adds one whole stream:

  | Scale | Messages | Nominal | Distractor mids kept |
  |---|---:|---:|---:|
  | s0 | 4,800 | 1.00× | 0 |
  | s1 | 6,496 | 1.35× | 405 |
  | s2 | 9,984 | 2.08× | 1,258 |
  | s3 | 13,472 | 2.81× | 2,111 |
  | s4 | 17,600 | 3.67× | 3,106 |

- **Arms.** Every arm uses `BAAI/bge-base-en-v1.5` (revision `a5beb1e`) on this study's own text-embeddings-inference 1.9.4 instance, embedding raw text, with cosine similarity and ties broken newest first.

  | Arm | Replication stored as | Ranking |
  |---|---|---|
  | S4 | B4 | The shipped recall: 2 newest, then the top 7 similar, at most 7 delivered, no budget |
  | B4, B8, B16 | B4, B8, B16 | Cosine |
  | T | B16 older than the store's newest 2,400 messages, B4 after | Cosine |
  | K | B16 values, B4 embeddings as keys | Cosine over keys; each key delivers its B16 value once |
  | X | B4 | FTS5 `bm25` and cosine, reciprocal rank fusion (k = 60) |
  | R | B4 | Cosine top 30 reordered by `BAAI/bge-reranker-base` (revision `2cfc18c`) on its own instance |

- **Budget.** C = 3,383.6 characters: S4's mean at s0 over the 280, counting each delivered unit as production renders it. Every arm except S4 fills per turn: its own 2 newest units, then its ranking, each unit once. A unit that would take the turn above C is skipped. At most 50 ranked units are tried.
- **Endpoint.** A query is a hit when an evidence source lies inside a delivered unit's interval, the replication's rule.
- **Decision rule.** Exact two-sided McNemar on the 280 paired queries, with Holm adjustment across arms.
  - Null condition: S4 at s4 against s0.
  - Comparison: each arm against budgeted B4 at the decisive scale.
  - "Delivers more": wins more than it loses, with adjusted p < 0.05.
  - "No worse for less storage": the paired interval's lower bound is at least −0.05, and the replication part stores fewer characters per source character than B4.

One detail the preregistration did not name was settled in the harness: X's lexical list breaks `bm25` ties newest first, as the dense list does.

## Controls

From [scores.json](evidence/scores.json) `controls` and [study.json](evidence/study.json) `controls`.

| Control | Result |
|---|---|
| S4 at 1× reproduces #858 parity | 212 of 280; delivered intervals identical to `../mid-memory-rag/evidence/production-parity/rankings.json` on 314 of 314 queries |
| Distractors outside gold | Every distractor interval lies above source 4,800, outside every evidence and obsolete source; 0 distractor messages carry a replication evidence message's text |
| Leak check | 380 planted values; 94 of 3,200 batch-4 distractor mids dropped, and 164 of 212 batch-8 mids |
| Mutation checks | [mutation.txt](evidence/mutation.txt): 13 of 13 mutations (delivery, budget, dedupe, leak check, drill-down, decision) fail a test |
| Reach | Every primary evidence source lies in a stored unit of every arm at every scale |
| Servers | Both `/info` responses are recorded in `study.json` `controls.servers` and match the preregistered models and revisions |

## Results

Primary hits of 280 ([scores.json](evidence/scores.json) `table`):

| Arm | s0 1.00× | s1 1.35× | s2 2.08× | s3 2.81× | s4 3.67× | Stored per source char, replication part |
|---|---:|---:|---:|---:|---:|---:|
| S4 (shipped, unbudgeted) | **212** | 209 | 205 | 204 | **201** | 0.684 |
| B4 | 211 | 191 | 194 | 193 | 203 | 0.684 |
| B8 | 176 | 159 | 156 | 157 | 170 | 0.450 |
| B16 | 114 | 102 | 101 | 105 | 112 | 0.280 |
| T | 172 | 118 | 101 | 105 | 112 | 0.472 at s0, 0.280 from s2 |
| K | 168 | 158 | 176 | 185 | 197 | 0.280 |
| X | 161 | 152 | 161 | 161 | 168 | 0.684 |
| R | not run | not run | not run | not run | not run | 0.684 |
| DR-rel (Amendment 1) | 206 | 200 | 200 | 198 | 199 | 1.684 |
| DR-new (Amendment 1) | 186 | 139 | 149 | 161 | 188 | 1.684 |

Per family at s0 and s4 (exact, paraphrase, implicit, correction; 70 each):

| Arm | s0 | s4 |
|---|---|---|
| S4 | 63, 56, 27, 66 | 61, 49, 25, 66 |
| B4 | 64, 56, 26, 65 | 62, 51, 24, 66 |
| B8 | 53, 45, 20, 58 | 51, 42, 19, 58 |
| B16 | 36, 26, 11, 41 | 39, 19, 8, 46 |
| T | 47, 40, 20, 65 | 39, 19, 8, 46 |
| K | 59, 39, 20, 50 | 61, 48, 22, 66 |
| X | 63, 22, 14, 62 | 62, 26, 17, 63 |
| R | not run | not run |
| DR-rel | 63, 55, 22, 66 | 61, 49, 23, 66 |
| DR-new | 54, 45, 21, 66 | 55, 45, 22, 66 |

- **Whole-store storage.** Distractor messages average 41 characters and their summaries 162, so they barely compress. With them, B4's whole store rises from 0.684 at s0 to 0.782 characters per source character at s4, and K's from 0.280 to 0.528.
- **Ranking before the budget.** Recall@5 of the ranking isolates retrieval from the fill ([scores.json](evidence/scores.json) `recall_before_budget`):

  | Arm | s0 @1 | s0 @5 | s0 @20 | s4 @1 | s4 @5 | s4 @20 |
  |---|---:|---:|---:|---:|---:|---:|
  | B4 (and S4) | 0.486 | 0.757 | 0.889 | 0.450 | 0.718 | 0.814 |
  | B8 | 0.429 | 0.671 | 0.871 | 0.375 | 0.636 | 0.743 |
  | B16 | 0.300 | 0.554 | 0.800 | 0.246 | 0.432 | 0.611 |
  | K | 0.532 | 0.807 | 0.925 | 0.489 | 0.757 | 0.850 |
  | X | 0.346 | 0.596 | 0.821 | 0.346 | 0.607 | 0.807 |
  | R | not run | not run | not run | not run | not run | not run |

  At @20 every arm's ranking loses ground from s0 to s4. K's value ranking has the highest recall at both scales; each value is reached through the best of its four keys.
- **Why budgeted arms are not monotone in scale.** Each arm's two newest units are charged to the budget, and from s1 on they are the newest distractor mids, whose length depends on the stream (`study.json` `storage[].recency_chars`):
  - s0: 606 characters for B4, 1,591 for B16 and K;
  - s1: about 1,640 for every arm (the #874 stream closes on unused replication chat, `../chained-recall/README.md` §The corpus);
  - s2 to s4: 1,430, 1,354 and 475, the same for every arm.

  So B4 falls to 191 at s1 while S4, which delivers a fixed count, falls only to 209. From s1 on, every arm pays the same recency, so comparisons within a scale are matched. Degradation read across scales mixes ranking loss with recency size.

## Decision

- **Null condition: it does not apply.** S4 at s4 against s0: 201 against 212, 0 gained, 11 lost, exact p = 0.001, interval [−0.039, −0.017]. 3.67× stresses the shipped recall measurably, so s4 is the decisive scale.
- **Degradation, s0 to s4** (Holm over the six budgeted arms that ran; R did not):

  | Arm | s0 | s4 | Gained | Lost | Holm p |
  |---|---:|---:|---:|---:|---:|
  | B4 | 211 | 203 | 2 | 10 | 0.15 |
  | B8 | 176 | 170 | 2 | 8 | 0.28 |
  | B16 | 114 | 112 | 14 | 16 | 0.86 |
  | T | 172 | 112 | 7 | 67 | < 0.001 |
  | K | 168 | 197 | 35 | 6 | < 0.001 (a gain) |
  | X | 161 | 168 | 10 | 3 | 0.28 |
  | R | not run | not run | not run | not run | not run |

  T degrades by design: as distractors arrive, the whole replication ages into B16. K's gain is the recency effect above: its s0 recency is two B16 values.
- **Against B4 at s4** (Holm over the five arms that ran; R did not):

  | Arm | Hits | B4 | Wins | Losses | 95% interval | Holm p | Stored per source char | Reading |
  |---|---:|---:|---:|---:|---|---:|---:|---|
  | B8 | 170 | 203 | 10 | 43 | [−0.154, −0.068] | < 0.001 | 0.450 | delivers less |
  | B16 | 112 | 203 | 5 | 96 | [−0.349, −0.280] | < 0.001 | 0.280 | delivers less |
  | T | 112 | 203 | 5 | 96 | [−0.349, −0.280] | < 0.001 | 0.280 | delivers less (equals B16 at s4) |
  | **K** | **197** | 203 | 4 | 10 | **[−0.042, +0.008]** | 0.18 | **0.280** | **no worse for less storage** |
  | X | 168 | 203 | 11 | 46 | [−0.163, −0.074] | < 0.001 | 0.684 | delivers less |
  | R | not run | 203 | | | | | 0.684 | not run |

- **Recommendation: K.** No arm delivers more than B4. K is the only arm that is no worse for less storage. It stores 59% less replication text and needs no extra server. Its added cost is a second synthesis size: B4 summaries must be written to compute the keys, and then only their vectors are kept.

## Secondary and exploratory readings

- **B4 under the cap against S4 at s0** (preregistered): 211 against 212, 3 wins and 4 losses. The per-turn cap alone costs one query.
- **Batch-8 bias estimate** (preregistered): B8m, B8 against the matched #874 batch-8 distractors, delivers 182 against B8's 159 at s1. As a bias estimate it is invalid.
  - The leak check dropped 162 of the 212 batch-8 mids for the planted value "2". Batch-8 summaries enumerate facts as "(1) … (2) …", and that key matches the enumeration.
  - So B8m faced 48 distractor mids where B8 faced 405, and the +23 measures fewer distractors, not granularity.
- **Coverage-matched bias estimate** (exploratory, added after the first ranking): B8c is B8 against only the batch-4 distractors inside B8m's 48 kept spans. B8m against B8c is 182 against 182, with 2 wins and 2 losses. So at matched coverage the distractors' granularity measured no bias for B8. The preregistered plan read B16, T and K as conservative by the B8 bias; the measured bias is 0.
- **K with B4-sized recency at s0** (exploratory, added after the first ranking): Kr is K's ranking behind B4's two newest mids. It delivers 202 against B4's 211, with 7 wins and 16 losses, interval [−0.060, +0.005]. K's s0 deficit (168) is mostly its recency cost.

## Amendment 1: mid-to-message drill-down

Added at the owner's proposal after the first ranking of every other arm, and preregistered as [Amendment 1](preregistration.md) (`54fb3fd`) before any of its rankings. It is secondary and enters neither Holm family nor the recommendation.

- **Structure.** Rank B4 mids by cosine and take the top 5. Rank the raw source messages inside them against the query, and deliver messages under the same budget and fill rule, after the 2 newest B4 mids.
  - DR-rel orders the messages by score.
  - DR-new groups them by mid, newer mid first, and by score within a mid.
- **Against B4** ([scores.json](evidence/scores.json) `secondary`, exact McNemar, unadjusted):

  | Ordering | Scale | Hits | B4 | Wins | Losses | 95% interval | p |
  |---|---|---:|---:|---:|---:|---|---:|
  | DR-rel | s0 | 206 | 211 | 4 | 9 | [−0.038, +0.011] | 0.27 |
  | DR-rel | s4 | 199 | 203 | 3 | 7 | [−0.031, +0.011] | 0.34 |
  | DR-new | s0 | 186 | 211 | 3 | 28 | [−0.106, −0.054] | < 0.001 |
  | DR-new | s4 | 188 | 203 | 3 | 18 | [−0.070, −0.020] | 0.002 |

- **Reading.**
  - DR-rel stays within B4's range at every scale. At s1 it holds 200 where B4 falls to 191, because messages are small units and still fit beside the long recency mids.
  - It cannot exceed B4's mid reach: a message is reachable only through a top-5 mid, and its ranking's recall@20 (0.757 at s0, 0.718 at s4) equals B4's recall@5.
  - DR-new spends the budget on the newest of the five mids whatever their score, and loses.
- **Need rate** (descriptive, from the existing rankings; [scores.json](evidence/scores.json) `need_rate`). This is the share of the 280 queries whose evidence DR-rel delivers as a raw message while the mid-only arm beside it delivers no summary covering it. It estimates how often an on-demand drill-down beside that arm would change delivery. None of DR-rel's hits come through its recency mids, at s0 or s4.

  | Beside | s0 | s1 | s2 | s3 | s4 |
  |---|---:|---:|---:|---:|---:|
  | B4 | 4 (1.4%) | 15 (5.4%) | 11 (3.9%) | 10 (3.6%) | 3 (1.1%) |
  | K | 47 (16.8%) | 51 (18.2%) | 34 (12.1%) | 23 (8.2%) | 11 (3.9%) |

  K's high rates at s0 to s2 are its recency cost (§Results), not its ranking.
- **Delivered cost per query.** The mean delivered characters over the 280 are B4 3,271 and 3,312, K 3,218 and 3,309, and DR-rel 3,339 and 3,177, at s0 and s4. Every budgeted arm fills to about the same size by construction. Tokens were not counted: the study has no tokenizer of the answering model, and the PM limited this reading to existing data.
- **A gated drill-down (DR-gate) was proposed and then withdrawn** by the PM before it was built. DR-rel shows no gain over B4 at either scale, so a gate choosing between the two cannot beat both.
- **Storage.** Raw messages held beside the mids count as stored: 1.684 characters per source character for the replication part, against B4's 0.684 and K's 0.280.
- **Beside K.** At s4, K delivers 197 on 0.280 stored and DR-rel 199 on 1.684. Delivery is the same within noise, and DR-rel stores six times the text. Whether a delivered raw message is used better than a summary is what a live run would show; delivery alone does not separate them.

## The distractor pool

- **Size.** 3,200 batch-4 mids over 12,800 messages. After the leak check, 3,106 are kept: #874 405 of 424, #885 853 of 872, #888 853 of 872, and #890 995 of 1,032. That is 2.6 times the replication's 1,200 mids, but only 530,454 source characters against the replication's 897,180.
- **Resemblance to the replication's own filler** (`study.json` `resemblance`; filler is the 891 replication B4 mids holding no primary evidence):
  - source messages average 41 characters against the replication's 187;
  - summaries average 162 characters against the filler's 530;
  - the distractors are templated fact exchanges (desk numbers, extensions, suppliers, house rules), not open-ended chats;
  - a primary query's most similar distractor has mean cosine 0.594, against 0.658 for its most similar filler mid; the distractor is the closer of the two for 61 of 280 queries.

  So the pool is less like the queries than the replication's own filler is, and it adds mids more than it adds text. Scale here is a count of competing mids.

## Cost

- No MiniMax, OpenAI or other paid call. Every summary is recorded.
- Embeddings: 5,568 summaries and queries, then 17,600 raw messages for Amendment 1, on `tei-901` (port 8130), this study's own instance, in batches of 16. Ranking every arm at five scales takes about 10 s from the cache.
- Reranking: none. Four attempts to serve `BAAI/bge-reranker-base` on this host ended as follows:
  - The first instance loaded without a memory cap and held 2.8 GiB. The PM stopped it before any rerank call, because the host had run out of memory and #900 lost its proxy.
  - After #896 and #900 finished their live stages, the PM allowed it back with nothing else of this study's running.
  - It was OOM-killed (exit 137) while loading under a 2 GiB cap.
  - It was OOM-killed again under 2 GiB with `--max-batch-tokens 4096 --max-client-batch-size 8`; batch limits change no score.
  - It was OOM-killed a third time under a 3 GiB cap with the same limits, right after the model download.
  - The PM's rule was to report R as not run if the 2 GiB and 3 GiB attempts both failed.

## Limits

- **One corpus, one embedder, one CPU host.** The replication's limits apply unchanged (`../mid-memory-rag/README.md` §Replication limits). Paraphrase and implicit queries are selected to miss lexically, which works against X.
- **No cross-encoder was measured.** R did not run, so whether reranking recovers what scale costs the dense ranking is still open.
- **The distractors are not the replication's kind of conversation.** They are short templated exchanges from four chain studies. Scale is measured in mids; at 3.67× in mids the store holds only 1.59× the replication's source characters.
- **Distractors are batch 4 in every arm.** A B16 or K store would hold coarser distractors. The one matched measurement (B8c) found no bias, but it covers batch 8 at s1 only.
- **K is measured offline, and its recency is the open design point.** At s4 its recency units are batch-4 distractors, as every arm's are. A deployed K store would hold its newest conversation as B16 values unless recency is kept at B4. Kr suggests that choice is worth about 34 queries at 1×.
- **T's cutoff is one value.** From s2 on the whole replication is older than 2,400 messages, so T equals B16.
- **Endpoint is delivery, not answering.** Whether the model uses a delivered B16 value as well as a B4 mid is what the live run measures.

## Reproduction

From the repository root, with Docker and `uv`:

```sh
IMG=ghcr.io/huggingface/text-embeddings-inference@sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d
docker run -d --name tei-901 -p 127.0.0.1:8130:80 $IMG \
  --model-id BAAI/bge-base-en-v1.5 --revision a5beb1e3e68b9ab74eb54cfd186867f64f240e1a
uv run --no-project --with numpy==2.3.3 python3 docs/research/memory-scaling/study.py --without-rerank   # writes evidence/rankings.jsonl and study.json
python3 docs/research/memory-scaling/evaluate.py                                       # offline; writes evidence/scores.json
python3 docs/research/memory-scaling/mutation.py                                       # writes evidence/mutation.txt
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s docs/research/memory-scaling -p '*_test.py'
docker rm -f tei-901
```

- `study.py` caches server answers in `.embed_cache-901*.json` beside itself; `.gitignore` excludes them. `--without-rerank` ranks every arm but R, as the committed evidence was produced; without it, R needs the reranker instance on port 8131.
- `rankings.jsonl` and every field of `study.json` except `run` are reproduced by a rerun; a rerun from the cache with both servers stopped reproduced them byte for byte. `evaluate.py` regenerates `scores.json` from them.
