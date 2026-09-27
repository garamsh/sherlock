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
- at `b4-k5`, at least half of the consecutive hop pairs of the chains of two or more hops are linked;
- the oracle, as every arm;
- the mutation checks in `mutation.py` (`evidence/mutation.txt`): removing the thread list, its ranking order, either direction of a link, the previous mid at `b4-prev`, the earlier-only rule at `b4-k5`, the refusal of an ID not given or of a null `continues`, the retry variation, the failed record, the recording check, or the control's own link test each breaks a test.

## Running it

```sh
D=docs/research/memory-encoding/validation
go run ./$D/arms/prev build   -dir $D -work <scratch> -setting b4-prev -proxy http://127.0.0.1:8101 -embed-url http://127.0.0.1:8100/v1   # paid until evidence/links-b4-prev.jsonl is complete
go run ./$D/arms/prev build   -dir $D -work <scratch> -setting b4-k5   -proxy http://127.0.0.1:8101 -embed-url http://127.0.0.1:8100/v1   # paid until evidence/links-b4-k5.jsonl is complete
go run ./$D/arms/prev offline -dir $D -work <scratch> -embed-url http://127.0.0.1:8100/v1
python3 $D/arms/prev/controls.py
python3 $D/arms/prev/mutation.py
```

`build` records each batch's answer as it goes and resumes after the batches already recorded; with the file complete it makes no call.
