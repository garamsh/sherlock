# Arm H: contiguous group summaries

Records arm H of the #874 chained-recall study (issue #881): batch-4 mids plus a second level of group episodes, one per contiguous window of G mids, ranked with the mids in one list. It is survey candidate H (`../../../associative-memory/README.md` §H), a test of compression that the literature predicts will not chain.

## Method

- **Store.** Each setting copies the batch-4 shared store (`study.Env.SharedStore(4)`) and adds a `level_group` table to the copy (`build.go`).
  - Settings `g4` and `g8`: G = 4 and G = 8 batch-4 mids.
  - Every complete window of G mids, in source order, gets one group row. It holds the window's union interval, one summary and that summary's embedding from the local embedder.
  - The corpus's 1,624 batch-4 mids fill 406 windows of 4 and 203 of 8, so no mid is left ungrouped.
- **Group synthesis.** One call per window of the production summarizer (`internal/agent/memory/summary.go` at `ed88e87`), unchanged, through the study proxy as phase `synth-g<G>`.
  - Its updates are the window's G mid summaries, each prefixed `summary of messages <first>-<last>:`.
  - The stored summary gets the production store's redaction and its 1-to-2,000-character bound.
  - Each summary is recorded in `evidence/groups-g<G>.jsonl` as it lands. A rebuild replays the recording without a call.
- **Ranking** (`memory.go`). One collapsed list: every mid and every group, by cosine similarity of its embedding to the query, newer first among equals. This follows RAPTOR's collapsed tree.
- **Recall(task).**
  - The two newest mids come first, as the shipped recall delivers them.
  - The collapsed list follows, without those two, in order. The list stops at the first item that would take the items past a budget, as RAPTOR's collapsed tree stops at its token budget.
  - **The budget is S's context for the same task:** the `study.ContextChars` of the shipped recall (two newest plus five similar mids) over the same batch-4 mids. So H never delivers more context characters than S at batch 4 does for that probe.
- **Search(query, limit).** The collapsed list's top `limit`, mids and groups alike. A group can be 2,000 characters, so five results can carry more characters than five mids.
- **Trace.** Every Recall and Search records the groups and mids it delivered; Recall also records its budget.
- **Instructions.** None.

## Preregistration

Committed before the first group-synthesis call and not edited after it. Everything here holds for the corpus with SHA-256 `034439bafec98e2462e91a0d4ce731aa1a9b3a2d4dd366d3c13cd437ba7c8e47` and the shared basis at `ed88e87`.

### Paid phase: group synthesis

- **What is called.** `MiniMax-M2` through the study proxy (`../../README.md` §Shared services), with the production summarizer as described under Method. No other model, endpoint or embedder is used.
- **How many.** One call per window, re-sent only by a re-run after a failure. That is 406 calls at G = 4 and then 203 at G = 8.
- **Estimate.** Input is G mid summaries. On the base, the replication's batch-4 summaries (`../../../mid-memory-rag/evidence/replication/synthesis-n4.jsonl`) give a mean window of 2,046 characters at G = 4 and 4,093 at G = 8.
  - The replication's synthesis calls averaged 557 input and 368 output tokens for about 750 characters of messages (`../../preregistration.md` on `sauce-git/test/chained-recall-874`, at `1982942`).
  - From that, a G = 4 call should take about 1,000 input and 700 output tokens, and a G = 8 call about 1,700 and 800.
  - Totals: about 0.75M input and 0.45M output tokens.
- **Pacing.**
  - One call at a time: G = 4 first, then G = 8.
  - The run starts only once the batch-4 shared store exists.
  - The first failed call stops the run, and a re-run resumes after the recorded windows. On HTTP 429 with code 2056 the proxy pauses and probes at most every 30 minutes, and the run waits for it to resume.
- **Ledger.** The proxy's `calls.jsonl` attributes every call to arm `H`, episode `g<G>-<window>` and phase `synth-g<G>`. The results table's tokens are read from it.

### Controls and thresholds

Required before H's offline comparison is read as a result:

- **Groups stored for every complete window.** `built.json` reports 406 groups at `g4` and 203 at `g8`, with no ungrouped mid, and each group's interval is its window's union. The test `TestBuild_StoresOneEmbeddedGroupPerCompleteWindow` checks the same rule on a fixture.
- **Groups delivered in the ranked list.** At least one group is in the Recall of at least 10% of the 240 probes (180 chains, 60 guards) at each setting. Below that, the ranking does not bring groups into context, and H is reported as not measured at that setting.
- **Each hop's oracle is reachable.** `Search` on the hop's own statement delivers, within the top 5, an item whose interval holds that statement. This must hold for at least 95% of the 405 hops (385), at each setting.
- **Mutation checks**, each seen to fail on `ed88e87` with the arm's tests:
  - removing group synthesis (Build stores no group) fails `TestBuild_StoresOneEmbeddedGroupPerCompleteWindow`, `TestSearch_RanksGroupsAndMidsInOneList` and `TestRecall_DeliversGroupsWithinTheCharactersOfTheShippedRecall`;
  - removing group ranking (Open reads no group) fails `TestSearch_RanksGroupsAndMidsInOneList` and `TestRecall_DeliversGroupsWithinTheCharactersOfTheShippedRecall`;
  - removing the budget stop fails `TestRecall_DeliversGroupsWithinTheCharactersOfTheShippedRecall`.

### Endpoints and the live-stage margin

- **Endpoints.** The study's offline endpoints (`../../preregistration.md` at `1982942` §Offline stage), scored by `score.py offline`:
  - chain reach on the 150 k ≥ 2 chains against S at batch 4, by exact McNemar;
  - per-hop, per-k and guard-family reach;
  - context characters per turn and stored characters per source character.
- **Beside them:**
  - group synthesis calls and tokens;
  - groups delivered per probe;
  - single-hop reach (C1 and the E and K guards) against S at batch 8.
- **Live-stage margin, fixed by the PM before any H result.** H goes live only if its full-chain reach on k ≥ 2, at equal context characters, trails S at batch 4 by no more than 5 percentage points.
- **Survey hypothesis** (`../../../associative-memory/README.md` §H):
  - H does not raise chain reach beyond S;
  - it reaches single-hop evidence at least as often as batch 8, at a lower stored-characters-per-source ratio.

## Results

Offline stage over the committed corpus, local embedder only.
- **Builds.** `go run ./research/chained-recall/arms/levels build -work <scratch> -proxy http://127.0.0.1:8091`, then `... offline -work <scratch>`.
- **Build basis.** A copy of the batch-4 shared store the #874 worker built, SHA-256 `d2b2bf29705cc87584ecf21274c298063c29591a6ca22432cd963b400010222e`.
- **Scoring.** `python3 score.py offline` at `367d0b1`, with S's records as committed there. The scorer's `scores.json` is left to #874 to regenerate.
- **Records.** `../../evidence/offline/H/g{4,8}/`, and the recorded groups in `evidence/groups-g{4,8}.jsonl`.

### Controls

| Control | Threshold | g4 | g8 |
|---|---|---:|---:|
| Groups stored, one per complete window | 406 / 203, none ungrouped | 406, 0 ungrouped | 203, 0 ungrouped |
| Group intervals equal their window's union | all | all | all |
| Probes whose Recall delivers a group | at least 24 of 240 (10%) | 87 | 44 |
| Oracle: hop's statement held by a top-5 Search item | at least 385 of 405 | 404 | 404 |
| Recalls over S's characters for the probe | 0 | 0 | 0 |

- **Every control passes.** The interval check reads each build's `level_group` rows against its mids.
- **The groups do the work they are counted for.** At g4, 106 of 1,471 recalled items are groups. In 9 hop deliveries only a group holds the hop's statement (6 at g8).
- **Mutation checks:** as preregistered, each mutation failed the tests named there, run on this branch before the paid phase.

### Chain reach at equal context characters

k ≥ 2 is the 150 primary chains. S is batch 4 and 8 as recorded at `367d0b1`.

| Arm | Chains k ≥ 2 | C1 | E | K | Context chars per turn | Stored chars per source char | Vectors |
|---|---:|---:|---:|---:|---:|---:|---:|
| S b4 | 0 | 29/30 | 17/20 | 16/20 | 3,497.5 | 0.705 | 1,624 |
| S b8 | 2 | 26/30 | 11/20 | 14/20 | 4,008.9 | 0.466 | 812 |
| H g4 | 0 | 29/30 | 17/20 | 16/20 | 3,320.8 | 1.107 | 2,030 |
| H g8 | 2 | 28/30 | 17/20 | 16/20 | 3,324.5 | 0.943 | 1,827 |

- **Per family and hop** (hops reached with both entities stated; S b4 in brackets):

  | Family | g4 | g8 |
  |---|---|---|
  | C2 | 47 / 0 (46 / 0) | 45 / 2 |
  | C3 | 44 / 0 / 2 (43 / 0 / 2) | 44 / 0 / 2 |
  | IC2 | 10 / 0 (10 / 0) | 10 / 0 |
  | IC3 | 21 / 0 / 0 (21 / 0 / 0) | 21 / 0 / 0 |

  Later hops are reached almost never, by S or H, as the chain check predicted.
- **Against S b4 on k ≥ 2**, exact McNemar:
  - g4: 0 discordant, p = 1.0;
  - g8: 2 chains H alone, 0 S alone, p = 0.5 and Holm p = 1.0.
- **The budget reproduces S.** H's mean budget is 3,497.5 characters, S b4's measured context per turn. H delivers 5% less because the list stops at the first item that does not fit.
- **Group synthesis** (the proxy's `calls.jsonl`, arm `H`):

  | Phase | Groups | Upstream attempts | Input tokens | Output tokens |
  |---|---:|---:|---:|---:|
  | synth-g4 | 406 | 414 | 260,597 | 213,375 |
  | synth-g8 | 203 | 214 | 216,720 | 133,339 |

  Both are below the preregistered estimate of 0.75M input and 0.45M output.
  - Group summaries average 969 characters at g4 and 1,148 at g8.
  - The group level alone stores 0.402 characters per source character at g4 and 0.238 at g8. That is 0.57 and 0.34 of the mid summaries it covers; RAPTOR reports 0.28.

### Against the hypothesis and the margin

- **Chaining: as predicted, no gain.** H does not raise k ≥ 2 reach beyond S: 0 and 2 of 150, where S b4 reaches 0.
- **Live-stage margin: within it.** H trails S b4 by 0.0 points at g4 and leads by 1.3 at g8, against a 5-point margin. By the margin, H is eligible for the live stage. Whether it goes is #874's choice between L and H, on offline k ≥ 2 reach.
- **Single hop: reached.** H reaches single-hop evidence at least as often as S b8: C1 29 and 28 against 26, E 17 against 11, K 16 against 14.
- **Compression: the stored-characters prediction fails.** H keeps every mid, so it stores more than S b4, not less than S b8: 1.107 and 0.943 against 0.466.
  - The compression H buys is in context, not storage. Recall holds at most S's characters and still reaches what S reaches.
  - A version that stored only groups would store 0.40 or 0.24. It would not be the collapsed tree measured here.
- **ADR 0006.** Each group is written once, over a closed window of G immutable mids, and never re-summarized. No group is an input to another. That is not "recursively summarizing every prior mid state", the running form the ADR rejects. The ADR does not say so, and would have to before H ships.

### Deviations from the preregistration

- **Window 263 at g4 (sources 4193 to 4208) was re-requested after a harness fix.**
  - Its first answer exceeded 2,000 characters and was rejected by the summarizer. The proxy had cached it, so a re-run got the same answer.
  - `367d0b1` makes the proxy cache only summaries the production summarizer accepts, and removed that entry. The window was then re-requested and recorded.
- **The proxy re-sends a failed attempt itself.** After `367d0b1`, 11 over-length answers at g8 were re-sent within the proxy's attempt bound, not by a re-run as the preregistration says. At g4, 6 answers of HTTP 429 `rate_limit_error` were retried the same way. Every window has one recorded summary, and every attempt is in the token table.

## Production estimate

The estimate is read from this arm's code (non-blank, non-comment lines of `build.go` and `memory.go`), dropping what only the study needs: the replay recording, S's budget and the trace.

- **Write, +40 to +55.**
  - After every G mids, one summarizer call over their summaries, bounded and redacted like a mid. The arm's call and window loop come to 25 lines without recording.
  - The group row and its embedding: 15 lines.
- **Read, +30 to +45.**
  - One list over mids and groups in the store's search: the arm's `rank` is 20 lines, half of which the shipped search already has.
  - A level on each hit, and recall's label for a group: 5 to 10 lines.
  - Recall filled to a fixed character budget in place of the fixed five: 10 to 15 lines.
- **Schema and existing databases, +15 to +25.** The group table, and groups written for mids that predate it.
- **Net +85 to +125**, above the survey's +70 to +115.

## Limits

- **The summarizer is the batch summarizer.** Its prompt describes conversation messages, and here it receives mid summaries. A production group call would likely get its own prompt; this arm measures the smallest change.
- **The budget is S's per probe.** Production would need a fixed budget. Here the per-probe budget makes "equal context characters" hold probe by probe.
- **Offline only.** Whether a delivered group leads the model to `memory_inspect` the right source is the live stage's question.
