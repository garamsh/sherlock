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

## Running it

```sh
D=docs/research/memory-encoding/validation
go run ./$D/arms/prev build   -dir $D -work <scratch> -setting b4-prev -proxy http://127.0.0.1:8101 -embed-url http://127.0.0.1:8100/v1   # paid until evidence/links-b4-prev.jsonl is complete
go run ./$D/arms/prev offline -dir $D -work <scratch> -setting b4-prev -embed-url http://127.0.0.1:8100/v1
python3 $D/arms/prev/lookup.py <scratch>/shared/b4/memory.db                                                                           # free: b4-k5's lookup ceiling
python3 $D/arms/prev/controls.py
python3 $D/arms/prev/mutation.py
```

`build` records each batch's answer as it goes and resumes after the batches already recorded; with the file complete it makes no call.
