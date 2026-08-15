# Memory-creation experiment

Which structure should create, correct and retire a memory entry. The cycle experiment (`memory-cycle/`) established that the write path must be shape-enforced; this one compares the structures that sit behind that enforcement.

## Method

488 turns, 152 probes, real embeddings and a real gpt-4o-mini decider, replicated on two dataset seeds. Three probe families, because a structure can be wrong in three ways:

| family | probes | scores |
|---|---|---|
| corrected | 100 | the new value is recalled and the old one is gone |
| control | 40 | a never-corrected fact is still recalled |
| coexist | 12 | two simultaneously-true values are both recalled |

`net` is the mean of the three. Values repeat across the 16 subjects, so a memory answers a probe only when it carries both the subject and the value. Retention, decay and salience tiering are off and ranking is pure relevance, so the only thing that can remove a memory is the structure's own logic. Every miss is classified against the row log as never-written, retired, or not-ranked.

Rounds 1–3 of this experiment used a dataset in which every planted topic was corrected, ten probes backed each number, and the store held about fifteen items. That dataset cannot detect over-retirement and cannot distinguish a structure working from a store being small; its numbers are superseded by the table below and its harness is kept only for the structures round 4 does not cover.

## No similarity threshold can decide supersession

ADR 0006 supersedes on embedding cosine; `internal/memory/ranking.go` uses 0.6. 250 pairs per family:

| pair family | must supersede? | median cosine |
|---|---|---|
| correction | yes | 0.630 |
| coexist | no | 0.789 |
| cross-subject | no | 0.642 |
| unrelated | no | 0.212 |

Facts that must both survive score higher than real corrections; every coexist pair exceeds 0.70. Sweeping every threshold:

| threshold | correct retirements | wrong retirements |
|---|---|---|
| 0.60 (shipped) | 162 | 399 |
| 0.70 (ADR 0006) | 52 | 328 |
| 0.91 (highest accuracy) | 0 | 0 |

The highest-accuracy setting is the one that supersedes nothing. Tracked as issue #334.

## The structures

| structure | what the decider is shown, and what it returns | latest-wins | control | coexist | net | net (s11) | control retired | items |
|---|---|---|---|---|---|---|---|---|
| B14 key-upsert, plural-aware | related keys with values; a key and value, narrower key when both hold | 0.75 | 0.98 | 1.00 | 0.91 | pending | 0.00 | 312 |
| B9 decider-neighbours, wide | 10 retrieved candidates; facts to store and ids to retire | 0.76 | 0.98 | 0.85 | 0.86 | — | 0.00 | 298 |
| B3 decider-neighbours | 5 retrieved candidates; facts to store and ids to retire | 0.71 | 0.97 | 0.85 | 0.84 | 0.85 | 0.00 | 300 |
| B10 neighbourhood rewrite | 10 retrieved candidates; the new state of that set | 0.71 | 0.93 | 0.71 | 0.78 | — | 0.05 | 294 |
| B12 key-upsert, key and value hints | related keys with their values; a key and value | 0.73 | 0.98 | 0.61 | 0.77 | 0.72 | 0.00 | 299 |
| B2 decider-window | the 10 most recent memories; same output | 0.01 | 0.95 | 0.97 | 0.64 | 0.67 | 0.00 | 379 |
| B11 rewrite, scope stated | as B10, told the set spans subjects | 0.70 | 0.80 | 0.40 | 0.63 | — | 0.18 | 246 |
| B4 key-upsert | nothing; a topic key and value, same key replaces | 0.24 | 0.93 | 0.53 | 0.56 | 0.62 | 0.05 | 354 |
| B5 key-upsert, key hints | related key names only; a key and value | 0.75 | 0.45 | 0.33 | 0.51 | 0.54 | 0.55 | 178 |
| B6 key-append | as B5, append-only, newest row per key at read | 0.66 | 0.35 | 0.31 | 0.44 | 0.34 | 0.00 | 184 |
| B7 key field on the main reply | as B5, written by the main model | 0.49 | 0.38 | 0.33 | 0.40 | 0.43 | 0.40 | 151 |
| B1 store threshold | nothing; the store supersedes at cosine ≥ 0.6 | 0.49 | 0.28 | 0.28 | 0.35 | 0.40 | 0.50 | 138 |

B1 and B7 write from the main response at the cycle experiment's assumed 80% compliance. The two seeds agree on the ranking except B1 and B6, which swap at the bottom.

- **What the decider is shown is the structure.** B2 and B3 differ only in that. Showing the store's retrieval instead of the recent writes moves corrections from 0.01 to 0.71; widening the offer reaches 0.76. B2's 0.60 in the earlier rounds measured a fifteen-item store, not the structure.
- **The candidate set is safe to widen; a threshold is not.** Retirement of control facts stays at 0.00 across B3 and B9. Offering more spends prompt tokens; no setting deletes data.
- **The shipped mechanism is last.** It retires half of the never-corrected facts and 61% of the coexisting ones.
- **A topic key works, once the decider can see what a key holds.** The first two key structures were wrong about the hint, not about the idea. B4 offers no hints, so the decider coins fresh keys and corrections never land (0.24). B5 offers key *names*, so it reuses a key without knowing whether it holds the same fact, and destroys 55% of the control facts. Offering each key with its current value (B12) drops control retirement to 0.00 and lifts net from 0.51 to 0.77. The residual was five keys holding several simultaneously-true facts — `otto.language` taking Korean, then English, then German, each write replacing the last — and telling the decider to coin a narrower key in that case (B14) reaches coexist 1.00 and net 0.91, the best measured, destroying nothing.
- **Append-only does not help.** B6 deletes nothing and still loses 65% of control facts, because read-time latest-per-key hides the colliding rows.
- **The protocol's silent case decides its safety.** B10 removes the vocabulary entirely: the decider returns what the shown set should now be, so deletion is omission. Classifying all 106 of its removals shows what that costs — 4% are paraphrase churn, the rest are collateral, a fact about another subject or another true value dropped from the reply. Stating the scope explicitly (B11) makes it worse, not better: 0.63. B14 keeps the same one-field output and does not have the problem, because a key that is not written is not touched. Whether the vocabulary is small matters less than whether silence means "no change"; a threshold firing on similarity, a key collision and an omitted line are the same failure.

Structures ruled out on the earlier dataset, none of which round 4 re-tests: journal with batched distillation and sleep-consolidation both lose the timing; pure decay without a rehearsal channel empties the store; an always-in-context anchor block pins stale facts; a view rebuilt from a log window forgets everything older than the window; deterministic platform extraction catches too little.

## Where the residual correction gap is

Replaying the 100 correction turns under B3 and recording whether the contradicted row was among the candidates offered:

| outcome | count |
|---|---|
| offered, retired | 73 |
| offered, kept | 9 |
| not offered | 11 |
| no live row held the old value | 7 |

The row reached the decider 82% of the time and was retired in 89% of those. The residual is retrieval, not judgment, which is why widening the offer closes part of it.

## Adopted direction

- The decider is shown the store's retrieval for the incoming turn, and owns what is stored and what is retired.
- Threshold supersession is removed from the store, not retuned. The platform retains, ranks and forgets.
- Key-addressed replacement leads on the seed measured so far (B14, net 0.91) with a strictly smaller output than B9: a key and a value, no retire list and no ids. It is not adopted until its second seed lands.
- Claim links, append-only read-time resolution, a main-model-written field, and neighbourhood rewrite are rejected.
- The write protocol keeps an explicit retire list. Silence must mean no change.
- Structured output on the write path (from `memory-cycle/`) is unchanged and remains the precondition.

## Limits

- The turn stream is scripted. Only a behavioural QA pass measures the agent.
- One decider model, temperature 0. Nine judgment misses in 100 corrections leaves little headroom to attribute to model choice.
- Embeddings are truncated to 512 dimensions; every structure uses the same embedder.
- B9, B10, B11, B14 and the claim graph ran on one seed. B14 leads the table on that seed and its replication is outstanding, which is the same gap this round was built to catch.
- A structure's first score measures the attempt, not the idea: B5 to B12 to B14 is one structure whose two diagnosed defects were in what the decider was shown. The claim graph is unranked: its decider stored 85 of roughly 290 facts, so the structure was never exercised.
- Coexist is the smallest probe family at 12.

## Reproducing

```
cd docs/experiments/memory-creation
python3 harness/round4/separability.py
python3 harness/round4/runner.py
SEED=11 python3 harness/round4/runner.py
python3 harness/round4/gap_diagnosis.py
python3 harness/round4/single_runner.py DeciderNeighborsWide
python3 harness/round4/single_runner.py NeighbourhoodRewrite
python3 harness/round4/single_runner.py KeyUpsertPluralAware
python3 harness/round4/rewrite_diagnosis.py
python3 harness/round4/key_diagnosis.py
```

`OPENAI_API_KEY` required. Results land in `results_round4/`; embeddings and decider replies are cached beside the harness, so a re-run costs nothing after the first. The rounds 1–3 runners are `harness/creation_runner.py`, `harness/real_runner.py` and `harness/real_new_runner.py`.
