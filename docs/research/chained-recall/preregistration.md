# Chained-recall preregistration: stream synthesis and the offline stage

Fixes the first paid phase of issue #874 before its first model call: the stream's synthesis at batch 4 and 8. It also fixes the offline stage's endpoints, controls and comparisons before any summary of the stream exists. The live stage gets its own preregistration after the offline results.

## Paid phase 1: stream synthesis

- **What is called.** The session model, `MiniMax-M2` at `https://api.minimax.io/v1/chat/completions`, through the production summarizer (`internal/agent/memory/summary.go` at `03cbdff`) and the study proxy. The request is the one the production store sends for a batch: each message as "role: content", after the store's redaction.
- **How many.** One call per stream batch, a call re-sent only as the proxy's retry rules allow:
  - 424 batches at batch 4;
  - 212 batches at batch 8.

  The base's batches are not called: the #856 replication's recorded summaries serve them (`../mid-memory-rag/evidence/replication/synthesis-n{4,8}.jsonl`).
- **Estimate.** The replication's synthesis calls averaged 557 input and 368 output tokens (its `calls.jsonl`, 2,112 calls with usage). The stream's exchanges are shorter, so 636 calls should take under 0.4M input and 0.25M output tokens.
- **Ledger.** `evidence/ledger/` holds `calls.jsonl`, `synthesis-cache.jsonl` and `probes.log`.
  - The proxy's ceiling is USD 1.00 at MiniMax-M2's list prices (USD 0.30 per million input and 1.20 per million output tokens). It is a guard only: the key is on a subscription, and the report states tokens, not dollars.
  - Every attempt is reserved against the ceiling before it is sent.
- **Pacing and the usage limit.**
  - Two requests at a time; the key is shared with the L and H workers.
  - One ledgered probe precedes collection (`harness proxy -probe`).
  - On HTTP 429 with code 2056, the proxy pauses every request and probes at most every 30 minutes until one succeeds. The synthesizer waits while it is paused, and a pause uses up no attempt.
  - A batch that still fails is re-run once in a second pass, and its one recorded summary is the one used.
- **Outputs.** `evidence/synthesis/stream-b4.jsonl` and `stream-b8.jsonl`, one summary per batch. `harness build -batch <n>` then writes the shared stores from them, with no call.

## Offline stage

Every arm's records come from `study.Offline` and are scored by `score.py offline` as committed here.

- **Primary offline endpoint:** chain reach on the 150 chains of k ≥ 2 (C2, C3, IC2, IC3), for each arm and setting. A chain is reached when every hop is reached, and a hop is reached when a delivered item's source interval holds the hop's statement and its text states both entities the hop joins.
- **Reported beside it:**
  - reach for each hop and for each k;
  - interval-only reach;
  - C1 reach;
  - guard reach (E, K), and the characters delivered for N;
  - mean context characters per turn;
  - stored characters per source character;
  - vectors stored.
- **Comparison.** Each arm and setting against S at batch 4, on the 150 chains, by the exact two-sided McNemar test at α = 0.05, Holm-corrected across the comparisons reported.
- **Controls**, required before any offline comparison is read as a result:
  - **Chain check.** On the batch-4 shared store, raw-query similarity ranks at most 11 of the 225 later hops (hops 2 and 3 over all chains, 5%) within the top 20 (`harness check -batch 4`). More than that means the corpus lets a raw query reach the chain, and the stage is reported undetermined.
  - **Oracle.** S at batch 4 ranks the hop's own mid within the top 5 for its own statement for at least 95% of the 405 hops (385).
  - **Each arm's mechanism ran.** Each arm's README states its control and its threshold before its offline run: for example, links present and walk steps taken for L, and groups stored and delivered for H.
  - **Every arm is built from the committed corpus.** `corpus.json` has the SHA-256 below.
- **The live stage** follows `../associative-memory/README.md` §Test plan for #874: S and M always, and of L and H the one with the higher offline chain reach at equal context characters. Its preregistration fixes how "equal context characters" is read, from the S settings' figures, before any live call.

## Revision

- Harness and interface: `ed88e87`.
- SHA-256:
  - `corpus.json`: `034439bafec98e2462e91a0d4ce731aa1a9b3a2d4dd366d3c13cd437ba7c8e47`
  - `score.py`: `8122a7b04f4415f7b1b8b55642385944022e34826bb32a150c1212acf0908257`
  - `corpus.py`: `e830e8742c1d269eeaafdb9cc53d7e73b0ba1c84129b9858e1c86366f5554e74`
  - `evidence/screen.json`: `cf578c3a235283803abc761a0a0b7c29e1f4e90caed3a162c094117a8b609945`

## Amendments

- **1.** Committed during the stream synthesis, before any stream summary was read or scored.
  - The proxy's list-price ceiling rises from USD 1.00 to 3.00, by the PM's decision. The same shared ledger then covers L's and H's synthesis phases too.
  - It stays a guard: the Token Plan refuses with 429 code 2056 rather than billing, and the 2056 pause is unchanged.
  - The proxy restarts to apply it. A batch whose request fails during the restart is re-run under the rule above.
  - `score.py` gained the `live` and `tokens` commands in `f3f7e62`. Its offline functions are unchanged from the SHA-256 above; `git diff 1982942 f3f7e62 -- score.py` shows additions only.
  - The PM fixed H's live-stage margin at 5 percentage points: H goes live only if its chain reach on k ≥ 2, at equal context characters, trails S at batch 4 by no more than 5 points.
- **2.** Committed after the post-synthesis chain check, and before any arm was built or any arm's records existed.
  - **The chain-check control failed.** At batch 4, raw-query similarity ranks 26 of the 225 later hops within the top 20, against at most 11 allowed. At batch 8 it ranks 23 (`evidence/check-b4.json`, `check-b8.json`).
    - All the leaking hops are final hops of explicit chains: C3 14 and C2 12 at batch 4.
    - Their summaries match the kind of answer the query asks for, not the chain's words. "The food for our event" meets "a catering booking … under reference NW-9992", and "which door" meets "works out of room 385". A query's chain-specific names are absent from them.
    - Of the 26, 6 rank within the 7 items the shipped recall delivers, and 2 within 5.
  - **Under this file's rule the offline comparison is therefore reported undetermined, and it will be.**
  - **Oracle.** It passed: 404 of 405 hops at batch 4.
  - **Clean subset.** Fixed now, from the check alone and not from any arm's records, for a secondary analysis:
    - The clean subset is the chains none of whose later hops ranks within the top 20 at batch 4 or at batch 8.
    - It holds 144 chains: 114 of k ≥ 2 (C2 35, C3 30, IC2 24, IC3 25) and all 30 C1.
    - `score.py` reports every offline figure for it beside the full set, labelled secondary.
    - The live stage's preregistration takes the clean subset's k ≥ 2 chains as its primary set, so that the live verdict does not rest on a corpus that fails its own check.
