# Chained-recall confirmation: preregistration

Fixes issue #885's confirmatory run before its first model call. The run decides whether entity-linked recall (L), with or without an abstention safeguard (L+A), makes the agent follow chains more often than the shipped recall (S), without answering absent questions from associated memories. It uses #874's harness, proxy, scorer and arm code (`../README.md`), except where this file says otherwise.

## Question

With the model acting, does L or L+A make the agent apply a chain's answer more often than S does, while holding the absent, correction, single-hop and exact guards?

## Arms

All three arms run at batch 4 over one shared store built from this corpus.

- **S.** The shipped structure (`../arms/shipped`, setting `b4`).
- **L.** Arm L as #874 measured it (`../arms/links`, setting `b4-model`). The synthesis call writes each batch's entities, and a Personalized PageRank walk from the entities the query names fills the shipped recall's historical slots in turn with similarity.
  - The base's 1,200 batches keep the entities #874 recorded. `arms/links/evidence/entities-b4.jsonl` starts as the 1,200 base rows of `../arms/links/evidence/entities-b4.jsonl`, copied unchanged, including the one failed batch (509–512), which stays unlinked.
  - The stream's 872 batches get new entity calls.
- **L+A.** L plus the smallest safeguard (`../arms/links`, setting `b4-model-a`), and nothing else:
  - **Label.** Recall and `memory_search` deliver each walk mid with the kind `associated`. Its text opens with `Associated through the named entity "<name>", not matched to the question: `, where the name is the entity the mid names with the highest walk score (`memory.go` `associatedLabel`, `walk.go` `linked`).
  - **Instruction.** One contract instruction, appended to the ego as #874's M was (`main.go` `Abstention`): "Answer a fact only from a memory or source that states that fact about the thing the task asks about. A memory labelled associated was reached through a named entity, not matched to the question: treat it as a lead to search or inspect, never as the answer itself."
- **Net production lines, before tests.**
  - L: about +270, as measured in `../arms/links/README.md` §Production estimate, measured.
  - L+A: about +292. That is L's +270, plus 22 lines the production port carries: 10 for the entity a mid was reached through, 11 for the label, and 1 for the instruction's sentence.
- **Common to every arm.** Everything else is as `../live-preregistration.md` §Arms fixes it:
  - a live window of 32 messages;
  - each probe one turn on its own copy of the arm's build;
  - MiniMax-M2 through the study proxy.

## Corpus

`confirm/corpus.py` over #874's templates and rules, with seed 885 (`../corpus.py` for the seed of #874).

- **Candidates** (`candidates.json`, written by `corpus.py assemble`):

  | Family | k | Candidates | Kept | Role |
  |---|---:|---:|---:|---|
  | C2 | 2 | 100 | 50 | primary |
  | C3 | 3 | 100 | 50 | primary |
  | C1 | 1 | 30 | 30 | guard |
  | IC2 | 2 | 30 | 25 | reported only |
  | IC3 | 3 | 30 | 25 | reported only |
  | N, absent | — | 90 | 60 | guard |
  | E, exact | — | 30 | 30 | guard |
  | K, correction | — | 40 | 40 | guard |

- **Stream.** Every candidate's exchanges are in the stream: 860 exchanges of four messages, then 48 messages of tail chat: 872 batches of four. So the stream, and every summary and entity list of it, is fixed before anything is selected. Unselected candidates stay in it as distractors.
  - The rules of #874 hold, and `corpus_test.py` tests them: each exchange fills one batch alone; parts of one item are at least 16 exchanges apart; later-hop words appear only in the hop before; the stream closes on a multiple of 32.
  - The one change is the scheduler. #874's greedy order strands an item's last parts at this size, so an eligible part of the item with the most parts left goes first.
- **Absent queries (N).** Each item joins two entities of one domain's final kind, E and F, in one exchange ("Anya Tobend shares an office with Pavel Botros."). It states F's value in another ("Pavel Botros answers on extension 7256."). Its query asks for E's value in the form of the domain's C1 query ("What number do I dial to reach Anya Tobend?").
  - E's value is stated nowhere, and E's distinctive word occurs only in the joining exchange.
  - The walk from E reaches F's episode, whose value is the wrong answer.
  - The two entities never share a first name.
- **E and K** are drawn from the #856 replication's exact and correction queries, as #874's were.

### Selection, before any live call

`corpus.py select` writes `corpus.json` from the candidates. It uses only the chain check and L's offline recall, and no arm's live record.

- **Chains.** For each family, it keeps the first candidates, in candidate order, none of whose later hops the raw query ranks within the top 20 at batch 4 (`harness check -batch 4`, `evidence/check-b4.json`).
  - This is #874's check over the real summaries, so the committed corpus passes it with no leaks, by construction.
- **Absent items.** It keeps the first 60, in candidate order, for which L's offline recall (`evidence/offline/L/b4-model/probes.jsonl`) delivers F's episode as a walk item. This is the deterministic check that the walk delivers an associated episode for every N question.
- **Too few.** If a family has too few passing candidates, `select` stops, and no live call is made until an amendment says what follows.

## Paid phases

MiniMax-M2 at `https://api.minimax.io/v1/chat/completions` only, through one proxy: `harness proxy` on `127.0.0.1:8096`, with its ledger in `evidence/ledger/`.

- `MINIMAX_KEY` is in the proxy's environment only.
- The list-price ceiling is USD 15.00, a guard only: the key is on a subscription, and nothing is bought.
- Embeddings come from local text-embeddings-inference 1.9.4, serving `BAAI/bge-base-en-v1.5` at the revision #874 used, on `127.0.0.1:8095` (container `tei-885`).

| Phase | Calls | Estimate (input / output tokens) |
|---|---:|---|
| Stream synthesis, batch 4 (`harness synthesize -batch 4`, phase `synth-b4`) | 872 | 0.3M / 0.2M, from #874's 335 / 205 per call |
| L's entity synthesis of the stream (`links build -setting b4-model`, phase `entities-b4`) | 872 | 0.45M / 0.35M, from #874's 466 / 342 |
| Live smoke, L+A: `C2`, `C3`, `N` and `E` probes, excluded from scoring | 4 | 0.05M / 0.003M |
| Live, S, L and L+A: 310 probes each | 930 | 10.5M / 0.7M, from #874's 9,800 to 12,500 input tokens per probe |

- **Pacing.** Two requests at a time.
- **Usage limit.** On HTTP 429 with code 2056 the proxy pauses every request and probes the key at most every 30 minutes (`harness/quota.go`). Callers wait, and a pause uses up no attempt.
- **Entities.** The proxy now refuses to cache a synthesis whose `entities` field is not an array of strings (`harness/proxy.go` `summarized`), so a retry can recover the failure #874 met on batch 509–512.
- **Re-runs.** A synthesis batch that still fails is re-run once in a second pass. A live probe that fails on the provider's limits, a server fault or the network runs once more, and its first record is kept in `results-attempt1.jsonl`. Any other failure counts as not applied.
- **Tokens** are reported per phase from the ledger (`score.py tokens`).

## Endpoints

Scored by `confirm/score.py` as committed here, which calls `../score.py` for hop reach, answer checks and McNemar.

- **Applied, the primary endpoint.** As in #874: the turn's reply, messages sent and files written state the chain's answer. For implicit chains, they state the required reference.
- **Guards.**
  - E: the value.
  - K: the corrected value.
  - C1: the answer.
  - N: an abstention. #874's pattern, extended to count a clarifying question ("could you tell…", "do you mean…"), because #874's `N-14` and `N-17` asked one and were counted as answers.
- **Reported beside them:**
  - for N, whether the output states F's value (the near-miss), and the count by #874's unextended pattern;
  - every hop seen;
  - searches per probe;
  - IC2 and IC3 applied.

## Comparison and decision

- **Test.** L against S and L+A against S, on the 100 primary chains (C2 and C3): the exact two-sided McNemar test on applied, Holm-corrected across the two comparisons, at family-wise α = 0.05.
- **Guardrails.** A guardrail holds when the arm loses no more probes against S than the table allows (the PM's reading of the issue's limits).

  | Family | n | Limit | Allowed loss |
  |---|---:|---|---:|
  | N | 60 | 5 percentage points | 3 probes |
  | K | 40 | 5 percentage points | 2 probes |
  | C1 | 30 | 3 probes | 3 probes |
  | E | 30 | 3 probes | 3 probes |

- **Valid.** An arm is valid when its difference is positive and significant and every guardrail holds. IC2 and IC3 are reported and never gate.
- **Decision.** The smallest valid arm by net production lines is the one to implement: L (+270) before L+A (+292).
- **Null.** If no arm is valid and every control passed, the shipped recall stands, and the report names no arm.
- **Offline stage.** A secondary analysis, and decidable because the chain check passes by selection. It compares L against S on chain reach over the 100 primary chains, by the same test, with guard reach on C1, E and K under the same allowances. L+A delivers L's items with labels, so its reach equals L's. The live verdict decides.

## Power

`python3 score.py power` (`evidence/power.json`), simulating the exact test at the first Holm step (α = 0.025) with 20,000 runs. The rates come from #874's paired live records.

- **The primary set, 100 chains.**
  - At #874's rates for L against S on C2 and C3 (61% of chains applied by L alone, 5% by S alone), the power is 1.00.
  - At a quarter of that gain (19% against 5%) it is 0.70.
  - 80% power needs an arm-only rate of about 21% at #874's S-only rate of 5%.
- **The guardrails.**

  | Family | Chance of failing with no true loss | Chance of failing at #874 L's loss |
  |---|---:|---:|
  | N | 0.18, at #874 M-against-S discordance of 25% | 1.00, at a 20% net loss |
  | K | 0.04, at 5% | 0.71, at a 10% net loss |
  | C1 | 0.003 | 0.02; #874's L gained on C1 |
  | E | 0.02 | 0.05; #874's L gained on E |

  - An arm as harmful on N as #874's L almost surely fails.
  - An arm with no true loss still fails N about one time in five. That is the price of a 3-probe allowance on a family whose probes flip between near-identical arms.

## Controls

A failed control makes the verdict undetermined, and the report names it.

- **Chain check.** No later hop of a selected chain ranks within the raw top 20 at batch 4 (by selection). The oracle ranks each hop's own mid within the top 5 for at least 95% of the selected chains' hops.
- **Entities.** At least 90% of mids hold an entity in L's build (`built.json` `share_with_entities`).
- **Walk delivery.**
  - Offline, F's episode is a walk item for every selected N query (by selection).
  - Live, walk items are in the recall block of at least half the primary probes, for L and for L+A.
- **Label.** In L+A's live recall blocks, every walk line carries the label and no unlabelled walk line appears. That must hold in every block that has walk items, and in at least half the primary probes.
- **Mutation checks.** `confirm/mutation.py` shows the tests fail with each behaviour #885 added removed. `evidence/mutation.txt` records it before collection.

## Collection

- **Order.**
  1. Stream synthesis.
  2. Shared store and check.
  3. L's entity synthesis and build; offline records for S, L and L+A.
  4. `select`, then the check and offline records again on the selected corpus.
  5. Mutation checks.
  6. The smoke run.
  7. Live S, L, L+A.
- **The smoke run.** 4 probes of L+A, one each of C2, C3, N and E, the first of each family in the selected corpus. They run into `evidence/live-smoke/`.
  - They are excluded from scoring.
  - A harness defect they reveal is fixed and committed before collection. None of the endpoints, controls or rules above changes.
- **Stores.** Every probe's store copy is kept until scoring, outside the repository.

## Revision

- Harness, study, arms, corpus generator and scorer at the commit that adds this file.
- SHA-256:
  - `candidates.json`: `6a29e40e767ede66b6954fc4c05ab7314f4ba62f0d97594c0edbce00bbe090bf`
  - `confirm/corpus.py`: `117ad191d1aca91dea87da21904e267c3d94adf2e8252221ee2c463e631d0f70`
  - `confirm/score.py`: `cdd0ae1d86a6ded7c0abb5fbaed674d28733310d44934e1d3b15d51e4fb3f9bb`
  - `../score.py`: `6b0bf50ce2bfd2642237024a373e908f67b5d2e79ad8c1c6e502af0aa6063d96`

## Amendments

- **1.** Committed after selection and the offline records, and before any live call.
  - **Where selection reads its inputs.** Re-running the check on the selected corpus rewrote `evidence/check-b4.json`, which `select` read as written above.
    - The check over the candidates is now kept at `evidence/selection/check-b4.json`.
    - L's offline records over the candidates are kept at `evidence/selection/L-b4-model-probes.jsonl`, re-recorded from the same build.
    - `select` and `corpus_test.py` read them from there, and `evidence/check-b4.json` is the check on the committed corpus.
    - `select` writes the same `corpus.json` from either run (`cmp` shows no difference). `corpus.py` changed in these paths only. Its SHA-256 is now `34df97ae1ef05f7cb0f8a94234c9f021882819b067d57b4fbe8c9f6c809ac83e`. `corpus.json`'s is `d15001efa1feaf95c3f53938a1ab9b55f90f3a84de68d45784917e18bf907190`.
  - **Mutation checks.** Two mutations first passed the tests:
    - picking a walk mid's first entity rather than its highest-scoring one;
    - allowing F's value in the query.

    `memory_test.go` now lists mid 2's farther entity first, and `corpus_test.py` has a control with the value in the query. Every mutation now fails (`evidence/mutation.txt`). No rule, endpoint or control above changes.
  - **Selection outcome.**
    - 18 candidate chains leaked; each family filled from the rest.
    - 89 of the 90 absent candidates have F's episode delivered by L's walk, and the first 60 are kept.
    - On the committed corpus, 0 of 225 later hops rank within the raw top 20, and the oracle reaches 399 of 405 hops (98.5%).
