# Recall-specificity experiment

This record measures the addressed-memory runtime at its recorded revision. Issue #823 retired the addressed recall, information, and record-selection test entry points from `tests/`; the results remain historical evidence, and new live-model evaluation must use a source-linked instrument.

What the running agent recalls when the question says less than the fact it is looking for, measured against the built binaries and the store its own turns wrote.

## Contents
- The bound, stated before the run
- Method
- The curve
- The loss is at the subject, and nowhere else
- No probe reached its address
- What L3 placed
- What the write put where
- What the run cost
- What this record does not say
- Limits
- Reproducing
- Information comparison (issue 803)
- Record selection comparison (issue 806)

## The bound, stated before the run

The ceilings are constants in `tests/recall_specificity_test.go`, read by a metering proxy between the agent process and the endpoint. It refuses the call that would pass any of them **before that call leaves the machine**, so the bound is a refusal and not an observation. Four ceilings rather than one: two of them need no price, so they hold if the price is wrong.

| ceiling | stated | what the run used |
|---|---|---|
| model | `MiniMax-M2` at `https://api.minimax.io/v1`, key from `MINIMAX_KEY` | the same |
| calls | 150 | 100 |
| input tokens | 1,200,000 | 216,876 |
| output tokens | 300,000 | 41,149 |
| USD | 0.29 | 0.1144 |

Nothing was refused. The USD figures are derived from the token counts the endpoint reported, at $0.30 per million input and $1.20 per million output — MiniMax's published `MiniMax-M2` rates on the run date, which is what the meter charged at and not an invoice.

The granularity is one call: a ceiling already reached refuses the next request, and a request's cost is not known until it answers.

## Method

Every previous round in this tree measured a port of the design. This one drives the two shipped binaries — `gagent workspace` serving the gRPC data plane and `gagent agent` serving its gateway, both built by `go build` inside the run — through `tests/`' own helpers (`buildGagentBinary`, `startWorkspaceBinary`, `startAgentBinary`, `emptyConfig`). The loop, the tool registry, the prompt and the memory service are the shipped ones. What the run supplies is the facts, the questions, and the meter.

**Every turn is its own agent process, against one memory file.** The agent's session is rolling, so a probe asked in the process that planted the fact could answer out of its own history instead of the store. A fresh process per turn removes that: the only thing a probe turn shares with a plant turn is `memory.db`.

**Plant.** Ten facts, one per turn, each a plain declarative about a different person, each a recent change. The turn stores what it stores — the writeback is the single write path (ADR 0011) and nothing here writes to the store directly.

**Snapshot.** After the tenth plant the memory file is copied under each probe, because a probe turn also ends in a writeback and would otherwise reach the next probe's read.

**Probe.** The same ten facts asked back at three specificities, one question per turn:

| level | what the question gives | example |
|---|---|---|
| L1 | the subject and the attribute | "Where does Sarah live now?" |
| L2 | the subject; the attribute implied | "Has anything changed for Sarah?" |
| L3 | neither, both implied | "Didn't someone move to a new city recently?" |

L1 is the control **inside this run**, and it is not comparable to `memory-frames`' `reachable` 0.394, which was a different model, a different prompt and a store this tree no longer has. L2 is one template with the name substituted, so between L1 and L2 the only thing that changes is that the question stops naming what it asks about. L3 names the change instead of the person; each fact's change is unique among the ten, so an L3 question has exactly one answer in the store and an empty result is never the store having nothing to give.

**Score**, per level, from the monitoring stream (ADR 0007) and the store, never from the model's account of itself:

- **placed** — the `recall` event's `recall_outcome` is anything but `unplaced`, which is `ReadAddress.Placed()` as the running loop reported it. ADR 0019's other three outcomes — `addressed`, `widened`, `empty` — all placed one.
- **planted memory injected** — the row the plant phase wrote is among the `recalled_ids` the turn injected. Its ID comes from reading the store after the plant phase through the shipped service.
- **the answer carries the value** — the turn's reply, which is the `memory_write` call's `reply` field and the only thing a person reads, contains the planted value on word boundaries and without case (#371's rule: without the boundary, `fish` is credited to "allergic to shellfish"). The ten values are distinct and none contains another.

Every figure below is 10 probes per level. Intervals are 95% Wilson, which is the interval to use at a proportion of 0 or 1 where a normal one has zero width.

## The curve

<!-- generated by the run; the counts are results/summary.md, the rates and intervals are computed from them -->

| level | the question | placed | reached an address that held something | planted memory injected | **the answer carries the planted value** |
|---|---|---|---|---|---|
| L1 | names subject and attribute | 10/10 | 0/10 | 8/10 | **1.000** [0.722, 1.000] |
| L2 | names the subject only | 10/10 | 0/10 | 8/10 | **1.000** [0.722, 1.000] |
| L3 | names neither | 10/10 | 0/10 | 0/10 | **0.000** [0.000, 0.278] |

**It is a cliff, not a slope.** Taking the attribute out of the question costs nothing at all — L1 and L2 are identical on every column. Taking the subject out costs everything. Nothing in between was measured, because there is nothing in between: the read step is given the question's own words and nothing else, and the store is keyed on the subject first.

## The loss is at the subject, and nowhere else

The read step resolves an address from the question alone. It never sees the store while resolving, so it cannot discover which subjects exist. `internal/memory/sqlite` matches a subject with SQL `=` on a `TEXT` column — exact, and case-sensitive by collation — so a subject the step invented reaches nothing however well it names the thing being asked about.

That is why L3 fails at 10 of 10 while placing an address 10 of 10 times. The step did its job and produced a well-formed address; the address is unreachable by construction.

It is also why L2 costs nothing. On this store the widened read returns one or two memories, and the reply is written from them.

## No probe reached its address

Not one of the 20 L1 and L2 probes ended in `addressed`. All 20 widened — 0/20, 95% Wilson [0.000, 0.161]. **Every correct answer this run produced came from ADR 0019's widening to the subject**, and none from the addressed read the widening is the fallback for.

The read step and the writeback share one text — `addressContract`, embedded in both `ReadInstruction` and `Contract` — and ADR 0019 gives the read step "agreement with the writeback rather than correctness of its own" as its target. On this workload it does not reach it. The wordings are close and never equal:

| planted question (the write) | placed question (the read, L1) |
|---|---|
| `where did Sarah move to` | `where does Sarah live now` |
| `what instrument did Tomas take up` | `which instrument does Tomas play` |
| `which team did Rafael transfer to` | `which team is Rafael on` |
| `what field did Priya switch to this year` | `what does Priya research now` |

The write words the question from the statement's verb — moved, took up, transferred, switched — and the read words it from the question it was handed. `addressContract` tells the write not to take the question word from the statement's verb; nothing stops the rest of the question coming from it.

At L2, nine of ten placed a subject and an empty question, which is the branch `ReadInstruction` provides for a question about a subject in general; the tenth placed `what changed for Marcus`, which held nothing and widened like the rest.

## What L3 placed

Every L3 probe named a subject, and every subject was invented:

| subject placed | probes |
|---|---|
| `someone` | 7 |
| `person`, `researcher`, `employee` | 1 each |

The questions it placed alongside them are good ones — `who moved to a new city recently`, `who adopted a pet recently` — and unreachable for the same reason: they ask about the change, and the store holds facts addressed to the person.

**No L3 probe confabulated.** All ten replies report having nothing rather than inventing a name: "I don't have any record of someone moving to a new city in my memory"; "My memory shows no relevant memories about anyone adopting a pet". The failure is a clean miss.

## What the write put where

Two of the ten facts were never stored as a value. For Marcus and Hana the writeback addressed the date and left the fact in the question:

| subject | question | value |
|---|---|---|
| `Marcus` | `when did Marcus start at Kestrel Robotics` | `in March` |
| `Hana` | `when did Hana start learning Finnish` | `September` |

`Kestrel Robotics` and `Finnish` exist in that store only inside an address. This is why "planted memory injected" reads 8/10 and not 10/10: for those two there is no value-carrying row to inject. Counted over the eight facts for which one exists, **the read step injected it 8 times out of 8**, 95% Wilson [0.676, 1.000] — the same 8 at L1 and at L2. The two misses are the write path's, not the read's.

Both answers were still correct, because a recalled memory is rendered to the model as its address and its value (`internal/agent/memory/recall.go`), so the company and the language reached the reply inside the question half.

Yusuf's turn wrote two memories — the club and the date — which is the only probe whose widened read returned two.

## What the run cost

100 calls over 40 turns, 216,876 input tokens, 41,149 output tokens, $0.1144 at the rates above, in 524 seconds. No call was refused, so no figure here is from a partial run.

| phase | turns | calls | input tokens | output tokens | USD | operator tools called |
|---|---|---|---|---|---|---|
| plant | 10 | 20 | 38,022 | 11,789 | 0.0256 | none |
| L1 | 10 | 24 | 51,539 | 11,514 | 0.0293 | `files` 2, `shell_exec` 2 |
| L2 | 10 | 26 | 57,804 | 5,979 | 0.0245 | `files` 5, `shell_exec` 1 |
| L3 | 10 | 30 | 69,511 | 11,867 | 0.0351 | `files` 7 |

Exactly one call per turn is the read step, so the other 60 are the act loop: 26 turns took one act call, 8 took two and 6 took three. Every one of the 40 turns ended on `memory_write`.

**What the extra act calls are is itself a result.** They are the model going to look in the workspace, and they concentrate where the read step returned nothing: 7 of the 14 `files` calls are L3's, and the L3 replies say so — "the workspace is empty and I have no prior context". A turn whose memory read comes back empty does not stop; it searches the only other place it has.

Two notes on the endpoint, both of them things this run was warned about:

- **No response format was sent anywhere.** `internal/agent/openai` carries none by design (#615), and every address and every writeback here arrived as tool-call arguments. 96 of the 100 calls returned `finish_reason: tool_calls` with well-formed JSON arguments.
- **The empty-answer trap did not fire, and a neighbour of it did.** No call returned empty content with no tool call, so the reasoning budget never swallowed an answer — the run sets no `max_tokens` and a 300-second per-call ceiling. But 4 calls returned `finish_reason: stop` with the model's reasoning in `content` as a `<think>` block and no tool call at all, and the two halves of the loop met that differently. Three were act calls, all at L3; the session answered each with the writeback-required notice and all three turns then ended on the writeback, so #615's guard fired three times and recovered every time. The fourth was a **read** call, on the plant turn for Marcus, and it is the single `unplaced` outcome in this run: the step reported that it could not place the question rather than inventing an address to read, which is what ADR 0019 says it does when a response carries no reserved call. That is the failure mode both #615 and ADR 0019 were written against, observed once in 40 read calls.

## What this record does not say

- It does not say the read step words its questions badly. It says the read's wording and the write's are never equal on these ten, and that the widening is what carries every answer.
- It does not measure the write path. That two facts were addressed by their date is `memory-creation`'s subject; it appears here because it bounds what any read could have returned.
- It recommends no change to ADR 0019. The widening is measured, not judged: on this workload it is the difference between 1.000 and 0.000 at L1 and L2.
- It says nothing about restrictions. Seven of the eleven planted rows carry one and no probe was written to test what a restriction does to a read.
- It says nothing about latency or money beyond this run: one model, one endpoint, one day.

## Limits

- **Ten facts per level.** At 10 of 10 the 95% Wilson interval still reaches down to 0.722, and at 0 of 10 it reaches up to 0.278. The L1-to-L3 gap is far outside both; the L1-to-L2 gap is zero and a difference smaller than the intervals would not have been seen.
- **One or two memories per subject.** The widened read returns everything under the subject, which here is almost always one row — so this run cannot say whether widening still delivers the answer under a subject holding fifty facts, where the reply would be written from all of them. That is the condition under which L2's 1.000 is most likely to fall, and it was not measured.
- **One wording of each question, and one of each statement.** The read/write disagreement above is a disagreement over these ten phrasings. A statement whose verb already matches the question a reader would ask might address and hit; none of these did.
- **The subject match was never probed off-form.** Every probe spelled the subject exactly as the plant did. What a lower-case name or a surname does to the widened read is untested, and the store's collation says it would matter.
- **L3's subjects are unreachable by construction, not by accident.** The design guarantees each L3 question has one answer in the store, so the zero is the read step failing to reach it — but it is one design of L3. A question implying its subject strongly enough to be named ("didn't my sister move recently") is a different case and is not here.
- **`answer carries the value` is a containment test.** It credits a reply for holding the token on a word boundary; it does not verify the rest of the sentence. The replies were read for L1 and L2 and are correct, but that reading is not a measurement.
- **Plant turns are also turns.** Each ran a read step against a store that did not yet hold what it was about, which is why nine of ten plants report `empty`; the tenth is the `unplaced` read described under §What the run cost. Nothing downstream reads those outcomes, and they are not part of the curve.
- **Nothing in `make ci` re-measures this.** The run needs `GAGENT_PAID_MODEL_TESTS=1` and a key, and CI sets neither. What CI does check is the procedure and the bound, through the two free tests named below.

## Reproducing

The run is `TestRecallFromImpliedInput` in `tests/`. It skips unless `GAGENT_PAID_MODEL_TESTS=1` and `MINIMAX_KEY` are both set, so `make ci` buys nothing:

```
GAGENT_PAID_MODEL_TESTS=1 go test ./tests/ -run '^TestRecallFromImpliedInput$' -timeout 90m -v
```

The 10-minute `go test` default is far too short for 40 turns. The run rewrites every file under `results/`; the committed ones are what the run above produced, and the figures in this record are pinned to them.

- `results/plants_minimax-m2.json` — the ten plant turns and every row the store held after them.
- `results/probes_minimax-m2.json` — the thirty scored probes, each with the address the read step placed and the reply the turn produced.
- `results/calls_minimax-m2.json` — every call the run bought: the advertised tools, the model's content and tool calls, the tokens the endpoint reported, and the ceiling it was charged against. Prompts are not kept — every request carries the same instructions and tool schemas, which the tree already holds.
- `results/summary.md` — the counts the curve above is drawn from, written by the run.

Two checks in the same package cost nothing and do run in `make ci`:

- `TestRecallFromImpliedInput_AgainstAScriptedEndpoint` drives the whole procedure — process per turn, snapshot, three levels, scoring, the result files — against an endpoint answering from a script, so what the paid run depends on is exercised before it is paid for.
- `TestSpendMeter_RefusesPastEachCeiling` is the evidence that the bound is a bound: each of the four ceilings refuses the call that would pass it, each paired with the control that admits the same calls under a wider ceiling.

## Information comparison (issue 803)

The paid comparison completed 28 probes on 2026-09-21: 25 valid and three invalid. C fails both explicit positive probes and the multi-actor completeness control; this is evidence publication, not an adoption result. The wider owner goal remains incomplete. The historical files in `results/` remain the earlier run's evidence. The PM's separate baseline at `fc5fca4c885e8bf015bab1a889042f091ff8ce53` is preserved at `/home/sauce/Workspace/garam/gagent/.local/verification/2026-09-21-baseline/`; its whole-record audit motivates keeping the original value-only score alongside the complete-record score.

`tests/recall_information_test.go` builds the actual agent using Go compiler overlays. The checkout's shipped source remains unchanged. Each run records its checked-out revision, instrument file hashes and binary hashes in `source.json`, and retains the overlay inputs. The prototype was developed against `4a86d0d58e232ee968d15e664afc4439686706b9`; all source measured in this paid collection is pinned to `ca121754269b582853784b63f5e12cd0c92fcb8f`, as recorded in [source.json](results/information-2026-09-21/source.json). Publication does not change that source.

| Variant | Resolver input and selection |
|---|---|
| `task` (A) | Shipped task-only resolver and addressed read/widening. |
| `history` (B) | The same resolver with the live session's conversation before the current task. |
| `grounded` (C) | The same conversation as B, plus every active row's ID, subject, question, restriction **and value**. A selection-specific system instruction requires verbatim offered keys, including questions that contain an answer; it replaces the shipped address-construction instruction only in this candidate. The resolver must select an offered subject/question pair; a fabricated key places nothing. The existing addressed read injects matching rows. |
| `names` (control) | The same conversation, but only active IDs and subject names. Its selection-specific system instruction asks for an offered subject and an empty question; selection must name an offered subject. Compare its anonymous probe with C's matched anonymous probes. |

The catalogue is a full-record offer, not an identities-only offer. Question and value disclosure are reported separately from selected-row injection. It is linear in the deliberately small active fixture, with no shortlist, ranking, embeddings or additional cache. `catalogue_bytes` records the extracted offer plus the exposure helper's appended newline; `read_input_tokens_including_catalogue` records provider-reported input tokens for the entire resolver request. The provider does not attribute tokens to prompt substrings, so this is the token cost of making the offer in context, not an independently measured marginal catalogue token count.

The run plants three statements through the unmodified real agent: Hana learning Finnish, Yusuf joining Thameside rowing club and Mira joining Hilltop chess club. It verifies complete-record evidence for all three before probing, and preserves the plant turns, active rows and SQLite snapshot under `frozen/`. A failure to store a required fact stops the run as partial. Each probe starts a fresh agent and workspace, initially using a copy of that same frozen store (or an empty store for the two empty-store controls). The post-cue checks below found three probes whose stores no longer met that condition.

| Scored probes | Input |
|---|---|
| 24 matched probes | Four conditions × A/B/C × two repetitions: explicit Hana/language question; answer-free cue identifying “she” as Hana followed by the same pronoun question; anonymous language-learning event after restart; known-absent fencing event after restart. |
| Four controls | Answer-bearing history with empty store; anonymous empty-store query; ambiguous club event supported by both Yusuf and Mira; names-only anonymous discovery. |
| Preparation, counted separately | Three plant turns and seven cue turns. Cues run in the same process as their probe; every other probe starts without conversation. |

A cue is a real turn and may itself recall or write. The instrument checks its actual act inputs and final reply for the target, compares the records immediately before the probe with the frozen fixture, and verifies that an empty-store control remains empty. Contaminated conditions are marked invalid, including when a cue introduces the answer or writes it into an initially empty store. The actual pre-probe rows and recalled IDs remain visible in invalid cases. This detects the fixture's literal target; paraphrased disclosure still requires transcript review.

`calls.json` retains complete model requests and responses, including tool-result messages, calls/tokens, read/act stage durations and refusals. `plants.json`, `held.json` and `probes.json` retain plant evidence, pre-probe stores, terminal events, tool events, selected keys, recalled IDs and replies. `run.json` distinguishes partial runs, invalid conditions and completed collections awaiting semantic review; its elapsed time includes building and preparation. A new output directory is mandatory, so this run cannot overwrite a previous result.

The automatic `answer_contains_finnish` field is only a word-boundary containment observation. Complete-record injection checks actual subject/question/restriction/value identities rather than just the value. For the absent event there are no target IDs; for the ambiguous control the target IDs cover both actors, and the boolean injection field means **any**, not all, of those IDs. Review every response against the actual stored evidence: assign semantic correctness and unsupported claims independently, checking absence/uncertainty for fencing and both supported possibilities or an explicit ambiguity for clubs. The original JSON adjudication fields remain `pending human review` byte-for-byte; the separately attributed semantic judgments below complete the review without rewriting collection evidence. A successful harness exit is not successful recall or semantic correctness. The scripted endpoint checks instrumentation and has no behavioral score.

Free reproduction, with no credential or network model required:

```sh
env -u GAGENT_PAID_MODEL_TESTS PATH=/home/sauce/go/bin:$PATH \
  GOCACHE=/tmp/gagent-803-go GOLANGCI_LINT_CACHE=/tmp/gagent-803-lint \
  go test ./tests -run '^TestRecallInformation_(Free|RejectsContaminatedControls)$' -count=1 -timeout=10m -v
```

Set `GAGENT_INFORMATION_FREE_RESULTS` to a new absolute directory to retain the free run's artifacts. The free checks exercise the real binaries and verify that reference input, full-record offers, question-field disclosure, selected IDs and empty-store controls are observable. Paired contamination checks reject answer-bearing cue inputs, answer-bearing cue replies and post-cue store changes while admitting clean counterparts.

Only after PM instrumentation review and separate spending authorization, reproduce the paid collection with an environment-provided key and a new absolute `GAGENT_INFORMATION_RESULTS` path:

```sh
GAGENT_PAID_MODEL_TESTS=1 GAGENT_INFORMATION_RESULTS=/absolute/new-run-directory \
  go test ./tests -run '^TestRecallInformation$' -count=1 -timeout=90m -v
```

The existing MiniMax-M2 configuration and meter ceilings apply; historical USD rates remain accounting assumptions. The design shares model, wording and the initial frozen agent-written store across arms; post-cue store equality must be checked rather than assumed. An interrupted process can leave only the incrementally written evidence; inspect its row count and terminal records before treating it as complete. No result here establishes scalable retrieval, sustained work, correction/withdrawal, effective forgetting or learning from failed experience. Those sequences remain follow-up evidence for the wider owner goal.

Instrumentation validation on the prototype: the restored free run completed 28 probes, three plants and seven cues (76 scripted calls, 7,600 synthetic input tokens and 1,520 synthetic output tokens, zero USD and no refusals). These are fixture-supplied usage counts, not a provider cost measurement. Five independent compiler-overlay removals were observed to fail their assertions:

| Removed mechanism | Failing free check |
|---|---|
| Full catalogue offered to C | `TestRecallInformation_Free`: the anonymous grounded probe no longer injects the target. |
| Conversation forwarded to B/C resolver | `TestRecallInformation_Free`: actual read input no longer contains the referent. |
| Whole-record injection replaced by legacy value-only injection | `TestRecallInformation_Free`: the question-carried target is no longer credited. |
| Contamination verdicts | `TestRecallInformation_RejectsContaminatedControls`: an answer-bearing cue is incorrectly admitted. |
| Complete request capture | `TestSpendMeter_RetainsCompleteExposure`: the original messages and tool result cannot be recovered. |

The removal runs changed only temporary compiler inputs and were restored before the passing run. The same tests run under `make ci`; the paid test remains skipped without opt-in.

### Paid evidence and attribution

- Published raw evidence: [calls.json](results/information-2026-09-21/calls.json), [plants.json](results/information-2026-09-21/plants.json), [held.json](results/information-2026-09-21/held.json), [probes.json](results/information-2026-09-21/probes.json), [run.json](results/information-2026-09-21/run.json), [source.json](results/information-2026-09-21/source.json) and [frozen/memory.db](results/information-2026-09-21/frozen/memory.db). [sha256.txt](results/information-2026-09-21/sha256.txt) pins all seven copies; run `sha256sum -c sha256.txt` from that directory.
- Copied byte-for-byte from PM-owned `/home/sauce/Workspace/garam/gagent/.local/verification/2026-09-21-information-ca12175/`. The original `run.json.status` remains `invalid conditions; semantic review pending`; collection completion and semantic success are separate.
- Semantic judgments below are the independent adjudication accepted by the PM in [issue 803's publication instruction](https://github.com/garamsh/gagent/issues/803#issuecomment-5750998207), checked again against the raw requests, final replies, terminal events and pre-probe records for publication. They are independent agent judgments reviewed by the PM agent, distinct from automatic harness scores; no human adjudicated this run. Attribution source: `/home/sauce/Workspace/garam/gagent/.local/research/information-adjudication-803.md`, SHA-256 `c2a089c27aeb4de550da3648b548ad6343cd2984d8df651bfd688c6369d74209`. This experiment record is the repository home for those judgments and conclusions.
- The fixed criteria require the Hana/Finnish relation for explicit/referent probes, both person and language for anonymous probes, supported uncertainty for absent/empty probes, and both supported club actors or explicit recognition of ambiguity for the club control. Positive misses fail even when uncertainty is supported by the act input. Unsupported assertions are assessed separately from correctness and literal containment.
- Source attribution: all three instrument hashes in `source.json` match the files at the measured revision. The retained history/grounded/names binaries match its hashes; the task binary hash is recorded but its binary was not retained for independent rehash. Compiler overlay inputs and binaries stay in the PM raw directory and are not duplicated here; the pinned test source defines their construction.
- Plant writebacks and the immutable SQLite snapshot agree with all three complete records in `held.json`; archive is empty. Finnish occurs in Hana's question, Thameside rowing club in Yusuf's question, and Hilltop chess club in Mira's value, with June as a restriction. The original historical result files remain unchanged.
- All 28 probes, three plants and seven cues have `turn_finished` evidence. All 89 recorded requests name `MiniMax-M2`, return HTTP 200 and reconcile with the flattened usage and response fields. No terminal failure was observed; HTTP success alone was not the completion criterion. PM's accepted collection report records test exit 0 in 433.70 seconds; `run.json.elapsed_ms` is 433,694.

### Paid cost and terminal behavior

| Phase | Turns | Calls | Input tokens | Output tokens | Sum of model-call durations (ms) |
|---|---:|---:|---:|---:|---:|
| Plant | 3 | 6 | 11,397 | 3,534 | 49,188 |
| Cue | 7 | 14 | 26,039 | 2,936 | 46,383 |
| Probe | 28 | 69 | 145,688 | 25,331 | 331,236 |
| Total | 38 | 89 | 183,124 | 31,801 | 426,807 |

Resolver stage: 38 calls, 246,087 ms summed; act stage: 51 calls, 180,720 ms summed. Run elapsed includes build/preparation and is not just model latency. Meter cost is $0.0930984 at its recorded $0.30/$1.20 per million input/output rates, an accounting estimate rather than an invoice. Ceilings were 150 calls, 1,200,000 input tokens, 300,000 output tokens and $0.29.

Latency is summed per-call duration, not a distribution or parallel wall time. Token totals are provider-reported usage from the full requests in `calls.json`, whose recorded meter rates are the cost basis; no current price or invoice is inferred.

- Actual tool-result messages and events show eight successful empty `files list .` results, one successful `shell_exec ls -la` with only directory entries, and one `message_send` queued to `unknown`/`unknown`; queueing does not prove external delivery. Inspection found no tool execution error in these ten outcomes.
- Three act calls stopped without writeback (`1/task/referent`, `2/grounded/referent`, `control/history-empty`); each emitted `writeback_error`, received the required-writeback notice and completed. `run.json.refusals` is null and all 89 calls were admitted. Inspection of recorded response text found no provider safety refusal; no-selection and unknown answers are different observations.

### Per-case semantic judgments

`Literal` means final-reply Finnish containment only. `Injection` lists actual recalled IDs; IDs 1/2/3 denote Hana/Yusuf/Mira. `Catalogue` describes resolver disclosure, not act exposure: `Q` is Finnish in question only (never value), `names` has no event, `empty` has no rows, and `none` has no catalogue. Every `Q` catalogue contains all three frozen records. `Correct unknown` passes the absent/empty criterion without establishing exhaustive memory search. A positive miss remains a semantic failure even when uncertainty is sensible from the act input. `None` in unsupported claims means no unsupported assertion found in the final reply under this fixture; scope cautions are explicit below.

| Case | Validity | Literal | Injection | Catalogue | Semantic verdict | Unsupported claims / qualification | Raw call sequences |
|---|---|---|---|---|---|---|---|
| 1/task/explicit | Valid | yes | 1 | none | Correct Hana/Finnish relation | None | 7,8 |
| 1/task/referent | Invalid: cue changed frozen records | no | none | none | Miss: no requested positive answer | No invented fact; lack-of-context claim applies to current act/session only | 11,12,13 |
| 1/task/anonymous | Valid | no | none | none | Miss: no requested positive answer | Scope caution: no other sources is broader than the workspace search | 14,15,16,17 |
| 1/task/absent | Valid | no | none | none | Correct unknown / clarification | None | 18,19 |
| 1/history/explicit | Valid | yes | 1 | none | Correct Hana/Finnish relation | None | 20,21 |
| 1/history/referent | Valid | yes | 1 | none | Correct Finnish for she = Hana | None | 24,25 |
| 1/history/anonymous | Valid | no | none | none | Miss: no requested positive answer | None | 26,27 |
| 1/history/absent | Valid | no | none | none | Correct unknown / clarification | None | 28,29 |
| 1/grounded/explicit | Valid | no | none | Q | Miss: no requested positive answer | Unsupported store-wide assertion that question matches no facts | 30,31,32 |
| 1/grounded/referent | Valid | yes | 1 | Q | Correct Finnish for she = Hana | None | 35,36,37 |
| 1/grounded/anonymous | Valid | yes | 1 | Q | Correct Hana/Finnish relation | None | 38,39 |
| 1/grounded/absent | Valid | no | none | Q | Correct unknown / clarification | None | 40,41,42 |
| 2/task/explicit | Valid | yes | 1 | none | Correct Hana/Finnish relation | None | 43,44 |
| 2/task/referent | Invalid: cue changed frozen records | no | none | none | Miss: no requested positive answer | Unsupported history-wide claim: not told yet; plant did tell it | 47,48 |
| 2/task/anonymous | Valid | no | none | none | Miss: no requested positive answer | None | 49,50,51 |
| 2/task/absent | Valid | no | none | none | Correct unknown / clarification | None | 52,53 |
| 2/history/explicit | Valid | yes | 1 | none | Correct Hana/Finnish relation | None | 54,55 |
| 2/history/referent | Valid | yes | 1 | none | Correct Finnish for she = Hana | None | 58,59 |
| 2/history/anonymous | Valid | no | none | none | Miss: no requested positive answer | None | 60,61,62 |
| 2/history/absent | Valid | no | none | none | Correct unknown / clarification | None | 63,64 |
| 2/grounded/explicit | Valid | no | none | Q | Miss: no requested positive answer | No invented fact; no information is true of act input, not full store | 65,66,67 |
| 2/grounded/referent | Valid | yes | 1 | Q | Correct Finnish for she = Hana | None | 70,71,72 |
| 2/grounded/anonymous | Valid | yes | 1 | Q | Correct Hana/Finnish relation | None | 73,74 |
| 2/grounded/absent | Valid | no | none | Q | Correct unknown / clarification | None | 75,76 |
| control/history-empty | Invalid: empty-store cue wrote records | yes | 1 | none | Correct Finnish for she = Hana | None | 79,80,81 |
| control/anonymous-empty | Valid | no | none | empty | Correct unknown / clarification | None | 82,83,84 |
| control/ambiguous | Valid | no | 3 | Q | Incomplete: Mira only; Yusuf omitted | Mira statement supported; no explicit uniqueness claim | 85,86 |
| control/names-only | Valid | no | 3 | names | Miss: no requested positive answer | No invented event in reply; resolver selected Mira without event evidence | 87,88,89 |

### Cue adjudication

- `1/task/referent` (calls 9–10) adds ID 4, `user / who does the user refer to in the next question = Hana`; `2/task/referent` (45–46) adds ID 4, `next question / who does the next question refer to = Hana`. Neither discloses Finnish to cue act or reply, but both violate the frozen-store condition. A resolver sees only the pronoun query (calls 11/47), while act sees the cue; both miss. These are invalid comparisons, not clean 0/2 controls.
- B cues (22–23, 56–57) and C cues (33–34, 68–69) acknowledge, recall no row and write no facts. Their act input/replies are answer-free, and all four pre-probe stores equal the frozen fixture. B's probe resolver gets the referent but no Finnish. C's cue resolver already sees Finnish in its catalogue, as intended for that arm; its output does not carry that answer into the conversation, and its probe independently gets the catalogue. The answer-free claim applies to conversation/act, not to the C resolver's catalogue.
- `control/history-empty` (77–78) writes the Hana timing record and acknowledges Finnish. At probe time it has one row; calls 79–81 both expose answer-bearing history and inject that row. Correct output does not isolate history from memory, so this control is invalid.

### Selection and answer-channel findings

- Grounded explicit repetition 1, call 30: full catalogue and verbatim instruction are present, yet the call requests `Hana / what language did Hana start learning`, a key not offered. Validation rejects it, and act calls 31–32 have no recalled row. Its final claim that the question matches no facts overstates what an unplaced read established.
- Grounded explicit repetition 2, call 65: resolver prose says “She started learning Finnish.” but makes no `memory_read` call. Calls 66–67 receive no row and finish with an unknown answer. This is direct catalogue comprehension without successful transfer to the act step, not a successful user answer. Resolver response text and final containment must not be conflated.
- Grounded anonymous repetitions (38–39, 73–74) select the exact offered Hana timing question, inject ID 1 and answer both person and language correctly. Their resolver already receives the answer in the question field; the result supports full-record discovery on this fixture, not identities-only retrieval.
- Grounded absent repetition 2, call 75, says it will make no call but actually calls the unoffered `fencing tournament / who won` key. Validation rejects it. The final clarification is correct under the fixed absent criterion; the selector's compliance is not. Repetition 1 makes no call. Correct abstention is therefore partly due to the validator in repetition 2.
- Ambiguous control, calls 85–86: resolver sees both clubs and its recorded text acknowledges both, but selects only Mira. Act receives ID 3 only and correctly states Mira's club, failing the fixed ambiguity/completeness criterion. The instrument's `whole_record_injected=true` means any one target, not both; no explicit false uniqueness statement appears in the final reply.
- Names-only control, calls 87–89: offer contains only IDs/names; resolver chooses Mira with no evidence linking her to language learning. It injects the unrelated club row and the act reply correctly says that fact is unrelated. This is failed discovery with an unsupported selection, not invented language-learning content in the final reply.

### Measured findings and limits

| Condition | A task | B history | C full-record grounded |
|---|---|---|---|
| Explicit positive, valid | 2/2 correct; widened ID 1 | 2/2 correct; widened ID 1 | 0/2 correct; unplaced despite disclosure |
| Referent positive | No valid denominator; both cues mutate store, observed misses | 2/2 correct; widened ID 1 with answer-free history | 2/2 correct; addressed ID 1 with separate catalogue disclosure |
| Anonymous positive, valid | 0/2 correct; no target injection | 0/2 correct; no target injection | 2/2 correct; addressed ID 1 |
| Absent event, valid | 2/2 appropriate clarification | 2/2 appropriate clarification | 2/2 appropriate clarification; one rejected fabricated key |

- Collection: 28/28 probes completed. Isolation: 25 valid, three invalid. Among valid cases, 10 positive answers and seven appropriate absent/empty responses meet their fixed semantic criteria; seven positive probes miss and the ambiguous control is incomplete. These heterogeneous totals are accounting, not a competence score. Including invalid cases gives 11 final Finnish-containing replies; all 11 correctly state the language, but one is the contaminated history-empty control.
- Whole-record target injection is true for 12/28 rows: ten valid Finnish answers, the contaminated history-empty answer, and partial club coverage. Legacy value-only Finnish injection is 0/28 because Finnish occurs only in the stored question. Actual recalled IDs also show the names-only control injected Mira despite its target-injection flag being false.
- Valid B referent successes establish that answer-free referent context can support correct subject resolution on these two observations. A's contaminated cues prevent the planned clean matched A-versus-B causal estimate; do not report a measured 0/2-to-2/2 improvement under controlled store conditions.
- Grounded anonymous success versus A/B failure shows a useful discovery path with this complete, three-row offer and this selection instruction. C also regresses on the explicit positive control, so it is not uniformly better. C changes instructions and exposes whole facts; this run cannot attribute the difference solely to identity access.
- The actual request catalogue is 410 bytes including its heading and trailing newline; the raw `catalogue_bytes` field is 411 because `informationExposure` appends another newline to each message. This corrects the accepted adjudication's interpretation of that field after comparing the request content with the pinned helper. It discloses Finnish via question on all nine nonempty C probes, even when no row is injected. The actual empty C catalogue is 209 bytes and names-only offer is 203 bytes; their raw fields remain 210 and 204 respectively. C nonempty probe resolver input costs range 426–542 tokens; names-only uses 386. C's shorter selection instruction replaces the much longer A/B instruction, so these totals are not marginal catalogue token cost and cannot establish lower scalable cost.
- No final reply invents a fencing winner, a wrong language or a wrong club. That does not erase the explicit unsupported store/history assertions and scope caution listed by row. General fencing examples are clarification examples, not claims of a particular winner. Empty workspace observations are supported only for the fresh experiment workspaces; they do not establish empty memory.
- Two repetitions share one agent-written three-row fixture, one model and one fixed arm order. They are not independently planted stores, a population estimate, a scale measurement or evidence of stable long-term behavior. The known-absent event probes safe uncertainty; they do not show exhaustive absence discovery by A/B, which lack a catalogue. No correction, withdrawal, growing memory, resumed work, forgetting or learning from failure was exercised.

The comparison therefore leaves the clean A/B referent contrast unresolved, and C is not an adoption candidate on this evidence. Repairing cue/store isolation, measuring selection separately from recognition/injection, and testing multi-record completeness are follow-up questions. [Issue 806](https://github.com/garamsh/gagent/issues/806) owns the later selection comparison; no new model calls or architecture changes are part of this publication.

### Publication validation

- `env -u GAGENT_PAID_MODEL_TESTS PATH=/home/sauce/go/bin:$PATH GOCACHE=/tmp/gagent-803-publication-go GOLANGCI_LINT_CACHE=/tmp/gagent-803-publication-lint make ci`: exit 0 on the measured source with this documentation/data publication; lint reported 0 issues, formatting checked 142 files, tests passed in 16 packages (five packages have no test files), and build covered 21 packages. Paid tests were not enabled. Exact output is retained at `/tmp/gagent-803-publication-ci.log`.
- `python3 /tmp/gagent-803-audit.py`: exit 0; independently compared all seven copied files with raw bytes and SHA-256, all four historical result files with `git show ca121754269b582853784b63f5e12cd0c92fcb8f:<path>`, all 89 wire/flattened records, phase/stage totals, 38 terminal turns and every probe's call mapping, catalogue and injected record identity. SQLite was opened with `mode=ro&immutable=1`; active rows match `held.json` and archive is empty. The audit script and output `/tmp/gagent-803-publication-audit.log` are retained for PM inspection.
- This publication changes only this record and its new data directory. It adds no tests and changes no measured source, so the previously documented removal checks were not rerun. `make ci` checks the existing instrument; it does not validate paid semantic conclusions or privileged deployment behavior.


## Record selection comparison (issue 806)

`tests/record_selection_test.go` compares the existing offered-key candidate with
selection by existing record ID. The single PM-authorized run is now collected
and independently adjudicated below; the published information comparison and all
historical result bytes are preserved.

### Method and premises

- Reuse the published `information-2026-09-21/frozen/memory.db`, never plant again.
  Check the published file hashes, read a disposable copy through the real store,
  compare it with `held.json`, and assert all three complete agent-written facts:
  Hana/Finnish/September, Yusuf/Thameside rowing/May and Mira/Hilltop chess/June.
  Finnish and Thameside occur in questions, so value-only matching is insufficient.
- Run two repetitions of explicit language, anonymous language, absent fencing and
  multi-actor club questions per candidate, then one fresh empty-store anonymous
  control per candidate: eighteen probes. Each probe gets a fresh agent, workspace
  and store copy, with its actual active records checked before the question. There
  are no preparation turns. Both candidates retain the same history forwarding,
  but fresh sessions contain no prior conversational evidence.
- `keys` is the existing grounded compiler overlay: complete record catalogue,
  exact offered subject/question selection, and the existing addressed read. It
  retains its original instruction and single-address limitation. `records` changes
  the read instruction, tool schema, validation and retrieval to select an array of
  existing IDs and render exactly those rows. Both still advertise `memory_read`;
  its actual parameter schema distinguishes them. An empty array is explicit
  abstention; all relevant actors can be selected together. Prose without a call,
  malformed arguments, duplicate IDs and unknown IDs fail this candidate's turn.
- Build real binaries using temporary Go compiler overlays. Product files stay
  unchanged. Retain the overlay inputs, source diffs (including records versus
  keys), source and binary hashes, model name and checkout revision. This compares
  two candidate contracts, not the isolated effect of integer IDs: instruction,
  schema, abstention representation and failure handling also differ.
- Retain complete requests and responses, actual selection arguments, every act
  exposure, pre-probe records and terminal events. The meter also retains response
  bytes as base64, including non-JSON failures and partial bodies on read errors.
  Tokens, calls, per-call latency, elapsed time, spend estimates and refusals remain
  in the raw evidence. A collection can finish while individual turns fail.
- Missing/wrong selection schemas, missing catalogue rows or missing resolver
  telemetry invalidate the measurement premise. Missing fixture semantics aborts
  before probing. Inspect these before interpreting a null result; a transport or
  spend failure is not an empty selection. Interrupted runs remain partial and
  write their collected transcript during cleanup.

### Scoring and adjudication

| Dimension | Criterion |
|---|---|
| Selection | Classify offered key, unoffered key, existing records, empty selection, prose without selection, invalid arguments, unknown/duplicate ID or transport/spend failure from actual responses. Keep raw arguments even when rejected. |
| Selection completeness | Exact expected ID coverage: Hana for language, Yusuf and Mira for club, none for absent/empty. Key-arm no-call abstention is scored under its own contract. |
| Evidence reaching act | Score recalled IDs separately from selection; one club actor is incomplete. Keep actual act requests to verify the complete records reached act. |
| Task behavior | Require an act request and `turn_finished`; a correct resolver-only answer is never a successful user answer. |
| Semantic correctness | Adjudicate the final reply against Hana/Finnish for explicit, both person/language for anonymous, both club actors or explicit recognition of ambiguity for club, and supported uncertainty for absent/empty. |
| Unsupported claims | Judge independently, including claims about the whole store that the act input does not establish. |

Semantic fields remain `pending adjudication` until an identified reviewer examines
requests, selections, exposures and final replies. Publish those judgments in this
record with per-case call references and reviewer attribution; do not silently
convert literal containment or synthetic endpoint replies into model competence.
Explicit recognition of club ambiguity may satisfy the answer criterion while
incomplete evidence selection still fails its separate coverage criterion.

### Instrument validation and execution

The free tests substitute only the external model endpoint. Real workspace and
agent binaries, SQLite, gateway events and act prompts execute. The scripted model
returns one key versus two IDs for club, so the accepted plural case is paired with
an incomplete single-actor case. Unknown-ID, duplicate-ID, null, key-shaped,
prose-only and HTTP failure responses are paired with a valid ID response.

- `env -u GAGENT_PAID_MODEL_TESTS GOCACHE=/tmp/gagent-806-go GAGENT_SELECTION_FREE_RESULTS=/tmp/gagent-806-free-results go test ./tests -run '^Test(RecordSelection_(Free|RejectsInvalidSelections)|SpendMeter_RetainsFailureBody)$' -count=1 -v` passed the initial restored instrument; its synthetic eighteen-probe transcript contains 36 calls, 3,600 input tokens, 720 output tokens, zero USD and no refusals. These counts are scripted, not provider usage. The final instrument is validated below.
- `python3 /tmp/gagent-806-removals.py` ran four temporary source removals and restored each file in `finally`. Each underlying `go test` exited 1: mapping the record candidate to the key binary failed `wrong selection contract`; replacing complete coverage with any-match failed `one club actor is incomplete`; ignoring unknown IDs failed the invalid-selection turn-failure assertion; removing response-byte capture failed both JSON and non-JSON failure-body assertions. The script and `/tmp/gagent-806-removal-*.log` retain exact mutations and commands for PM review.
- The first free run (`env -u GAGENT_PAID_MODEL_TESTS GOCACHE=/tmp/gagent-806-go go test ./tests -run '^TestRecordSelection_(Free|RejectsInvalidSelections)$' -count=1 -v`, `/tmp/gagent-806-free.log`, exit 1) failed because the new observer read event `type` instead of
  the actual `kind`; both terminal-success assertions caught it. The first
  `make ci` reached lint and failed on numeric HTTP 503 in the new endpoint;
  it now uses the named constant (`/tmp/gagent-806-ci.log`, exit 2, same `make ci` command as the passing run). A plain `go mod tidy` failed on the read-only
  default Go cache; `GOCACHE=/tmp/gagent-806-go go mod tidy` passed without module
  changes. These are instrument/environment failures, not model observations.

- `env -u GAGENT_PAID_MODEL_TESTS PATH=/home/sauce/go/bin:$PATH GOCACHE=/tmp/gagent-806-go GOLANGCI_LINT_CACHE=/tmp/gagent-806-lint make ci` exited 0: lint reported 0 issues, formatting checked 143 Go files, tests passed in 16 packages (five have no test files), and build covered 21 packages. The final log is `/tmp/gagent-806-ci-reviewed.log`; paid tests remained disabled.
- Final free run: `env -u GAGENT_PAID_MODEL_TESTS GOCACHE=/tmp/gagent-806-go GAGENT_SELECTION_FREE_RESULTS=/tmp/gagent-806-reviewed-results go test ./tests -run '^Test(RecordSelection_(Free|RejectsInvalidSelections)|SpendMeter_RetainsFailureBody)$' -count=1 -v` exited 0 in 7.395 seconds. Its eighteen-probe transcript again contains 36 synthetic calls, 3,600/720 tokens, zero USD and no refusals; every probe reached act and finished. The seven accepted/failure controls and two failure-body cases also passed. Output: `/tmp/gagent-806-reviewed-free.log`.
- `python3 /tmp/gagent-806-audit.py` exited 0: all twelve historical result files match their bytes at `dcc252b8171ac5d889369eb9a65dc84f3d682da7`; all 89 historical wire usage records reconcile; the two explicit failure catalogues and club catalogue contain every frozen record. The final free transcript has eighteen rows, complete record-arm selections and no invalid premises. Output: `/tmp/gagent-806-audit.log`.

The reviewed instrument used this opt-in command shape:
`GAGENT_PAID_MODEL_TESTS=1 GAGENT_SELECTION_RESULTS=/absolute/new/record-selection-2026-09-21 go test ./tests -run '^TestRecordSelection$' -count=1 -v`,
with the existing MiniMax key supplied by the operator through the environment.
The result directory must be nonempty, absolute and not already exist; the assigned
name belongs to this round, while the instrument also permits later named reruns. The existing ceilings remain 150 calls,
1,200,000 input tokens, 300,000 output tokens and $0.29 estimated at the recorded
$0.30/$1.20 per-million rates; they are accounting assumptions, not a current price
quote. A token/USD limit can be exceeded by the last admitted call's unknown usage.
The instrument implementation round read no credentials and made no paid calls;
the subsequent authorized PM run is recorded below.

### Collected result and provenance

The PM agent ran the authorized collection once on 2026-09-21 at measured source
`efd0fdec7912ac0ba3dc076ee66ffa70fab0ed7e`, using MiniMax-M2. An independent
worker agent (Orca task `task_cd751602d36d`, dispatch `ctx_958482918ea3`), who did
not author the instrument, adjudicated the raw requests, responses and events below.
For the subsequent PM agent publication review, see
[PR 808](https://github.com/garamsh/gagent/pull/808); these are agent reviews,
not human adjudication. The publication commit is separate from the measured source and is
identified in that PR's body after committing.

- [Raw publication](results/record-selection-2026-09-21/) contains unchanged
  `run.json`, `probes.json`, `calls.json`, `held.json`, `source.json`, both overlay
  JSON files, original `sha256.txt`, invocation/exit JSON and run log, four source
  differences and four exact compiler sources. Compiler sources have `.go.txt`
  suffixes so archival inputs do not become build/format targets; their bytes are
  unchanged. `publication-manifest.json` maps all nineteen copies to original
  paths and SHA-256 values. Original absolute overlay paths remain unchanged.
- Original evidence and both binaries remain under
  `/home/sauce/Workspace/garam/gagent/.local/verification/2026-09-21-selection-efd0fde/record-selection-2026-09-21/`.
  The verified binary SHA-256 values are keys
  `eb2d72a2fbe7dcc754c5a3c42eee98f562a75311df374f572f9b390361e7d80a`
  and records `781e35705e7f924363f9267363374fde3006712d10af907a4b8e8aad7c1815b6`.
  Binaries are retained locally, not duplicated in git. All eight source hashes in
  `source.json` match both the measured commit and the publication checkout.
- `invocation.json` records `go test ./tests -run '^TestRecordSelection$' -count=1 -v -timeout 45m`;
  `exit.json` reports exit 0. This proves completed collection, not semantic success.
  The raw pending semantic fields stay untouched; this section owns adjudication.
- The instrument checkpoint, restored free/CI logs and four exact removal
  mutations/logs were read at
  `/home/sauce/Workspace/garam/gagent/.local/verification/806-review/`.
  Candidate distinction, plural coverage, unknown-ID rejection and response-byte
  capture removals each failed their named assertion; these are instrument checks,
  not additional paid observations. This publication round made no paid calls and
  read no credentials.

### Premise audit

All eighteen comparisons have valid setup premises. The prior plant messages and
writebacks establish Hana/Finnish/September, Yusuf/Thameside rowing/May and
Mira/Hilltop chess/June; an immutable read-only SQLite query confirms exactly those
three active records and an empty archive. All sixteen nonempty pre-probe snapshots
match them; the two empty snapshots contain zero records. Every resolver request
has an empty conversation, its intended question, the correct arm's actual tool
schema, and exactly the full catalogue (or no rows for the empty controls). No
preparation turn or prior answer contaminates these probes.

The actual key schema requires `subject` and `question`; the record schema requires
an integer `record_ids` array with unique items. Both prohibit extra properties.
Every request exposes only `memory_read` to the resolver. All eighteen responses
contain one selection call: six offered and three unoffered keys, six nonempty ID
arrays and three empty ID arrays. There are no paid unknown-ID, duplicate-ID,
malformed-argument or resolver-prose-only cases. All forty responses are HTTP 200,
with no recorded transport error or spend refusal; decoded response bytes match
parsed responses, flattened arguments and provider usage on every call. Thus no
selection failure here is explained by transport or missing catalogue evidence.

All eighteen turns reach act and emit `turn_finished`. Actual act requests contain
only the selected complete rows, never the resolver's full catalogue. The key
club cases expose only Mira; the record club cases expose both actors, including
Yusuf's club in the question and Mira's June restriction. Empty evidence in a
negative control is the expected exposure, independently of whether selection
obeyed the contract.

### Per-case adjudication

Call numbers refer to `calls.json`'s one-based `seq`; the first call in each row is
the resolver and all remaining calls are act. `H` is offered Hana key / ID 1;
`M` is offered Mira key / ID 3; `Y+M` is IDs 2 and 3. Coverage means exact expected
IDs and complete record content in every act request. All rows have valid premises,
reach act and finish. Each final reply was checked independently for unsupported
person/language/club/winner and store/history claims: none was found in these final
replies. Generic tournament examples are requests for clarification, not asserted
winners; incomplete club replies do not explicitly claim uniqueness.

| Case | Calls | Selection validity and completeness | Act coverage | Final semantic decision |
|---|---|---|---|---|
| 1/keys/explicit | 1–2 | H: valid, complete | Complete: 1 | Pass: Hana/Finnish |
| 1/keys/anonymous | 3–4 | H: valid, complete | Complete: 1 | Pass: Hana/Finnish/September |
| 1/keys/absent | 5–6 | Unoffered key: invalid, incomplete | Expected none | Pass: uncertainty and clarification |
| 1/keys/club | 7–8 | M: valid, incomplete | Incomplete: 3 only | Fail: Mira only, no ambiguity acknowledged |
| 1/records/explicit | 9–10 | ID 1: valid, complete | Complete: 1 | Pass: Hana/Finnish |
| 1/records/anonymous | 11–12 | ID 1: valid, complete | Complete: 1 | Pass: Hana/Finnish/September |
| 1/records/absent | 13–14 | Empty array: valid, complete | Expected none | Pass: uncertainty and clarification |
| 1/records/club | 15–16 | Y+M: valid, complete | Complete: 2,3 | Pass: both actors, clubs and months |
| 2/keys/explicit | 17–20 | H: valid, complete | Complete: 1 | Pass after writeback correction: Hana/Finnish |
| 2/keys/anonymous | 21–22 | H: valid, complete | Complete: 1 | Pass: Hana/Finnish |
| 2/keys/absent | 23–24 | Unoffered key: invalid, incomplete | Expected none | Pass: uncertainty and clarification |
| 2/keys/club | 25–26 | M: valid, incomplete | Incomplete: 3 only | Fail: Mira only, no ambiguity acknowledged |
| 2/records/explicit | 27–28 | ID 1: valid, complete | Complete: 1 | Pass: Hana/Finnish |
| 2/records/anonymous | 29–30 | ID 1: valid, complete | Complete: 1 | Pass: Hana/Finnish |
| 2/records/absent | 31–32 | Empty array: valid, complete | Expected none | Pass: clarification without a winner claim |
| 2/records/club | 33–34 | Y+M: valid, complete | Complete: 2,3 | Pass: both actors, clubs and months |
| control/keys/empty | 35–37 | Unoffered key: invalid, incomplete | Expected none | Pass: context absent; requests clarification |
| control/records/empty | 38–40 | Empty array: valid, complete | Expected none | Pass: uncertainty; requests clarification |

- Calls 5 and 23 explicitly recognize no relevant fencing record but nevertheless
  call `Mira / who won a fencing tournament` and `fencing tournament / who won`.
  Call 35 mistakes the current task's `unknown` sender for a candidate and calls
  `unknown / Who started learning a language, and which language was it?` despite
  an empty catalogue. All three are rejected as unoffered. Their act input says
  memory was not consulted, so correct final uncertainty is validator-mediated,
  not compliant key-arm abstention. The ID controls select `[]` in calls 13,31,38.
- Club resolver text in calls 7 and 25 recognizes both actors before selecting
  Mira alone. That comprehension is neither complete selection nor a final answer.
  The final replies in calls 8 and 26 omit Yusuf and acknowledge no ambiguity;
  they fail the fixed answer criterion while making supported Mira statements.
- Call 19 answers in prose and prints a JSON-like writeback without calling
  `memory_write`. A `writeback_error` event records this; call 20 receives the
  correction instruction and completes the writeback. It is a recovered act
  protocol error, not a transport failure or resolver failure. The terminal reply
  meets the answer criterion; collection exit alone would hide this intermediate
  failure. Calls 18,36 list empty workspaces; call 39 runs `ls -la` successfully.
  These extra tools supply no target fact and do not establish memory-store absence.
- Six final writebacks contain memory operations (calls 2,10,20,22,26,30): four
  express Hana/Finnish under language questions, call 22 repeats her September
  timing fact, and call 26 writes a Mira-only club answer. These writes occur
  after selection inside disposable probe stores, so they do not contaminate the
  next verified fixture copy. This is neither a read-only nor a fixed two-call
  workflow; consequences of retaining those new addresses across turns are unmeasured.

### Counts, costs and observed differences

| Dimension | Keys (9 probes) | Records (9 probes) |
|---|---|---|
| Valid selection under own contract | 6/9 (66.7%) | 9/9 (100%) |
| Complete valid selection | 4/9 (44.4%) | 9/9 (100%) |
| Expected act evidence coverage, including empty controls | 7/9 (77.8%) | 9/9 (100%) |
| Explicit / anonymous final answers | 2/2 / 2/2 | 2/2 / 2/2 |
| Club coverage and final adequacy | 0/2 / 0/2 | 2/2 / 2/2 |
| Absent plus empty compliant selection / adequate final uncertainty | 0/3 / 3/3 | 3/3 / 3/3 |
| Adequate final replies across these mixed cases | 7/9 (77.8%) | 9/9 (100%) |
| Act reached / terminal finished | 9/9 / 9/9 | 9/9 / 9/9 |

These mixed-case totals account for this collection; they are not a general
competence rate. No final unsupported target or store/history claim was found in
18/18 inspected replies, within the scope defined above. This does not make the
three rejected keys valid or the two partial club replies complete.

| Calls | Count | Input tokens | Output tokens | Estimated USD | Sum of call latency (ms) | Min / median / max call latency (ms) |
|---|---|---|---|---|---|---|
| Keys | 21 | 40,728 | 8,598 | 0.022536 | 105,413 | 1,651 / 3,442 / 21,592 |
| Records | 19 | 33,632 | 3,987 | 0.014874 | 56,281 | 1,479 / 2,742 / 5,238 |
| Resolver | 18 | 7,071 | 7,496 | 0.0111165 | 90,173 | 2,039 / 2,929 / 21,592 |
| Act | 22 | 67,289 | 5,089 | 0.0262935 | 71,521 | 1,479 / 2,978.5 / 6,924 |
| Total | 40 | 74,360 | 12,585 | 0.037410 | 161,694 | 1,479 / 2,929 / 21,592 |

Costs use the retained meter assumptions of $0.30/$1.20 per million input/output
tokens, not an invoice or current price claim. Every wire usage record reconciles
with the flattened counts and both aggregate spend records. No ceiling refused a
call. `run.json` elapsed time is 165,766 ms (including setup/build); invocation to
process exit is 188.576647 seconds. Per-call latency and each turn's duration are
retained separately, not substituted for wall time.

Compared with the prior #805 information run at
`ca121754269b582853784b63f5e12cd0c92fcb8f`, the key candidate's explicit failures
(calls 30 and 65 there) did not repeat: it now selects Hana correctly twice.
Anonymous success repeats in both arms. The earlier single Mira club selection
(call 85 there) repeats twice in the key arm, while the ID contract selects and
transfers both actors twice. Earlier grounded absent probes included one no-call
abstention and one rejected key (call 75); this run's two key absent probes and
its empty control all submit rejected keys. This is evidence of observed contract
behavior, not proof that explicit-key failure was repaired: the key instruction
and schema remain the same. The complete catalogue still discloses the answers.

The new run removes preparation cues, compares only these two candidates, repeats
the club case and tests fresh empty stores in each arm. It does not retest the
prior A/B referent comparison or resolve its contaminated controls. All twelve
historical result files were compared byte-for-byte against measured commit
`efd0fdec7912ac0ba3dc076ee66ffa70fab0ed7e` and remain unchanged. The shorter ID
requests and lower observed time/cost do not isolate an ID effect: instructions,
schema, plural selection, abstention and failure contracts change together;
fixed order, output length and the key arm's extra act calls also differ.

### What this result leaves undecided

The measured ID candidate removes read-side question reconstruction,
verbatim key copying, single-subject selection and address-to-subject widening
from the candidate read path. Its complete selection and supported answers on these
nine cases do not establish that question-shaped writes
or the store's current schema should be removed. Failure to retain abstention or
multi-record coverage prevents adoption even if key reconstruction disappears.

The ID candidate reads `Store.Active` and renders selected rows directly, bypassing
`memory.Service` access recording. It neither preserves nor measures last-used/access
bookkeeping and cannot establish retention equivalence with addressed reads.

The full three-record catalogue already discloses answers to the resolver. This
experiment measures selecting and transferring that evidence, not scalable search.
Two repetitions share one frozen store, one model and fixed arm order. Clean
conversational-reference isolation, corrections/withdrawals, retention under growth,
resumed complex work and experience transfer remain untested. No accepted
architecture decision changes, and no result of these eighteen probes would alone
establish reliable continuation or learning.

### Publication verification

- `env -u GAGENT_PAID_MODEL_TESTS PATH=/home/sauce/go/bin:$PATH GOCACHE=/tmp/gagent-806-publication-go GOLANGCI_LINT_CACHE=/tmp/gagent-806-publication-lint make ci` exited 0: lint 0 issues, format 143 Go files, tests passed in 16 packages with five having no test files, build 21 packages. Log: `/tmp/gagent-806-publication-ci.log`. Paid opt-in was absent; this checks the instrument, not semantic judgments or privileged deployment.
- `python3 /tmp/gagent-806-publication-audit.py` exited 0: eighteen request/schema/catalogue/pre-store/selection/act/event mappings, forty response-byte/parsed-response/argument/usage reconciliations, eight measured source hashes, two retained binary hashes, seven original manifest entries and nineteen publication copies. The twelve historical files match the measured head byte-for-byte. SQLite was opened with `mode=ro&immutable=1`. The script initially assumed recall lived in the first system message; inspecting the actual message list corrected the audit to inspect all system messages. No evidence was changed. Log: `/tmp/gagent-806-publication-audit.log`.
- `GOCACHE=/tmp/gagent-806-publication-go go mod tidy` and `git diff --check` passed without module changes. This publication changes no test or product source; the instrument's four removal checks were inspected, not rerun. PM checkpoint/free/removal/CI evidence remains at the path above.
- GitHub reported draft PR 808 at the measured head with no check suites. `.github/workflows/ci.yml` targets only `main`, so this task PR targeting `dev` has no host run; local `make ci` is the reported check, not a claim of hosted verification. Publication-head state is recorded in the PR.
