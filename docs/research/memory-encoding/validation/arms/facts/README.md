# Arm F: standing facts written by the synthesis call

Records arm F of issue #890's validation study: survey candidate F (`../../../README.md` §F), built on the shared interface (`../../../../chained-recall/study/arm.go`) and measured on the study's corpus (`../../README.md`).

## Method

- **Store.** The setting `b4` copies the shared batch-4 store and adds one table, `mid_fact`. It holds each fact a stream mid's synthesis wrote, its kind (`user` or `other`) and its embedding by the local embedder (`main.go`).
- **Facts.** The synthesis call is production's (`internal/agent/memory/summary.go` at `3c37947`) with two changes (`synthesis.go`):
  - the `memory_summary` schema gains a required `facts` array of at most 5 objects, each a `kind` of `user` or `other` and a `text` of 1 to 200 characters;
  - the prompt's last sentence also asks for the batch's standing facts: `user` for the user's attributes, preferences, style, standing rules and the people and things in their life, `other` for any other lasting fact, and an empty list when there are none.

  `synthesis_test.go` holds the request to production's apart from those two changes. The returned summary is checked as production checks one, then discarded, so the mid keeps its recorded summary and F differs from S in the facts alone, as L did in #874.
- **Only the stream is synthesised.** The base's mids have no facts (`../../preregistration.md` §Arms).
- **Retries.** A batch is tried at most three times, and a pause on the usage limit uses up no attempt. The study proxy caches any response with a well-formed summary, so a retry appends " (Attempt n.)" to the prompt, and the cache cannot answer it with the malformed facts it answered before.
- **Recall** (`memory.go`) is the shipped recall, unchanged and first, then two additions:
  - **the profile block:** the newest `user` facts, newest mid first, whole, while their texts fit in 1,000 characters, each of kind `profile`;
  - **the similar facts:** the 3 facts most similar to the task by cosine, of kind `fact`, leaving out those already in the profile.

  Each fact item carries its mid's source interval and the level `fact`.
- **Search.** `memory_search` is the shipped search.
- **Trace.** Every call records the mids, profile facts and similar facts it delivered.

## Controls

Fixed in `../../preregistration.md` §F's controls before the first call, and computed by `controls.py` into `evidence/controls.json`:
- at least 90% of the selected UP items have a `user` fact stating the fact's word in the batch holding the fact;
- at least 90% of stream batches have a recorded facts array;
- the oracle, as every arm;
- the mutation checks in `../../mutation.py` (`../../evidence/mutation.txt`): removing the profile block, the similar facts, the user filter, the character bound, the profile exclusion, the facts requirement or the retry variation each breaks a test.

## Running it

```sh
D=docs/research/memory-encoding/validation
go run ./$D/arms/facts build   -dir $D -work <scratch> -proxy http://127.0.0.1:8101 -embed-url http://127.0.0.1:8100/v1   # paid until evidence/facts-b4.jsonl is complete
go run ./$D/arms/facts offline -dir $D -work <scratch> -embed-url http://127.0.0.1:8100/v1
python3 $D/arms/facts/controls.py
```

`build` records each batch's facts to `evidence/facts-b4.jsonl` as it goes, and resumes after the batches already recorded; with the file complete it makes no call.
