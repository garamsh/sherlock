# Arm C: context at synthesis

Records arm C of issue #891 in #890's validation study: survey candidate C (`../../../README.md` §C) at `b4-w8`, without the note, built on the shared interface (`../../../../chained-recall/study/arm.go`) and measured on the study's corpus (`../../README.md`). `preregistration.md` fixed its paid phase and controls before the first call.

## Method

- **Synthesis** (`synthesis.go`). Each of the 1,032 stream batches is synthesised once, through the study proxy, with production's request (`internal/agent/memory/summary.go`) changed in three places:
  - the input gains `context_messages`, the 8 source messages before the batch, sent as production sends a batch;
  - the prompt also asks for `context`, at most 300 characters naming who, where and what thread the batch belongs to, stated only from the batch and its context messages, and says the context messages are never summarised;
  - the schema gains a required `context` string of at most 300 characters.

  `synthesis_test.go` holds the request to production's apart from those changes. The note is not sent: it is empty offline and live.
- **Retries.** A batch is tried at most three times, and a pause on the usage limit uses up no attempt. The proxy caches any response with a well-formed summary, so a retry appends " (Attempt n.)" to the prompt and the cache cannot answer it with the malformed context it answered before.
- **Store** (`main.go`). The setting copies the shared batch-4 store. For every stream mid it stores C's summary and, in a new `context` column, C's context, both redacted as production redacts. The base's mids keep their recorded summaries and have no context.
- **Embedding.** A stream mid's one vector is computed from its context, a blank line and its summary, context first. The local TEI truncates at 512 positions, so a cut removes the summary's end.
- **Recall and search** (`memory.go`) are the shipped ones. A delivered mid with a context shows it on its own line after its summary, `Context when written: …`.

## Controls

Fixed in `preregistration.md` §Controls and computed by `controls.py` into `evidence/controls.json`:
- at least 90% of stream batches have a non-empty context;
- of the selected SC items, at least 80% have a context in the evidence batch that states the subject;
- the oracle, as every arm;
- the mutation checks in `mutation.py` (`evidence/mutation.txt`): removing the window, the context bound, the context requirement, the retry variation, embedding the context, the context's place before the summary, storing C's summary, the recall label or the SC control each breaks a named test.

## Results

C clears the admission margin on SC but fails two of its preregistered controls, so under `preregistration.md` §Controls it is not admitted to the live stage. The reach it gained came mainly through the summaries, which took in the context messages the prompt told them to leave out, not through the context line.

Figures from `python3 score.py offline` against `S/b4`, run over `../../evidence/offline/C/b4-w8/` and S's records at `b01ed7d`, and from `controls.py` with the proxy's ledger (`evidence/controls.json`).

### Reach offline, against S

| Family | n | C | S | C only | S only | p |
|---|---:|---:|---:|---:|---:|---:|
| SC (primary) | 50 | 39 | 0 | 39 | 0 | 3.6e-12 |
| C1 | 30 | 24 | 27 | 3 | 6 | 0.51 |
| C2, C3, IC2, IC3 | 150 | 0 | 0 | 0 | 0 | 1.0 |
| UP | 50 | 1 | 0 | 1 | 0 | 1.0 |
| KU | 30 | 20 | 27 | 1 | 8 | 0.039 |
| E | 30 | 27 | 26 | 1 | 0 | 1.0 |
| K | 40 | 35 | 35 | 0 | 0 | 1.0 |

- **Admission margin met** on SC: +39 of 50 and p < 0.05.
- **Risk measures.** NC shows the near-miss value in one item with the entity in 24 of 30 items, against S's 0. On N it is beside the entity in 0 of 60 for both arms, and in view at all in 6 (S 7). KU leaves only the obsolete name in view in 4 of 30 (S 3).

### Controls

| Control | Result | Threshold | Passed |
|---|---:|---:|---|
| stream batches with a non-empty context | 991 of 1,032 (96.0%) | 90% | yes |
| SC evidence contexts that state the subject | 37 of 50 (74%) | 80% | no |
| oracle, hops delivered within 5 by their own statement | 420 of 505 (83.2%) | 95% | no |
| mutation checks | 9 of 9 fail a test | all | yes |

- **Diagnostic, the summary.** The SC evidence summary states the subject in 49 of 50 items, though the prompt says the summary covers the batch alone. C's stream summaries average 268.7 characters against S's 171.5, and many restate earlier exchanges ("This adds to previous memories"). That likely explains the lower oracle: a mid whose summary also covers its neighbours ranks lower for its own statement.
- **Diagnostic, NC.** The second exchange's context names the entity in 16 of 30 NC items.
- In the SC misses, the context line names the thread in general terms ("Daimox Hotel arrangements") and not the subject.

### Compression and cost

| Measure | C | S |
|---|---:|---:|
| context characters per turn | 3,990 | 2,943 |
| stored characters per source character | 0.933 | 0.745 |
| vectors | 2,232 | 2,232 |
| context lines stored | 991, 99,747 characters | none |

- Storage rises by 0.188 characters per source character, more than L's 0.083. Of that, 0.094 is the context lines and the rest is the longer summaries.
- **Write-call tokens.** The mean over the 1,032 batches, last successful call per batch:

  | Phase | input | output |
  |---|---:|---:|
  | `synth-b4` (S) | 327.2 | 205.4 |
  | `context-b4-w8` (C) | 521.2 | 489.3 |

- **The phase in all.** The ledger counts 1,034 calls: none cached and one HTTP 429. It used 538,367 input and 505,564 output tokens. `preregistration.md` estimated 0.55M input and 0.29M output. Output ran 0.22M over, because the summaries grew and the context line was added.

## Production estimate

Net production lines before tests, counted from the production files at `165bef8` against the code in this directory that stands in for each change. The note's part is left out, as the run leaves it out.

| Change | Production file | Lines |
|---|---|---:|
| input field, context in the schema, parsing and bound, a two-value result | `internal/agent/memory/summary.go` | +9 to +12 |
| the summarizer's signature and its one caller | `internal/agent/conversation.go`, `internal/memory/sqlite/conversation.go` | +2 to +4 |
| read the 8 sources before the batch, store the context | `internal/memory/sqlite/accumulate.go` | +18 to +22 |
| the column and its `ALTER` for existing files | `internal/memory/sqlite/conversation.go` | +7 to +9 |
| embed context first | `internal/memory/sqlite/accumulate.go` | +6 to +8 |
| carry the context on a hit and show it labelled on recall and search | `internal/agent/conversation.go`, `internal/agent/memory/conversation.go`, `internal/memory/sqlite/conversation.go` | +5 to +8 |
| **Net** | | **+47 to +63** |

## Limits

- The note part of C is untested, as the survey's test plan says.
- The stream's summaries are C's, so C differs from S in its summaries as well as the context line. The run cannot separate the two: no setting kept S's summaries with C's context line.
- The summaries broke the prompt's rule that they cover the batch alone. That is the propagation ADR 0006 rejects, here from raw neighbouring messages. It is a property of this prompt on MiniMax-M2, not necessarily of C.
- Both SC and NC place their two exchanges next to each other, within W. So the reach and the NC risk measure the same adjacency, with an answer present in one and absent in the other.
- The token figures come from the proxy's ledger, which the #890 worker's worktree holds and which is not committed here.
- The base's 1,200 mids have no context, so E and K, whose evidence is in the base, change only by what the stream's mids displace.

## Running it

```sh
D=docs/research/memory-encoding/validation
go run ./$D/arms/context build   -dir $D -work <scratch> -proxy http://127.0.0.1:8101 -embed-url http://127.0.0.1:8100/v1   # paid until evidence/context-b4-w8.jsonl is complete
go run ./$D/arms/context offline -dir $D -work <scratch> -embed-url http://127.0.0.1:8100/v1
python3 $D/arms/context/controls.py
python3 $D/arms/context/mutation.py
```

`build` records each batch to `evidence/context-b4-w8.jsonl` as it goes and resumes after the batches already recorded; with the file complete it makes no call.
