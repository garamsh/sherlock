# Mid-memory replication preregistration

Fixes the corpus procedure, arms, power, decision rule and budgets for issue #856 before its first paid call. The #854 [preregistration](preregistration.md) and evidence stay as they are.

## Question

With enough queries that a null result is an answer, does the owner's structure (B: embedding similarity and recency lists, merged by ID, no pre-turn call) or a hybrid H of it with the shipped recall deliver the evidence for an older-conversation query more often than the shipped recall A? How does the batch size `n` change B?

## Corpus

[`replication.py`](replication.py) produces it in four phases, all with MiniMax-M2 through `https://api.minimax.io/v1/chat/completions`. Every request and response is recorded in `evidence/replication/calls.jsonl`.

1. **Design** (`design`). The model writes items in batches of 10 per domain, drawn by seed 856 from 30 everyday domains. Counts: exact 90, paraphrase 130, implicit 160, correction 90, absent 40, plus 240 filler topics. Exact and paraphrase items share one shape: a fact sentence with a distinctive key, an exact query and a paraphrase query. The prompts are in `replication.py` (`INSTRUCTIONS`).
2. **Item checks** (deterministic, `check_item`), before any conversation:
   - the key is in its sentence;
   - an exact query shares at least 2 production search tokens (`agent.SearchTokens`) with the sentence;
   - a paraphrase query and an implicit task get no hit from production `Search` over a store holding only the sentence, while the sentence finds itself;
   - correction keys differ, and each appears only in its own sentence;
   - an absent query does not contain its key;
   - no accepted key repeats an earlier accepted key.
3. **Conversations** (`converse`). One chat per item: an exact, paraphrase, implicit or absent item gets one, a correction gets two (older and newer), and each filler topic gets one. Length is 6, 8, 10, 12 or 14 messages, drawn per chat ID. Messages alternate user and assistant and stay on the item's own topic. The user says the item's sentence word for word inside one message. Chats are written for the first passing items only, up to these caps: exact 84, paraphrase 98, implicit 105, correction 84, absent 36, filler 140. A chat is rejected on wrong length, roles out of order, or a missing verbatim sentence, and is rewritten at most twice. If assembly falls short of 70 items in any family, a cap may be raised in steps of 20. Because selection takes items in design order, a raise cannot change any item already selected.
4. **Assembly** (`assemble`, deterministic, seed 858):
   - per primary family, the first 70 items in design order whose chats exist, and 30 absent;
   - a paraphrase or implicit item is used only if production `Search` misses every evidence message, meaning each message of its chat that carries the key;
   - an item whose key appears in another selected chat is banned and the next one taken, until no key leaks;
   - filler chats containing a selected key are dropped;
   - placement: each chat gets a seeded rank; a correction's older chat falls in the first half, and its newer chat at least a quarter of the stream later;
   - filler chats in ID order fill the stream to exactly 4,792 messages, with the last one truncated;
   - an authored 8-message chat closes the stream and carries 4 recent queries.
   Evidence IDs are the messages of the item's chat that contain its key; for a correction, those of the newer chat, with the older chat's key messages as obsolete. The corpus is committed after assembly and before synthesis and ranking.

The corpus is 4,800 messages: 1,200 mids at `n` = 4, 600 at 8, and 300 at 16. It holds 280 primary queries (70 each of exact, paraphrase, implicit and correction), 30 absent and 4 recent.

## Summaries

The production summarizer synthesizes every batch at each `n` through `bridge.go`, as in #854: the exact production request is recorded by SHA-256, and the response is replayed through production validation. A rejected synthesis is retried up to 5 attempts in total. At `n` = 8 a batch that is never accepted stops the run, because the production store cannot be built without it. At 4 and 16 it becomes an empty, unreachable summary, and the count is reported.

## Arms

Every arm delivers at most 7 mids: the 2 newest first, then relevant candidates, deduplicated, capped at 7. That is the production merge.

| Arm | Relevant list | `n` | Pre-turn call |
|---|---|---:|---|
| A, shipped | production `ConversationRecall` with the phrase call, FTS5 over summaries | 8 | 1 |
| A0 | production `ConversationRecall` without a model | 8 | 0 |
| B4, B8, B16 | cosine of the raw query embedding against summary embeddings; recency list unchanged (2 newest mids at that `n`) | 4, 8, 16 | 0 |
| H, hybrid | A's first 7 FTS candidates and B8's first 7 cosine candidates, alternated A first and deduplicated | 8 | 1 (A's) |

- The embedder is #854's: `BAAI/bge-base-en-v1.5` through `fastembed==0.7.3`, with the query instruction prefixed.
- The first paid call probes `embo-01` once. If it returns vectors, collection stops before design and the owner decides; otherwise the local embedder stands.
- A and H share one recorded phrase call per query.

## Controls

Required before any verdict:

- **Oracle reach on the evidence mids.** For every primary query, the mid holding its first evidence source must be reached when the arm is given that mid's own summary. A and A0 must deliver it, B at each `n` must rank it first, and H must deliver it. Each arm passes at 95%.
- **Similarity is not recency.** For each of B4, B8 and B16, top-2 equals the recency top-2 in fewer than half of the 314 queries, and top-1 takes at least 20 distinct mids.
- **Implicit and paraphrase token checks,** as in item checks and assembly above.
- **Scorer and harness tests fail with their behaviour removed.** The mutation output is recorded before collection.

## Power

Primary queries n = 280, paired by query. [`replication_score.py power`](replication_score.py) simulates the decision rule below, with 3,000 repetitions and seed 856 ([output](evidence/replication/power.json)). ψ is the fraction of queries on which the two arms disagree. #854 observed ψ = 0.15 for B against A; 0.30 is the conservative bound used here.

| ψ | Smallest gain with P(valid) ≥ 0.80 | P(valid) at 0.10 | P(not valid) at 0 |
|---:|---:|---:|---:|
| 0.15 | 0.07 | 0.997 | 0.979 |
| 0.20 | 0.08 | 0.967 | 0.961 |
| 0.25 | 0.09 | 0.913 | 0.909 |
| 0.30 | 0.10 | 0.847 | 0.852 |

The detectable difference is 10 percentage points or less for every ψ up to 0.30. If the observed ψ exceeds 0.30, the report restates the power at the observed value; the rule does not change.

## Decision rule

For each of B8 and H against A, over the 280 primary queries:

- **Measure.** Delivered hit (source-level, as in #854). `arm_only` and `A_only` are the discordant counts, with `m` their sum. The 95% interval for the paired difference is `m(2p − 1)/280`, where `p` runs over the exact Clopper–Pearson interval for `arm_only` out of `m`.
- **Guardrails.** In every primary family, the arm's hits are at least A's minus 7 (10% of 70). The arm's correction-newest count is at least A's minus 7.

The outcomes, checked in order:

1. **Undetermined** if a required control fails for A or the arm; for H, also B8's.
2. **Valid** if the interval's lower bound is above 0 and the guardrails hold.
3. **Not valid** if the interval's upper bound is below +0.10. The interval then excludes a gain of the detectable size.
4. **Undetermined** otherwise.

A0, B4 and B16 get the same statistics and are reported; they do not decide. The two verdicts are separate tests, each at alpha 0.05. If B8 or H is valid, the #854 code-size estimate is revised for that arm; H keeps the FTS path and adds an embedder.

## Other metrics

Per family and primary:
- delivered hits with Wilson intervals;
- recall@1/5/7/20 and MRR@20 over the relevant list;
- correction newest, obsolete-only, and newer ranked first;
- absent non-empty rate, and the AUC of B8's top-1 cosine for present against absent queries;
- cost per turn: phrase-call tokens and latency, and embedding seconds per text.

## Budgets

- At most 4,200 HTTP calls, 6,000,000 input tokens, 2,600,000 output tokens and USD 2.90, at MiniMax-M2's USD 0.30/M input and USD 1.20/M output ([pricing](https://platform.minimax.io/docs/guides/pricing-paygo), read 2026-09-25). The issue caps this run at USD 3.00.
- Expected use is about 3,300 calls and about USD 2.0: design 63 calls, conversations about 630, synthesis 2,100, phrase 314.
- `max_tokens`: design 6000, conversation 4096, synthesis 2048, phrase 1024.
- Each request reserves its body bytes plus 1,024 input tokens and its full `max_tokens` before HTTP. The check covers all in-flight reservations across 4 worker threads.
- Unknown usage is charged at the reservation.
- A transport failure, HTTP 429 or 5xx, or `base_resp.status_code` 1002 is retried after 10, 30 and 60 seconds. Each retry is its own recorded call. A content rejection follows the per-phase retry limits above. A phase resumes after its last recorded result.

## What ran before this commit

- The power simulation above.
- The unit tests.
- A no-network dry run of `assemble`, `rank` and the scorer on synthetic stand-in items: templated sentences and a 15-word vocabulary, written only to crash-test the pipeline. It is not kept as evidence, and no measured corpus existed then.

## Amendment 1, before any conversation or retrieval

Committed after the design phase and its deterministic item checks, and before any conversation call. No chat, summary or ranking of the replication corpus existed at the time.

- The item checks passed exact 73 of 90, paraphrase 68 of 130, implicit 67 of 160, correction 73 of 90, and absent 33 of 40. That leaves paraphrase and implicit below 70 before any chat-level check, and no family with the margin those checks need.
- A supplementary design round is added: exact 40, paraphrase 80, implicit 100, correction 40, absent 10 items. It uses the same prompts, with domains drawn by seed 866, and its calls are appended after the original calls. Every original item keeps its place in design order, so the round can only add candidates after them.
- The conversation caps become exact 100, paraphrase 100, implicit 100, correction 100, absent 38, filler 140.
- Nothing else changes: checks, selection, arms, controls, power and the decision rule stay as preregistered.

## Amendment 2, before any synthesis or retrieval

Committed after the first `assemble` attempt failed, and before any synthesis, phrase call or ranking of the replication corpus. That attempt ([its counts](evidence/replication/assembly-attempt-1.json)) selected exact 70, paraphrase 47, implicit 60, correction 42 and absent 16. It banned 117 items for key leaks, and the stream was 1,194 messages short of filler.

- **Leak rule.** Most bans came from generic keys ("72", "v1", "Tuesdays") that appear in unrelated chats. A chat that merely mentions such a key does not state the item's fact, so it does not make the evidence ambiguous. An item now leaks only when a message of another selected chat carries its key and at least two more of the item sentence's production search tokens, counting only tokens of four or more characters that are not part of the key. Short function words and the key itself do not tie a chat to the fact.
- **Paraphrase supply.** Only 56 of 91 paraphrase items survived the chat-level FTS check. A second supplementary round adds 100 paraphrase items with the same prompt, domains drawn by seed 876, and calls appended after all earlier calls.
- **Caps.** Paraphrase becomes 160, implicit 120 and filler 240, so every passing item and filler topic gets a chat.
- Selection order, checks, arms, controls, power and the decision rule are unchanged. If a family still ends below 70, the report states its size, and the power is recomputed at the realized primary count.

