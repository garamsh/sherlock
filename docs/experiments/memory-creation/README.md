# Memory-creation experiment

Which structure should create, correct and retire a memory entry. The cycle experiment (`memory-cycle/`) established that the write path must be shape-enforced; this one compares the structures that sit behind that enforcement.

## Method

488 turns, 152 probes, real embeddings and a real gpt-4o-mini decider, replicated on two dataset seeds. A second seed redraws the corrected, control and distractor assignments; it does not redraw the coexist set, and eight of the seventeen structures ran on one seed. What that costs the ranking is in Limits. Three probe families, because a structure can be wrong in three ways:

| family | probes | scores |
|---|---|---|
| corrected | 100 | the new value is recalled and the old one is gone |
| control | 40 | a never-corrected fact is still recalled |
| coexist | 12 | two simultaneously-true values are both recalled |

`net` is the mean of the three. Values repeat across the 16 subjects, so a memory answers a probe only when it carries both the subject and the value. Retention, decay and salience tiering are off and ranking is pure relevance, so the only thing that can remove a memory is the structure's own logic. Every miss is classified against the row log as never-written, retired, or not-ranked.

Rounds 1–3 of this experiment used a dataset in which every planted topic was corrected, ten probes backed each number, and the store held about fifteen items. That dataset cannot detect over-retirement and cannot distinguish a structure working from a store being small; its numbers are superseded by the table below and its harness is kept only for the structures round 4 does not cover.

## No similarity threshold can decide supersession

ADR 0006 supersedes on embedding cosine; `internal/memory/ranking.go` uses 0.6. 250 pairs per family, measured at the round's 512 dimensions and again at the embedder's native 1536:

| pair family | must supersede? | median (512) | IQR (512) | median (1536) | IQR (1536) |
|---|---|---|---|---|---|
| correction | yes | 0.630 | 0.121 | 0.624 | 0.120 |
| coexist | no | 0.789 | 0.060 | 0.791 | 0.033 |
| cross-subject | no | 0.642 | 0.165 | 0.616 | 0.166 |
| unrelated | no | 0.212 | 0.098 | 0.183 | 0.089 |

Facts that must both survive score higher than real corrections, at both dimensions. Every coexist pair exceeds 0.70: the family's minimum is 0.704 at 512 and 0.718 at 1536. Correction and coexist share the range 0.704–0.826 at 512 and 0.718–0.814 at 1536, and a coexist pair scores at least as high as a correction pair in 95.5% of the 62,500 cross pairs at 512, 97.9% at 1536. Sweeping every threshold:

| threshold | correct retirements (512) | wrong retirements (512) | correct (1536) | wrong (1536) |
|---|---|---|---|---|
| 0.60 (shipped) | 162 | 399 | 154 | 389 |
| 0.70 (ADR 0006) | 52 | 328 | 37 | 305 |
| 0.91 (highest accuracy) | 0 | 0 | 0 | 0 |

The highest-accuracy setting is the one that supersedes nothing, at both dimensions, at the same 0.75 accuracy. Truncation is not what produces the overlap. Going to full dimension moves the correction median down 0.006 and the coexist median up 0.002, so the inversion the conclusion rests on is 0.008 wider; the coexist family's IQR narrows from 0.060 to 0.033 and its minimum rises further above 0.70. Tracked as issue #334.

## The structures

| structure | what the decider is shown, and what it returns | latest-wins | control | coexist | net | net (s11) | control retired | items |
|---|---|---|---|---|---|---|---|---|
| **B14 key-upsert, plural-aware** | related keys with values; a key and value, narrower key when both hold | 0.75 | 0.98 | **1.00** | **0.91** | **0.91** | 0.00 | 312 |
| B17 key-upsert, question-shaped key | as B14, key names the question a fact answers | **0.87** | 0.93 | 0.78 | 0.86 | — | 0.03 | 288 |
| B18 key-upsert, example off the probe set | as B17, narrowing example names an unprobed family | 0.85 | 0.95 | 0.64 | 0.81 | — | 0.03 | 288 |
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

B1 and B7 write from the main response at the cycle experiment's assumed 80% compliance. The two seeds agree on the ranking except B6, which falls from fifth to last past both B7 and B1; B7 stays ahead of B1 on both.

- **What the decider is shown is the structure.** B2 and B3 differ only in that. Showing the store's retrieval instead of the recent writes moves corrections from 0.01 to 0.71 — a gap far outside anything this round's precision could blur. Widening the offer beyond that is a smaller move than the round can order (§What the net column can and cannot separate); what it demonstrably does is a count, taking the contradicted rows that never reached the decider from 11 of 100 to 2. B2's 0.60 in the earlier rounds measured a fifteen-item store, not the structure.
- **The candidate set is safe to widen; a threshold is not.** Retirement of control facts stays at 0.00 across B3 and B9. No setting deletes data — but the offer stops paying at 10, and §The offer is wide enough, and wider does not pay has the cost curve.
- **The shipped mechanism is last.** It retires half of the never-corrected facts and 61% of the coexisting ones.
- **A topic key works, once the decider can see what a key holds.** The first two key structures were wrong about the hint, not about the idea. B4 offers no hints, so the decider coins fresh keys and corrections never land (0.24). B5 offers key *names*, so it reuses a key without knowing whether it holds the same fact, and destroys 55% of the control facts. Offering each key with its current value (B12) drops control retirement to 0.00 and lifts net from 0.51 to 0.77. The residual was five keys holding several simultaneously-true facts — `otto.language` taking Korean, then English, then German, each write replacing the last — and telling the decider to coin a narrower key in that case (B14) reaches coexist 1.00 and net 0.91, the highest net in the round, destroying nothing. Neither figure puts it ahead of B9: the round does not order the top group (§What the net column can and cannot separate), and the 1.00 was measured under one narrowing example, an instruction the per-family decomposition below shows the coexist column is sensitive to.
- **Append-only does not help.** B6 deletes nothing and still loses 65% of control facts, because read-time latest-per-key hides the colliding rows.
- **Output obligations crowd out the write.** Three structures added something the decider had to produce every turn, and all three lost facts rather than judgment. B10 must re-emit the neighbourhood, and drops lines from it. B15 must match against 32-99 offered keys, and answers 49 of 388 fact turns with an empty write list against B14's 5. B16 must return a verdict for every shown candidate, and stores 268 rows against 388 — 68 of its 69 never-written facts come from a turn that stored nothing at all, not from drift. B16 used the vocabulary correctly in bulk (3,846 candidates judged, none skipped) at the same one call per turn as the rest, so this is not disobedience or cost: a reply has a budget, and bookkeeping spends it. B14's output is one key and one value and has no bookkeeping.
- **A multi-valued topic needs somewhere to put its second value.** All four of B9's coexist losses are a sibling statement retiring its twin — "Lisa plays the cello" retires "Lisa plays the drums", and the same for Otto, Maria and Lisa's Korean. B9 has no way to express that both hold; B14's narrower key gives each sibling its own address.
- **The narrowing example moves the coexist column, and this round cannot say by how much for B14.** Coexist preservation per family, over three key-addressed runs:

| family | values | B14 (topic key, example names languages) | B17 (question key, example names allergies) | B18 (question key, example names nothing in the data) |
|---|---|---|---|---|
| allergy | 8 | 8 | 8 | 8 |
| instrument | 8 | 8 | 8 | 4 |
| languages | 12 | 12 | 4 | 5 |

  Only one of the two comparisons is clean. B18 is B17 with the narrowing example moved off the probe set and nothing else changed, and it costs instrument 4 of 8 — a family neither example names — taking coexist from 0.78 to 0.64. So the plural split does not carry to families the prompt does not name, and the coexist column moves with the example. B17 against B14 changes two things at once: B17 takes B15's key definition as well as a different example, so `languages` falling from 12 to 4 is confounded between the two and these runs cannot separate them. No run scores B14's own instruction under a different example, so what B14's 1.00 would be without an example naming `languages` is not measured. The family at stake is large — `languages` is 12 of the 28 planted coexist values and 4 of the 12 coexist probes, a third of the column — and allergy survives all three.
- **Key-upsert's silent case is unsafe where B9's is safe.** In B9 an unmentioned memory is left alone, so a coexist family the decider never thinks about survives by default. In a key-addressed store the default is one key per topic, so a family the decider does not recognise as multi-answer is swallowed — `maria.languages` holding three `speaks-X` rows with two dropped, `otto.hobbies` swallowing cello and drums. A plural stem does not save it. The structure needs a per-family signal that nothing in it currently carries.
- **The key definition is worth keeping on its own.** B17 posts the round's best correction number — latest-wins 0.87 against B14's 0.75, keys reused 87 against 80, coined 8 against 14, stale 0.09 against 0.22 — and every value-named key is gone (no `alma.tea`, no `kofi.pets.turtle`). Its remaining eight misses are genuine topic ambiguity, such as "Maria moved to fish" read as a diet. It also settles B15: empty writes are 8 of 388 here against B15's 49, so B15's write collapse came from the offer size, not from the key definition the two share.
- **The protocol's silent case decides its safety.** B10 removes the vocabulary entirely: the decider returns what the shown set should now be, so deletion is omission. Classifying all 106 of its removals shows what that costs — 4% are paraphrase churn, the rest are collateral, a fact about another subject or another true value dropped from the reply. Stating the scope explicitly (B11) makes it worse, not better: 0.63. B14 keeps the same one-field output and does not have the problem, because a key that is not written is not touched. Whether the vocabulary is small matters less than whether silence means "no change"; a threshold firing on similarity, a key collision and an omitted line are the same failure.

Structures ruled out on the earlier dataset, none of which round 4 re-tests: journal with batched distillation and sleep-consolidation both lose the timing; pure decay without a rehearsal channel empties the store; an always-in-context anchor block pins stale facts; a view rebuilt from a log window forgets everything older than the window; deterministic platform extraction catches too little.

## What the net column can and cannot separate

Each family's score is a mean over its own probes — 100 corrections, 40 controls, 12 coexist probes — so `net` carries their sampling error. Computing it treats the coexist probes as Bernoulli, which overstates their variance because their per-probe values are fractions rather than 0 or 1; probes sharing one store are if anything positively correlated, which widens the true interval further. Both approximations make the figures below a floor on the uncertainty, not a ceiling.

The 95% half-width on `net` runs from 0.033 (B14, whose coexist column is saturated at 1.00 and contributes no variance) to 0.109 (B16). **The eight structures from B14 down to B12 all overlap B9's interval of 0.786 to 0.936. The eight from B2 down to B1 do not.** The round separates the structures that keep data from the structures that destroy it, and orders nothing inside the top group — B9 against B3 included, though the widening claim itself rests on a count (11 unoffered corrections falling to 2) rather than on the difference between two net values.

Almost all of that width is the coexist family:

| corrected probes | coexist probes | 95% half-width on `net` |
|---|---|---|
| 100 | 12 | ±0.083 |
| 400 | 12 | ±0.080 |
| 100 | 60 | ±0.048 |
| 100 | 200 | ±0.039 |

Quadrupling the corrected family buys 0.003. Taking coexist from 12 probes to 60 buys 0.035. A second seed buys nothing at all on the column that sets the width, because the coexist set does not vary with the seed.

What survives at this precision is the counts, not the ordering: the offered-versus-not-offered split behind the correction gap, the rows a structure writes against the 388 fact turns, its retire events, and the family-level coexist breakdown. Every conclusion in this document that rests on counting events stands. Every conclusion that rests on ordering two net values inside the top group does not.

## Where the residual correction gap is

Replaying the 100 correction turns under B3 and recording whether the contradicted row was among the candidates offered:

| outcome | count |
|---|---|
| offered, retired | 73 |
| offered, kept | 9 |
| not offered | 11 |
| no live row held the old value | 7 |

The row reached the decider 82% of the time and was retired in 89% of those. Replaying the same 100 turns under B9's wider offer moves the split to 77 offered and retired, 16 offered and kept, 2 not offered, 5 with no live row: widening closed the retrieval half almost entirely, and what remains is judgment — in 14 of the 16 the retire list came back empty with the contradicted row at rank 1 or 2.

### The offer is wide enough, and wider does not pay

Sweeping the offer width over B9's trajectory, against the 95 corrections whose old value still had a live row:

| K | contradicted row reached | candidates per turn | candidate rows across the run |
|---|---|---|---|
| 5 | 84 | 4.40 | 2,147 |
| 10 | 93 | 8.15 | 3,976 |
| 20 | **95** | 14.70 | 7,174 |
| 40 | 95 | 23.38 | 11,410 |
| unbounded | 95 | 29.10 | 14,199 |

The row is at rank 1 for 56 of the 95 and within rank 5 for 84; rank 15 is the deepest anywhere in the run. Coverage therefore saturates at 20 and nothing above it buys anything. The floor is not a free parameter either: the lowest contradicted-row cosine in the run is 0.374, so 0.3 is a ceiling rather than a setting, and 0.6 collapses coverage to 63.

**K stays at 10.** Going 5 → 10 costs 203 extra candidate rows per correction recovered; going 10 → 20 costs 1,599, and buys 2 corrections of 100 — 0.02 on latest-wins and 0.007 on `net`, an order of magnitude below what any affordable run can resolve. Against that, B16 is this round's measured evidence that loading more into one reply degrades it, and a 3% degradation on the 93 corrections already reaching the decider would be four times the gain in the opposite direction. The retrieval half is closed to 98% and the residual is judgment.

### What the corrected column is measuring

Every correction template in the dataset announces the change — `switched`, `migrated`, `now`, `these days`, `instead`. Four phrasings of each correction, judged with the offer held fixed at what the announced turn retrieved, on the 93 whose offer carried the contradicted row:

| arm | marker | phrasing | retired | mean cosine to the row it contradicts |
|---|---|---|---|---|
| A | yes | the announced correction the dataset writes | 77 — 0.83 | 0.641 |
| B | no | the stored sentence carrying the new value | 86 — 0.92 | 0.800 |
| C | yes | B with a temporal marker prepended | 89 — 0.96 | 0.743 |
| D | no | the category's other initial template | 66 — 0.71 | 0.673 |

**The decider is not following the marker.** C and B share a base sentence and differ only by the prepended phrase: 3 discordant pairs, exact p = 0.25. The prepend also lowers surface similarity while raising the rate.

**It is not matching surface either.** Similarity would order the arms B > C > D > A. The measurement gives C > B > A > D, and D sits at higher mean cosine than A while retiring eleven fewer (p = 0.019). Across all 372 outcomes the correlation with cosine is 0.29, and the quantile bands are a floor rather than a slope — everything above 0.61 lands between 0.79 and 0.95.

What separates the arms is whether the incoming sentence is unambiguously an answer to the same question. D's largest collapses are where it is not. All 20 categories, all 93 judgeable corrections:

| category | n | A | B | C | D |
|---|---|---|---|---|---|
| gym | 5 | 5 | 5 | 5 | **1** |
| pet | 6 | 5 | 5 | 5 | **1** |
| testing | 7 | 5 | 5 | 6 | **3** |
| manager | 5 | 2 | 5 | 5 | 1 |
| ci, cloud, commute, database, framework, notes, os, timezone | 32 | 32 | 32 | 32 | 32 |
| drink, editor, indent, language, music, shell, terminal, tracing | 38 | 28 | 34 | 36 | 28 |
| all 20 | 93 | 77 | 86 | 89 | 66 |

Eight categories are perfect under every arm. The other eight are not a remainder that can be ignored: D trails C by 8 inside them — music 1 against 3, editor 1 against 2, language 3 against 5, drink 4 against 6, indent 6 against 7 — so D is below n in 10 of the 20 categories, not only in the four the table names.

Three of those four are the multi-valued case. A person can do two sports, keep two pets and run two kinds of test. "Nina has a turtle at home." beside a stored "Nina keeps a cat." is a fact that can coexist, and keeping the stored row is the decider judging correctly rather than failing. **In gym, pet and testing — 18 of the 93 — the announcement is what makes the item a correction at all.** Strip it and the ground truth goes with it. `manager` is not in that 18 and is a different case: a person reports to one manager, so a second name is a correction, and what the row shows is the announced arm doing worst — A retires 2 of 5 where B retires 5.

So the corrected column measures the decider on the 75 items whose topic is single-valued, and on the other 18 it measures whether the template said `instead`. That is a property of the dataset, not of any structure, and it applies to every row of the table equally.

It also supplies the mechanism behind the key structures' correction scores. A key forces the stored fact and the incoming one through a single address, which manufactures "the same question" whatever the phrasing — which is why the question-shaped key posts the round's best correction number without changing anything about what the decider is shown.

Two smaller results fall out. The span across the four arms is 0.25, from D's 0.71 to C's 0.96, so **phrasing moves the corrected column further than any difference between the structures in the top group.** And both rows unreachable at K=10 are reached at rank 1 under arm B, confirming that the retrieval tail is corrections sharing no surface with what they contradict rather than a limit of the retriever.

## One decider call per turn, and what a cheaper schedule would buy

Every structure calls the decider once per turn, on all 488 turns. Classifying what each call returned, against the turn that prompted it:

- **100 of 488 calls (20.5%) returned no operation at all under B9, 97 under B14** — and only 1 of B9's and 5 of B14's fall on a fact turn. The idle calls are the chatter turns and essentially nothing else, so there is no cheap subset of fact turns to skip.
- **No retire ever landed on a chatter turn** — 0 across 200 chatter calls in both structures, against 399 wrong retirements at the shipped 0.60 threshold. A skipped call fails to write and cannot delete, so a call gate fails in the opposite direction to the mechanism this round falsified.
- A gate that skipped every chatter turn saves 20.5% of calls and changes nothing measurable: B14's replay returns `net` 0.9083 unchanged, 312 items down to 304, no probe value moved.

**No gate is worth building on this evidence.** The 20.5% ceiling is `chatter_ratio`, a scripted parameter rather than measured traffic, while the 388 fact turns carry the whole of `net` between them at 0.0026 each — so a gate needs 96.7% recall on fact turns to stay inside this round's own interval, and at 95% it costs more than the distance from the adopted structure to the round's best-scoring one. The number that would change this is the no-fact share of real traffic; build one when that is measured above 50%.

## Adopted direction

The ordering inside the top group is not resolved at this round's precision (§What the net column can and cannot separate). Each item below is adopted on a property that was counted directly, not on its place in the net column.

- Threshold supersession is removed from the store, not retuned. The platform retains, ranks and forgets.
- **The decider is shown the store's retrieval for the incoming turn and returns what to store and what to retire** (B9, net 0.86). It is adopted for the property the key structures cannot match: an unmentioned memory is left alone, so facts the decider never considers survive. Widening the candidate offer deletes nothing at any setting, but it stops paying at 10: the retrieval half of its residual is closed to 98%, and what remains is judgment.
- **Key-addressed replacement is not adopted.** Its losses are coexisting facts silently swallowed by a single key, and the coexist column that carries a third of its net moves with the narrowing example the prompt happens to give: B18, the question-shaped key definition under an example that names nothing in the data, scores coexist 0.64 and net 0.81. B14's 0.91 was measured under one example and no run repeats it under another, so it is not a figure to adopt on. The key-addressed family holds the round's two best correction numbers — B17 at 0.87 and B18 at 0.85, against B9's 0.76 — though B14 itself is not among them at 0.75, so what carries corrections is the key *definition* rather than the replacement rule. The shape should be revisited if the multi-answer case is carried by something the decider returns rather than a key it has to infer.
- **The question-shaped key definition is worth keeping on its evidence.** Its correction gain is example-independent — latest-wins 0.87 and 0.85 across two unrelated examples, no value-named keys, no write suppression — and it is the round's best correction result. It is an improvement to the write vocabulary, not to the store shape.
- Claim links, append-only read-time resolution, a main-model-written field, and neighbourhood rewrite are rejected.
- The write protocol keeps an explicit retire list. Silence must mean no change.
- Structured output on the write path (from `memory-cycle/`) is unchanged and remains the precondition.

## Limits

- The turn stream is scripted. Only a behavioural QA pass measures the agent.
- One decider model, temperature 0. The 16 judgment misses in 100 corrections under B9 are the whole headroom, and nothing here separates what a different model would do from what a different phrasing does — §What the corrected column is measuring puts the second at 0.25 across four phrasings of the same corrections.
- 18 of the 93 judgeable corrections are in categories where a person can hold two values at once, so they are corrections only because the template announces them (§What the corrected column is measuring). Every structure is scored on the same 18, so the ranking is not distorted, but the corrected column is not purely a measure of judgment.
- Every structure ran on embeddings truncated to 512 dimensions, with the same embedder. The threshold result was re-measured at the native 1536 and does not depend on the truncation; the structure scores were not re-run at full dimension.
- **A probe value is credited on a substring match.** `harness/round4/scoring.py` tests `value in row.lower()`, so `Zed` is credited by "Vera has since standardized on Python.", `fish` by "Maria is allergic to shellfish." — a coexist statement scoring a corrected `shell` probe — and `tea` by "team" and by "instead". Counting the credits that hold only as a substring, replayed on seed 7: B9 2, B14 3, B17 3, all in the corrected family, some crediting the new value and some holding the old one present. It is systematic rather than random and it favours structures that write more rows, but at ±0.083 on `net` it moves nothing in this document, so round 4 is not rescored (#371). Round 5 credits a value only where a row states it as a whole word, and fails its dataset check if a value it adds sits inside another value or inside the fixed sentence vocabulary.
- B9, B10, B11, B15, B16, B17, B18 and the claim graph ran on one seed. B14, B12 and B1 through B7 ran on two. B14's two seeds agree with each other; agreement across seeds does not detect an effect that is in the instruction rather than the data.
- **The coexist set does not vary with the seed.** `harness/round4/dataset.py` builds it from `PERSONS[:4]` and a module-level `COEXIST` constant, so the 12 coexist probes and the 28 fact turns behind them are identical under every seed. Replication redraws the corrected, control and distractor assignments around a fixed coexist set; it never draws a second one. B14's coexist 1.00 "on two seeds" is one measurement reported twice, which is the mechanical reason seed agreement says nothing about the narrowing example.
- The coexist probe set is 28 values in three families, and one family is 12 of them, carried by 4 of the 12 coexist probes, so losing that one family moves the column by 0.33. One probe is 8.3% of the column, against 1% for a corrected probe, because `net` weights the three families equally. A larger and flatter coexist set, redrawn per seed, would make the metric harder to move this way.
- **The coexist column is also the noisiest, on data that does not change.** B12 scores coexist 0.61 on seed 7 and 0.44 on seed 11 against the identical coexist set, so 0.167 of that column is the surrounding store rather than the probes. A fixed, small, high-variance measurement carries a third of `net`.
- **The measured seed movement agrees with the computed interval.** B12 moves 0.049 on net between its two seeds and B14 moves 0.005, both inside what §What the net column can and cannot separate allows. The two lines of evidence — sampling error computed from the probe counts, and movement observed between seeds — give the same answer about what the ranking can carry.
- A structure's first score measures the attempt, not the idea: B5 to B12 to B14 is one structure whose two diagnosed defects were in what the decider was shown. The claim graph is unranked: its decider stored 85 of roughly 290 facts, so the structure was never exercised.

## Reproducing

```
cd docs/experiments/memory-creation
python3 harness/round4/separability.py
python3 harness/round4/runner.py
SEED=11 python3 harness/round4/runner.py
python3 harness/round4/claimgraph_runner.py
python3 harness/round4/gap_diagnosis.py
python3 harness/round4/single_runner.py DeciderNeighborsWide
python3 harness/round4/single_runner.py NeighbourhoodRewrite
python3 harness/round4/single_runner.py RewriteScoped
python3 harness/round4/single_runner.py KeyUpsertValueHints
SEED=11 python3 harness/round4/single_runner.py KeyUpsertValueHints
python3 harness/round4/single_runner.py KeyUpsertPluralAware
SEED=11 python3 harness/round4/single_runner.py KeyUpsertPluralAware
python3 harness/round4/rewrite_diagnosis.py
python3 harness/round4/key_diagnosis.py
python3 harness/round4/b9_diagnosis.py
python3 harness/round4/b16_writes.py
python3 harness/round4/coexist_families.py
python3 harness/round4/call_schedule.py
python3 harness/round4/precision.py
python3 harness/round4/offer_width.py
EMBED_DIMS=1536 python3 harness/round4/separability.py
python3 harness/round4/marker_following.py
```

The B15, B16, B17 and B18 rows in §The structures have no command here: `single_runner.py` looks its argument up in `harness/round4/structures.py`, and those four structures are defined in `structures_a.py` and `structures_b.py`. `coexist_families.py` and `b16_writes.py` re-run three of them but write only their own diagnostics, so the four scored rows cannot be regenerated as the harness stands.

`OPENAI_API_KEY` required. Results land in `results_round4/`; embeddings and decider replies are cached beside the harness, so a re-run costs nothing after the first. `EMBED_DIMS` defaults to 512 and caches each dimension separately, so the first run at another dimension re-embeds and writes its own result files. The rounds 1–3 runners are `harness/creation_runner.py`, `harness/real_runner.py` and `harness/real_new_runner.py`.

`python3 harness/round5/dataset_check.py` checks the round-5 dataset, its value vocabulary and its match rule. It needs no key and writes nothing.
