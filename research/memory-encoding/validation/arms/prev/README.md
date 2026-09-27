# Arm P: episodes chained on earlier episodes

Records arm P of issue #890's validation study (#892): survey candidate P (`../../../README.md` §P), built on the shared interface (`../../../../chained-recall/study/arm.go`) and measured on the study's corpus (`../../README.md`).

## Method

- **Settings.** `b4-prev` gives each stream batch's synthesis call the mid just before its own; `b4-k5` gives it the 5 earlier mids the production search ranks first for the batch's text. Both copy the shared batch-4 store.
- **Synthesis** (`synthesis.go`, fixed in `preregistration.md` beside this file). The production call with the earlier episodes added to its input, each labelled "earlier episode, for reference, do not restate", and a required `continues`: a boolean at `b4-prev`, a list of the given IDs at `b4-k5`. The returned summary is checked as production checks one, then discarded, so the mid keeps its recorded summary and P differs from S in the links alone.
- **Only the stream is synthesised.** The base's mids link to nothing (`../../preregistration.md` §Arms).
- **Retries.** A retry appends " (Attempt n.)" to the prompt, so the proxy's cache cannot answer it with the malformed `continues` it cached before. A batch whose three attempts are all malformed is recorded as failed and keeps no link.
- **Store.** One table, `mid_link` (mid, linked mid), each link pointing to an earlier mid. Links carry no text and no vector (`main.go`).
- **Recall** (`memory.go`) keeps the shipped budget: the two newest mids, then five historical slots filled in turn from two lists, as L's `alternate` does in #874.
  - The similar list is the shipped one: the first seven mids of the production search's ranking.
  - The thread list's next mid is the one the same ranking puts first among the linked neighbours, in both directions, of the mids delivered in these slots so far. So a thread is followed hop by hop as its mids are delivered.
  - A mid from the thread list has the kind `thread`.
  - The two recent mids are delivered by age, not by the task, so their links are not followed; offline they are the same two mids for every probe.
  - An exhausted list yields its turns to the other.
- **Search.** `memory_search` fills its limit the same way, from the first `limit` mids of the ranking and the thread list, with no recent mids.
- **Trace.** Every call records the thread mids it delivered and those beyond the similar list.

## Controls

Fixed in `preregistration.md` §Controls before the first call, and computed by `controls.py` into `evidence/controls.json`:
- at each setting, at least 10% of stream batches link a mid;
- at `b4-k5`, at least half of the consecutive hop pairs of the chains of two or more hops are linked. It failed by ceiling before any call, and `b4-k5` was not run (§Similarity-chosen candidates do not reach chains);
- the oracle, as every arm;
- the mutation checks in `mutation.py` (`evidence/mutation.txt`): removing the thread list, its ranking order, either direction of a link, the previous mid at `b4-prev`, the earlier-only rule at `b4-k5`, the refusal of an ID not given or of a null `continues`, the retry variation, the failed record, the recording check, or the control's own link test each breaks a test.

## Similarity-chosen candidates do not reach chains

`b4-k5` gives each batch the 5 earlier mids most similar to its text, and the model may link only those. On this corpus that lookup gives the later hop's batch the earlier hop's mid for 26 of the 225 consecutive hop pairs (`lookup.py`, `evidence/lookup-b4-k5.json`, on the shared store at `cdb2758`, no model call):

| Family | Pairs | Given within 5 |
|---|---:|---:|
| C2 | 50 | 8 |
| C3 | 100 | 17 |
| IC2 | 25 | 1 |
| IC3 | 50 | 0 |

- The earlier mid's median rank is 28 of the mids before the later hop; 49, 91, 154 and 194 pairs fall within the top 10, 20, 50 and 100.
- Each hop is stated in a templated exchange, and the templates recur across chains. For C2, "Our account at Zevai Components is looked after by Chloe Traike." is followed 389 mids later by "Chloe Traike answers on extension 6200."; the second batch's text is nearer to other chains' exchanges of its own template than to the first, which shares only the name.
- So the hop-pair control (at least half linked) could not exceed 12%, and the paid phase was dropped before any call (`preregistration.md` §Amendments, 1). L (#874, #885) linked the same kind of chains through the names both hops state. A link chosen from similarity candidates does not reach them: the candidate step, not the model's choice, is what fails.

## Results

`b4-prev` only (§Similarity-chosen candidates do not reach chains). Offline, on the selected corpus, against S at `b4` from `../../evidence/offline/S/b4/`. The figures come from `python3 ../../score.py offline` at `deba2f7` with this arm's records, and from `controls.py` over the proxy's ledger (`evidence/controls.json`).

**Controls.**

| Control | Result | Threshold | Passed |
|---|---:|---:|---|
| Stream batches continuing the previous mid | 281 of 1,032 (27.2%), none failed | 10% | yes |
| Oracle: hops whose own statement delivers their mid within 5 | 483 of 505 (95.6%); S 493 | 95% | yes |
| `b4-k5` hop pairs linked | not run; ceiling 26 of 225 (11.6%) | 50% | no |
| Mutation checks (`evidence/mutation.txt`) | 13 of 13 break a test | all | yes |

**Reach per family** (items reached; for N and NC, items with the near-miss value in view):

| Family | n | S | P | P only | S only |
|---|---:|---:|---:|---:|---:|
| C2 | 50 | 0 | 0 | 0 | 0 |
| C3 | 50 | 0 | 0 | 0 | 0 |
| IC2 | 25 | 0 | 0 | 0 | 0 |
| IC3 | 25 | 0 | 0 | 0 | 0 |
| SC | 50 | 0 | 0 | 0 | 0 |
| UP | 50 | 0 | 1 | 1 | 0 |
| C1 | 30 | 27 | 27 | 0 | 0 |
| E | 30 | 26 | 26 | 0 | 0 |
| K | 40 | 35 | 35 | 0 | 0 |
| KU | 30 | 27 | 26 | 0 | 1 |
| N, near miss in view | 60 | 7 | 6 | | |
| NC, near miss in view | 30 | 0 | 17 | | |

- **Primary family.** C2 and C3 pooled: 0 of 100 for both, p = 1.0. The margin is not met, so `b4-prev` is not admitted to the live stage. The survey predicted this: hops sit at least 16 exchanges apart, so the previous mid is never a later hop.
- **Where the link does act.** An SC item's introduction is the exchange right before its evidence, so the two fill consecutive batches. 38 of the 50 evidence batches said they continue the introduction's mid, and P delivers the SC evidence mid in 21 items where S delivers it in none. None counts as reached, because the evidence mid's summary does not name the subject and reach requires one item to state both. The two mids arrive together, one of them as `thread`, which only a live turn can show being combined.
- **The risk the link adds.** An NC item's second exchange, stating someone else's value, follows its introduction the same way. 24 of the 30 second batches linked the introduction, and P puts the near-miss value in view for 17 NC items against S's 0. Never in the same item as the query's entity (0 for both), so the offline check that gates nothing is held. This is the abstention risk `../../../README.md` §P expected.
- Across the 150 chain probes of two or more hops, recall delivered a thread mid in 129, and one beyond the similar list in 124. Those mids took similar mids' places without reaching a later hop.

**Compression and cost** against S:

| Measure | S | P `b4-prev` |
|---|---:|---:|
| Stored characters per source character | 0.7445 | 0.7445 (links carry no text) |
| Context characters per turn | 2,943.1 | 2,934.6 |
| Vectors stored | 2,232 | 2,232 |
| Link rows | 0 | 281 |
| Write-call input tokens per batch | 327.2 | 445.6 (+118.4) |
| Write-call output tokens per batch | 205.4 | 378.2 (+172.8) |

- Tokens: the phase `links-b4-prev` took 1,052 calls: 1,032 answered and 20 refused with HTTP 429 and retried. They used 459,890 input and 390,273 output tokens. The per-batch differences pair each batch's call with the shared `synth-b4` call of the same batch (1,032 paired).
- The preregistration estimated 0.46M input and 0.21M output. Input held; output was 1.9 times the estimate. The judgement on continuity added about as many output tokens as the summary itself.

## Net production lines

Measured from this arm's code: the non-blank, non-comment lines of each function, and what production would carry of each for `b4-prev`.

| Production change | Arm code it corresponds to | Arm lines | Production estimate |
|---|---|---:|---:|
| `summary.go`: the earlier episode in the input, the prompt sentence, `continues` in the schema and its parse | `synthesizeLinks`, the `b4-prev` branch of `parseLinks`, `earlier` | 24 + 10 + 5 | +12 to +15 |
| `internal/agent/conversation.go`: the summarizer receives the previous summary | none; the arm calls the model directly | 0 | +2 to +4 |
| `accumulate.go`: read the previous mid before synthesis and write the link with the episode | the `b4-prev` branch of `requestsFor`, `writeLinks` | 3 + 25 | +12 to +18 |
| Schema and link reading: `mid_link` and a neighbours query | `linkSchema`, `readLinks` | 6 + 22 | +18 to +25 |
| Recall and `memory_search` slot filling | `alternate`, `Recall`, `Search` | 49 + 17 + 7 | +40 to +50 |
| Net | | | **+84 to +112** |

The survey's estimate was +75 to +115, with `prev` at the low end. The recall's slot filling is most of it, as it was for L.

## Limits

- **Offline only.** `b4-prev` was not admitted live, so nothing here shows whether a `thread` mid delivered beside an SC introduction is applied, or whether the NC near miss in view is answered from.
- **Recorded summaries as the reference.** Each batch was shown S's recorded summary of the previous mid, and P's own summary was discarded. In production the reference would be P's own earlier output, so any wording carried from one episode into the next would compound, and this run cannot measure that (`../../../README.md` §P, tension with ADR 0006).
- **The base is not linked.** Only the stream was synthesised, so E and K, whose evidence is in the base, can change only through what stream links displace.
- **One corpus.** The `b4-k5` ceiling comes from templates recurring across chains. A conversation whose hops are not stated in recurring templates could give a different lookup, and this run does not test one.

## Running it

```sh
D=research/memory-encoding/validation
go run ./$D/arms/prev build   -dir $D -work <scratch> -setting b4-prev -proxy http://127.0.0.1:8101 -embed-url http://127.0.0.1:8100/v1   # paid until evidence/links-b4-prev.jsonl is complete
go run ./$D/arms/prev offline -dir $D -work <scratch> -setting b4-prev -embed-url http://127.0.0.1:8100/v1
python3 $D/arms/prev/lookup.py <scratch>/shared/b4/memory.db                                                                           # free: b4-k5's lookup ceiling
python3 $D/arms/prev/controls.py
python3 $D/arms/prev/mutation.py
```

`build` records each batch's answer as it goes and resumes after the batches already recorded; with the file complete it makes no call.
