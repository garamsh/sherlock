# Preregistration: K against the shipped recall at long-term scale (#913)

This fixes issue #913's arms, scale, K's recency rule, endpoints, margin, guardrails, controls, gate, decision rule and paid-phase order. It is committed before the screen ranks or scores a single query, and before any paid call.

## Contents
- Question
- Material and scale
- Arms
- Free screen
- Gate
- Live phase
- Controls
- Cost and paid-phase order
- What is already known

## Question

With the model answering, is K no worse than the shipped recall on a store grown to 3.67× the replication's messages, while it stores about 2.4 times less replication text? A free screen of delivery decides first whether a live run can pass.

## Material and scale

Everything is #901's, read through its harness by import (`../study.py`), unchanged:

- the #856 replication (`../../mid-memory-rag/evidence/replication/`): 4,800 messages, the recorded B4 and B16 syntheses, the 280 primary queries (exact, paraphrase, implicit, correction; 70 each) and the 30 absent queries with their near-miss sources;
- the four distractor streams' recorded B4 mids after #901's leak check, placed newer than the replication;
- summaries read through the production redaction and trimmed;
- `BAAI/bge-base-en-v1.5` at revision `a5beb1e3e68b9ab74eb54cfd186867f64f240e1a`, on text-embeddings-inference 1.9.4 CPU (image digest `sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d`), on this study's own instance `tei-913` at `127.0.0.1:8132` with `--memory 1g`, stopped when idle.

**Scale: s4 only** (17,600 messages, 3.67×, all four streams), the decisive scale #901's null condition named. s0 is ranked only to compute the budget and to reproduce #901 (§Controls); no comparison is read there. No second scale is added: at s4 each arm's two recency units are the newest distractor mids, identical across arms, so a second long-term scale would test the same recency and differ only in distractor count.

## Arms

| Arm | Replication stored as | Delivery |
|---|---|---|
| **S4** (shipped) | B4 mids | The shipped recall: the 2 newest mids, then the 7 most similar, deduplicated, at most 7; no character budget |
| **B4** (budgeted) | B4 mids | #901's fill: the 2 newest mids, then cosine order, under budget C |
| **Kr** (K as it would ship) | B16 values keyed by B4 vectors | The 2 newest units at batch-4 size, then K's value order, under budget C |

- **K's recency rule.** Recency is the 2 newest batch-4 units of the store. The store keeps its newest conversation as B4 mids until a complete B16 value covers them, so recency is never delivered at B16 size. At s4 the 2 newest units are distractor B4 mids, which every store holds at B4.
- **K's value order.** Rank every B4 key by cosine against the raw query, ties newest first; each key delivers the value covering it, each value once, in first-hit order (`../study.py` `values_in_order`). A distractor B4 mid is its own key and value.
- **Budget C.** S4's mean delivered characters at s0 over the 280 primary queries, each unit rendered with its production suffix (#901: 3,383.6). The fill takes recency and then at most 50 ranked units, each once, skipping any that would take the turn above C (`../study.py` `deliver`).
- **Stored characters per source character**, replication part: B4 and S4 0.684, Kr 0.280 (#901's `study.json` `storage`); recomputed and reported.

## Free screen

No model call. Kr, B4 and S4 are ranked at s4 over the same store content, and each query's delivered units recorded.

| Family | n | Measure, per query | Better is |
|---|---:|---|---|
| Primary (exact, paraphrase, implicit, correction) | 280 | **hit**: an evidence source lies inside a delivered unit's source interval (#901's endpoint) | more |
| Exact (guardrail) | 70 | hit | more |
| Correction (guardrail) | 70 | hit (the new statement's sources); and **stale**: an obsolete source delivered while no evidence source is | more hits; fewer stale |
| Absent (guardrail) | 30 | **near-miss exposure**: a near-miss source lies inside a delivered unit | fewer |

Each measure compares Kr with S4 and with B4, paired per query. The paired 95% interval is `../../mid-memory-rag/replication_score.py` `paired_interval`, and p is the exact two-sided McNemar test. The report also gives per-family hits for paraphrase and implicit, recall@1, @5 and @20 before the budget, mean delivered characters, and whether the delivered value text states the near-miss key (case-insensitive, flanked by no letter or digit).

## Gate

The paid phase goes ahead only if every one of these holds, and the PM then sends a go:

1. **Delivery margin.** On the 280 primary queries, the lower bound of Kr's paired interval is at least **−0.05** against S4 and against B4. −0.05 is #901's no-worse bound.
2. **Guardrail allowance: 3 queries** net, the allowance #885 to #896 used, against S4 and against B4:
   - exact: Kr's losses minus wins on hit at most 3;
   - correction: losses minus wins on hit at most 3, and queries stale under Kr but not the other arm, minus the reverse, at most 3;
   - absent: queries with near-miss exposure under Kr but not the other arm, minus the reverse, at most 3.
3. **Controls.** Every screen control in §Controls passes.

If any fails, no paid call is made and the report says which and by how much. A control that fails makes the screen undetermined, which also closes the paid phase.

## Live phase

Run only after the gate passes and the PM's go.

- **Arms.** S: the shipped recall on the s4 store. K: Kr as screened, the same store with its replication part as B16 values over B4 keys. `memory_search` during work ranks the same way in each arm, K's through its keys to values, at the shipped result count. `memory_inspect` reads the same raw sources in both.
- **Store.** One SQLite store per arm built from recorded summaries and raw messages, with no synthesis call: the replication's 4,800 messages, then the four streams' messages from their `corpus.json`, and every kept mid of #901's leak check. Each probe runs on its own copy.
- **Probes.** The 280 primary and 30 absent queries, one live turn each, with a live window of the store's newest 32 messages and an empty note, as #890 fixed the live turn.
- **Model.** MiniMax-M2 through one study proxy, `../../chained-recall/harness` `proxy`, with its ledger in `evidence/ledger/`. On HTTP 429 with code 2056 it pauses every request and probes the key at most every 30 minutes; a pause uses no attempt.
- **Harness.** The live arms and the store builder are committed, with their SHA-256 recorded in an amendment to this file, before the first paid call. Nothing else in this file changes after the screen.
- **Applied**, mechanically, by `../../chained-recall/score.py` `applied` over the reply, messages sent and files written:
  - exact, paraphrase, implicit: the item's `key` is stated (`contains`);
  - correction: the `new_key` is stated (`correction`);
  - absent: the visible answer matches #862's abstention pattern (`abstain`).
- **Endpoint.** Applied on the 280 primary probes, K against S: paired 95% interval and exact McNemar. Per family (exact, paraphrase, implicit, correction, absent), exact McNemar and counts, unadjusted.
- **Decision.** K is **valid** when all hold:
  - the lower bound of K − S on primary applied is at least −0.05;
  - exact, correction and absent each lose at most 3 net queries against S;
  - every live control passes.

  K is **not valid** when the upper bound of K − S is below −0.05 or a guardrail fails with every control passing. Otherwise the verdict is undetermined. If valid, the report states the production change K implies (store, synthesis, recall, migration) and its line estimate.

## Controls

Screen:

1. **Reproduction.** S4 at s0 delivers 212 of 280 and the same intervals as #901's `rankings.jsonl` on all 314 queries; C equals #901's `study.json` `budget`.
2. **#901 at s4.** S4, B4 and K at s4 deliver the same intervals as #901's rankings on all 314 queries. Kr at s4 equals #901's K at s4 because its recency units are the same distractor mids (§What is already known); a difference is explained before any result.
3. **Reach.** Every primary evidence source lies in some stored unit of Kr's store at s4.
4. **Kr's recency is batch-4 sized.** Every recency unit Kr delivers at s0 and at s4 spans at most 4 sources; at s0 this is the replication's own newest B4 mids, where K would deliver B16 values.
5. **Server.** `tei-913`'s `/info` reports the model and revision above.
6. **Mutation checks.** Each of these, removed, fails a test: the hit rule, the stale rule, the near-miss rule, Kr's batch-4 recency, the delivery margin, and the guardrail allowance (`mutation.py`, `evidence/mutation.txt`).

Live, fixed now and checked from the live records:

7. **S at s0 reproduces #901's delivery**: the S arm's recall block on an s0 store holds #901's S4 s0 intervals for all 280 primary queries, read without a model call.
8. **K delivers what the screen says**: for every primary probe, the K arm's live recall block holds exactly the units the screen delivered, so where the screen counts the gold evidence as delivered, the live block carries it.
9. **Live mutation checks**: the applied check, the abstention check and the decision rule each fail a test when removed.

## Cost and paid-phase order

- The screen is free: embeddings only, on `tei-913`. No MiniMax, OpenAI or other paid API, and no credit purchase.
- `MINIMAX_KEY` comes from the repository root `.env`, exported into the proxy's environment only.
- Paid phases, in order, each after the previous one's records are complete:

  | Order | Phase | Probes | Estimate (input / output tokens) |
  |---:|---|---:|---|
  | 1 | Key probe | 1 request | under 0.001M |
  | 2 | Smoke, 2 probes per arm, excluded from scoring | 4 | 0.05M / 0.003M |
  | 3 | S live | 310 | 4.0M / 0.2M |
  | 4 | K live | 310 | 4.0M / 0.2M |

  The estimate is #896's measured S live turn (4,989,761 input and 268,967 output tokens over 420 probes, `../../memory-encoding/structural/evidence/tokens.json`, about 11,900 and 640 per probe) plus about 1,000 input tokens per turn, the replication mids being longer than #890's. Tokens are reported per phase from the ledger.
- **Pacing.** Two requests at a time; S completes before K starts.
- **Re-runs.** A probe that fails on the provider's limits, a server fault or the network runs once more, its first record kept in `results-attempt1.jsonl`. Any other failure counts as not applied.

## What is already known

Written down so the screen is read for what it adds.

- **K at s4 is #901's result.** #901 reported K at s4 at 197 against B4's 203, interval [−0.042, +0.008], and per family exact 61 against 62 and correction 66 against 66. Because K's recency at s4 is already the two newest distractor B4 mids, Kr at s4 is expected to equal it (control 2).
- **What is new.** The comparison with S4 at s4 (S4 delivered 201 there) and the stale and near-miss guardrails, none of which #901 computed.
- **Not known.** Nothing here has been ranked or scored for #913.
