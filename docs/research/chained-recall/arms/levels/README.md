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

Pending: the batch-4 shared store and the group synthesis.

## Limits

- **The summarizer is the batch summarizer.** Its prompt describes conversation messages, and here it receives mid summaries. A production group call would likely get its own prompt; this arm measures the smallest change.
- **The budget is S's per probe.** Production would need a fixed budget. Here the per-probe budget makes "equal context characters" hold probe by probe.
- **Offline only.** Whether a delivered group leads the model to `memory_inspect` the right source is the live stage's question.
