# Read-time variants of entity-linked recall: preregistration

Fixes issue #888's run before its first model call. The run decides whether a read-time variant of entity-linked recall keeps L's chain gain while holding the shipped recall's (S) abstention and correction behaviour. If none does, it records the tradeoff each variant makes. It uses #885's harness, proxy, corpus generator, scorer and arm code (`../confirm/preregistration.md`), except where this file says otherwise. Everything of this run is in `readtime/`, beside #885's `confirm/`.

## Question

With the model acting, does L+G, L-T or L+V make the agent apply a chain's answer more often than S does, while holding the absent, correction, single-hop and exact guards? L is measured beside them, as #885 measured it.

## Arms

All five arms run at batch 4 over one shared store built from this corpus. Each is a setting of `../arms/links` except S (`../arms/shipped`, setting `b4`).

| Arm | Setting | What changes from L | Net production lines, before tests |
|---|---|---|---:|
| S | `b4` | The shipped recall | 0 |
| L | `b4-model` | Nothing: #885's L | about +270 |
| L+G | `b4-model-g` | A gate on walk-delivered mids | about +284 |
| L-T | `b4-model-t` | The walk only as a call the model makes | about +265 |
| L+V | `b4-model-v` | One instruction: inspect before stating a linked fact | about +272 |

- **L** is `../arms/links/README.md` §Production estimate's +270.
- **L+G.** Recall and `memory_search` fill their slots from the similar list and the walk's list in turn, as L's do. A walk mid enters the walk's list only if it names one of the query's own entities (a walk seed), or its walk score reaches the threshold (`memory.go` `gate`, `gateScore`).
  - **Threshold: infinity.** So the gate passes only walk mids that name one of the query's entities.
  - It was fixed by `score.py calibrate` from #885's records, and from no record of this run:
    - `../confirm/corpus.json`;
    - `evidence/calibration/probes.jsonl`. That is `links offline -setting b4-model-g` over #885's stores, rebuilt from `../confirm/evidence/synthesis/` and `../confirm/arms/links/evidence/entities-b4.jsonl`, recorded into the same scratch study directory as the equivalence check and copied here.
    - Output: `evidence/calibration/gate.json`.
  - **The rule.** Among thresholds equal to an observed score, or infinity, take the one that keeps the most later hops of #885's primary chains. It may keep the associated episode of at most 3 of #885's 60 absent queries, the N guardrail's allowance. Ties go to fewer associated episodes, then to the smaller threshold.
  - **What the records show.** The two score ranges overlap:
    - later hops of primary chains score 0.0016 to 0.0238, with a median of 0.0222;
    - absent queries' associated episodes score 0.0148 to 0.0244, and 50 of 60 score at least 0.0223.
    - An associated episode sits at the same walk distance from the query's entity as a chain's second hop. No finite threshold keeps a later hop within the allowance.
  - **Lines.** L's +270, plus about 14: the score each reached mid carries, the gate and its threshold.
- **L-T.** Pre-turn recall and `memory_search` are S's. The walk answers only `memory_associate`:
  - The call takes a query, and returns the walk's mids from the entities that query names, at most 5 per call (`study/workspace.go` `AssociateDescriptor`, `AssociateLimit`).
  - Its result has `memory_search`'s shape. The session's budget of 6 calls per tool per turn bounds it as it bounds `memory_search`.
  - The study serves the call beside the file tools, because it cannot add a reserved call to the session. Production would reserve it as it reserves `memory_search`.
  - One instruction (`../arms/links/main.go` `OnCall`): "When the task depends on someone or something a memory names and the answer is not in view, call memory_associate with that name to find the episodes linked to it."
  - **Lines.** L's +270, less the 43 lines that fill recall's slots from both lists, plus about 5 lines for a walk-only list, about 31 for the reserved call, and 1 for the instruction.
- **L+V.** L's recall and search, with walk mids delivered under L's age label `linked`. One instruction (`main.go` `Verify`): "A memory marked linked was reached through a name it shares with the task, not by matching the question. Before you state a fact taken from a linked memory, call memory_inspect on the source that states it in this turn, and state only what that source says."
  - **Scoring check.** The check is enforced in scoring (§Endpoints). No runtime check is added: the reply leaves the session through the reserved writeback, which the study cannot intercept without changing production code.
  - **Lines.** L's +270, plus the sentence in the platform contract.
- **Common to every arm.** As `../confirm/preregistration.md` §Arms fixes it:
  - a live window of 32 messages;
  - each probe one turn on its own copy of the arm's build;
  - MiniMax-M2 through the study proxy.
- **Existing settings.** L's settings `b4`, `b8`, `b4-model` and `b4-model-a` behave exactly as before.
  - S, L and L+A were re-built and re-recorded offline over #885's rebuilt stores with this code, into a scratch study directory outside the repository. Nothing was written under `../confirm/`.
  - The records equal the committed `../confirm/evidence/offline/` in every field but timing (`equivalence.py`, `evidence/equivalence.txt`).

## Corpus

`corpus.py` runs `../confirm/corpus.py` with seed 888, writing beside itself. Every family, size, rule, the scheduler and the selection are #885's:
- primary: C2 50 and C3 50;
- guards: C1 30, E 30, K 40 and N 60;
- reported only: IC2 25 and IC3 25.

- **Candidates** (`candidates.json`): 30 C1, 100 C2, 100 C3, 30 IC2 and 30 IC3 chains, 90 absent items, and the replication's exact and correction guards.
  - All of them are in one 3,488-message stream after the base.
  - `corpus_test.py` runs `../confirm/corpus_test.py` over them.
- **Absent items (N)** are built as in #885, so that L's walk from the query's entity reaches the episode stating the associated entity's value.
- **Selection, before any live call**, exactly as `../confirm/preregistration.md` §Selection with its Amendment 1:
  - Chains: the first of each family with no later hop in the raw top 20 at batch 4, by `evidence/selection/check-b4.json`.
  - Absent items: the first 60 whose associated episode L's offline recall delivers as a walk item, by `evidence/selection/L-b4-model-probes.jsonl`.
  - If a family has too few, `select` stops and no live call is made until an amendment says what follows.
- **Base entities.** `arms/links/evidence/entities-b4.jsonl` starts as the 1,200 base rows of `../confirm/arms/links/evidence/entities-b4.jsonl`. They equal #874's base rows as a set. The stream's 872 batches get new entity calls.

## Paid phases

MiniMax-M2 at `https://api.minimax.io/v1/chat/completions` only, through one proxy: `harness proxy` on `127.0.0.1:8098`, with its ledger in `evidence/ledger/`.

- `MINIMAX_KEY` is in the proxy's environment only.
- The list-price ceiling is USD 20.00, a guard only: the key is on a subscription, and nothing is bought.
- No other paid API is called.
- Embeddings come from local text-embeddings-inference 1.9.4, serving `BAAI/bge-base-en-v1.5` at #874's revision, on `127.0.0.1:8097` (container `tei-888`).

| Phase | Calls | Estimate (input / output tokens) |
|---|---:|---|
| Stream synthesis, batch 4 (`harness synthesize -batch 4`, phase `synth-b4`) | 872 | 0.29M / 0.18M, from #885's 328 / 201 per call |
| L's entity synthesis of the stream (`links build -setting b4-model`, phase `entities-b4`) | 872 | 0.34M / 0.21M, from #885's 389 / 234 |
| Live smoke: one C2 and one N probe each of L+G, L-T and L+V, excluded from scoring | 6 | 0.06M / 0.004M |
| Live, S, L, L+G, L-T and L+V: 310 probes each | 1,550 | 16M / 1.1M, from #885's 10,000 to 11,000 input tokens per probe |

- **Pacing.** Two requests at a time in every phase, and the five live arms run one after another, never together. Another study may share the key.
- **Usage limit.** On HTTP 429 with code 2056 the proxy pauses every request and probes the key at most every 30 minutes (`../harness/quota.go`). Callers wait, and a pause uses up no attempt.
- **Re-runs.**
  - A synthesis batch that still fails is re-run once in a second pass.
  - A live probe that fails on the provider's limits, a server fault or the network runs once more. Its first record is kept in `results-attempt1.jsonl`.
  - Any other failure counts as not applied.
- **Tokens** are reported per phase from the ledger (`score.py tokens`).
  - The ledger routes every L setting under arm L. Their live calls are told apart by the start and end time of each setting's run, recorded in `evidence/live/run.json`.

## Endpoints

Scored by `score.py` as committed here. It calls `../confirm/score.py` and `../score.py` for hop reach, answer checks, abstention and McNemar.

- **Applied, the primary endpoint.** As in #885: the turn's reply, messages sent and files written state the chain's answer.
- **L+V's check, part of its endpoints.** For every L+V probe of a chain family or N:
  - **The fact.** It is the chain's answer, or N's associated value.
  - **Stated from a walk mid.** The probe states the fact, and the source that states it lies in a `linked` mid of the recall block and in no other mid of it.
  - **Inspected first.** A successful `memory_inspect` of a source in that mid's interval comes before the first message or file that states the fact. The reply comes last, so any such inspect in the turn precedes a fact stated only in the reply.
  - **Scoring.** A fact stated from a walk mid without the inspect counts as not applied, or for N as not abstained. L+V's unenforced counts are reported beside the enforced ones.
- **Guards**, as #885:
  - E: the value;
  - K: the corrected value;
  - C1: the answer;
  - N: an abstention by #885's pattern, which counts a clarifying question.
- **Reported beside them:**
  - for N, whether the output states the associated value without abstaining;
  - every hop seen, with L-T's `memory_associate` results counted as seen, as `memory_search` results are;
  - searches and `memory_associate` calls per probe;
  - IC2 and IC3 applied.

## Comparison and decision

- **Test.** Each of L, L+G, L-T and L+V against S, on the 100 primary chains: the exact two-sided McNemar test on applied, Holm-corrected across the four comparisons, at family-wise α = 0.05.
- **Guardrails.** #885's: an arm may lose against S at most 3 probes on N (5 percentage points of 60), 2 on K (5 points of 40), and 3 each on C1 and E.
- **Valid.** An arm is valid when its difference is positive and significant and every guardrail holds. IC2 and IC3 never gate.
- **Decision.** The smallest valid arm by net production lines is the one to implement, in the order L-T (+265), L (+270), L+V (+272), L+G (+284).
- **Best read-time variant.**
  - If an arm is valid, it is the decision above.
  - If none is, the report still names the best of L+G, L-T and L+V, by the first of these that separates them:
    1. the fewest guardrails failed;
    2. the fewest probes lost beyond the allowances, summed over the four families;
    3. the largest net gain on the primary set (arm only minus S only);
    4. the fewest net lines.
  - Every arm's tradeoff is tabled: primary applied, the four guard changes, the associated value stated on N, and net lines.
- **Null.** If no arm is valid and every control passed, the shipped recall stands, and no arm is named for implementation.
- **Offline stage.** Secondary, and decidable because the chain check passes by selection. It uses the same test on chain reach over the primary chains, with guard reach on C1, E and K. L+V delivers L's items, and L-T's recall is S's.

## Power

`python3 score.py power` (`evidence/power.json`) simulates the exact test at the first of four Holm steps (α = 0.0125), with 20,000 runs. The rates come from #885's paired live records, where L applied 48 primary chains S did not, and S 1 that L did not.

| Arm-only rate, at an S-only rate of 1% | Power |
|---|---:|
| 48%, #885's L | 1.00 |
| 24.5%, half that gain | 1.00 |
| 12.75%, a quarter of that gain | 0.77 |

The guardrails' chance of failing with no true loss is #885's (`../confirm/evidence/power.json`), since the family sizes are the same: N 0.18, K 0.04, C1 0.003, E 0.02.

## Controls

A failed control makes that arm's verdict undetermined, and the report names it.

- **Chain check.** No later hop of a selected chain ranks within the raw top 20 at batch 4 (by selection). The oracle ranks each hop's own mid within the top 5 for at least 95% of the selected chains' hops.
- **Entities.** At least 90% of mids hold an entity in L's build (`built.json` `share_with_entities`).
- **Walk delivery.**
  - Offline, the associated episode is a walk item of L's recall for every selected N query (by selection).
  - Live, walk items are in the recall block of at least half the primary probes, for L and L+V. L+G's gate removes walk items by design, so its gate control stands in for this one.
- **L+G's gate.** Offline, over the selected N queries, the gate passes at least one walk item and drops at least one (`score.py offline`, from the `gate` trace of `evidence/offline/L/b4-model-g/probes.jsonl`). Live recall runs the same code over the same store. On #885's records it passed 37 and dropped 61 (`evidence/calibration/probes.jsonl`).
- **L-T's call.** `memory_associate` is called in at least 20 of the 100 primary probes.
- **L+V's check.** The check meets at least one fact stated from a walk mid. Each such fact is scored for an inspect before it (§Endpoints), and the counts are reported.
- **Mutation checks.** `mutation.py` shows the tests fail with each behaviour #888 added removed: L+G's gate and its trace, L-T's shipped recall and its call, L+V's and L-T's instructions, the live session's offer of `memory_associate`, and the scorer's threshold rule, inspect check, call control and ranking. `evidence/mutation.txt` records it before collection.
- **Existing settings.** §Arms.

## Collection

1. The mutation and equivalence checks, and this file, committed.
2. Stream synthesis.
3. Shared store and check; the check over the candidates copied to `evidence/selection/check-b4.json`.
4. L's entity synthesis and build. L's offline records over the candidates are saved to `evidence/selection/L-b4-model-probes.jsonl`.
5. `select`, then the check and every arm's offline records on the selected corpus.
6. The smoke run: the first C2 and the first N probe of the selected corpus, for L+G, L-T and L+V, into `evidence/live-smoke/`.
   - They are excluded from scoring.
   - A harness defect they reveal is fixed and committed before collection. None of the endpoints, controls or rules above changes.
7. Live, one arm after another: S, L, L+G, L-T, L+V.

Every probe's store copy is kept until scoring, outside the repository.

## Revision

- Harness, study, arms, corpus generator and scorer at the commit that adds this file.
- SHA-256:
  - `candidates.json`: `c8c490f7da40240b7ca89fdb7e4f9cc39f5e7d9413e2381df06951f62a95c660`
  - `corpus.py`: `25b9c3e5188a35c71a0fefcba78fbeb9d498644e342dd459cd2a6bb98b852d02`
  - `score.py`: `bf155f5f2c21f13d9e5611185b6770143c44c0d49f0b33434b405d5ff601ad7d`
  - `../confirm/corpus.py`: `34df97ae1ef05f7cb0f8a94234c9f021882819b067d57b4fbe8c9f6c809ac83e`
  - `../confirm/score.py`: `5a99b44b2fbd846c463cde9a8dcfaa4a7dd91dc2c147fee73322df6ed31ef3e0`
  - `../score.py`: `6b0bf50ce2bfd2642237024a373e908f67b5d2e79ad8c1c6e502af0aa6063d96`

## Amendments

- **1.** Committed after S, L, L+G and L-T were collected and L+V's first pass ended, and before any L+V probe is re-run.
  - **The loss.** While L+V's live run was writing, I moved `evidence/live/L/b4-model-v/` to `/tmp`, another filesystem, to score the four finished arms without it, and moved it back. The runner kept appending to the deleted original.
    - Only the first 119 of L+V's 310 results survive in `results.jsonl`.
    - The ledger shows all 310 probes sent: 860 calls over 310 episodes.
  - **Never read.** No outcome of the 191 lost probes was read by anyone, and the lost records cannot be recovered.
  - **The re-run.** Exactly the 191 lost probes are run once more, through `live -only`, and receive fresh model responses. None of the 119 survivors is re-run. The lost probes are:
    - C3: `C3-050`, `C3-051`, `C3-052`, `C3-053`, `C3-054`, `C3-055`, `C3-056`, `C3-057`, `C3-058`, `C3-059`, `C3-060`;
    - every IC2, IC3, E, K and N probe.
  - **Tokens.** Both passes count under L+V live. The re-run gets a second window in `evidence/live/run.json`.
  - **Collection.** The runner's output directory is not touched while it writes; scoring reads a copy afterwards.
  - Nothing else in this file changes.
