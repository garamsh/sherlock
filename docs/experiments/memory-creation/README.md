# Memory-creation experiment

One question, answered with numbers: **how should a memory entry come to exist — who decides, with what vocabulary, and when does maintenance happen?** This is the third experiment in the series. The cycle experiment (`memory-cycle/`) established that the write path must be shape-enforced; this experiment compares the structures that can sit behind that enforcement.

Four rounds have run. The fourth was built to falsify the third, and did.

## Contents

- The question
- Round 1: the candidate structures
- Round 2: real infrastructure
- Round 3: novel structures and decider models
- Round 4: falsification — what the earlier rounds could not see
- Can a similarity threshold decide supersession?
- The structures compared under the corrected measurement
- Where the remaining correction gap lives
- Adopted direction
- Limits
- Reproducing

## The question

The adopted memory model (ADR 0006) writes through an ops vocabulary (`keep`/`update`/`delete`/`add` over recalled IDs) emitted by the main model inside its response. The cycle experiment showed the voluntary form scores 0.00 — but even with the shape enforced (structured output), the ops vocabulary is one choice among many. The owner asked for the alternatives to be measured: everything-as-update, a dedicated memory model, a journal with background distillation, freeform notes, entity records, temporal claims, and append-only with read-time latest-wins.

## Round 1: the candidate structures

Each candidate is a small simulated write path over the same scripted 20-turn conversation used in the cycle experiment. The store's supersession path (cosine ≥ 0.6 replaces) is shared. Compliance inputs are the cycle experiment's measured or marked rates: voluntary 0% (measured), structured main-response field 80%, dedicated narrow call 95%, deterministic platform paths 100%.

- **C1 Ops vocabulary** (production) — the main model writes ops in a fenced block. Voluntary.
- **C2 Everything-is-update** — the store is a set of topic documents; every write updates a topic.
- **C3 Dedicated memory model** — a separate narrow call after each turn decides what to write. Store-level supersession is a similarity threshold.
- **C4 Journal + distillation** — the platform appends every turn; a background call distills periodically.
- **C5 Freeform notes** — unstructured "worth remembering" text, split and superseded by similarity.
- **C6 Entity records** — the store is keyed by entity; writes upsert attributes.
- **C7 Claims with temporal ordering** — timestamped claims; newest wins at read time.
- **C8 Append-only** — never update or delete; read-time latest-wins across similarity clusters.
- **C9 Decider-owned corrections** — C3 without the store threshold: the decider retires the contradicted entry itself.

| system | checkpoint recall | latest-wins | items | calls/turn | LoC |
|---|---|---|---|---|---|
| C1 ops-voluntary (production) | **0.00** | 0.00 | 0 | 0.00 | 16 |
| C2 everything-update | 0.96 | 0.00 | 17 | 0.00 | 28 |
| C3 dedicated model | 1.00 | 0.00 | 17 | 1.00 | 23 |
| C4 journal+distill | 0.29 | 0.00 | 24 | 0.20 | 31 |
| C5 freeform notes | 0.96 | 0.00 | 15 | 0.00 | 19 |
| C6 entity records | 0.96 | 0.00 | 18 | 0.00 | 31 |
| C7 claims-temporal | 0.96 | 0.00 | 18 | 0.00 | 27 |
| C8 append-only | 0.96 | 0.00 | 18 | 0.00 | 26 |
| C9 decider-owned corrections | **1.00** | **1.00** | 18 | 1.00 | 38 |

What survives round 4: the voluntary write path scores 0.00, and journal distillation loses the timing (0.29). What round 4 overturned: C9's 1.00 latest-wins, which the harness produced with a subject-token stand-in for semantic judgment over an 18-item store.

## Round 2: real infrastructure

Real OpenAI embeddings (text-embedding-3-small) for the store's similarity and a real gpt-4o-mini call as the dedicated decider, over 100 sessions × 10 turns.

| system | recall | latest-wins (net) | stale-present | items | decider calls |
|---|---|---|---|---|---|
| C1 ops-voluntary | 0.00 | 0.00 | 0.00 | 0 | 0 |
| C2 everything-update (store threshold) | 0.80 | 0.00 | 0.80 | 19 | 0 |
| C3/C9 dedicated decider | 1.00 | 0.60 | 0.40 | 14 | 1,000 |

The store's threshold never fired for a natural correction. That finding stands and round 4 strengthens it. The decider's 0.60 does not stand: see round 4.

## Round 3: novel structures and decider models

Five structures that relocate where the intelligence lives, plus a decider-model comparison, on the same 1,000-turn dataset.

| system | recall | latest-wins (net) | stale-present | items | decider calls |
|---|---|---|---|---|---|
| S1 sleep-consolidation | 0.90 | 0.00 | 0.90 | 375 | 200 |
| S2 claim-graph (typed `corrects` links) | 0.90 | 0.60 | 0.30 | 25 | 1,000 |
| S3 attention-sink (top-K anchors in context) | 0.70 | -0.30 | 1.00 | 40 | 0 |
| S4 confidence-decay (no delete; decay alone) | 0.00 | 0.00 | 0.00 | 0 | 0 |
| S5 materialized-view (log is truth) | 0.10 | 0.10 | 0.00 | 5 | 100 |
| decider = gpt-4o-mini | 1.00 | 0.60 | 0.40 | 14 | 1,000 |
| decider = gpt-4o | 0.40 | 0.20 | 0.20 | 6 | 1,000 |

Pure decay without reinforcement loses everything (S4), anchors pin stale facts (S3), and a rebuilt view forgets past the window (S5). These stand. The decider-model comparison does not carry: it rests on ten probes over a fourteen-item store, and round 4 shows that store size is the variable those runs were actually measuring.

## Round 4: falsification — what the earlier rounds could not see

Rounds 1–3 shared one dataset design, and it had three properties that made the numbers unable to detect a failure:

1. **Every planted topic was corrected.** There was no fact that had to survive, so a structure that retires aggressively scored perfectly. Over-retirement was invisible by construction.
2. **Ten probes backed every number.** Every metric moved in steps of 0.10; the gpt-4o-vs-mini difference reported in round 3 is four probes.
3. **The store held about fifteen items.** A decider shown "the ten most recent memories" saw the contradicted fact by accident. That is the mechanism round 2 and round 3 measured.

The round-4 dataset fixes all three, and the scoring separates outcomes the earlier rounds merged.

- 16 subjects × 20 topic categories, so values repeat across subjects and a memory only answers a probe when it carries **both** the subject and the value. (Scoring on the value alone lets "Yuki uses Neovim" answer a probe about Lisa; this round's first pass had that defect and its numbers were discarded.)
- 488 turns, **152 probes** in three families: 100 corrected, 40 control (never corrected — must survive), 12 coexist (two simultaneously-true values — both must survive).
- Every initial statement and distractor lands first, then chatter, then every correction. A correction therefore arrives with hundreds of items already stored: no recency window can contain the fact it contradicts.
- Retention, decay and salience tiering are **off**, and ranking is pure relevance. The store experiment already answered those, and leaving them on would let decay hide a memory the structure never removed. The only thing that can remove a memory here is the structure's own logic.
- Every miss is classified against the row log, which keeps removed rows: `never_written` (the decider never stored it), `retired` (stored, then removed), `not_ranked` (live but outside the top-5).

## Can a similarity threshold decide supersession?

ADR 0006 supersedes on embedding cosine ≥ 0.7; the code has used 0.6 since issue #324. The rule is sound only if "this replaces that" pairs sit above the cut and everything else sits below it. 250 pairs per family, real embeddings:

| pair family | must supersede? | min | p25 | median | p75 | max |
|---|---|---|---|---|---|---|
| correction | yes | 0.319 | 0.569 | 0.630 | 0.689 | 0.826 |
| coexist | no | 0.704 | 0.751 | 0.789 | 0.811 | 0.902 |
| cross-subject | no | 0.350 | 0.549 | 0.642 | 0.715 | 0.814 |
| unrelated | no | 0.034 | 0.157 | 0.212 | 0.254 | 0.424 |

The families are ordered wrongly for the mechanism. Two simultaneously-true facts about one person score **higher** than a real correction: every coexist pair in the sample exceeds 0.70, and their median (0.789) sits above the correction p75 (0.689). The same topic about a different person is indistinguishable from a correction (median 0.642 against 0.630). Only the unrelated family separates cleanly, and that is the case no mechanism needed help with.

Sweeping every threshold from 0.00 to 1.00 over 250 pairs per family (250 must supersede, 750 must survive):

| threshold | corrections caught | pairs wrongly superseded | correct deletions | wrong deletions |
|---|---|---|---|---|
| 0.60 (production, `internal/memory/ranking.go`) | 0.648 | **0.532** | 162 | **399** |
| 0.70 (ADR 0006, `docs/architecture/memory.md`) | 0.208 | 0.437 | 52 | 328 |
| 0.91 (highest accuracy in the sweep) | 0.000 | 0.000 | 0 | 0 |

At the production value the mechanism deletes **2.5 rows that had to survive for every row it correctly retires**. The highest-accuracy threshold in the sweep is the one that supersedes nothing at all — its 0.75 accuracy is exactly the share of pairs that must survive. **No threshold setting is better than not having the mechanism.** This is not a tuning problem: similarity does not carry the distinction the mechanism is asked to make.

## The structures compared under the corrected measurement

Columns are seed 7; `net (s11)` is the same measure on an independent dataset draw.

| structure | what the decider sees / how writes land | latest-wins | control survival | coexist preserved | net | net (s11) | control retired | coexist retired | items | extra calls/turn |
|---|---|---|---|---|---|---|---|---|---|---|
| **B9 decider-neighbours, wide** | as B3, offering 10 candidates down to cosine 0.3 | **0.76** | **0.98** | 0.85 | **0.86** | — | **0.00** | 0.14 | 298 | 1.0 |
| **B3 decider-neighbours** | the store's retrieval for the incoming message | 0.71 | 0.97 | 0.85 | 0.84 | **0.85** | **0.00** | 0.14 | 300 | 1.0 |
| B2 decider-window | the 10 most recently written memories | 0.01 | 0.95 | **0.97** | 0.64 | 0.67 | 0.00 | 0.04 | 379 | 1.0 |
| B4 key-upsert | decider names a topic key; same key replaces | 0.24 | 0.93 | 0.53 | 0.56 | 0.62 | 0.05 | 0.50 | 354 | 1.0 |
| B5 key-upsert + key hints | as B4, with related existing keys offered back | 0.75 | 0.45 | 0.33 | 0.51 | 0.54 | 0.55 | 0.68 | 178 | 1.0 |
| B6 key-append, read-time latest | as B5, append-only; newest row per key at read | 0.66 | 0.35 | 0.31 | 0.44 | 0.34 | 0.00 | 0.00 | 184 | 1.0 |
| B7 key-upsert from the main response | as B5, written as a field on the main reply | 0.49 | 0.38 | 0.33 | 0.40 | 0.43 | 0.40 | 0.57 | 151 | 0.0 |
| B1 store similarity threshold | main-model field; store supersedes at cosine ≥ 0.6 | 0.49 | 0.28 | 0.28 | 0.35 | 0.40 | 0.50 | 0.61 | 138 | 0.0 |

`net` is the mean of the three probe families: the share of the store's job done right. B1 and B7 write from the main response at the cycle experiment's 80% compliance rate, which caps their survival columns; the round's own diagnosis shows their losses are retirement, not absent writes.

The two seeds agree on the ranking (only B1 and B6 swap at the bottom), on B2's correction collapse (0.01 and 0.05), and on B1 destroying coexisting facts (0.61 in both).

A claim-graph structure (round 3's S2 — claims with a typed `corrects` link, rebuilt at this scale) is **not ranked here**. Its decider stored 85 of roughly 290 facts, and 80% of the control probes it missed were never written at all, so the structure was never exercised. That is a property of the prompt it was given, not a measured verdict on claim graphs; it is recorded so the omission is not read as a result.

What this establishes:

1. **The previous rounds' winner does not survive scale.** B2 is the structure round 2 and round 3 called the answer — a dedicated decider shown its recent memories. At fifteen items it scored 0.60 on corrections; at store scale it scores **0.01**, leaving the stale value recallable after 98% of corrections. Its earlier score measured the store being small, not the structure working.
2. **What the decider is shown is the structure.** B3 differs from B2 in one respect — it offers the store's retrieval for the incoming message instead of the last ten writes — and correction handling moves from 0.01 to 0.71 with no new vocabulary, no threshold, and no extra call. The store already knows how to find related memories; the earlier design simply never asked it.
3. **Today's production mechanism is the worst structure measured.** B1 retires **half** of the facts that were never corrected and 61% of the facts that must coexist, for a net of 0.35 — below every alternative. The separability numbers predict exactly this, and the behavioural run confirms it.
4. **Key-addressing relocates the failure; it does not remove it.** Making the topic key the unit of replacement should make latest-wins structural — a correction is just another write to the same key, with no threshold and no retire vocabulary. It does, and the cost lands elsewhere: without key hints the decider coins fresh keys and corrections stop landing (B4, 0.24); with key hints the same-key collapse also merges facts that are not the same fact, retiring 55% of control facts and 68% of coexisting ones (B5). Key collision is the similarity threshold under a different name.
5. **Never deleting does not save it.** B6 removes nothing at all — its retirement columns are 0.00 — and still loses 65% of control facts, because read-time latest-per-key hides the colliding rows instead of deleting them. Append-only changes what the log holds, not what the reader sees.

## Where the remaining correction gap lives

B3's residual splits cleanly. Replaying the 100 correction turns and recording whether the contradicted row was among the candidates the store offered:

| outcome at the correction turn | count |
|---|---|
| the contradicted row was offered, and the decider retired it | 73 |
| the contradicted row was offered, and the decider kept it | 9 |
| the contradicted row was not offered | 11 |
| no live row held the old value | 7 |

The contradicted row reached the decider 82% of the time, and **when it did, the decider retired it in 89% of cases**. The residual is 11 retrieval misses against 9 judgment misses — so the remaining work is in what gets offered, not in the decider's semantic ability. Round 3's conclusion that the gap is a judgment-policy ceiling does not hold: it was a retrieval gap all along, hidden by a recency window.

Widening the offer confirms it. B9 is B3 with ten candidates down to cosine 0.3 instead of five down to 0.4, and nothing else changed: latest-wins rises from 0.71 to 0.76 and net from 0.84 to 0.86, while control survival stays at 0.98 and **retirement of control facts stays at 0.00**. This is the asymmetry that matters for the design — the candidate set is a knob that trades prompt tokens for correction coverage and cannot destroy data at any setting, whereas the threshold it replaces destroyed data at every setting that caught anything.

## Adopted direction

The evidence supports one structural change and one removal:

- **The decider is shown the store's retrieval for the incoming turn.** This is B3, and B9 is the same structure with a wider offer (ten candidates down to cosine 0.3, the better setting measured). It replaces the recency window, needs no new vocabulary, and costs the same one narrow call. The candidate set is a retrieval parameter: widening it spends prompt tokens and cannot destroy data, unlike the threshold it replaces.
- **The store's similarity-threshold supersession is removed, not retuned.** The sweep shows no threshold beats not superseding at all, and the behavioural run shows the current value destroying half of what must survive. The platform retains, ranks and forgets; it does not decide what contradicts what.
- Key-addressed replacement, claim links, append-only-with-read-time-resolution, and the main-model-written field are **rejected** on this evidence.
- The write-path enforcement (structured output, from the cycle experiment) is unchanged and remains the precondition.

## Limits

- The turn stream is scripted, not a live agent. It measures the memory structure, not the agent's behaviour; only a behavioural QA pass measures that.
- One decider model (gpt-4o-mini, temperature 0). The gap diagnosis shows only 9 judgment misses in 100 corrections, so a stronger decider has little headroom here — but that is an inference about this dataset, not a measurement of other models.
- Embeddings are truncated to 512 dimensions to keep a few-hundred-item store scoreable in pure Python. Every structure uses the same embedder.
- B1 and B7 carry the cycle experiment's assumed 80% main-response compliance; it is an assumption, marked as one since that experiment.
- The wide-offer variant (B9) and the claim graph ran on seed 7 only.
- Coexist probes are 12 of 152. They are the smallest family and the coexist column is correspondingly the noisiest.

## Reproducing

```
cd docs/experiments/memory-creation
python3 harness/creation_runner.py             # round 1
python3 harness/real_runner.py                 # round 2
python3 harness/real_new_runner.py             # round 3
python3 harness/round4/separability.py         # round 4: threshold separability
python3 harness/round4/runner.py               # round 4: structure comparison
SEED=11 python3 harness/round4/runner.py       # round 4: replication
python3 harness/round4/gap_diagnosis.py        # round 4: where B3's residual lives
python3 harness/round4/claimgraph_runner.py    # round 4: claim graph, run alone
python3 harness/round4/single_runner.py DeciderNeighborsWide   # round 4: wide offer
```

Rounds 2–4 need `OPENAI_API_KEY`. Rounds 1–3 write into `results/` and the files beside this report; round 4 writes into `results_round4/`. The round-4 runners cache every embedding and decider reply next to the harness, so a re-run costs nothing after the first.
