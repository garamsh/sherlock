# Preregistration: recall as the store grows, at a fixed context budget (#901)

This fixes the scales, arms, endpoint, controls and decision rule of issue #901 before any ranking or score exists. It is committed before the harness ranks a single query.

## Contents
- Question
- Material
- Scales
- Arms
- Delivery and budget
- Endpoint
- Controls
- Decision rule
- Descriptive measures
- Cost

## Question

As the memory store grows with other conversations, does evidence delivery fall for the shipped recall, and which way of storing and retrieving keeps delivery highest for the least stored text, inside the shipped context budget?

## Material

- **Replication.** The #856 replication (`../mid-memory-rag/evidence/replication/`): 4,800 source messages (IDs 1 to 4,800), 314 queries of which 280 are primary (exact, paraphrase, implicit and correction, 70 each), and the recorded syntheses at n = 4, 8 and 16 (1,200, 600 and 300 mids).
- **Summaries as the store keeps them.** Every summary, replication and distractor alike, is read through the production store's credential redaction and trimmed (`../mid-memory-rag/replication.py` `redact`, the copy the #856 bridge checked against `internal/memory/sqlite/conversation.go`).
- **Distractor pool.** Recorded batch-4 syntheses of other conversations only, one stream per study, each renumbered to follow the previous one:

  | Order | Study | Stream file | Messages | Mids |
  |---:|---|---|---:|---:|
  | 1 | #874 | `../chained-recall/evidence/synthesis/stream-b4.jsonl` | 1,696 | 424 |
  | 2 | #885 | `../chained-recall/confirm/evidence/synthesis/stream-b4.jsonl` | 3,488 | 872 |
  | 3 | #888 | `../chained-recall/readtime/evidence/synthesis/stream-b4.jsonl` | 3,488 | 872 |
  | 4 | #890 | `../memory-encoding/validation/evidence/synthesis/stream-b4.jsonl` | 4,128 | 1,032 |

  Source messages come from each study's `corpus.json` `stream`. Only stream mids (source IDs above 4,800 in their own study) are used; each study's base is the replication itself and is never a distractor.
- **Placement.** Distractors are newer than the replication: the queried memory is the old one, the long-term case. Every arm stores the same distractor mids at batch 4, the only granularity recorded in bulk. Only the replication's own 4,800 messages are stored differently per arm.
- **Matched batch-8 distractors.** `../chained-recall/evidence/synthesis/stream-b8.jsonl` (#874, 212 mids over the same 1,696 messages) is used only by the bias estimate in §Arms.

### Leak check

Run before any ranking. A distractor mid is dropped when its summary or any of its source messages contains a planted value of any of the 314 queries' items: the item's `key`, and for corrections both `old_key` and `new_key` (`../mid-memory-rag/evidence/replication/items.json`). The key is the value each query asks for, so it is also the answer entity. Matching is case-insensitive, and a match flanked by a letter or digit on either side does not count. The same check runs on the batch-8 distractors. Dropped mids leave their messages in the store's source count but no mid.

## Scales

Scale is the store's source messages over the replication's 4,800. Each step adds the next whole stream:

| Scale | Streams | Messages | Nominal scale |
|---|---|---:|---:|
| s0 | none | 4,800 | 1.00× |
| s1 | 1 | 6,496 | 1.35× |
| s2 | 1–2 | 9,984 | 2.08× |
| s3 | 1–3 | 13,472 | 2.81× |
| s4 | 1–4 | 17,600 | 3.67× |

s4 is the largest scale the pool allows. The report states the mid counts after the leak check.

## Arms

Every arm embeds with `BAAI/bge-base-en-v1.5` at revision `a5beb1e3e68b9ab74eb54cfd186867f64f240e1a`, served by Hugging Face text-embeddings-inference 1.9.4 on CPU (image digest `sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d`, the #858 parity image) on this study's own instance. Summaries and raw query text are embedded with no instruction prefix, and similarity is cosine with ties broken newest first, as `internal/memory/sqlite/conversation.go` `Search` does.

| Arm | Replication stored as | Ranking |
|---|---|---|
| **S4** (shipped) | B4 mids | Production recall: the 2 newest mids, then the 7 most similar, deduplicated, at most 7 delivered (`internal/agent/memory/conversation.go`). No budget beyond that rule. |
| **B4** | B4 mids | Cosine |
| **B8** | B8 mids | Cosine |
| **B16** | B16 mids | Cosine |
| **T** (aged tier) | B4 for recent sources, B16 for older ones | Cosine over the mixed store |
| **K** (keys over values) | B16 mids as delivered values; B4 embeddings as keys | Cosine over B4 keys; each hit delivers the B16 mid covering it; a distractor key delivers its own mid |
| **X** (hybrid) | B4 mids | Lexical and dense lists fused |
| **R** (rerank) | B4 mids | Cosine top 30, reordered by a local cross-encoder |

- **T's cutoff.** A replication B16 batch stays at B16 when its last source is older than the store's newest 2,400 messages; otherwise its four B4 batches are stored instead. At s0 that is sources 1 to 2,400 at B16 and 2,401 to 4,800 at B4. As distractors arrive the whole replication ages into B16, which is what an aged tier does. 2,400 is half the replication, so at s0 the tier stores the older half at the coarser size.
- **K's keys.** Keys are vectors only. The B4 summaries they are computed from are not stored, so K stores the B16 text and 1,200 vectors.
- **X's rule.** Lexical: SQLite FTS5 (`unicode61` tokenizer) over each mid's summary, `bm25()` ascending. The query is its lowercase `[a-z0-9]+` tokens of at least two characters, less a fixed English stop-word list in the harness, joined with `OR`. Dense: cosine. Fusion: reciprocal rank fusion, score `1/(60 + rank)` summed over the two top-50 lists, ties broken by dense rank.
- **R's reranker.** `BAAI/bge-reranker-base` at revision `2cfc18c9415c912f9d8155881c133215df768a70`, on its own text-embeddings-inference instance from the same image, scoring (raw query, summary) pairs over the dense top 30; the rest keep dense order below them.
- **Recency.** Each budgeted arm first delivers the 2 newest units of its own store, as production does: at s0 the replication's newest B4, B8 or B16 mids (K: B16 values; T, X, R: B4); from s1 on, the newest distractor mids.
- **Added arms.** R's depth of 30 and X's constant 60 are the only tuning constants, both fixed here. No other arm is added.

### Batch-8 bias estimate (secondary)

Distractors at batch 4 are finer than a batch-8 or batch-16 store would hold, which may bias against the compressed arms. At s1 only, **B8m** is B8 against the matched #874 batch-8 distractors. B8m minus B8 at s1 (hits on the 280) estimates that bias for B8. It is not an endpoint. The report reads B16, T and K as conservative by at most that amount, as far as B8 generalises.

## Delivery and budget

- **Budget C.** The mean, over the 280 primary queries, of the characters S4 delivers at s0. A delivered unit's characters are its content as production renders it: the stored summary and its `[mid:<id> sources:<first>-<last>; summary, not verified source text]` suffix.
- **Fill rule, every arm but S4.** Per query, units are taken in order (the 2 recency units, then the ranking), each once, and a unit is delivered only while the running total stays at or below C; a unit that would exceed C is skipped and the next is tried. At most 50 ranked candidates are tried. K deduplicates by delivered value, so two keys under one B16 mid deliver it once.
- The cap holds per turn, which is stricter than S4's mean. B4 against S4 at s0 shows what the cap alone costs.

## Endpoint

**Source-level evidence delivery:** a query is a hit when one of its evidence source IDs lies inside a delivered unit's source interval, the replication's rule. Reported per arm and scale on the 280 primary queries, with per-family counts, beside stored characters per source character: stored summary characters over source-message characters, for the replication part and for the whole store.

## Controls

1. **Reproduction.** S4 at s0 delivers 212 of 280, as #858 production parity did (`../mid-memory-rag/evidence/production-parity/`), and the delivered intervals of each of the 314 queries equal parity's. Any difference is explained in the report before any other result.
2. **Distractors outside gold.** Every distractor source ID lies outside every query's evidence and obsolete sources, and no distractor source message has the text of a replication evidence message.
3. **Mutation checks.** Each of these is shown to fail a test: the hit rule (delivery), the per-turn cap (budget), and deduplication (dedupe), plus the leak check. `mutation.py` records the output.
4. **Servers.** Each instance's `/info` reports the model and revision above.
5. **Reach.** Every primary evidence source lies in some stored unit of every arm at every scale, so a miss is a ranking or budget miss.

A failed control 2, 3 or 5 makes every arm comparison undetermined. Control 1 is handled as the issue says: explained before any result.

## Decision rule

Exact two-sided McNemar on the 280 paired primary queries at α = 0.05; paired 95% intervals are the conditional exact interval of `../mid-memory-rag/replication_score.py` `paired_interval`.

1. **Null condition.** S4 at s4 against S4 at s0. S4 degrades measurably when it loses more queries than it gains and p < 0.05.
   - If it does not, the report says s4 does not stress retrieval, makes no ranking claim on degradation, and reports only the s0 comparison and storage. The decisive scale is s0.
   - If it does, the decisive scale is s4, and each budgeted arm's degradation (s0 against s4) is reported with its McNemar p, Holm-adjusted over the seven budgeted arms.
2. **Comparison.** At the decisive scale, each of B8, B16, T, K, X and R against B4, Holm-adjusted over the six.
   - An arm **delivers more** when it wins more than it loses and its adjusted p < 0.05.
   - An arm is **no worse for less storage** when the lower bound of its paired interval is at least −0.05 and its replication-part stored characters per source character are below B4's.
3. **Recommendation** for a live answer run:
   - the arm that delivers more with the most hits (ties: less storage);
   - else the no-worse arm with the least storage;
   - else none, and the shipped recall stands.
4. The report states each recommended arm's added cost: a reranker server for R, a second synthesis size for T and K.

## Descriptive measures

- The pool's size after the leak check, per stream.
- Resemblance to the replication's own filler, where filler means replication B4 mids holding no primary evidence:
  - characters per source message and per summary;
  - per primary query, the top-1 cosine among distractor mids and among filler mids, their means, and the share of queries where the distractor top-1 is the higher.
- Recall@1, @5 and @20 of each arm's ranking before the budget, at s0 and s4.

## Cost

- No MiniMax call, no OpenAI call and no other paid API. Every summary is recorded; only embeddings and reranking are computed.
- Both servers run locally on this study's own ports (8130 and 8131), not the shared instance or the #896/#900 proxy.

## Amendment 1: mid-to-message drill-down

Added at the owner's proposal, relayed by the PM, after the first ranking of every other arm and before any ranking of this one. The arm is secondary to the decision rule above: it enters neither Holm family nor the recommendation, and the report sets it beside K.

- **Store.** B4 mids as in B4, plus every raw source message beside them: the replication's 4,800 messages and, at each scale, the distractor streams' messages from the same `corpus.json` files whose mids are already used. A message inside a mid the leak check dropped is never reachable, since only a ranked mid opens its messages.
- **Ranking.** Rank the B4 mids by cosine and take the top M = 5, the number of similar mids the shipped recall delivers beside its 2 newest. Rank every source message inside those 5 mids by cosine between the raw query and the raw message text, embedded on this study's embedding instance (`tei-901`) in batches of 16, with ties broken newest first.
- **Two orderings.**
  - **DR-rel**: the messages by score, highest first.
  - **DR-new**: the messages grouped by mid, the newer mid first, and by score within a mid.
- **Delivery.** The fill rule and budget C of §Delivery and budget, unchanged: the 2 newest B4 mids first, then the messages, each once, skipping any that would exceed C, at most 50 tried. A delivered message is rendered as its content and the suffix ` [source:<id>; message text]`.
- **Endpoint.** A hit when a delivered message's source ID, or a source inside a delivered recency mid, is an evidence source. Reported at every scale on the 280 primary queries, with per-family counts.
- **Storage.** B4 summaries plus the raw messages held beside them, over source characters, for the replication part and the whole store.
- **Comparison.** Each ordering against B4 at s0 and s4, exact McNemar with its paired interval, reported without adjustment and without entering the recommendation.
