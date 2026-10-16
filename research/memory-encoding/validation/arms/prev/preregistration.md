# Arm P preregistration: episodes chained on earlier episodes

Fixes arm P's paid synthesis phase before its first model call, and the controls its offline stage is judged by. It adds to `../../preregistration.md` and changes nothing that file fixes. The arm is `../../../README.md` §P at the two settings `../../preregistration.md` §Arms names, `b4-prev` and `b4-k5`.

## What is called

- **Model.** The session model, `MiniMax-M2`, through the one study proxy (`../../../../chained-recall/harness/proxy.go`) on `127.0.0.1:8101`. Calls use the route `/P/b4-<batch>/links-<setting>/`, so the ledger counts them under arm `P` and the phases `links-b4-prev` and `links-b4-k5`. `<batch>` numbers the corpus's batches of four from the first base batch.
- **Request.** `synthesis.go` `synthesizeLinks`: the production summarizer's request (`internal/agent/memory/summary.go` at `3c37947`) with three changes. `synthesis_test.go` holds it to production's apart from them.
  - The input gains `earlier`, a list of earlier episodes, each with its mid ID, the label "earlier episode, for reference, do not restate" and its summary.
  - The prompt's last sentence, "Call memory_summary with the episode summary.", is replaced by one that says the earlier list is for reference and the summary is about the batch alone, and asks for `continues`.
  - The `memory_summary` schema gains a required `continues`: a boolean at `b4-prev`, whether the batch continues the earlier episode's thread; at `b4-k5`, an array of at most 5 integers, the IDs of the given episodes the batch continues or builds on.
- **What each batch is given** (`main.go` `requestsFor`). Every one of the 1,032 stream batches, and no base batch (`../../preregistration.md` §Arms):
  - `b4-prev`: the mid just before the batch's own. For the first stream batch that is the base's last mid.
  - `b4-k5`: the 5 mids before the batch's own that the production store's search ranks first for the batch's text, the four messages as the store sends them ("role: content" after redaction) joined by newlines. The search runs over the shared batch-4 store's embeddings with the local embedder: one embedding per batch, no model call.
  - Each is given with its recorded summary, the one the shared store holds.
- **What is kept.** Only `continues`, as a row of `mid_link` (mid, linked mid). The returned summary is checked as production checks one, and a response production would reject is a failed attempt. Then it is discarded: the mid keeps its recorded summary, so P differs from S in the links alone, as L did in #874 and F does here. Batches are therefore independent of one another and can be sent two at a time.
- **Malformed.** A `continues` that is missing, null or of the wrong type, or at `b4-k5` names an ID that was not given, is malformed. An ID named twice counts once.

## Retries

- A batch is tried at most three times, and a pause on the usage limit uses up no attempt.
- The proxy caches every response whose summary is well-formed, so a malformed `continues` would answer every identical retry. Attempt n > 1 therefore appends " (Attempt n.)" to the prompt, and each attempt is a request the cache has not seen.
- A batch whose three attempts all came back with a malformed `continues` is recorded as failed and keeps no link. Its three answers are cached, so sending it again would change nothing. It counts against the continuing control.
- Any other failure (transport, a summary production rejects) is recorded nowhere, and running `build` again retries the batch.

## How many

- One call per stream batch per setting: 1,032 at each, 2,064 in all, plus retries.
- `b4-prev` runs first, then `b4-k5`, one setting at a time, at most two requests at once (`synthesis.go` `linkWorkers`).

## Estimate

- The shared stream synthesis read 325.5 and wrote 194.4 tokens per call, over the 293 successful `synth-b4` calls in #890's ledger when this was written (`evidence/ledger/calls.jsonl` of the #890 run, read with `jq`). Its 290 recorded stream summaries averaged 157 characters, and the replication's 1,200 base summaries 512 (`../../../../mid-memory-rag/evidence/replication/synthesis-n4.jsonl`).
- `b4-prev` adds one summary with its label and ID, and about 250 characters of prompt: about 120 input tokens. So about 450 input and 200 output tokens per call, 0.46M and 0.21M for the phase.
- `b4-k5` adds five summaries, drawn from base and stream, estimated at 350 characters each with their labels, and the same prompt: about 500 input tokens. So about 830 input and 205 output tokens per call, 0.86M and 0.21M for the phase.
- Together about 1.3M input and 0.4M output tokens, about USD 0.90 at the proxy's list prices, within its USD 30 ceiling. The key is on a subscription, and the report states tokens, not dollars.

## Pacing and the usage limit

- **Start.** After `evidence/synthesis/stream-b4.jsonl` is complete and pushed, since every request carries recorded summaries, and in the order `../../preregistration.md` §Paid phases, in order, fixes.
- **Usage limit.** On HTTP 429 with code 2056 the proxy pauses every request and probes at most every 30 minutes. P reads the proxy's `/budget` while it is paused and sends nothing.
- **Command.** `go run ./docs/research/memory-encoding/validation/arms/prev build -dir docs/research/memory-encoding/validation -work <scratch> -setting <setting> -proxy http://127.0.0.1:8101 -embed-url http://127.0.0.1:8100/v1`. It resumes after the batches already recorded. A complete recording makes no call, and a recording whose given mids differ from what the store now gives is refused.

## Outputs

- `evidence/links-b4-prev.jsonl` and `evidence/links-b4-k5.jsonl`: one line per stream batch, holding its first and last source, the mids it was given, those it continues, and why it failed where it did.
- The ledger rows of the phases `links-b4-prev` and `links-b4-k5`.

## Controls

Fixed before the first call, computed by `controls.py` into `evidence/controls.json`.

- **Continuing.** At each setting, at least 10% of stream batches link at least one mid (`../../../README.md` §Test plan).
- **Hop pairs, at `b4-k5`.** At least half of the pairs of consecutive hops, over every chain of two or more hops (C2, C3, IC2, IC3 pooled), have their mids linked. Hops lie in random order, so the link may run either way. Reported beside it, per family: the pairs whose later mid's batch was given the earlier mid, which separates the lookup from the model's choice.
- **Oracle.** As every arm: each hop's own statement delivers its mid within 5 by P's `Search` for at least 95% of hops.
- **Mutation checks.** Removing each mechanism P adds breaks a named test (`mutation.py`, `evidence/mutation.txt`).
- **Recorded, not gated.** Of the chain probes with two or more hops, how many delivered a thread mid, and one beyond the similar list; the tokens each call added over the shared `synth-b4` call of its batch.

## Measures

`../../score.py offline` scores each setting like every arm: reach per family, context characters per turn, stored characters per source character and vectors, against S at `b4`. Admission to the live stage is `../../preregistration.md` §Offline stage's, on C2 and C3 pooled, for each setting separately.

## Amendments

1. **`b4-k5` is not run; its control is recorded as failed by ceiling.** Made before any paid call of either setting, and approved by the PM on the #892 dispatch.
   - **Finding.** `lookup.py` (`evidence/lookup-b4-k5.json`) ran `b4-k5`'s candidate lookup on the shared batch-4 store built at `cdb2758`, with the local embedder and no model call. Of the 225 consecutive hop pairs of the chains of two or more hops, the later hop's batch is given the earlier hop's mid in 26: C2 8 of 50, C3 17 of 100, IC2 1 of 25, IC3 0 of 50. The earlier mid's median rank is 28, and 49, 91, 154 and 194 pairs fall within the top 10, 20, 50 and 100.
   - **Consequence.** The model can link only a mid it is given, so the hop-pair control cannot exceed 26 of 225 (12%) against its 50%, whatever the model chooses. The phase could only confirm the failure, at about 0.86M input tokens.
   - **Why.** The corpus states each hop in a templated exchange, and the same templates recur across chains ("Our account at X Components is looked after by Y"). A batch's text is nearer to other chains' exchanges of the same template than to the one hop it continues, whose summary shares only the names.
   - **What changes.** `b4-prev` runs as fixed above. `b4-k5` makes no call and has no offline record, and `controls.py` reports its control as failed from the lookup. A larger k was considered and not taken: at k = 20 the ceiling is 91 of 225, still below 50%, and the input grows fourfold.
