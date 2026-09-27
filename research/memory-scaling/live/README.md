# K against the shipped recall at long-term scale

Records issue #913's free screen: whether K could be no worse than the shipped recall on a store grown to 3.67× the replication's messages, before any live answer run. K ranks batch-4 keys and delivers the batch-16 summaries they fall in, with recency kept at batch 4 (Kr).

**The paid phase was cut. No MiniMax call was made, so no arm is named, and the shipped recall stands.**

- **Delivery passes.** At 3.67×, Kr delivers a source interval holding the evidence for 197 of 280 primary queries. S4 (the shipped recall) delivers 201 and budgeted B4 203. Both lower bounds are inside the −0.05 margin. This part reproduces #901's K result exactly, as the preregistration expected.
- **Stated fails.** The PM required a second gate before the go (Amendment 1): a delivered text must also state the gold value. Kr states it for 187 queries, against 197 for S4 (interval [−0.073, +0.009]) and 201 for B4 (interval [−0.081, −0.009], p = 0.016). Exact also loses 5 net against B4, beyond the allowance of 3.
- **Why.** The batch-16 summaries drop the value. For 19 primary queries, Kr delivers the batch-16 summary covering the evidence, but no delivered text states the value. S4 and B4 have 9 such queries each. Of Kr's 22 stated losses against B4, 13 are of this kind.

## Contents
- Question
- Method
- Controls
- Results
- Decision
- Cost
- Limits
- Reproduction

## Question

#901 found K no worse than B4 on evidence delivery at 3.67× while storing 59% less replication text (`../README.md`). #913 asks whether K is no worse than the shipped recall with the model answering. The live run was gated on a free screen, so that no paid call is spent where the screen already says K cannot pass.

## Method

The [preregistration](preregistration.md) fixed the arms, the scale, K's recency rule, the endpoints, the margin, the guardrails, the controls, the gate and the paid-phase order. It was committed as `8f45edd` at 00:04:17 +09:00 on 2026-09-28.

- **Commit order.**
  - The harness `5cde48c` (00:06:12) came before the first ranking. The delivery evidence `705222e` came after it.
  - Amendment 1 `17bfe5d` (00:21:13) was committed before any stated measure was computed, and its harness `dbe83b9` (00:22:06) before its run. The stated evidence is `9a200cd`.
- **Material.** #901's, read through its harness by import (`../study.py`), unchanged:
  - the #856 replication's recorded B4 and B16 syntheses;
  - the 280 primary and 30 absent queries;
  - the four distractor streams' B4 mids after #901's leak check;
  - `BAAI/bge-base-en-v1.5` at revision `a5beb1e` on this study's own text-embeddings-inference 1.9.4 instance, `tei-913`, run with `--memory 1g`.
- **Scale.** s4 only: 17,600 messages, 3.67×. s0 is ranked only for the budget and the reproduction control.
- **Arms.**

  | Arm | Replication stored as | Delivery |
  |---|---|---|
  | S4 | B4 | The shipped recall: 2 newest, then top 7 similar, at most 7 delivered; no budget |
  | B4 | B4 | The 2 newest, then cosine, under budget C = 3,383.55 characters |
  | Kr | B16 values over B4 keys | The 2 newest batch-4 units, then K's value order, under C |

- **Delivery screen.** A query is a hit when an evidence source lies inside a delivered interval. Guardrails:
  - exact hits;
  - correction hits;
  - stale corrections: an obsolete source delivered and no evidence source;
  - absent queries with a near-miss source delivered.
- **Stated screen (Amendment 1).** A query is stated when a delivered text states the gold value. The matcher is the live scorer's (`../../chained-recall/score.py` `states`): the `key` for exact, paraphrase and implicit, and the `new_key` for correction. Its guardrails are exact stated and correction stated. They also include stale-stated (the old value stated without the new one) and absent near-miss stated.
- **Gate, for each screen.**
  - Kr's paired 95% interval on the 280 has a lower bound of at least −0.05 against both S4 and B4.
  - Each guardrail loses at most 3 net queries.
  - Every control passes.

## Controls

From [scores.json](evidence/scores.json) `controls` and `controls_pass`, and [screen.json](evidence/screen.json).

| Control | Result |
|---|---|
| S4 at s0 reproduces #901 | 212 of 280, with intervals identical to `../evidence/rankings.jsonl` on 314 of 314 queries. C = 3,383.55, equal to #901's `study.json` |
| S4, B4 and Kr at s4 equal #901's S4, B4 and K | 314 of 314 each. Kr equals K because at s4 both take the same distractor B4 mids as recency |
| Reach | Every primary evidence source lies in a unit of Kr's store at s0 and s4 |
| Kr's recency is batch-4 sized | Every recency unit Kr delivered spans 4 sources, at s0 (the replication's own newest B4 mids) and at s4 |
| Server | `tei-913` `/info`: `BAAI/bge-base-en-v1.5`, `a5beb1e3e68b9ab74eb54cfd186867f64f240e1a` |
| Stated rerun reproduces deliveries | Rerunning from the embedding cache, with the server stopped, reproduced every field of the 1,884 recorded rows that the delivery screen had scored. The check compared each row's `scale`, `arm`, `query`, `delivered`, `chars`, `rank` and `states_near_miss` against `git show 705222e:research/memory-scaling/live/evidence/rankings.jsonl` |
| Mutation checks | [mutation.txt](evidence/mutation.txt): 8 of 8 mutations fail a test. They cover hit, stale, near-miss, Kr's batch-4 recency, margin, allowance, stated and stale-stated |

## Results

At s4, over 280 primary queries, 70 per family, with 30 absent queries ([scores.json](evidence/scores.json) `table`):

| Arm | Hit | Hit by family (exact, paraphrase, implicit, correction) | Stated | Stated by family | Hit and the covering unit states it | Stale | Stale-stated | Absent near-miss delivered | Absent near-miss stated | Replication stored per source char |
|---|---:|---|---:|---|---:|---:|---:|---:|---:|---:|
| S4 | 201 | 61, 49, 25, 66 | 197 | 60, 50, 23, 64 | 192 | 2 | 1 | 16 | 18 | 0.684 |
| B4 | 203 | 62, 51, 24, 66 | 201 | 62, 52, 23, 64 | 194 | 3 | 2 | 17 | 19 | 0.684 |
| Kr | 197 | 61, 48, 22, 66 | 187 | 57, 47, 18, 65 | 178 | 4 | 3 | 15 | 19 | 0.280 |

Recall before the budget, @1, @5 and @20: S4 and B4 0.450, 0.718 and 0.814; Kr 0.489, 0.757 and 0.850. Mean delivered characters: S4 2,942; B4 3,312; Kr 3,309.

### Gate

| Screen | Against | Kr | Other | Wins | Losses | 95% interval | p | Margin | Guardrails, net losses (exact, correction, stale, absent) | Holds |
|---|---|---:|---:|---:|---:|---|---:|---|---|---|
| Delivery | S4 | 197 | 201 | 8 | 12 | [−0.044, +0.020] | 0.50 | holds | 0, 0, 2, −1 | yes |
| Delivery | B4 | 197 | 203 | 4 | 10 | [−0.042, +0.008] | 0.18 | holds | 1, 0, 1, −2 | yes |
| Stated | S4 | 187 | 197 | 12 | 22 | [−0.073, +0.009] | 0.12 | **fails** | 3, −1, 2, 1 | no |
| Stated | B4 | 187 | 201 | 8 | 22 | [−0.081, −0.009] | 0.016 | **fails** | **5**, −1, 1, 0 | no |

- **Where Kr loses the stated value.** Of Kr's 22 stated losses against B4, 13 are queries where Kr delivered the B16 value covering the evidence and no delivered text states the value. The other 9 are ranking or budget misses. By family the losses are 8 implicit, 7 paraphrase, 6 exact and 1 correction.
- **Compression in general.** Across the 280, the hit is delivered but the value unstated for 19 queries under Kr, against 9 each under S4 and B4 (from `per_query_s4`).
- **The ranking is not the loss.** Kr's value ranking has the highest recall before the budget. What costs it is the text of the value it delivers.

## Decision

- **Delivery gate: passes** against S4 and B4, with every guardrail inside its allowance.
- **Stated gate: fails.** Kr's lower bound is below −0.05 against both S4 and B4. Against B4 the loss is significant (p = 0.016), and exact loses 5 net, beyond the allowance of 3.
- **Paid phase: cut.** Per Amendment 1 and the PM's go, which was conditional on this gate, no paid call was made.
- **Verdict.** K is not named. The shipped recall stands. No production change follows, so no store, synthesis, recall or migration estimate is given.

## Cost

- **MiniMax: none.** No key was read, no proxy was started, and no paid API was called. The paid-phase estimate the PM approved (about 8.05M input and 0.40M output tokens) was not spent.
- **Embeddings.** 4,620 texts (314 queries, 1,200 replication B4 summaries and 3,106 distractor summaries) were embedded in batches of 16 on `tei-913` (`127.0.0.1:8132`, `--memory 1g`, `--max-batch-tokens 4096 --max-client-batch-size 16`). The container was stopped after the run. Batch limits change no vector used here: control 1 reproduces #901's deliveries exactly.
- **Tokens per phase:** delivery screen 0, stated screen 0, paid phases 0 (not run).

## Limits

- **The screen is offline.** Stated means a delivered text contains the value, not that the model uses it. A live run could only lower applied below stated, so it could not recover what the batch-16 summaries dropped.
- **One synthesis.** The B16 summaries are #856's single recording. A synthesis prompt that preserved values could change the stated count. That would be a new study, not a re-read of this one.
- **Distractors are batch 4 in every arm**, as in #901, and the matched B8 measurement there found no granularity bias.
- **At s4, K's recency rule is not exercised.** Recency units are distractor mids in every arm, so the delivery screen reproduces #901's K and adds only the S4 comparison and the guardrails. The stated screen is where #913 adds information.
- **The matcher is mechanical.** An implicit key such as "check diaper temperature" is stated only when every word appears. This applies the same way to every arm.

## Reproduction

From the repository root, with Docker and `uv`:

```sh
IMG=ghcr.io/huggingface/text-embeddings-inference@sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d
docker run -d --name tei-913 --memory 1g -p 127.0.0.1:8132:80 $IMG \
  --model-id BAAI/bge-base-en-v1.5 --revision a5beb1e3e68b9ab74eb54cfd186867f64f240e1a \
  --max-batch-tokens 4096 --max-client-batch-size 16
uv run --no-project --with numpy==2.3.3 python3 research/memory-scaling/live/screen.py   # writes evidence/rankings.jsonl and screen.json
docker rm -f tei-913
python3 research/memory-scaling/live/score.py      # offline; writes evidence/scores.json
python3 research/memory-scaling/live/mutation.py   # writes evidence/mutation.txt
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s research/memory-scaling/live -p '*_test.py'
```

- `screen.py` caches embeddings and the server's `/info` in `.embed_cache-913*.json` beside itself. `.gitignore` excludes them. A rerun from the cache needs no server.
- `score.py` reads #901's `../evidence/rankings.jsonl` and `study.json` for the reproduction controls.
