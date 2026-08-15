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
| **B14 key-upsert, plural-aware** | related keys with values; a key and value, narrower key when both hold | 0.75 | 0.98 | **1.00** | **0.91** | **0.91** | 0.00 | 312 |
| B15 key-upsert, whole namespace | every key held about the subjects in play | 0.77 | 0.85 | 0.94 | 0.85 | — | 0.00 | 258 |
| B9 decider-neighbours, wide | 10 retrieved candidates; facts to store and ids to retire | 0.76 | 0.98 | 0.85 | 0.86 | — | 0.00 | 298 |
| B3 decider-neighbours | 5 retrieved candidates; facts to store and ids to retire | 0.71 | 0.97 | 0.85 | 0.84 | 0.85 | 0.00 | 300 |
| B10 neighbourhood rewrite | 10 retrieved candidates; the new state of that set | 0.71 | 0.93 | 0.71 | 0.78 | — | 0.05 | 294 |
| B12 key-upsert, key and value hints | related keys with their values; a key and value | 0.73 | 0.98 | 0.61 | 0.77 | 0.72 | 0.00 | 299 |
| B2 decider-window | the 10 most recent memories; same output | 0.01 | 0.95 | 0.97 | 0.64 | 0.67 | 0.00 | 379 |
| B11 rewrite, scope stated | as B10, told the set spans subjects | 0.70 | 0.80 | 0.40 | 0.63 | — | 0.18 | 246 |
| B16 per-candidate verdict | as B9, a forced verdict per shown id instead of a retire list | 0.69 | 0.60 | 0.38 | 0.56 | — | 0.08 | 174 |
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
- **Output obligations crowd out the write.** Three structures added something the decider had to produce every turn, and all three lost facts rather than judgment. B10 must re-emit the neighbourhood, and drops lines from it. B15 must match against 32-99 offered keys, and answers 49 of 388 fact turns with an empty write list against B14's 5. B16 must return a verdict for every shown candidate, and stores 268 rows against 388 — 68 of its 69 never-written facts come from a turn that stored nothing at all, not from drift. B16 used the vocabulary correctly in bulk (3,846 candidates judged, none skipped) at the same one call per turn as the rest, so this is not disobedience or cost: a reply has a budget, and bookkeeping spends it. B14's output is one key and one value and has no bookkeeping.
- **A multi-valued topic needs somewhere to put its second value.** All four of B9's coexist losses are a sibling statement retiring its twin — "Lisa plays the cello" retires "Lisa plays the drums", and the same for Otto, Maria and Lisa's Korean. B9 has no way to express that both hold; B14's narrower key gives each sibling its own address, which is the whole of its 1.00 against B9's 0.85.
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

The row reached the decider 82% of the time and was retired in 89% of those. Replaying the same 100 turns under B9's wider offer moves the split to 77 offered and retired, 16 offered and kept, 2 not offered, 5 with no live row: widening closed the retrieval half almost entirely, and what remains is judgment — in 14 of the 16 the retire list came back empty with the contradicted row at rank 1 or 2.

## Adopted direction

- The decider is shown the store's retrieval for the incoming turn, and owns what is stored and what is retired.
- Threshold supersession is removed from the store, not retuned. The platform retains, ranks and forgets.
- **Key-addressed replacement is the adopted structure** (B14): net 0.91 on both seeds, coexist 1.00 on both, and nothing retired that had to survive on either. Its output is one key and one value — no retire list, no ids, no verdicts — which is also the smallest output measured.
- The decider is offered the retrieved keys with the values they hold, and coins a narrower key when the new fact and the held value can both be true. Offering more than that loses writes (B15).
- Claim links, append-only read-time resolution, a main-model-written field, and neighbourhood rewrite are rejected.
- The write protocol keeps an explicit retire list. Silence must mean no change.
- Structured output on the write path (from `memory-cycle/`) is unchanged and remains the precondition.

## Limits

- The turn stream is scripted. Only a behavioural QA pass measures the agent.
- One decider model, temperature 0. Nine judgment misses in 100 corrections leaves little headroom to attribute to model choice.
- Embeddings are truncated to 512 dimensions; every structure uses the same embedder.
- B9, B10, B11, B15, B16 and the claim graph ran on one seed. B14, B12, and the structures in the first table ran on two.
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
SEED=11 python3 harness/round4/single_runner.py KeyUpsertPluralAware
python3 harness/round4/rewrite_diagnosis.py
python3 harness/round4/key_diagnosis.py
```

`OPENAI_API_KEY` required. Results land in `results_round4/`; embeddings and decider replies are cached beside the harness, so a re-run costs nothing after the first. The rounds 1–3 runners are `harness/creation_runner.py`, `harness/real_runner.py` and `harness/real_new_runner.py`.
