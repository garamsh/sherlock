# Memory-retrieval experiment

What the read path returns for a task statement, measured against the memory implementation this project ships.

## Method

Every other experiment in this tree reimplements the idea under test in Python, so its numbers describe a model of the design rather than the code. This one plants a corpus into the store `internal/memory` ships and queries it through the service that ships: `memory.NewService` over `internal/memory/sqlite`, with the `hashing` embedder that is the shipped default. The harness supplies the corpus and a fixed clock; the store, the embedder, the hybrid search and the ranking function are the ones that run in production, called through `Search`, `Recall` and `Similar`. It is a Go command because that is the shape that can call the package directly — a harness in another language would have to re-describe the thing it is measuring, which is the gap this experiment exists to close.

The facts are written with the store's own insert rather than through `memory.Service.Store`. Supersession on insert archives a new memory's most similar predecessor at cosine 0.6 and above, which would silently remove planted facts and leave the ground truth describing a store that no longer holds them. The write path is `memory-creation/`'s subject and the threshold is #334's; this experiment measures the read.

The run is offline. No API key is read, no network call is made, and every stage — corpus, embedder, store, ranking — is deterministic given the seed.

### The corpus

`results/corpus_seed7.json` is the planted store, deterministic from seed 7 and rebuilt by `harness/corpus.go`:

- 9,000 facts over 1,800 subject-topic pairs: 90 subjects, 20 topics, each pair holding 3 to 7 facts drawn from that topic's values.
- 120 of those pairs are probed. Their 605 facts are the ground truth, and all of them sit inside the first 1,000 items, so a larger store is the same store with more distractors rather than a different one.
- Every fact is a hard negative for some probe. The full store holds all 1,800 pairs, so a probe's subject appears under 19 other topics and its topic under 89 other subjects and neither alone selects an answer; the smaller stores are prefixes of the same order and hold a subset of those distractors.
- A probe is a task statement, not a copy of the fact it looks for. The shipped recall step queries with the frame's task statement, so the query shares the topic's head word with its answers and differs elsewhere.
- Salience is drawn from the three values `internal/gateway/writeback.go` derives from a writeback op — 0.5, 0.7, 0.8 — and ages are drawn over a 30-day window. Both are drawn independently of whether a fact answers a probe, so the ranking function's salience and recency terms carry no information about the answer. A corpus that correlated them would be measuring itself.

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

`flat` leaves the ranking function adding the same salience and the same recency to every row, so the order it produces is the order of its relevance term alone. It is the control that says whether the corpus is retrievable at all.

### The interval

Every figure is a mean over the 120 probes, and every figure carries the 95% half-width computed from the sample standard deviation of the per-probe values. `memory-creation/harness/round4/precision.py` could only approximate this from family means and treated fractional scores as Bernoulli; here each probe's own value is committed in `results/retrieval_seed7.json`, so the interval comes from them directly. One approximation remains and it widens rather than narrows: the probes share one store, so their errors are if anything positively correlated.

## What the current configuration scores

`recall@k` is planted answers returned over answers planted; `precision@k` is planted answers returned over memories returned. `@10` is `DefaultSearchLimit`, the shipped cap on what one search returns. Best rank is where the highest-ranked answer sits in the whole ranked store, so it reads against the store size beside it.

| planting | store | recall@1 | precision@1 | recall@5 | precision@5 | recall@10 | precision@10 | best rank |
|---|---|---|---|---|---|---|---|---|
| `mixed-30d` | 1,000 | 0.030 ±0.013 | 0.150 ±0.064 | 0.063 ±0.021 | 0.058 ±0.019 | **0.092** ±0.024 | 0.044 ±0.011 | 29.6 ±5.8 |
| `mixed-30d` | 4,000 | 0.018 ±0.010 | 0.092 ±0.052 | 0.042 ±0.018 | 0.040 ±0.016 | **0.056** ±0.020 | 0.027 ±0.009 | 103.2 ±21.1 |
| `mixed-30d` | 9,000 | 0.013 ±0.009 | 0.067 ±0.045 | 0.024 ±0.014 | 0.025 ±0.014 | **0.037** ±0.017 | 0.018 ±0.008 | 242.9 ±49.8 |
| `mixed-360d` | 1,000 | 0.008 ±0.007 | 0.042 ±0.036 | 0.157 ±0.029 | 0.147 ±0.027 | 0.478 ±0.049 | 0.235 ±0.026 | 10.9 ±3.4 |
| `mixed-360d` | 4,000 | 0.003 ±0.004 | 0.017 ±0.023 | 0.008 ±0.007 | 0.008 ±0.007 | 0.038 ±0.017 | 0.018 ±0.008 | 37.7 ±13.4 |
| `mixed-360d` | 9,000 | 0.003 ±0.004 | 0.017 ±0.023 | 0.004 ±0.004 | 0.005 ±0.006 | 0.008 ±0.007 | 0.004 ±0.004 | 86.8 ±32.9 |
| `flat` | 1,000 | 0.189 ±0.019 | 0.833 ±0.067 | 0.775 ±0.060 | 0.735 ±0.060 | 0.875 ±0.059 | 0.433 ±0.039 | 10.4 ±4.7 |
| `flat` | 4,000 | 0.175 ±0.021 | 0.767 ±0.076 | 0.726 ±0.067 | 0.678 ±0.066 | 0.807 ±0.071 | 0.392 ±0.042 | 35.1 ±17.5 |
| `flat` | 9,000 | 0.174 ±0.021 | 0.758 ±0.077 | 0.712 ±0.068 | 0.663 ±0.067 | 0.799 ±0.070 | 0.388 ±0.041 | 76.1 ±41.8 |

## What the numbers say

- **The corpus is retrievable and the relevance term retrieves it.** `flat` returns 0.875 of the planted answers in the top ten at 1,000 items and 0.799 at 9,000, on the same probes, the same facts and the same ranking function as every other row. Whatever the other rows measure, it is not a corpus nobody could search.
- **With ages and saliences spread, the top ten stops answering the query.** `mixed-30d` returns 0.092 of the planted answers in the top ten at 1,000 items and 0.037 at 9,000, where one returned memory in 57 is an answer. The gap to `flat` is far outside both intervals at every size.
- **The answers are ranked well and the window is narrow.** In `mixed-30d` at 9,000 the best answer sits at rank 243 on average — the top 2.7% of the store, against about 1,490 if the order were random — while `DefaultSearchLimit` shows the top 0.11%. The ranking is not blind to the query; the ten rows it returns are settled before the query is consulted.
- **What fills those ten is age and salience.** At 9,000 items the ten memories returned average 6.8 hours old against a store averaging 361, and carry salience 0.72 against the store's 0.63. A memory written in the last hour scores its salience plus a recency term near 1.0 before any relevance is added, and the relevance term is bounded by 1.0 in total.
- **The age window moves the figure, and not monotonically.** Stretching the same draws twelvefold takes recall@10 at 1,000 items from 0.092 to 0.478, because almost nothing is then young enough for recency to matter; at 9,000 items enough is, and the same stretch takes it from 0.037 to 0.008. A corpus parameter that no constant in `ranking.go` mentions moves the figure by many times either interval, which is why the window is stated here rather than assumed.
- **Store size costs recall.** `mixed-30d` falls from 0.092 to 0.037 between 1,000 and 9,000 items, which the intervals separate. `flat` falls from 0.875 to 0.799 over the same range, which they do not — the probes are the same at both sizes, so a paired comparison could be sharper than these unpaired intervals, and this run does not make one.
- **An empty noise floor is not enough for precision.** ADR 0006 records as a consequence that "retrieval is precise because the noise floor is empty". This store holds nothing but curated facts — no chatter, no auto-capture — and precision@10 is 0.018 at 9,000 items. Curation removes one cause of imprecision; on this corpus it does not deliver the consequence on its own.
- **The recall token budget never binds.** `Recall` takes its candidates from a search capped at `DefaultSearchLimit`, so it returns at most ten memories whatever budget it is given; the 512-token budget can only trim below that, and only when the ten average more than 204.8 characters. The corpus averages 30.7. `returned@budget` is exactly 10.0 in all nine rows, and `recall@budget` equals `recall@10` in all nine.

## What this record does not say

- It recommends no value for any constant. `WeightVector`, `WeightFTS`, `RecencyHalfLife` and `DefaultRecallTokenBudget` are unchanged and unmeasured here; sweeping them is #411, and comparing embedders is #410. Both were blocked on this harness.
- It does not claim the ranking function's shape is wrong. What it reports is that two of the three terms decide the result on this corpus, which is a finding #411 is told to escalate rather than implement.
- It says nothing about the write path. Whether a real agent would have stored these 9,000 facts is `memory-creation/`'s question.

## Limits

- **One corpus shape.** Facts are short single-clause statements averaging 30.7 characters, and one topic's facts differ only in their value. Longer or more varied memories would change both the relevance term and whether the token budget binds.
- **Age is independent of relevance.** A workload where the answer is usually the newest memory would score higher, and this corpus does not model one. The independence is deliberate: it is what makes the figures a measurement of the read path rather than of the corpus.
- **One seed.** Every stage is deterministic, so a second seed would redraw only which 120 pairs are probed — the variation the stated interval already carries. The memory-creation rounds needed replication because a model decided their outcomes; nothing here is stochastic at run time.
- **The results are not regenerated by `make ci`.** A full run is minutes of CPU, so CI compiles the harness, runs its tests, and checks the committed record against the committed results; it does not re-measure. A change in `internal/memory` that moves these figures will not fail CI, and re-running is a manual step.
- **`ranking.go` is measured through its callers.** The harness reads its constants for the run header but never calls `score` directly, so a figure here can only be attributed to the ranking function as a whole.

## Reproducing

- `go run ./docs/experiments/memory-retrieval/harness -seed 7` — plants and measures every planting at every store size, rewriting `results/`. Offline, no API key.
- `go test ./docs/experiments/memory-retrieval/harness` — the corpus, credit-rule and interval tests, the check that `results/corpus_seed7.json` is what seed 7 generates, and the register that compares every figure above to the committed results.
