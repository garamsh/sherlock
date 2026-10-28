# General-agent preregistration

Fixes issue #1164's comparison of the shipped loop S against the file-and-skill loop H before its first paid call: the corpus, the scorer, the arm commits, the model and how it is chosen, the decision rule, the guardrails and the controls.

## Question

Does a loop in the style of Claude Code and Hermes — bounded memory files the agent edits itself, skills loaded on demand, and older conversation reached only by a search the agent makes — do general work better than the shipped loop, for no more than a stated margin of extra cost? General work is coding, writing to a format, multi-step file work, and continuing work across a session boundary. The probe is whether the work is done and a correction the person gave on the first instance still holds on later instances, after the correction has left the conversation window.

## Arms

| Arm | Commit | Source |
|---|---|---|
| S | `90e403add20b670fae1794f2e3e7b02961581710` | origin/dev when the study branch was cut: pre-turn recall (two newest mid episodes, then similar and entity-walk episodes, then pending previews), the 2,000-character note restated in every terminal call, `memory_search` and `memory_inspect` |
| H | `e435edbf3928fca3b8c03ba8effb21501b4ce1e1` | branch `sauce-git/test/general-agent-arm-h`, one commit on S, never merged |

H differs from S in three ways, each in `internal/agent/` with the store change it needs in `internal/memory/sqlite/`:

1. **Memory files, no pre-turn recall.** `USER.md` (at most 1,375 characters) and `MEMORY.md` (at most 2,200) lead every turn under the instructions. The agent replaces one whole with the reserved `memory_file_write`; a write over the bound is refused as the call's result. The terminal call carries the reply alone. Nothing is recalled before the act loop, and the per-turn frame block is gone. A note saved before H becomes `MEMORY.md` once, on open.
2. **Skills.** The reserved `skill_write` saves a skill as a kebab-case name, a one-line description of at most 200 characters and a body of at most 4,000; at most 16 are kept. Every turn shows each skill's name and description under "Your skills"; `skill_view` returns a body when the agent asks for it.
3. **The source-linked store stays.** Synthesis, the episode store, `memory_search` and `memory_inspect` are S's, reached only when the agent calls them.

Production lines, from `git diff --numstat 90e403add20b670fae1794f2e3e7b02961581710 e435edbf3928fca3b8c03ba8effb21501b4ce1e1 -- . ':(exclude)*_test.go'`: +347 −394, net −47.

### Where H departs from Claude Code and Hermes

Every claim about the two agents is `research/agent-context/README.md` §The agents at this commit, which holds for Claude Code 2.1.283 and Hermes Agent `v2026.9.24`.

- **Bounds and refusal follow Hermes.** Hermes caps `MEMORY.md` at 2,200 and `USER.md` at 1,375 characters and returns an error on a write over the cap so the model consolidates; H takes both numbers and the refusal. Claude Code loads the first 200 lines or 25 KB of its `MEMORY.md` index instead.
- **One whole-file write.** H's call replaces a file whole. The survey does not record what edits Hermes's `memory` tool offers; Claude Code's model writes typed notes plus an index. H keeps S's practice of restating the whole, but as a call the agent may skip, where S's note is a required field of the terminal call. #862's pilot found MiniMax-M2 left an optional note null on every turn (`research/agent-context/README.md` §Pilot), which is why the exercise control below exists.
- **Stored in `memory.db`, not on disk.** Both agents keep these files in the filesystem; H keeps them in the agent's SQLite file beside the conversation (ADR 0002), because the file tools reach the workspace, which programs the agent runs share, and not the agent's own memory.
- **Read every turn.** Hermes snapshots the files at session start and shows a write from the next session on; H re-reads them every turn, as S re-reads the note. Every live input in this study starts from a restart, so the two do not differ here.
- **Placement.** Hermes puts the files in the volatile tier of its system prompt, Claude Code in a user message after the system prompt. H puts them in the leading system message after the platform contract, where S puts the note.
- **No background writer.** Hermes's forked review every ten user turns may write memory and skills, and Claude Code has an extractor behind a remote flag. H writes only during the turn.
- **Skills.** Hermes shows a skills index and loads bodies with `skill_view`, and opencode lists skills in its system message; H does both, with bounds of its own, since the survey records none. H has no way for the person to invoke a skill by name, as Codex's `$skill` does.
- **Search.** Hermes's `session_search` runs FTS5 over past sessions; H keeps S's `memory_search` over mid-episode summaries, by embedding similarity and the entity walk, and `memory_inspect` for exact wording.
- **No compaction summary.** Every surveyed agent summarizes on compaction; H, like S, drops the oldest messages. That is outside this study.

### What H leaves unchanged that an adoption would change

The monitoring stream loses its `recall` event and the `note` field of `writeback` in H. `deploy/runtime-fixture/snapshot.sh` and the opt-in real-model runtime fixture still read the note, the earlier studies' harnesses under `research/` no longer build against H, and `docs/architecture/agent.md`, `memory.md` and ADR 0011 and 0019 still describe S. H's store keeps `RecentMid` and `PendingSources`, which only tests call after the change. None of this is read by this study.

## Model

One model for both arms, every call, reached through OpenRouter (`https://openrouter.ai/api/v1`, `openai-compatible`) and pinned to one provider endpoint with fallbacks off.

- **The ladder** is [`evidence/models.json`](evidence/models.json), written by [`models.py`](models.py) from OpenRouter's public model and endpoint lists, read at 2026-10-28T10:32:40Z. It keeps endpoints that list `tools`, are up and hold at least 32,768 tokens, of fixed models (no `:free`, `:batch`, router or `~` alias). It ranks them at 20 input tokens to one output token. The list is complete for every price below USD 0.1095 per million tokens at that ratio. `:free` variants are left out because their rate limits do not allow a run of this size.
- **The rungs**, each model at its cheapest eligible endpoint, USD per million input and output tokens:
  1. `mistralai/mistral-nemo` at `io-net/fp16`: 0.01656, 0.0216
  2. `openai/gpt-oss-20b` at `darkbloom/fp8`: 0.018, 0.09
  3. `inclusionai/ling-3.0-flash` at `novita`: 0.021, 0.063
  4. `nvidia/nemotron-3.5-lightning` at `darkbloom/int4`: 0.025, 0.12
  5. `openai/gpt-5-nano` at `openai/flex`: 0.025, 0.2
  6. `qwen/qwen3.7-flash` at `alibaba`: 0.03, 0.13
- **Tool calling works** on a rung when the pilot (below), over its live turns in both arms, has all three:
  - at least 90% of live turns end through the terminal call without error;
  - at least 80% of synthesis calls answered with exactly one usable `memory_summary` call;
  - a successful `file_write` in at least 50% of the live turns of the code, file-work and continuation families.
- **The model is the first rung that works.** The pilot runs rung 1 first and moves down the ladder only when a rung fails. If no rung of the six works, or the pilot reaches its ceiling first, the study stops and reports before collection.
- **Amendment 1** records which rung worked, its pilot figures, its measured cost per episode and the projected collection cost. It is committed before the first collection call. It changes nothing else in this file.

## Embedder

Local text-embeddings-inference as pinned in `research/mid-memory-rag/README.md` §Production parity: image `ghcr.io/huggingface/text-embeddings-inference@sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d`, `BAAI/bge-base-en-v1.5` at revision `a5beb1e3e68b9ab74eb54cfd186867f64f240e1a`, in a container named `tei-1164` on `127.0.0.1:8090`, removed when the run ends.

## Corpus

[`corpus.json`](corpus.json), from [`corpus.py`](corpus.py) at seed 1164: 92 families in four kinds, each with 15 primary, 4 control and 4 reversal families. Every family is one episode of three instances of one kind of task, on an empty store.

- **Code (K):** a Python module with a function to a precise spec, scored by its cases run in a `bwrap` sandbox with no network, no home directory and a 10-second limit. Rules: an SPDX first line, modules under `src/`, a pytest file beside the module, type hints, docstrings.
- **Writing (W):** exactly three bullet points about the subject, read from what the person reads (the sent message, else the reply). Rules: a `STATUS:` or `TL;DR:` opening, a `— Juniper` sign-off, lowercase, no full stop at a bullet's end.
- **File work (T):** five multi-step jobs over seeded files (sum a ledger, find a file, extract lines, count lines, find a maximum), each scored by the exact content of its output file. Rules: list before reading, results under `out/`, a `worklog.md` line, reading the result back.
- **Continuation (C):** a four-part guide whose part order is stated only on instance 1; instances 2 and 3 say "write the next part" and name neither the guide nor the part. The next part is the first one the guide lacked before the turn; it must be added after the earlier parts with text under it, and no later part may appear. Rules: a `Status: draft` line under the new heading, numbered headings, an `In short:` closing line, a `_Draft by Sherlock_` last line.

Within a family, instance 1 is the task, then (primary and reversal) a turn that states the rule and asks for the same task again. Then come 40 messages of recorded filler from #1153's corpus (`research/mid-memory-rag/evidence/replication/conversations.jsonl`), then instance 2, then 40 more, then instance 3. Every live input runs in a runtime built for it, so it starts from what a restart restores. The window is 32 messages, so the correction has left it at every probe. A reversal family's instance 3 states a new rule that replaces the first.

There are 184 probes: 120 primary (instances 2 and 3), 48 control (every instance) and 16 reversal (instance 3), over 352 live turns per arm.
- **Primary probe:** the task is done and the rule holds.
- **Control probe:** the task is done and none of the kind's rule markers appears.
- **Reversal probe:** the task is done, the new rule holds and the old one does not.

## Endpoint, guardrails and decision rule

[`score.py`](score.py) computes every figure below from the arms' `results.jsonl` and the proxy ledger.

- **Primary endpoint.** H against S on the 120 primary probes, paired by family and step, by the exact two-sided McNemar test. There is one comparison.
- **Guardrails.**
  - Control probes: H holds at least S's count less 3, of 48.
  - Reversal probes: H holds at least S's count less 2, of 16.
  - Cost per completed task: H's is at most 1.20 times S's. A completed task is a live turn whose task check holds. An arm's cost is what the ledger charged it, with a synthesis the cache answered charged what it first cost.
- **H is valid** when it holds more primary probes than S with p < 0.05, every guardrail holds and every run-time control below holds. Otherwise the shipped structure stands. A run missing a family in either arm, or with a failed control, is undetermined.

## Controls

Each control names the near miss it must reject, and where that was shown before this commit.

| Control | Required | Near miss it rejects | Shown by |
|---|---|---|---|
| Every rule and task check accepts the work it names | every case | a prefix in mid-reply, a sign-off on the first line, a numbered list for bullets, a fourth bullet, the SPDX line on line 2, a module in `source/`, a test file with no test or for another module, a missing return annotation, a comment for a docstring, an off-by-one total, the right content under another file name, the result read before it was written, a skipped or headless part, the rule on the first part only, the footer on the first line | `score_test.py`, 35 tests pass |
| Every code task's cases have one right answer | 12 of 12 references pass | a near miss of each function that gets one case wrong fails, 12 of 12 | `score_test.py` `CodeTaskTest` |
| The sandbox contains the code it runs | no network, no home, a hang fails | a module that loops forever fails within 10 s; a probe that tries the network and `/home` reads neither | `score_test.py` `CodeTaskTest` |
| A probe holds only when task and rule both hold | — | a rule kept on a task not done | `score_test.py` `test_a_rule_kept_on_a_task_not_done_does_not_hold` |
| The correction has left the window: statically | more than 32 filler messages between the correction and every probe | the same corpus with 24-message blocks | `corpus_test.py` |
| No probe restates its rule | never | the correction turn, which carries it | `corpus_test.py` |
| The correction has left the window: at run time | no probe turn's restored history holds an earlier correction, in either arm | the same family run with a 256-message window holds it | `harness_test.go` `TestRun_TheWindowControlSeesACorrectionTheWindowHolds`; `score.py` `controls` |
| S recalls and H does not | the recall block in every S turn and no H turn | the S tree shows it to the model (the harness test logs `true` in the S tree and `false` in the H tree) | `harness_test.go` `TestRun_TheRecallControlReportsWhatTheModelWasShown`, run in both trees; `score.py` `controls` |
| Each arm's structure is exercised | at least 30 of 60 primary correction turns leave S's note non-empty, and at least 30 of 60 make a successful H `memory_file_write` or `skill_write` | a refused write, and a file H holds but did not write in that turn | `score_test.py` `ControlsTest` |
| The proxy routes every request to the pinned model and provider with usage reporting, and refuses a request past the ceiling before sending it | every request | a ceiling below the request's worst case | `harness_test.go` |
| Spend is OpenRouter's | the ledger's total within USD 0.05 or 10% of the key's usage delta over the run | — | readings of `GET /api/v1/key` before and after each paid phase |

A failed run-time control makes the verdict undetermined, and the report names it. A null result answers the question only when the exercise control holds: a run in which neither arm keeps a correction measures the model, not the structures.

## Budget and collection

- **Ceilings.** The pilot's proxy ceiling is USD 3.00. Collection's ceiling is USD 25.00 less the pilot's spend, and collection does not start if its projection crosses it. The proxy reserves every attempt's worst case before sending it, at the rung's listed prices, and settles at the cost OpenRouter reports.
- **Pilot.** Families `K01 K16 K20 W01 W16 W20 T01 T16 T20 C01 C16 C20` (one primary, control and reversal family of each kind), in both arms, on one ledger. Pilot results are not scored for the verdict; collection runs every family afresh.
- **Projection before the pilot**, to be replaced by Amendment 1: about 15 million input and 2 million output tokens for both arms. That is 352 live turns, three act calls of about 5,000 tokens each, and up to 30 syntheses of about 700 tokens per family. The act-call size is from a free smoke run whose probe calls held 11,859 (S) and 10,959 (H) characters of messages on average, plus tool schemas. At rung 1 that is USD 0.29, and USD 0.71 at rung 6. Reasoning models' hidden output tokens are not in it.
- **Re-runs.** A family that fails on HTTP 402, 429, a server fault or a transport error is moved to a `results-attempt-*.jsonl` file and re-run from scratch. The report gives the count per arm. Any other failure counts against the arm.
- **The key.** `OPENROUTER_API_KEY` is exported only into the proxy's process and is never printed, logged or committed.
- **Workers.** Two families at a time per arm.
- Nothing in the arms, the harness, the corpus or the scorer changes between this commit and the end of scoring, except by a dated amendment to this file committed before the call it governs.

## Revision

- Corpus and scorer: `75008ea9d587162c6107c45793e5f61f0d4d6075`, `66df62d8095a2dce4ab837380b131b3d6d37793d` and `020f08ab1d03dbae07ceacd14160e5a85e684c47`. Harness: `4f2385e81a229dbb437219324e417b1ec66807b0`. Model ladder: `0f066c0ecebeb4100e48e9f84ec70ed16079ab8e`.
- Each arm's harness binary is built from that arm's tree with `research/general-agent/harness/` copied in.

SHA-256 of the inputs:

- `corpus.json`: `f6741da44f451be5453d311fe806528f4632d0d2780cc4794954044f493da52b`
- `corpus.py`: `14801575cee9d4a83f63145dd4c893e927c7cf7abf4d1ab5546ddc12f24bac6d`
- `score.py`: `41d83c0313f1725d4f879d9f2e9a38664804b00f83c182f874dbeffc2287382e`
- `harness/main.go`: `2247b3c682df04cadeeca011b1e04e5f22d1bf590be64f62429dfef44b4f78f0`
- `harness/proxy.go`: `402237b21194476c574ea0cea44fa1b000650102790f120fe13e338a514c139d`
- `harness/run.go`: `81fb0dee75913f2652998385d3c6c585f52d098e15eca687322b81125db3beb1`
- `harness/workspace.go`: `ac2c147d8d1647d4421efc1b36b92fb09c2b8788158897ed3fc4951ec71385f3`
- `evidence/models.json`: `b5c4d26548a8edd873de562c038ec45bc33e1a84eb6a2cb3196b481fd0bc7c23`

## Amendment 1 — the model is MiniMax-M2 (2026-10-28, before the first paid call)

The PM changed the model at the pre-pilot gate (PR #1168, decision of 2026-10-28T10:44:19Z). This amendment replaces §Model's ladder and its rule "the first rung that works". Nothing else in this file changes.

- **Model.** `minimax/minimax-m2` for both arms and every call, pinned to OpenRouter's `minimax/fp8` endpoint with fallbacks off. Its list price, read on 2026-10-28 from `https://openrouter.ai/api/v1/models/minimax/minimax-m2/endpoints`, is USD 0.255 per million input tokens and USD 1.020 per million output tokens. That endpoint lists `tools` and a 204,800-token context. The proxy runs with `-model minimax/minimax-m2 -provider minimax/fp8 -input-usd 0.255 -output-usd 1.02`.
- **Reason.**
  - The deployed agent calls MiniMax-M2 (garamsh/garam#1645), and every earlier study in `research/` ran on it, so a result on M2 speaks for the deployed agent and can be compared with them.
  - A null on the cheapest model would not answer the question: it could mean that H does not help, or that the model cannot use files and skills at all.
- **The tool-calling check stays, as a validity gate.** The pilot applies §Model's three criteria to M2. If M2 fails any of them, the study stops and reports, with no fallback to another model. [`evidence/models.json`](evidence/models.json) and the ladder stay as the record of what was planned.
- **The pilot's record is Amendment 2.** It holds the check's result, the measured cost per episode per arm and the projection.
- **Projection on M2, before the pilot.** M2's reasoning tokens are billed as output. MiniMax-M2's completions in #862, #864 and #870 averaged 213–233 tokens per act call and 198–240 per synthesis, with 90th percentiles of 386–417 and 300–406 (`research/agent-context/evidence/{stage2,confirm,unmixed}/ledger/calls.jsonl`, uncached calls with HTTP 200). Act prompts averaged about 4,300 tokens there, at a window of 32.
  - With three act calls of about 5,000 input tokens per live turn and up to 30 syntheses of about 380 input tokens per family, both arms come to about 12.7 million input tokens. The output is about 1.8 million at those means, or 3.1 million at the 90th percentiles.
  - That is USD 5.1 at the means and USD 6.3 at the 90th percentiles. The pilot replaces this estimate.
- **Output bound.** The proxy's bound stays at 2,048 tokens per act call and 1,024 per synthesis. Both are above the 90th percentiles above.

### Amendment 1a — routing (2026-10-28, before any charged call)

- The first pilot request, for family W01 in arm S at 10:46:38Z, was refused by OpenRouter with HTTP 404 "No endpoints found that can handle the requested parameters". The proxy had set `require_parameters: true`. Production sends `parallel_tool_calls`, which the `minimax/fp8` endpoint does not list among its supported parameters, so no endpoint was left. Every call got 404, and nothing was charged.
- `b95712c6f66898e9386c51cd51817bd8d616a6aa` drops `require_parameters`. The request is still routed only to `minimax/fp8` with fallbacks off, and OpenRouter ignores the parameter that endpoint lacks.
- The refused attempt's ledger and record are kept under [`evidence/pilot-routing-404/`](evidence/pilot-routing-404/), and the pilot starts again in a fresh directory.
- `harness/proxy.go` is now SHA-256 `21bf8dfe9a62069fc1ebf827ebb2743f738f551f776ef1a7386e400af656a336`.

## Amendment 2 — pilot record (2026-10-28, after the pilot, before any collection call)

This amendment records the pilot. The PM's brief calls it "Amendment 1"; that number had already gone to the model change. Evidence is under [`evidence/pilot/`](evidence/pilot/): results, transcripts, the ledger, and `scores.json` from `score.py evidence/pilot`. The pilot ran the twelve families of §Budget and collection in both arms on `minimax/minimax-m2` at `minimax/fp8`, from 10:47Z to 11:21Z.

### Spend

| Reading of `GET /api/v1/key` | Time (UTC) | `usage` (USD) | `limit` (USD) |
|---|---|---:|---:|
| before the pilot | 2026-10-28T10:45:20Z | 0.007 | 50 |
| during the pilot (the PM's reading) | 2026-10-28T10:55:03Z | 0.0700 | — |
| after the pilot | 2026-10-28T11:21:22Z | 0.49056313 | 50 |

- **The pilot's spend** is USD 0.4836 by the key, or USD 0.4895 by the ledger. The ledger holds 530 paid calls, each with the cost OpenRouter reported, and 400 synthesis cache hits. The ledger is USD 0.006 above the key's delta, inside the spend control's USD 0.05.
- The key is shared, so the delta bounds the pilot's spend rather than isolating it.
- The refused attempt of Amendment 1a cost nothing.

### Cost per episode

An arm is charged its paid calls plus what each cache hit first cost (§Endpoint). OpenRouter reports completion tokens with M2's reasoning tokens included, and its reported cost covers them.

| Arm | Episodes | USD charged | USD per episode | Act calls (per live turn) | Act tokens in / out | Paid synthesis tokens in / out |
|---|---:|---:|---:|---:|---|---|
| S | 12 | 0.2688 | 0.0224 | 125 (2.84) | 468,031 / 23,860 | 34,041 / 21,867 |
| H | 12 | 0.4110 | 0.0343 | 238 (5.41) | 882,617 / 36,930 | 53,859 / 37,623 |

- 108 of H's 238 act calls ended in prose with no tool call, against 19 of S's 125. The session then asks again for the terminal call, which is most of H's extra calls.
- Cost per completed task in the pilot was USD 0.0114 for H against USD 0.0066 for S, 74% higher. The guardrail allows 20%. The pilot is not scored for the verdict.

### Projection for collection

- **Shared-cache basis:** 92 families at the pilot's spend per family pair (USD 0.4895 / 12 = USD 0.0408) come to USD 3.75.
- **No-cache basis:** the sum of the two arms' per-episode charges (USD 0.0567) comes to USD 5.22.
- **With half again for re-runs and variance:** at most USD 7.8.
- **Fit:** this is under USD 24.51, the study ceiling less the pilot's ledger spend, with more than three times margin.

### The tool-calling check on M2

| Criterion | Required | Result |
|---|---|---|
| Live turns ending through the terminal call without error | ≥ 90% | 86 of 88 (97.7%). S 44 of 44. H 42 of 44: W01 step 3 ran out of iterations, and one turn failed to record its conversation after three unusable syntheses. |
| Synthesis calls answered with one usable `memory_summary` call | ≥ 80% | Paid answers only: 119 of 167 (71.3%), **fails**. Counting answers served from the cache: 519 of 567 (91.5%), passes. |
| A successful `file_write` in code, file-work and continuation turns | ≥ 50% | 65 of 66 (98.5%). S 33 of 33, H 32 of 33. |

- **How usable was counted.** The ledger does not record a response's tool call, only its `finish_reason`. A paid synthesis counts as usable when it finished on `tool_calls`. The 48 that finished on `stop` carried no tool call and are unusable on any reading, so 71.3% is an upper bound for paid answers.
- **Where the unusable syntheses are.**
  - By arm: S 62 of 64 usable, H 57 of 103.
  - All 46 unusable H syntheses come from two continuation episodes, C01 (25) and C20 (21). Those episodes took 817 s and 976 s.
  - They left 10 batches pending in H after three attempts each, which production retries at the next completion.
  - What in those batches made M2 answer in prose cannot be read from the evidence, because the proxy keeps no request bodies.
- **§Model leaves the denominator unstated.** It does not say whether cache-served answers count. The amendment does not choose: under the paid-answer reading M2 fails, and the PM's ruling of 2026-10-28 is to stop and report. Collection does not start until the PM rules on the reading.

### Controls in the pilot

- **Window:** no probe turn's restored history held a correction, 0 of 20 probes in each arm. Holds.
- **Recall:** the recall block was in 44 of 44 S turns and 0 of 44 H turns. Holds.
- **Exercise:** each arm kept the correction in 4 of 4 primary families: S's note non-empty, and a successful H `memory_file_write`. `score.py` reports `exercise_holds` false only because its floor of 30 is set for the 60 primary families of collection.
- **Spend:** the ledger is within USD 0.006 of the key. Holds.
- **Scorer near miss not foreseen.** S's W01 probe at step 5 begins `**STATUS:**`, with the prefix in bold, and the prefix check fails it. §Controls lists a prefix in mid-reply as a near miss, not a prefix in emphasis. Whether emphasis around a required prefix keeps the rule is a question for the PM. The scorer is unchanged.
