# Memory-retrieval experiment

What the read path returns for a task statement, and what the choice of embedder costs it, measured against the memory implementation this project ships.

## Method

Every other experiment in this tree reimplements the idea under test in Python, so its numbers describe a model of the design rather than the code. This one plants a corpus into the store `internal/memory` ships and queries it through the service that ships: `memory.NewService` over `internal/memory/sqlite`. The harness supplies the corpus and a fixed clock; the store, the embedders, the hybrid search and the ranking function are the ones that run in production, called through `Search`, `Recall` and `Similar`. It is a Go command because that is the shape that can call the package directly — a harness in another language would have to re-describe the thing it is measuring, which is the gap this experiment exists to close.

The facts are written with the store's own insert rather than through `memory.Service.Store`. Supersession on insert archives a new memory's most similar predecessor at cosine 0.6 and above, which would silently remove planted facts and leave the ground truth describing a store that no longer holds them. The write path is `memory-creation/`'s subject and the threshold is #334's; the retrieval half of this record measures the read. The supersession half below measures that mechanism head-on, one pair at a time, where it cannot corrupt anything.

Every figure names the embedder that produced it, and every result file carries that name too. A retrieval figure without one is unreadable: the same corpus, probes and ranking function score differently under each.

### The two embedders

| embedder | dimensions | what it is |
|---|---|---|
| `hashing` | 256 | the deterministic offline embedder in `internal/memory/hashing.go`, which `cmd/gagent` defaults to |
| `openai` | 1536 | `internal/memory/openai` against `text-embedding-3-small`, as it ships |

The provider sends no `dimensions` parameter, so the model answers at its native 1536. Every committed `memory-creation` figure was measured at 512, which that experiment asked for explicitly. Where a figure here sits beside one of theirs, the two are at different dimensions and are marked as such; the third column below exists so the comparison has a like-for-like row.

The `hashing` run is offline and needs no API key. The `openai` run reads its key from the environment, embeds each distinct text once, and caches vectors by text outside the repository; no vector and no key is committed.

### The corpus

`results/corpus_seed7.json` is the planted store, deterministic from seed 7 and rebuilt by `harness/corpus.go`:

- 9,000 facts over 1,800 subject-topic pairs: 90 subjects, 20 topics, each pair holding 3 to 7 facts drawn from that topic's values.
- 120 of those pairs are probed. Their 605 facts are the ground truth, and all of them sit inside the first 1,000 items, so a larger store is the same store with more distractors rather than a different one.
- Every fact is a hard negative for some probe. The full store holds all 1,800 pairs, so a probe's subject appears under 19 other topics and its topic under 89 other subjects and neither alone selects an answer; the smaller stores are prefixes of the same order and hold a subset of those distractors.
- A probe is a task statement, not a copy of the fact it looks for. The shipped recall step queries with the frame's task statement, so the query shares the topic's head word with its answers and differs elsewhere.
- Salience is drawn from the three values `internal/gateway/writeback.go` derives from a writeback op — 0.5, 0.7, 0.8 — and ages are drawn over a 30-day window. Both are drawn independently of whether a fact answers a probe, so the ranking function's salience and recency terms carry no information about the answer. A corpus that correlated them would be measuring itself, and a comparison run on it would attribute to the embedder whatever the correlation did.

### What counts as a correct retrieval

A returned memory is credited when its store ID is one of the IDs the generator planted for that probe's subject-topic pair. Nothing is matched on content.

The alternative was the rule `memory-creation` used, which credits a probe when the expected value appears in the memory's text, and which credited `fish` to "Maria is allergic to shellfish." until #371 required a word boundary. The corpus keeps that trap live rather than avoiding it: among the 200 planted values, seven pairs contain each other — `fish` in `shellfish` and in `goldfish`, `nuts` in `walnuts`, `atlas` in `atlases`, `car` in `cartography`, `top` in `desktop` and in `laptop` — and 562 of the 9,000 facts carry one of them. `harness/measure_test.go` runs the smallest store where the two rules disagree: one planted answer, one fact with the same value under another subject, one with the same value under another topic, all three returned, exactly one credited.

### The three plantings

The read path is identical in all three. They differ only in the two fields the ranking function reads besides relevance:

| planting | salience | ages |
|---|---|---|
| `mixed-30d` | the writeback mix, 0.5 / 0.7 / 0.8 | over 30 days |
| `mixed-360d` | the same mix | the same draws, stretched twelvefold |
| `flat` | 0.7 on every memory | every memory at the query time |

`flat` leaves the ranking function adding the same salience and the same recency to every row, so the order it produces is the order of its relevance term alone. It is the control that says whether the corpus is retrievable at all, and it is the only row in which the embedders can be compared on the job an embedder does.

### The interval

Every figure is a mean over the 120 probes, and every figure carries the 95% half-width computed from the sample standard deviation of the per-probe values. `memory-creation/harness/round4/precision.py` could only approximate this from family means and treated fractional scores as Bernoulli; here each probe's own value is committed in `results/retrieval_seed7_<embedder>.json`, so the interval comes from them directly. One approximation remains and it widens rather than narrows: the probes share one store, so their errors are if anything positively correlated.

## What the read path scores, under each embedder

`recall@k` is planted answers returned over answers planted; `precision@k` is planted answers returned over memories returned. `@10` is `DefaultSearchLimit`, the shipped cap on what one search returns. Best rank is where the highest-ranked answer sits in the whole ranked store, so it reads against the store size beside it.

| embedder | planting | store | recall@1 | precision@1 | recall@5 | precision@5 | recall@10 | precision@10 | best rank |
|---|---|---|---|---|---|---|---|---|---|
| `hashing` | `mixed-30d` | 1,000 | 0.030 ±0.013 | 0.150 ±0.064 | 0.063 ±0.021 | 0.058 ±0.019 | 0.092 ±0.024 | 0.044 ±0.011 | 29.6 ±5.8 |
| `hashing` | `mixed-30d` | 4,000 | 0.018 ±0.010 | 0.092 ±0.052 | 0.042 ±0.018 | 0.040 ±0.016 | 0.056 ±0.020 | 0.027 ±0.009 | 103.2 ±21.1 |
| `hashing` | `mixed-30d` | 9,000 | 0.013 ±0.009 | 0.067 ±0.045 | 0.024 ±0.014 | 0.025 ±0.014 | 0.037 ±0.017 | 0.018 ±0.008 | 242.9 ±49.8 |
| `hashing` | `mixed-360d` | 1,000 | 0.008 ±0.007 | 0.042 ±0.036 | 0.157 ±0.029 | 0.147 ±0.027 | 0.478 ±0.049 | 0.235 ±0.026 | 10.9 ±3.4 |
| `hashing` | `mixed-360d` | 4,000 | 0.003 ±0.004 | 0.017 ±0.023 | 0.008 ±0.007 | 0.008 ±0.007 | 0.038 ±0.017 | 0.018 ±0.008 | 37.7 ±13.4 |
| `hashing` | `mixed-360d` | 9,000 | 0.003 ±0.004 | 0.017 ±0.023 | 0.004 ±0.004 | 0.005 ±0.006 | 0.008 ±0.007 | 0.004 ±0.004 | 86.8 ±32.9 |
| `hashing` | `flat` | 1,000 | 0.189 ±0.019 | 0.833 ±0.067 | 0.775 ±0.060 | 0.735 ±0.060 | 0.875 ±0.059 | 0.433 ±0.039 | 10.4 ±4.7 |
| `hashing` | `flat` | 4,000 | 0.175 ±0.021 | 0.767 ±0.076 | 0.726 ±0.067 | 0.678 ±0.066 | 0.807 ±0.071 | 0.392 ±0.042 | 35.1 ±17.5 |
| `hashing` | `flat` | 9,000 | 0.174 ±0.021 | 0.758 ±0.077 | 0.712 ±0.068 | 0.663 ±0.067 | 0.799 ±0.070 | 0.388 ±0.041 | 76.1 ±41.8 |
| `openai` | `mixed-30d` | 1,000 | 0.044 ±0.016 | 0.208 ±0.073 | 0.074 ±0.023 | 0.073 ±0.021 | 0.100 ±0.025 | 0.049 ±0.012 | 19.1 ±3.1 |
| `openai` | `mixed-30d` | 4,000 | 0.024 ±0.012 | 0.125 ±0.059 | 0.056 ±0.019 | 0.055 ±0.019 | 0.070 ±0.023 | 0.034 ±0.011 | 68.7 ±11.1 |
| `openai` | `mixed-30d` | 9,000 | 0.014 ±0.010 | 0.075 ±0.047 | 0.037 ±0.016 | 0.037 ±0.015 | 0.054 ±0.019 | 0.027 ±0.009 | 165.1 ±26.4 |
| `openai` | `mixed-360d` | 1,000 | 0.007 ±0.007 | 0.033 ±0.032 | 0.220 ±0.029 | 0.205 ±0.025 | 0.587 ±0.044 | 0.290 ±0.024 | 4.8 ±0.3 |
| `openai` | `mixed-360d` | 4,000 | 0.004 ±0.004 | 0.025 ±0.028 | 0.007 ±0.007 | 0.007 ±0.006 | 0.031 ±0.015 | 0.016 ±0.007 | 15.0 ±1.0 |
| `openai` | `mixed-360d` | 9,000 | 0.004 ±0.004 | 0.025 ±0.028 | 0.004 ±0.004 | 0.005 ±0.006 | 0.007 ±0.007 | 0.003 ±0.003 | 33.8 ±2.8 |
| `openai` | `flat` | 1,000 | 0.219 ±0.013 | 1.000 ±0.000 | 0.899 ±0.022 | 0.873 ±0.030 | 1.000 ±0.000 | 0.504 ±0.027 | 1.0 ±0.0 |
| `openai` | `flat` | 4,000 | 0.209 ±0.016 | 0.942 ±0.042 | 0.878 ±0.029 | 0.850 ±0.034 | 0.999 ±0.002 | 0.503 ±0.027 | 1.1 ±0.1 |
| `openai` | `flat` | 9,000 | 0.207 ±0.016 | 0.925 ±0.047 | 0.843 ±0.044 | 0.807 ±0.047 | 0.982 ±0.013 | 0.493 ±0.026 | 1.4 ±0.2 |

## What the numbers say

Both embedders are in every bullet, because every one of these findings was first drawn on `hashing` alone and the second embedder is what says whether it was a finding about the read path or about a vector space.

- **The corpus is retrievable and the relevance term retrieves it.** Under `flat`, `hashing` returns 0.875 of the planted answers in the top ten at 1,000 items and 0.799 at 9,000; `openai` returns 1.000 and 0.982. Whatever the other rows measure, it is not a corpus nobody could search.
- **With ages and saliences spread, the top ten stops answering the query.** `mixed-30d` at 9,000 items returns 0.037 of the answers under `hashing` and 0.054 under `openai`, where one returned memory in 57 and one in 37 respectively is an answer. Both gaps to `flat` are far outside every interval at every size.
- **The answers are ranked well and the window is narrow.** In `mixed-30d` at 9,000 the best answer sits at rank 242.9 under `hashing` and 165.1 under `openai` — the top 2.7% and 1.8% of the store, against about 1,490 if the order were random — while `DefaultSearchLimit` shows the top 0.11%. Neither ranking is blind to the query; the ten rows either returns are settled before the query is consulted.
- **What fills those ten is age and salience.** At 9,000 items the ten memories returned average 6.82 hours old under `hashing` and 6.99 under `openai`, against a store averaging 361, and carry salience 0.72 and 0.73 against the store's 0.63. A memory written in the last hour scores its salience plus a recency term near 1.0 before any relevance is added, and the relevance term is bounded by 1.0 in total whichever embedder computes it.
- **The age window moves the figure, and not monotonically, under both.** Stretching the same draws twelvefold takes recall@10 at 1,000 items from 0.092 to 0.478 under `hashing` and from 0.100 to 0.587 under `openai`, because almost nothing is then young enough for recency to matter; at 9,000 items enough is, and the same stretch takes it from 0.037 to 0.008 and from 0.054 to 0.007. A corpus parameter that no constant in `ranking.go` mentions moves the figure by many times either interval, which is why the window is stated here rather than assumed.
- **Store size costs recall.** `hashing` under `mixed-30d` falls from 0.092 to 0.037 between 1,000 and 9,000 items, which the intervals separate; its `flat` fall from 0.875 to 0.799 they do not. The probes are the same at both sizes, so a paired comparison could be sharper than these unpaired intervals, and this run does not make one.
- **An empty noise floor is not enough for precision.** ADR 0006 records as a consequence that "retrieval is precise because the noise floor is empty". This store holds nothing but curated facts — no chatter, no auto-capture — and precision@10 at 9,000 items is 0.018 under `hashing` and 0.027 under `openai`. Curation removes one cause of imprecision; on this corpus it does not deliver the consequence on its own under either embedder.
- **The recall token budget never binds.** `Recall` takes its candidates from a search capped at `DefaultSearchLimit`, so it returns at most ten memories whatever budget it is given; the 512-token budget can only trim below that, and only when the ten average more than 204.8 characters. The corpus averages 30.7. `returned@budget` is exactly 10.0 in all eighteen rows, and `recall@budget` equals `recall@10` in all eighteen.

## What the embedder costs

- **On the job an embedder does, the difference is large and unambiguous.** Under `flat`, where relevance alone orders the store, `openai` returns every planted answer in its top ten at 1,000 items and 0.982 of them at 9,000, against `hashing`'s 0.875 and 0.799. The intervals do not overlap at any of the three store sizes. Its best answer sits at rank 1.0 to 1.4 of the whole store against `hashing`'s 10.4 to 76.1, and at 1,000 items every probe's first result is a planted answer — precision@1 of 1.000.
- **On what the read path actually returns, the difference disappears.** Under `mixed-30d`, the planting that carries the salience mix and age spread a running store has, recall@10 is 0.100 against 0.092 at 1,000 items, 0.070 against 0.056 at 4,000, and 0.054 against 0.037 at 9,000. The intervals overlap at every one of the three sizes, so this run does not separate the two embedders there at all.
- **The better embedder is spent before the query is read.** The ten memories `openai` returns at 9,000 items average 6.99 hours old with salience 0.731, against `hashing`'s 6.82 hours and 0.725 — the same ten-row window, filled the same way. An embedder decides the relevance term, and the relevance term is not what selects those ten.
- **A better embedder does not rescue the age window either.** Stretching ages twelvefold moves `openai` exactly as it moves `hashing`, up at 1,000 items and down at 9,000, in the figures §What the numbers say carries for both.
- **Cost and reproducibility run the other way.** The `hashing` embedder needs no key, no network and no money, and returns the same vector for the same text forever. The `openai` run embedded 10,483 distinct texts, at a published price of $0.02 per million tokens and is not reproducible to the last bit: see the two-pair difference under §Whether a threshold separates.

## Whether a threshold separates corrections from facts that must both survive

`internal/memory/ranking.go` sets `SupersessionSimilarity` to 0.6 and says it is "calibrated to the hashing embedder's similarity distribution: a correction restating a fact lands around 0.66 (issue #324)". The measurement that refutes the mechanism — `memory-creation`'s separability sweep — was run on OpenAI embeddings. So the number defending the threshold and the number attacking it came from different embedders, and #334 has been waiting to know whether the refutation transfers.

It does, and it is worse on the embedder that ships.

The pair set is `memory-creation`'s own: `harness/pairs_from_round4.py` calls that experiment's `build_pairs(seed=7)` unchanged and writes the 1,000 pairs to `results/separability_pairs_seed7.json`, 250 per family. Each pair is measured twice over: the harness takes the embedding cosine, and the shipped `memory.Service.Store` writes the old statement and then the new one into a store holding nothing else, so its retire-or-not is the mechanism's own answer rather than a prediction. The two agree on 1,000 of 1,000 pairs under every embedder, which is what makes the sweep below a sweep of the number the mechanism actually reads.

| embedder | family | must supersede? | min | median | max | IQR | retired by the shipped store |
|---|---|---|---|---|---|---|---|
| `hashing` 256 | correction | yes | 0.000 | 0.309 | 0.676 | 0.195 | 6 of 250 |
| `hashing` 256 | coexist | no | 0.667 | 0.750 | 0.800 | 0.133 | 250 of 250 |
| `hashing` 256 | cross-subject | no | 0.000 | 0.507 | 0.909 | 0.484 | 115 of 250 |
| `hashing` 256 | unrelated | no | -0.236 | 0.000 | 0.447 | 0.000 | 0 of 250 |
| `openai` 1536 | correction | yes | 0.322 | 0.624 | 0.814 | 0.120 | 154 of 250 |
| `openai` 1536 | coexist | no | 0.718 | 0.791 | 0.908 | 0.033 | 250 of 250 |
| `openai` 1536 | cross-subject | no | 0.353 | 0.616 | 0.800 | 0.166 | 139 of 250 |
| `openai` 1536 | unrelated | no | 0.038 | 0.183 | 0.384 | 0.089 | 0 of 250 |
| `openai512-cache` 512 | correction | yes | 0.319 | 0.630 | 0.826 | 0.121 | 162 of 250 |
| `openai512-cache` 512 | coexist | no | 0.704 | 0.789 | 0.902 | 0.060 | 250 of 250 |
| `openai512-cache` 512 | cross-subject | no | 0.350 | 0.642 | 0.814 | 0.165 | 149 of 250 |
| `openai512-cache` 512 | unrelated | no | 0.034 | 0.212 | 0.424 | 0.098 | 0 of 250 |

What each embedder does with the thresholds the project has used:

| embedder | at 0.60, the shipped value | at 0.70, ADR 0006's value | best possible threshold | coexist scores at or above a correction |
|---|---|---|---|---|
| `hashing` 256 | 6 correct, 365 wrong | 0 correct, 281 wrong | 0.91, accuracy 0.750 | 0.999 of cross pairs |
| `openai` 1536 | 154 correct, 389 wrong | 37 correct, 307 wrong | 0.91, accuracy 0.750 | 0.979 of cross pairs |
| `openai512-cache` 512 | 162 correct, 399 wrong | 52 correct, 328 wrong | 0.91, accuracy 0.750 | 0.955 of cross pairs |

- **The refutation transfers, and the shipped embedder is the worst case.** On all three, the correction family's median sits below the coexist family's minimum, the highest-accuracy threshold is the one that supersedes nothing, and every one of the 250 coexist pairs is retired by the shipped store. On `hashing` the inversion is near-total: a coexisting pair scores at or above a correction in 0.999 of the 62,500 cross pairs, against 0.955 at 512 and 0.979 at 1536. #334 is strengthened rather than qualified.
- **What the shipped store does with the shipped threshold is not what the OpenAI figure says.** At 0.60, `hashing` retires 6 of 250 corrections where `openai512-cache` retires 162. The mechanism on the embedder that ships catches 2.4% of the corrections it exists for while destroying every coexisting fact and 115 of 250 statements about a different person entirely.
- **The constant's own justification does not survive being asked of a distribution.** `ranking.go` cites 0.66 as where a correction lands on this embedder; #324 measured that on one pair. Across 250, the correction family's median is 0.309 and its 75th percentile 0.378 — 0.66 is not where corrections land but where the family's extreme sits, its maximum being 0.676. The coexist family's minimum on the same embedder is 0.667. A threshold low enough to catch the pair the constant was calibrated on is at or below every pair that must survive, so it retires all of them.
- **Why the hashing numbers fall where they do.** A correction in this dataset restates the fact in different words, so under a bag-of-tokens embedder it shares few tokens with what it corrects; two coexisting facts share a template and differ in one value, so they share almost all of them. Token overlap tracks phrasing, and supersession needs it to track fact identity.
- **This half of the record replays the old measurement rather than quoting it.** Running the committed round-4 512-dimension vectors through this harness reproduces `memory-creation`'s published separability figures exactly: correction 0.630 median and 0.121 IQR, coexist 0.789 and 0.060, cross-subject 0.642 and 0.165, unrelated 0.212 and 0.098, and 162 correct against 399 wrong retirements at 0.60. The pair set here is therefore the pair set there.
- **The OpenAI embedder is not reproducible to the last bit and the hashing one is.** The live 1536 run matches every committed 1536 figure except one: 307 wrong retirements at 0.70 against the record's 305, out of 750 pairs that must survive. Seventeen pairs sit within 0.005 of that threshold, so a boundary count moves between two runs months apart. `hashing` has no pair within 0.005 of it and returns the same vector for the same text by construction.

## Which conclusions may be quoted across embedders

| conclusion | quotable for the other embedder? |
|---|---|
| No similarity threshold separates corrections from coexisting facts (#334) | Yes. It holds on all three, and the best-accuracy threshold is 0.91 — supersede nothing — on all three. |
| The counts behind it: 162 corrections retired at 0.60 | No. That is 6 on the embedder that ships, a factor of 27. Quoting the OpenAI count for the shipped store overstates what the mechanism does by that much. |
| `ranking.go`'s "a correction restating a fact lands around 0.66" | No, and not for the hashing embedder either. It describes one pair, not the family, whose median is 0.309. |
| Retrieval quality where relevance decides the order (`flat`) | No. 0.799 against 0.982 recall@10 at 9,000 items, intervals apart at every size. Any weight or blend tuned under one belongs to that one. |
| Retrieval quality as the read path ships (`mixed-30d`) | Yes, in the only sense that matters here: both collapse, and the intervals overlap at all three sizes. |
| That the top ten is settled by age and salience before the query is read | Yes. Both embedders return ten memories averaging under seven hours old from a store averaging 361. |

## What this record does not say

- It recommends no embedder and no value for any constant. Which embedder ships is a decision, not a finding; sweeping the ranking constants is #411.
- It does not claim the ranking function's shape is wrong. What it reports is that two of the three terms decide the result on this corpus under both embedders, which is a finding #411 is told to escalate rather than implement.
- It does not measure cost in latency or money beyond the run itself. The `openai` figures come from one provider on one day.
- It says nothing about the write path. Whether a real agent would have stored these 9,000 facts is `memory-creation/`'s question.

## Limits

- **One corpus shape.** Facts are short single-clause statements averaging 30.7 characters, and one topic's facts differ only in their value. Longer or more varied memories would change both the relevance term and whether the token budget binds. The hashing embedder is the more exposed of the two to that choice, because token overlap is all it has.
- **Age is independent of relevance.** A workload where the answer is usually the newest memory would score higher under both embedders, and this corpus does not model one. The independence is deliberate: it is what keeps the comparison a comparison of embedders.
- **One seed.** Every stage is deterministic under `hashing`, so a second seed would redraw only which 120 pairs are probed — the variation the stated interval already carries.
- **The separability pairs are one experiment's dataset.** They are `memory-creation`'s, which is what makes the comparison with its published figures exact, and equally what bounds it: corrections there are restatements in fresh words, and a dataset of near-verbatim corrections would score the hashing embedder differently.
- **The OpenAI vectors are not committed.** They are 202 MB for this corpus; the run caches them outside the repository. The 512-dimension column replays `memory-creation`'s round-4 embedding cache, which no directory in this tree carries. What is committed is every figure derived from them, per-pair and per-probe, and the check that the replay reproduces the published numbers.
- **The results are not regenerated by `make ci`.** A full run is minutes of CPU and the `openai` half needs a key, so CI compiles the harness, runs its tests, and checks the committed record against the committed results; it does not re-measure. A change in `internal/memory` that moves these figures will not fail CI, and re-running is a manual step.
- **`ranking.go` is measured through its callers.** The harness reads its constants for the run header and calls the shipped `Store` for every supersession decision, but computes the pair cosine itself, because `internal/memory` does not export one. The two agree on all 1,000 pairs under every embedder, which is the check that the harness's cosine is the one the mechanism reads.

## Reproducing

- `go run ./docs/experiments/memory-retrieval/harness -seed 7` — the whole `hashing` measurement, retrieval and separability. Offline, no API key, no cost.
- `go run ./docs/experiments/memory-retrieval/harness -seed 7 -embedder openai -cache <path outside the repo>` — the same under the shipped OpenAI provider, with `OPENAI_API_KEY` already exported into the environment. The harness reads it from there and nowhere else: it takes no key flag, and the cache it writes holds vectors only.
- `go run ./docs/experiments/memory-retrieval/harness -seed 7 -embedder openai512-cache -measure separability -cache <round-4 .embed_cache.json>` — replays the committed 512-dimension vectors. Needs that cache, which this tree does not carry.
- `python3 docs/experiments/memory-retrieval/harness/pairs_from_round4.py` — rewrites the pair set from `memory-creation`'s own builder.
- `go test ./docs/experiments/memory-retrieval/harness` — the corpus, credit-rule, separability and interval tests, the check that `results/corpus_seed7.json` is what seed 7 generates, and the register that compares every figure above to the committed results.
