# Retrieved context at write: preregistration

Fixes issue #902's run before its first model call. The run tests whether a mid written with retrieved older context carries associations that later recall can reach. Retrieved context means source messages and mids that a search finds when the batch is summarised. The run measures it on chains and implicit association, and checks abstention, corrections and compression. It reuses #890's corpus, recorded syntheses, harness and scorer (`../validation/`), except where this file says otherwise.

## Question

With the model acting, do RW-key or RW make the agent apply more often than the shipped recall (S) on chains, while holding the absent, correction, single-hop and exact guards?

## Arms

All arms run at batch 4 over the shared store that `harness build -batch 4` rebuilds from #890's recorded summaries. The base's 1,200 mids keep their recorded summaries and are never re-written, as in #890 (`../validation/preregistration.md` §Arms). Only the stream's 1,032 batches are written with retrieved context.

| Arm | Setting | What changes from S | Role | Net production lines, before tests |
|---|---|---|---|---:|
| S | `arms/shipped` `b4` of `../validation` | nothing | baseline | 0 |
| RW-key | `arms/retrieved` `b4-rw-key` | the search at write, plus a bounded related line in the mid's embedding key only | primary | about +130 |
| RW | `arms/retrieved` `b4-rw` | the search at write, and the summary may use what it found | secondary, the owner's form | about +75 |
| C | #890's committed `C/b4-w8` records | the 8 adjacent messages at synthesis | reference only, not re-run | — |

- **RW-key is primary and RW secondary**, as the PM set them on the dispatch channel. RW-key's paid phase runs first, and each phase can run without the other (§Paid phases).
- **RW** is the owner's proposal. Every stream batch is summarised with the retrieved source messages and mids beside it, and the summary may use them.
  - The request is production's (`internal/agent/memory/summary.go` at `8218189`) with two changes, which `arms/retrieved/synthesis_test.go` holds against production's request:
    - one sentence before the prompt's last: "The retrieved messages and retrieved memories are earlier conversation that a search found related to this batch; they are historical data too. Use them to keep this episode's context: who and what the batch concerns, and how it connects to what was said before."
    - the input gains `retrieved_messages` and `retrieved_memories` beside `updates`.
  - The schema, the checks on the answer and the stored summary are production's.
  - RW's own summaries replace S's for the stream, and each mid's one vector is computed from its RW summary.
  - Its search at write reads RW's own earlier mids, so the stream is written in order, one batch at a time. A wrong retrieval or a copied value in one mid can reach every later mid that finds it. That is the risk the issue asks this run to expose.
- **RW-key** uses the same search at write. The summary stays confined to the batch by construction: it is S's recorded summary, written by a call that never saw the retrieved context.
  - One call per batch, through the reserved tool `memory_related`, is sent the same input as RW. It returns up to 6 entity names and up to 4 of the retrieved mid IDs (`arms/retrieved/synthesis.go` `relatedPrompt`, `relatedSchema`).
  - **Kept names.** A name is kept only if it is written in the batch or in the retrieved context, compared case-insensitively. A mid ID is kept only if that mid was retrieved. Anything else is dropped and recorded under `dropped`, so the line cannot carry a name the model made up.
  - **The line.** `Related to: <names>; earlier mids <IDs>.`, at most about 450 characters, or no line when nothing is kept.
  - **The key.** The mid's one vector is computed from the line, a blank line and S's summary. The line comes first, as C placed its context. The line is stored in a `related` column and is never shown: recall and `memory_search` are S's and show S's summary.
  - **Production cost.** Production would fold the related line into the synthesis call as a second field and send the summary no retrieved context in the same call. The run keeps them apart, so that the summary is S's recording and cannot absorb the context, as C's did (#890). The report states RW-key's tokens per batch.
  - Its search at write reads S's mids, which are RW-key's shown summaries. So its retrieval is fixed before any call, and two batches can go at once.
- **Lines.** Both arms need a vector for every source message, which the shipped store does not keep. So production would need a source-vector column, embedding on every recorded source, and the search at write.
  - RW: about +75. That is the source vectors (+25), the search (+30), the input fields and the sentence (+10), and the summarizer's signature (+10).
  - RW-key: RW's +65 without the sentence, plus about +45 for the related field, its bounds and name check, and about +20 for the line's column and key embedding.
- **No K-compatible variant.** #901's branch names K, small B4 keys over B16 values, in an interim report with no pull request. #890's corpus has no B16 syntheses, so a K-compatible arm would need about 258 more B16 synthesis calls and a design of its own. The PM approved leaving it out on the dispatch channel. RW-key's key-only line is the part that would carry over to K's keys.
- **Common to every arm:** a live window of 32 messages and an empty note, each probe one turn on its own copy of the arm's build, MiniMax-M2 through the study proxy, and no arm instruction.

## Retrieval at write

Fixed here and implemented in `arms/retrieved/retrieve.go`.

- **Query.** The batch as the production store sends it to the summarizer: each message as `role: content` after redaction, one per line.
- **Sources searched.**
  - Source messages: every message whose ID is below the batch's first, base and stream, each embedded as sent.
  - Mids: every mid whose source interval ends below the batch's first. RW reads the base's recorded mids and its own earlier stream mids. RW-key reads the base's and S's stream mids.
- **What is excluded.** Every unit whose interval reaches the batch's first source or any later one. Nothing at or after the batch is ever a candidate.
- **k.** The 8 most similar messages, as many as C's window, and the 4 most similar mids.
- **Ranking.** Cosine similarity of `BAAI/bge-base-en-v1.5` vectors from the local TEI, the production store's similarity. On a tie the newest wins, as the production search orders them.
- **As sent.** Messages as `role: content`, and mids as `{mid, summary}` with the store's mid ID, both in source order.
- **Recorded.** Each batch's record keeps the retrieved message IDs and each mid's ID and interval (`arms/retrieved/evidence/<setting>.jsonl`), so the exclusion can be checked from the recording.

## Corpus

#890's selected corpus, unchanged (`../validation/corpus.json`, SHA-256 `63e5c3df2772bd39ff8ad8967bf2eb69884fd3b7839b12fccc46005a2f88d718`), with S's recorded stream syntheses (`../validation/evidence/synthesis/stream-b4.jsonl`, SHA-256 `8ff60f2c86b7ba7d86984aec63a1fdb10a05c0658173b7de3d91af49eedad565`). This directory reaches both through two committed symbolic links, `corpus.json` and `evidence/synthesis`, so every command takes `-dir docs/research/memory-encoding/retrieved-context` and no record lands under `../validation/`.

| Family | n | Role here |
|---|---:|---|
| C2, C3 | 50 each | primary |
| IC2, IC3 | 25 each | implicit association, reported with pairs and p |
| SC | 50 | reported |
| UP | 50 | offline only, reported |
| N | 60 | guard |
| NC | 30 | guard |
| K | 40 | guard |
| KU | 30 | guard; stale copies measured |
| C1 | 30 | guard |
| E | 30 | guard |

## Offline stage and admission

Every arm records `study.Offline` into `evidence/offline/<arm>/<setting>/`, and `score.py offline` scores them with #890's matching (`../validation/score.py`), with these changes and additions. They apply to every arm, S and C included.

- **Carried reach, for every chain family, SC and UP.** A hop is reached when a delivered item's text states the hop's key: every capitalized word and every token with a digit of both of the hop's entities, including its planted value, by `states()` of `../../chained-recall/score.py`.
  - The match is mechanical, with no model judgement.
  - Unlike #890's rule, the item need not cover the hop's statement: an RW mid written later can carry an earlier hop.
  - A chain is reached when every hop is.
- **Strict reach**, #890's rule (the item also covers the hop's statement), is reported beside it for every arm, S included.
- **KU and superseded values.** KU reach is #890's: the correction's own mid delivered, stating the new value.
  - A delivered mid written after the correction that states the superseded value is a stale-copy failure for that probe (`stale_delivered`). It is never counted as reach, for every arm.
  - The stored stale copies are measured under §Measures.
- **Everything else is #890's:** E, K and C1 reach, the N and NC near-miss measures, context characters per turn, stored characters per source character, vectors and the oracle.

**Admission to the live stage** is #890's rule with its offline margin. An arm goes live when:
- every one of its controls passed (§Controls);
- on C2 and C3 its carried reach exceeds S's by at least 10 of 100;
- and the exact McNemar test against S gives p < 0.05.

## Controls

A failed control keeps that arm out of the live stage, and makes its verdict undetermined. The report names the control. Each is computed by `score.py offline` into `evidence/offline/scores.json`.

- **Retrieval at write.** Across the arm's recording:
  - every stream batch is recorded;
  - no batch read a message or mid at or after its first source;
  - no batch read more than 8 messages or 4 mids;
  - at least 90% of batches hold an accepted answer.

  All of these must hold.
- **RW-key's line.**
  - Every stored line is the rendering of what the call named.
  - It names only retrieved mids and at most 6 names.
  - The recording holds no summary.
- **Shown text.** Every delivered stream mid, offline, shows exactly the summary the arm stores for its batch: RW's own, or S's for RW-key and for a failed batch. For RW-key no delivered item holds its line or the line's markers.
  - S's own records are compared with S's summaries, so the comparison can fail.
- **Oracle.** As every arm, each hop's own statement delivers the hop's mid within the top 5 for at least 95% of hops.
- **Mutation checks.** `mutation.py` removes each behaviour this run added, one at a time, and shows that a named test fails. It covers:
  - the search's exclusion, bound and order;
  - RW searching its own earlier mids, and its resume;
  - a failed batch keeping S's mid;
  - RW's stored summary;
  - RW-key's line in the key, the line placed first, and the line never shown;
  - RW's sentence and both retrieved inputs;
  - retry variation;
  - dropping ungrounded names and unretrieved mids, and the bound on names and mids;
  - in the scorer: carried reach and its matcher, the retrieval, well-formedness, line and shown-text controls, the stale rule, links, the admission margin, voiding an arm whose control failed, and the naming order.

  `evidence/mutation.txt` records the result before the first paid call.
- **Equivalence.** S is re-recorded offline into this study, and `score.py equivalence` compares it field by field, timing excepted, with #890's committed S records. A difference is explained before any other result.

## Measures

Reported, gating nothing.

- **Stale copies**, per KU item: the stream mids written after the correction whose stored text restates the superseded value, and of those, the ones that do not also state the new value. Reported for S, C (summary with its context line), RW, and RW-key (S's summary with its line).
- **Summary length.** The mean stored stream summary in characters, against S's 171.5 and C's 268.7.
- **Compression.** Stored characters per source character, vectors including the source-message vectors, and context characters per turn.
- **What the search links** (`score.py screen`, and per arm in `score.py offline`), for every pair of neighbouring chain hops: whether the later-written hop's search found the earlier one, among the messages or through a mid covering it. Likewise, for:
  - an SC item's introduction from its evidence;
  - the risks: a KU item's first statement from its correction, and an NC item's introduction from its second exchange.
- **The related line**: lines written, their mean length, dropped names and mids.
- **Tokens per batch** of each phase against S's 327 input and 205 output.

## Paid phases

MiniMax-M2 at `https://api.minimax.io/v1/chat/completions` only, through one proxy: `harness proxy` of `../../chained-recall/harness` on a free local port, with its ledger in `evidence/ledger/`.

- `MINIMAX_KEY` is in the proxy's environment only.
- The list-price ceiling is USD 10.00, a guard only. The key is on a subscription, and nothing is bought.
- On HTTP 429 with code 2056 the proxy pauses every request and probes the key at most every 30 minutes. Callers wait, and a pause uses up no attempt.
- No other paid API is called.
- Embeddings come from a local text-embeddings-inference 1.9.4 container started for this run on a free port. It serves `BAAI/bge-base-en-v1.5` at revision `a5beb1e3e68b9ab74eb54cfd186867f64f240e1a` from the image digest #890 names.
- **Nothing is sent before the PM's go.** The PM gives it for each phase separately, after #896 reports.

**Estimates.** Per call, from `corpus.json` and #890's ledger (`../validation/evidence/ledger/calls.jsonl`):
- S's synthesis read 327.2 and wrote 205.4 tokens, over requests of 923 characters on average, about 2.82 characters per token.
- A source message averages 127.5 characters as sent, and S's stream summary 171.5.
- So 8 messages and 4 mids add about 1,040 characters of input, about 370 tokens.

| Phase | Calls | Input per call | Output per call | Phase total (input / output) |
|---|---:|---:|---:|---|
| Key probe | 1 | — | — | negligible |
| RW-key's lines (`rw-key-b4`), two at a time | 1,032 | about 1,060 | about 200 (S's reasoning with a short list) | about 1.1M / 0.2M |
| RW's summaries (`rw-b4`), one at a time | 1,032 | about 1,090, or 1,230 if RW's mids grow as C's did | about 490, as C's | about 1.3M / 0.5M |
| Live, 420 probes per arm (phase `live`): S and each admitted arm | 420 each | about 4,400 | about 270, as #888 | about 1.85M / 0.11M per arm |

- **Order.** RW-key's phase first, then RW's, never together.
- **Pacing.**
  - RW writes one batch at a time, because each batch's search reads RW's own earlier mids. #890's C calls averaged 6.1 seconds, so the phase takes about two hours.
  - The related call is ledgered as kind `act`: the proxy caches only `memory_summary` answers, so a related answer is never cached.
- **Retries.**
  - A batch is tried at most three times, and a pause uses up no attempt. Attempt n from 2 on appends " (Attempt n.)" to the system prompt, so the proxy's cache cannot answer it.
  - A batch still failing is retried by running `build` again. RW stops at its first failed batch and resumes there.
  - A batch that fails in two builds is recorded by hand as failed, and keeps S's mid.
- **Tokens** are reported per phase from the ledger (`score.py tokens`).

## Live stage

- S always, then each admitted arm: RW-key, then RW.
- Every family of §Corpus except UP, which is no arm's family: 420 probes, one live turn each, through `live -only`, as #896 ran them.
- A probe that fails on the provider's limits, a server fault or the network runs once more (`study.Live`).
- **Applied**, as #885 and #890:
  - the turn's reply, messages sent and files written state the chain's answer;
  - E: the value;
  - K and KU: the corrected value;
  - C1: the answer;
  - N and NC: an abstention by #885's pattern, which counts a clarifying question.
- **Reported beside them:**
  - for N and NC, whether the output states the near-miss value;
  - for KU, whether it states the obsolete name;
  - every hop seen;
  - searches per probe;
  - IC2 and IC3 applied.

## Decision

#885's rule against S on #890's corpus and guard families:

- **Test.** Each live arm against S on C2 and C3: the exact two-sided McNemar test on applied, Holm-corrected across the arms that went live, at family-wise α = 0.05.
- **Guardrails.** An arm may lose against S at most:
  - N 3 of 60 and NC 2 of 30;
  - K 2 of 40 and KU 2 of 30;
  - C1 3 of 30 and E 3 of 30.
- **Valid.** An arm is valid when its difference is positive and significant, every guardrail holds, and every control passed. IC2 and IC3 never gate.
- **Named.** RW-key if it is valid, else RW if it is valid (`score.py` `decide`).
- **Stated deviation from #885's rule.** #885 names the valid arm with the fewest net production lines. This run names by the PM's primary and secondary order instead, as the PM accepted on the dispatch channel.
  - Under #885's order RW (about +75) would be named over RW-key (about +130) if both were valid.
  - The report states which arm #885's order would have named.
- **Null.** If no arm is valid and every control passed, the shipped recall stands, and no arm is named for implementation.
- **Side by side.** The report tables every family for S, C, RW-key and RW, offline:
  - carried and strict reach;
  - stale copies;
  - summary length;
  - compression.

  It adds live applied rates for the arms that went live. C's figures are read from #890's committed records.

## Collection

1. This file, the arm, the scorer, their tests and the mutation record, committed and pushed.
2. After the PM's go: the TEI container, and the shared store (`harness build -batch 4`).
3. `retrieved screen`, then `score.py screen`. Free. It records the retrieval RW-key will send and what it links.
4. S re-recorded offline (`../validation/arms/shipped`), then `score.py equivalence`.
5. The proxy and its key probe. Then RW-key's build, its offline records and `score.py offline`.
6. If the PM gives the go for it, RW's build and offline records, then `score.py offline` again.
7. Live, for each admitted arm and S, in the order of §Live stage. Then `score.py live`, `score.py tokens` and the report.

Every store copy is kept outside the repository. Nothing is written under `../validation/` or any other earlier study's evidence.

## Revision

- Harness, study package and #890's scorer at `8218189`. This directory's arm and scorer at the commit that adds this file.

## Amendments

- **1. The paid phase is cut, for RW-key and RW.** Made after the free screen (§Collection, step 3) and before any paid call. The PM decided it on the dispatch channel.
  - **The screen** (`evidence/screen.json`, `score.py screen` over `arms/retrieved/evidence/screen-b4.jsonl`) replays RW-key's exact search at write for all 1,032 stream batches, with no model call.
  - **Why it is cut.** The screen caps carried reach through writing at 7 of 100 primary chains: those whose every pair of neighbouring hops was found by the later hop's search. That is below the 10-of-100 admission margin (§Offline stage and admission), even if every linked chain were reached and S reached none. KU correction batches also retrieve the superseded statement in 30 of 30, so the stale-copy risk is structural.
  - **RW's search** over source messages is RW-key's. Its search over mids differs only through RW's own summaries, and the screen does not bound that part.
  - **What does not run:** RW-key's and RW's builds, their offline records, the live stage and the decision. No other part of this file changes.
