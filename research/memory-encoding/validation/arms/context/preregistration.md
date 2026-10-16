# Arm C preregistration: context at synthesis

Fixes arm C's one paid phase before its first model call, and the controls and measures its offline stage reports. It adds to `../../preregistration.md` and changes nothing that file fixes. C is `../../../README.md` §C at `b4-w8`, without the note.

## What is called

- **Model.** `MiniMax-M2` through the study proxy on `127.0.0.1:8101`, on the route `/C/b4-<batch>/context-b4-w8/`. The ledger counts the calls under arm `C` and phase `context-b4-w8`.
- **Request.** `synthesis.go` `synthesizeContext`: the production summarizer's request (`internal/agent/memory/summary.go` at `165bef8`) with three changes, which `synthesis_test.go` holds against production's request:
  - The input gains `context_messages`, the W = 8 source messages before the batch, beside the batch's `updates`. Both are sent as the production store sends a batch: each message as "role: content", after the store's redaction.
  - The prompt's last sentence also asks for `context`: at most 300 characters naming who, where and what thread the batch belongs to, stated only from the batch and its context messages, or an empty string. A further sentence says the context messages only situate the batch, are never summarised, and the summary stays about the batch alone.
  - The `memory_summary` schema gains a required `context` string of at most 300 characters.
- **The note is not sent.** It is empty offline and in the live stage (`../../preregistration.md` §Arms), so this run tests the window part of C alone.
- **Accepted answer.** Production's check on the summary, plus a `context` that is present and holds at most 300 characters after trimming. An empty context is an answer. Anything else is a failed attempt.
- **What is kept, unlike F and L.** Both the summary and the context. C changes the synthesis input, and the survey's C keeps the summary that input produces (`../../../README.md` §C, "only its input grows"). So C differs from S in its stream summaries as well as the context line. The diagnostic below counts how often the subject entered the summary.
- **Only the stream.** The base's 1,200 mids keep their recorded summaries and get no context (`../../preregistration.md` §Arms). The first stream batch's context messages are the last 8 base messages.

## Storage and use

- The stream mid's summary becomes C's, and a `context` column holds its context, both after the production redaction.
- Its vector is computed from the context, a blank line and the summary, context first. The local TEI truncates at 512 positions (`/info` reports `auto_truncate: true`), so a cut removes the summary's end.
- Recall and `memory_search` are the shipped ones. Each delivered mid with a context shows it on its own line after the summary: `Context when written: …`.
- One vector per mid, as S.

## How many

- One call per stream batch: 1,032 calls.
- A batch is tried at most three times, and a pause on the usage limit uses up no attempt.
- **Retries vary the request.** The proxy caches every response whose summary is well-formed, so a malformed context would be cached and returned to an identical retry. Attempt n from 2 on appends " (Attempt n.)" to the system prompt, so its request differs from every earlier one.
- A batch still failing is retried by running `build` once more. A batch that fails in two builds is recorded by hand as failed. Its mid keeps its shared summary and vector without a context, and it counts against the context control.

## Estimate

- #885's stream synthesis read 328 and wrote 201 tokens per call (`../../preregistration.md` §Paid phases, in order).
- The context messages add 386 characters per batch on average (at most 3,199), measured on `corpus.json`'s stream as "role: content" of the 8 messages before each batch. The added prompt text (341 characters), the schema field (54) and the input key (22) add about 420 more. Together that is about 200 tokens more input per call.
- The context adds at most 300 characters of output, about 75 tokens.
- So the phase is estimated at about 0.55M input and 0.29M output tokens.

## Pacing and the usage limit

- **Start.** Only after the proxy's other paid phases of this study that `../../preregistration.md` §Paid phases, in order, orders before C's (the stream synthesis and F's facts) have ended, and once the PM confirms the key is free.
- **Concurrency.** Two requests at a time (`synthesis.go` `contextWorkers`).
- **Usage limit.** On HTTP 429 with code 2056 the proxy pauses and probes at most every 30 minutes. C reads `/budget` while paused and sends nothing.
- **Command.** `go run ./docs/research/memory-encoding/validation/arms/context build -dir docs/research/memory-encoding/validation -work <scratch> -proxy http://127.0.0.1:8101 -embed-url http://127.0.0.1:8100/v1`. The recording is written before the shared store is read, and a complete recording makes no call.

## Outputs

- `evidence/context-b4-w8.jsonl`: one line per stream batch, holding its first and last source, the summary and the context.
- The ledger rows of phase `context-b4-w8`.

## Controls

Fixed here before the first call, from the survey's test plan (`../../../README.md` §Test plan), and computed by `controls.py` into `evidence/controls.json`:
- **Context written.** At least 90% of the 1,032 stream batches have a non-empty context.
- **Subject named.** Of the 50 selected SC items, at least 80% have a context in the evidence batch that states the subject. "States" is `score.py`'s `states`: every capitalized word and every token with a digit of the subject appears.
- **Oracle.** As every arm, at least 95% of hops, from C's `oracle.jsonl`.
- **Mutation checks.** Removing the window, the context bound, the context requirement, the retry variation, embedding the context, the context's place before the summary, storing C's summary, the recall label or the SC control each breaks a named test (`mutation.py`, `evidence/mutation.txt`).

A failed control is reported, and C is then not admitted to the live stage (`../../preregistration.md` §Offline stage).

## Diagnostics, reported and gating nothing

- The SC items whose evidence summary states the subject, which the prompt forbids.
- The NC items whose second exchange's context states the entity, the situated absent risk. `score.py offline` reports the near-miss value in view with the entity beside it.

## Measures

- `score.py offline` scores C's `b4-w8` against `S/b4`, per family, with context characters per turn, stored characters per source character and vectors.
- **Write-call tokens against S's.** The mean and total input and output tokens of the last successful call per batch, of phase `context-b4-w8` against phase `synth-b4` (`controls.py` `tokens`).
