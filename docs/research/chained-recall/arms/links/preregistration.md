# Arm L preregistration: entities written by the synthesis call

Fixes arm L's one paid phase before its first model call. The phase has the synthesis call write each batch-4 mid's entities. It also fixes how the added tokens are measured and how the resulting setting, `b4-model`, is compared. The PM chose this scope in #880's dispatch: batch 4 only, and batch 8 keeps entities extracted by rule.

## What is called

- **Model.** The session model, `MiniMax-M2`, through the study proxy (`../../harness/proxy.go`) on `127.0.0.1:8091`. Calls use the route `/L/b4-<batch>/entities-b4/`, so the ledger counts them under arm `L` and phase `entities-b4`.
- **Request.** `synthesis.go` `synthesizeEntities`, which is the production summarizer's request (`internal/agent/memory/summary.go` at `03cbdff`) with two changes:
  - the `memory_summary` schema gains a required `entities` array of names, up to 50;
  - the prompt's last sentence also asks for the names of the people, places, organizations, projects and other named things the batch mentions.

  `synthesis_test.go` holds the request to production's, apart from those two changes. The batch is sent as the production store sends it: each message as "role: content", after the store's redaction.
- **What is kept.** Only the entities. The mid keeps its recorded summary, so `b4` and `b4-model` differ in the entity list alone. The returned summary is checked as production checks one, and a response production would reject is a failed attempt. Then it is discarded.

## How many

- One call per batch of the whole corpus at batch 4: 1,624 batches, 1,200 in the base and 424 in the stream.
- A batch is tried at most three times, and a pause on the usage limit uses up no attempt (`synthesis.go` `entityBatch`).
- A batch that still fails is retried by running `build` again. Its one recorded answer is the one used.

## Estimate

- The replication's batch-4 syntheses averaged 440.9 input and 314.1 output tokens (`../../../mid-memory-rag/evidence/replication/synthesis-n4.jsonl`, `response.usage` of all 1,200). The longer prompt and schema, and the entity list, are estimated to add up to 100 tokens to each side.
- So the phase is estimated at about 0.9M input and 0.7M output tokens.
- At the proxy's list prices that is about USD 1.10. The PM raised the shared ledger's ceiling from USD 1.00 to 3.00 to cover L's and H's synthesis phases too (`../../preregistration.md` §Amendments, 1). #874's stream synthesis draws on the same ceiling. The key is on a subscription, and the report states tokens, not dollars.

## Pacing and the usage limit

- **Start.** Only after #874's stream synthesis at batch 4 and 8 is recorded, so the shared key carries no second synthesis phase of this study at once.
- **Concurrency.** Two requests at a time (`synthesis.go` `entityWorkers`).
- **Usage limit.** On HTTP 429 with code 2056 the proxy pauses every request and probes at most every 30 minutes. Arm L reads the proxy's `/budget` once a minute while it is paused, and sends nothing.
- **Command.** `go run ./docs/research/chained-recall/arms/links build -work <scratch> -setting b4-model -proxy http://127.0.0.1:8091`. It resumes after the batches already recorded, and a complete recording makes no call.

## Outputs

- `evidence/entities-b4.jsonl`: one line per batch, holding its first and last source and the entities written.
- The ledger rows of phase `entities-b4`, in the ledger the proxy keeps.

## Measures

- **Added synthesis tokens per mid.** For each batch: the output tokens of its `entities-b4` call, minus the output tokens of its recorded synthesis.
  - For base batches, the recorded synthesis is the replication's `synthesis-n4.jsonl` `response.usage`.
  - For stream batches, it is the ledger's `synth-b4` row for the same batch.
  - The input-token difference is reported beside it.
  - Only the last successful attempt of each call is paired. The report gives the mean, the median and the number of batches paired.
- **Entity control.** At least 90% of mids have at least one entity stored, as for every L setting (`README.md` §Controls).
- **Comparison.** `b4-model` is scored by `score.py offline` like every setting, and so against S at batch 4. It is reported beside `b4` (rule), with the chains each reaches and the other does not. No further test is added.

## Revision

- Arm code: `3deb6ab`.
