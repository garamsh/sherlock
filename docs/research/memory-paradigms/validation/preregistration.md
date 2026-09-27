# Paradigm validation: preregistration

Fixes issue #900's run before its first model call. The run measures the full-context ceiling W and tests the recall sub-agent R and the idle-time memory document D against the shipped recall S, on #890's corpus. The design is `../README.md` §Candidates for a validation run and §Test plan, which stand as written except where issue #900 changes them: one corpus, and W on a stratified sample. The run uses #874's harness, proxy and study package (`../../chained-recall/`) and #890's corpus, scorer and matching (`../../memory-encoding/validation/`).

## Question

Does a recall step whose citations are checked in code (R), or an idle-time memory document with cited lines (D), make the agent apply what its primary family needs more often than S, while holding the absent, correction, single-hop, exact, situated-absent and update guards? And with the history under a 180,000-token window in view (W), how far does the agent get, and does it still answer absent questions from associated material?

## Corpus and shared store

- **Corpus.** `../../memory-encoding/validation/corpus.json` as merged in `47a15a5`, SHA-256 `63e5c3df2772bd39…`. It is the corpus `../tokens.json` measured at `46ca289`; the two files are byte-identical. Every command reads it through the study's new `-corpus` flag and writes its evidence under this directory, so nothing is written under #890's.
- **Families.** C2 50, C3 50, IC2 25, IC3 25, SC 50, UP 50, C1 30, E 30, K 40, KU 30, N 60, NC 30: 470 probes.
- **Shared store.** `harness build -batch 4 -dir docs/research/memory-encoding/validation`, from #890's recorded stream syntheses and the replication's base; no call.
- **S offline** is recorded again here and must equal #890's `evidence/offline/S/b4/probes.jsonl` in every field but timing (`score.py offline`, `S_equals_890`). If it does not, the run stops.

## Arms

| Arm | Command | Setting | What it is |
|---|---|---|---|
| S | `../../memory-encoding/validation/arms/shipped` | `b4` | the shipped recall, `study.ShippedArm` |
| W | `arms/full` | `w180k` | S with the live window raised to the newest 6,890 messages, which fit 180,000 tokens in M2's chat template (`../tokens.json`, oldest source 2,039) |
| R | `arms/agent` | `b4-agent` | before the act loop, a recall step of at most 6 calls with `memory_search`, `memory_inspect` and `recall_result`, the last call offering only `recall_result`; it reads the task and the 4 newest window messages. A fact reaches the actor only when every source it cites was read by `memory_inspect` in that step. The actor keeps its own search and inspection, and sees the kept facts, or one line saying none were found, in place of the seven recalled mids |
| D | `arms/document` | `b4-doc16k` | a pass over every 8 mids, base and stream in order (279 passes), returns operations only: add, supersede line n, drop line n. Code writes each line with the pass's mids it cites, rejects an operation citing another mid, refuses a pass dropping more than a quarter of the lines or passing 16,000 characters and asks again with the reason, and after a third attempt applies it partially. Every version is kept. Recall is every line, then S's recall |

Each arm's code comments state its rules; its tests name each one.

## W's sample

W gates nothing. It runs on a stratified sample, drawn by `sample.py` with `random.Random(900)` from each family's probe IDs in corpus order: C2 25, C3 25, N 30, K 20, NC 10, 110 probes. `sample.json` (SHA-256 `801fb08e5887b5ef…`) lists them.

## Controls

Fixed before any call. A null for an arm is an answer only when its controls passed (`score.py controls`, `evidence/controls.json`).

- **W.**
  - Every W act call in the ledger has between 180,000 and 196,608 prompt tokens.
  - Every W probe's restored window starts at source 2,039 (`window_first` in its live record).
  - A smoke run of 5 probes, the first of each sampled family, records per-call latency first. If its p90 exceeds the study's 300-second request timeout, the full run stops, and a budget revision is committed as an amendment before it resumes.
- **R.**
  - The recall step ran in at least 95% of R's live probes.
  - The citation check dropped at least one fact across the run; otherwise it was never exercised.
  - Reported: the share of kept facts whose cited sources state every value token of the statement but its first word (#874's matching, `score.py` `supported`).
- **D.**
  - The finished document holds at most 16,000 characters.
  - Every line cites mids, and only mids its pass was given.
  - The history holds every version: one per applied add, supersede and drop.
  - At least half of the 50 UP facts are stated by a line (the fact's word, #874's matching).
- **Mutation checks.** Removing each behaviour `mutation.py` names breaks a named test (`evidence/mutation.txt`).

## Offline stage

- S and D record `study.Offline`. `score.py offline` scores them with #890's reach. D's lines are recall items whose interval spans the mids they cite.
- **D is admitted to the live stage** when its controls passed and, on UP, it reaches at least 5 of 50 more than S, with more D-only than S-only items and exact McNemar p < 0.05 (#890's margin).
- W and R have no offline stage. W's reach is set by its window, and R's recall step is live.

## Live stage

- S, R and W always, and D if admitted.
- S, R and D answer all 470 probes; W answers its 110.
- One live turn per probe on its own copy of the arm's build, with an empty note and a 32-message window, W's excepted. MiniMax-M2 through the study proxy, two probes at a time. A probe that fails on infrastructure is re-run once (`study.Live`).
- *Applied* as #890's `score.py live` scores it: the answer stated (chains, SC, UP), the new value (K, KU), or an abstention, counting a clarifying question (N, NC).

## Decision

- R on C2 and C3, and D on UP if live, each against S: exact McNemar on applied, Holm across the candidates compared, α = 0.05.
- **Guardrails,** probes lost against S at most: N 3 of 60, K 2 of 40, C1 3 of 30, E 3 of 30, NC 2 of 30, KU 2 of 30.
- IC2 and IC3 are reported with their pairs and p, and gate nothing.
- A candidate is valid when significant with its guardrails held. Among valid candidates, the one with the fewest net production lines is named, by the upper ends of `../README.md`'s estimates: R +190, D +215. If none is valid and every control passed, the shipped recall stands.
- **W is outside the decision and the Holm family.** On each sampled family it is reported beside S with pairs and p, and beside every #890 arm's offline reach on the same probes. If #896's live records on this corpus are on `dev` or #896's branch when the report is written, #896's arms are reported beside W as well. K is split by whether its evidence lies in W's window.

## W's hypotheses, and how each is read

From `../README.md` §W, on the sample:
1. **Chains.** With every hop in view, W applies C2 and C3 at least as often as L did. On this corpus L has no live record, so W's 50 sampled chains are read against L's offline reach on the same probes, and against L's live rate on #885's and #888's corpora (56 and 60 of 100). The hypothesis holds if W applies at least 28 of 50. Fewer means length costs reach on this model.
2. **Absent questions.** W holds S's abstention if it loses at most 2 of its 30 N probes against S, the share #890 allows for its 30-probe guards.
   - If it holds, L's losses came from how memories were presented.
   - If W loses more, association in view costs abstention whatever the structure.
   - How often W states the associated value is reported beside it.
3. **Corrections.** W holds S's K if it loses at most 1 of the K probes whose evidence lies in its window.
4. **Implicit chains.** IC is not in the sample, so this hypothesis cannot be read, and the report says so.

## Paid phases, in order

Each is MiniMax-M2 through the one study proxy (`harness proxy`, ledger `evidence/ledger/`, list-price ceiling USD 40). This issue's worker runs it with `MINIMAX_KEY` in its environment only. One paid phase at a time, and at most two requests at once.

1. **D's consolidation:** 279 passes in order, one at a time. Estimate: 1.3M input and 0.3M output tokens.
2. **The live stage,** after the PM confirms that #896's live stage has ended, in this order:
   - S's live stage: about 5M input tokens;
   - R's live stage: about 10M;
   - D's live stage, if admitted: about 10M;
   - W's smoke run, 5 probes;
   - W's live stage, the other 105 probes: about 41M at 369,000 per probe.

## Cost rules

- MiniMax only, on the subscription, through the one proxy; nothing is bought; no other paid API.
- On HTTP 429 with code 2056 the proxy pauses every request and probes at most every 30 minutes; callers wait on `/budget`.
- Embeddings come only from local text-embeddings-inference.
- Tokens are reported per phase (`score.py tokens`).
