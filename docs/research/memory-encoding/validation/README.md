# Write-time encoding validation

Records issue #890's study. It measures the memory-encoding survey's write-time candidates (`../README.md` §Candidates for sherlock) against the shipped recall, offline and live, under the survey's test plan and `preregistration.md`:
- C, context at synthesis;
- P, an episode linked to the one it continues;
- F, standing facts written by the synthesis call.

- **No arm goes live, and no arm is named for implementation.** Under `preregistration.md`, an arm goes live only when its mechanism control passed and it beat S offline on its primary family by the margin. None did both:
  - **C** (context at synthesis) meets the margin: SC 39 of 50 against S's 0, p = 3.6e-12. But it fails two of its controls: its context line names the subject in 37 of 50 SC items (80% required), and its oracle is 420 of 505 (95% required).
  - **P** at `b4-prev` passes its control and reaches no chain (0 of 100, as S). `b4-k5` was not run: its similarity lookup could link at most 26 of 225 chain hop pairs.
  - **F** (standing facts) reaches UP in 2 of 50 against 0 (p = 0.5), and fails its control: 44 of 50 UP facts were typed `user`, 90% required.
- **The shipped recall stands, but not on a clean null.** C's and F's controls failed, so for them the run is undetermined; for P it is null.
- **What the offline stage does show:**
  - C's gain came through summaries that took in the neighbouring messages, against the prompt, and it puts a wrong value of the asked kind beside the query's entity in 24 of 30 NC items (S 0). That is the abstention risk the survey predicted.
  - L, the entity walk measured as reference, reaches 63 of 100 chains against S's 0, and has the associated near-miss in view for 54 of 60 absent queries (S 7). This matches #885.
  - No write-time arm reaches an implicit chain.

This file also holds the shared basis every arm was built on.

## Contents
- Layout
- The corpus
- Writing an arm
- Shared services
- Offline results
- Live admission
- Cost
- Verdict
- Limits
- Reproduction

## Layout

- `corpus.py`: `assemble` writes `candidates.json` and `corpus.json` (candidate stage); `select` keeps the candidates the check passes. `corpus_test.py` checks the corpus rules.
- `screen.py`: the free pre-screen on raw exchange text, before any synthesis (`evidence/screen.json`).
- `score.py`: `check`, `offline`, `live` and `tokens`. It extends #874's matching (`../../chained-recall/score.py`) with SC, UP, KU and NC, each arm's primary family, the live-admission margin and the decision rule. `score_test.py` tests it.
- `preregistration.md`: fixed before the first model call.
- `arms/<name>/`: one command per arm.
  - This issue writes `arms/shipped/` (S) and `arms/facts/` (F).
  - `arms/context/` (C, #891) and `arms/prev/` (P, #892) are the other workers'.
- `evidence/`:
  - `screen.json`;
  - `synthesis/stream-b4.jsonl`, the shipped call's summaries of the stream;
  - `selection/check-b4.json`, the check over the candidates;
  - `check-b4.json` and `check-summary.json`, the check over the selected corpus;
  - `offline/<arm>/<setting>/` and `offline/scores.json`; no arm was admitted live, so there are no live records;
  - `ledger/`, every model call, and `tokens.json`.

## The corpus

`python3 corpus.py assemble` writes every candidate into one stream of 4,128 messages after the replication's 4,800 (1,032 batches of four). Seed 890, #874's chain templates and #885's rules (`../../chained-recall/corpus.py`, `../../chained-recall/confirm/corpus.py`).

| Family | Candidates | Kept | What a probe asks |
|---|---:|---:|---|
| C1, C2, C3 | 30, 70, 70 | 30, 50, 50 | a value reached through k hops, as #885 |
| IC2, IC3 | 30 each | 25 each | an artifact that must obey a rule reached through k hops, as #885 |
| SC | 70 | 50 | a subject's value, stated in the exchange after the one naming the subject |
| UP | 106 | 50 | a task that needs a standing fact about the user without naming it |
| E, K | — | 30, 40 | the replication's exact and correction queries, as #885 |
| KU | 30 | 30 | who holds a role for the user now, after a change |
| N | 60 | 60 | an absent value, whose associate's value is stated, as #885 built it |
| NC | 30 | 30 | an absent value, where the exchange after the subject's introduction states someone else's |

- **Each exchange is four messages and fills one batch alone.** Two exchanges of one item sit at least 16 exchanges apart, in random order.
  - The exceptions: an SC or NC introduction is the exchange right before its second, so it lies within the eight messages C reads, and a KU change follows its first statement.
- **Where evidence lives.** SC and UP are in `chains`, each with one hop: the evidence, or the fact. `intro` holds an SC item's introduction. KU, N and NC are in `guards`, with `sources` naming their exchanges.
- **Selection.** After the shipped call has summarised the stream, `harness check` over the candidates writes `evidence/selection/check-b4.json`. `corpus.py select` keeps, in candidate order, the first candidates whose needed sources the raw query ranks beyond the top 20: every chain hop but the first, the SC evidence, the UP fact.
- **UP templates.** Each fact is a slot of its own (`corpus.py` `STANDING`), so no two contradict.
  - The first draft stated each fact alone, and the facts sat too close to their tasks: the pre-screen (`screen.py`) passed 19 of 60. Framing the facts apart from their tasks passed 37 of 75.
  - Each fact now comes as an aside after an unrelated opening sentence (`OPENINGS`), as such facts come up in conversation, and 31 slots were added. All of this was settled before any call. `evidence/screen.json` is the pre-screen of the committed candidates.

## Writing an arm

An arm is a command under `arms/<name>/` whose main is `study.Main(New())`, with `study` the chained-recall package (`../../chained-recall/study/`; its `arm.go` doc comments are the contract). Everything `../../chained-recall/README.md` §Writing an arm says holds, with these differences:

- **Every command takes `-dir docs/research/memory-encoding/validation`.** Without it, `study.Main` reads #874's corpus. Records then land under this directory's `evidence/offline/<arm>/<setting>/` and `evidence/live/<arm>/<setting>/`.
- **Ports.** `-embed-url http://127.0.0.1:8100/v1` for the local TEI, and `-proxy http://127.0.0.1:8101` for the proxy (§Shared services).
- **The shared store.** `env.SharedStore(4)` is the production store over base and stream from the recorded summaries. It is built into a worker's own `-work` directory with `harness build -batch 4 -dir <this dir> -work <scratch>`, free, once `evidence/synthesis/stream-b4.jsonl` is committed.
- **Base and stream.** An arm adds its structure to the stream only, and the base's 1,200 mids keep their recorded summaries (`preregistration.md` §Arms).
  - The corpus's `stream` is what an arm synthesises: 1,032 batches, `env.Corpus.Stream` in groups of four.
  - The messages before a batch are `env.Corpus.Messages()`.
- **Names and settings.** The arm names are `S`, `C`, `P`, `F` and `L`, and the settings are those `preregistration.md` fixes: `b4`, `b4-w8`, `b4-prev`, `b4-k5`, `b4-model`. `score.py` maps an arm's name to its primary family.
- **Paid phases.** Name them by phase in `env.Model(episode, phase)`, so `score.py tokens` counts them. Run one at a time on the shared key, with at most two requests at once.
- **The proxy caches synthesis responses.** It keys on the request, and caches every response whose `memory_summary` summary is well-formed (and, for L, whose `entities` is an array of strings). A malformed extra field is therefore cached, and every identical retry returns it. An arm whose extended call can come back malformed varies the request on retry.
- **Scoring.** `python3 score.py offline` reads every arm's records under `evidence/offline/`. An arm's controls live in its own directory.

## Shared services

Run by #890's worker for all three workers:
- **Embedder:** text-embeddings-inference 1.9.4 image, `BAAI/bge-base-en-v1.5` at revision `a5beb1e3e68b9ab74eb54cfd186867f64f240e1a`, container `tei-890` on `127.0.0.1:8100`.
- **Proxy:** `harness proxy` of `../../chained-recall/harness`, on `127.0.0.1:8101`, ledger `evidence/ledger/`, ceiling USD 30, `MINIMAX_KEY` in its environment only. On HTTP 429 code 2056 it pauses every request and probes at most every 30 minutes, and `/budget` reports `"paused": true`.

## Offline results

`python3 score.py offline` over every arm's records (`evidence/offline/scores.json`), local embedder only. Reach per family: a chain when every hop's mid is delivered stating both entities; SC when the evidence mid is delivered stating subject and value; UP when the fact's mid (or a fact item over it) states the fact's word; KU when the change's mid states the new name; E and K as #874. For N and NC, the items with a value of the asked kind that is not the answer in view, and beside the query's entity.

| Family (n) | S | C `b4-w8` | P `b4-prev` | F `b4` | L `b4-model` (reference) |
|---|---:|---:|---:|---:|---:|
| C2 (50) | 0 | 0 | 0 | 0 | 42 |
| C3 (50) | 0 | 0 | 0 | 0 | 21 |
| IC2 (25) | 0 | 0 | 0 | 0 | 0 |
| IC3 (25) | 0 | 0 | 0 | 0 | 1 |
| SC (50) | 0 | **39** | 0 | 0 | 0 |
| UP (50) | 0 | 1 | 1 | 2 | 0 |
| C1 (30) | 27 | 24 | 27 | 29 | 30 |
| E (30) | 26 | 27 | 26 | 26 | 27 |
| K (40) | 35 | 35 | 35 | 35 | 34 |
| KU (30) | 27 | 20 | 26 | 29 | 25 |
| N, near-miss in view (60) | 7 | 6 | 6 | 7 | 54 |
| N, near-miss beside the entity | 0 | 0 | 0 | 0 | 0 |
| NC, near-miss in view (30) | 0 | 24 | 17 | 0 | 0 |
| NC, near-miss beside the entity | 0 | **24** | 0 | 0 | 0 |
| Context characters per turn | 2,943 | 3,990 | 2,935 | 5,236 | 3,049 |
| Stored characters per source character | 0.745 | 0.933 | 0.745 | 0.853 | 0.826 |
| Vectors | 2,232 | 2,232 | 2,232 | 3,971 | 2,232 |
| Oracle, hops within 5 (505) | 493 | 420 | 483 | 493 | 505 |

- **C** reaches SC through its evidence mids: the summary names the subject in 49 of 50 of them, the context line in 37 (`arms/context/README.md`). Its summaries grew from 171.5 to 268.7 characters and restate earlier exchanges, which lowers the oracle and costs KU (8 lost, 1 gained, p = 0.039).
- **P** delivers the SC evidence mid beside its introduction in 21 of 50 items, but the evidence summary never names the subject, so none counts as reached. Its thread links reach no chain hop, whose exchanges sit 16 or more apart (`arms/prev/README.md`).
- **F**'s profile block holds the ten newest user facts; the old UP facts never enter it (0 of 50 in the profile), and the three facts nearest the task hold the UP fact in 2 (`arms/facts/README.md`).
- **L**, run exactly as #885 ran it with this corpus's stream entities, repeats #885's offline chain reach (58 there) and its absent-query exposure: its walk puts the associated entity's value in view for 54 of 60 N queries.
- **Compression.** No arm compresses. C stores 0.188 more characters per source character than S, F 0.108 (facts and their vectors), L 0.081; P adds link rows only.

## Live admission

Applied as `preregistration.md` §Offline stage fixes it: a setting goes live when its mechanism control passed and its primary reach exceeds S's by at least 10% of the family with exact McNemar p < 0.05.

| Setting | Primary family | Arm vs S | p | Margin | Mechanism control | Live |
|---|---|---:|---:|---|---|---|
| C `b4-w8` | SC | 39 vs 0 | 3.6e-12 | met | failed: subject in context 74% (80%), oracle 83.2% (95%) | no |
| P `b4-prev` | C2, C3 | 0 vs 0 | 1.0 | not met | passed | no |
| P `b4-k5` | C2, C3 | not run | — | — | failed by ceiling: 26 of 225 hop pairs (50%) | no |
| F `b4` | UP | 2 vs 0 | 0.5 | not met | failed: user facts 88% (90%) | no |

With no arm admitted, the live stage has nothing to compare with S, and no live call was made. The live scorer (`score.py live`) is in place and tested but has no records.

## Cost

MiniMax-M2 only, through the one proxy (`evidence/ledger/`, `python3 score.py tokens`); every embedding was local.

| Phase | Calls | HTTP 429 | Input tokens | Output tokens |
|---|---:|---:|---:|---:|
| Shipped stream synthesis (S) | 1,049 | 17 | 337,684 | 211,993 |
| F's facts | 1,046 | 13 | 504,324 | 448,546 |
| C's context synthesis | 1,034 | 1 | 538,367 | 505,564 |
| P's links, `b4-prev` | 1,052 | 20 | 459,890 | 390,273 |
| L's stream entities | 1,060 | 23 | 402,558 | 244,653 |
| **Total** | **5,241** | **74** | **2,242,823** | **1,801,029** |

- Every 429 was code 2062, the plan's rate limit, and recovered within the proxy's retries. The 2056 usage pause never engaged.
- Nothing was bought. At list prices the calls come to about USD 2.8, a guard only (`/budget` read USD 2.42 before L's phase).
- Write-call tokens per batch against S's 327 input and 205 output: F 489 and 435, C 521 and 489, P +118 and +173, L 390 and 237 (last successful call per batch for C and P, from their READMEs; F and L from the phase totals above).

## Verdict

- **No arm is named.** None was admitted to the live stage, so none can be valid under the decision rule.
- **The null is not clean.** The rule lets the shipped structure stand on a null only when every control passed. C and F each failed a control, so the run cannot say whether C's context line or F's facts would have been valid had their mechanisms run as specified. P's control passed, and P reaches nothing S does not: for P the answer is null.
- **What the measurements establish outside the rule:**
  - Write-time context from the neighbouring messages reaches a situated value the shipped recall cannot (C: SC 39 of 50 against 0), but on MiniMax-M2 the summaries themselves absorb the neighbours, and the same material puts wrong values beside the query's entity for absent questions (24 of 30).
  - Linking an episode to the previous one or to its 5 nearest earlier ones does not reach chains whose hops are far apart (P); a walk over the names both hops state does (L, 63 of 100), with #885's abstention exposure (54 of 60 near-misses in view).
  - A bounded profile of the newest user facts does not bring back old standing facts; similarity over short facts rarely does (F, 2 of 50).
- **Next, if the owner wants one:** C's context line alone, with S's summaries kept, would separate the context line from the absorbing summaries; the corpus, shared store and scorer here support it offline at one synthesis phase.

## Limits

- **Offline only.** No arm was admitted, so nothing here measures what the agent applies. Offline reach counts delivery with the values stated, and #874 and #885 found applying follows seeing, but that was not re-measured.
- **One model, one run, a templated corpus.** MiniMax-M2 at a provider alias; invented names reused exactly. UP facts come as asides after unrelated openings, which the check required and which real conversation may not match.
- **The stream only.** The base's 1,200 mids keep their recorded summaries with no arm structure, so E and K change only by what stream mids displace.
- **C's and P's settings kept or replaced summaries differently.** C stores its own summaries; P and F keep S's. C's gain and its oracle loss cannot be split between its context line and its summaries.
- **Controls fixed per arm.** C's, P's and F's controls were preregistered by their workers or here before their calls; the verdict above follows them as written.

## Reproduction

From the repository root, with Docker, Go 1.26 and Python 3:

```sh
docker run -d --name tei-890 -p 127.0.0.1:8100:80 \
  ghcr.io/huggingface/text-embeddings-inference@sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d \
  --model-id BAAI/bge-base-en-v1.5 --revision a5beb1e3e68b9ab74eb54cfd186867f64f240e1a
python3 docs/research/memory-encoding/validation/corpus.py assemble        # byte-identical candidates.json
python3 docs/research/memory-encoding/validation/screen.py <scratch>       # evidence/screen.json; free
D=docs/research/memory-encoding/validation
go run ./docs/research/chained-recall/harness synthesize -batch 4 -dir $D -proxy http://127.0.0.1:8101 -embed-url http://127.0.0.1:8100/v1   # paid
go run ./docs/research/chained-recall/harness build -batch 4 -dir $D -work <scratch> -embed-url http://127.0.0.1:8100/v1
go run ./docs/research/chained-recall/harness check -batch 4 -dir $D -work <scratch> -embed-url http://127.0.0.1:8100/v1
python3 $D/corpus.py select                                              # evidence/selection/check-b4.json is the check above, copied
go run ./docs/research/chained-recall/harness check -batch 4 -dir $D -work <scratch> -embed-url http://127.0.0.1:8100/v1   # again, on the selected corpus
python3 $D/score.py check
go run ./$D/arms/shipped build -dir $D -work <scratch> -embed-url http://127.0.0.1:8100/v1   # then offline; likewise arms/facts, arms/context, arms/prev
go run ./docs/research/chained-recall/arms/links build -setting b4-model -dir $D -work <scratch> -proxy http://127.0.0.1:8101 -embed-url http://127.0.0.1:8100/v1   # then offline
python3 $D/score.py offline
python3 $D/score.py tokens
python3 $D/arms/facts/controls.py
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s $D -p '*_test.py'
python3 -B $D/mutation.py
```

- Every paid phase (`synthesize`, each arm's `build`) resumes after its last record, so re-running one against the committed evidence sends nothing.
- `arms/links/evidence/entities-b4.jsonl` starts as #874's 1,200 base rows, copied unchanged, including the failed batch 509–512; the stream's 1,032 rows were written by phase 3.
- **Revisions:** the basis at `11a3931`, S and F at `165bef8`, the selected corpus at `cdb2758`, C at `deba2f7`, P at `f5a98e2`, L's reference and the ledger at `e24191b`.
