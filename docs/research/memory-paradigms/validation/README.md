# Paradigm validation

Records issue #900's run: the full-context ceiling W, the recall sub-agent R and the idle-time memory document D of `../README.md` §Candidates for a validation run, measured against the shipped recall S on #890's corpus under `preregistration.md`.

<!-- summary pending the live stage -->

## Contents
- Method
- Offline stage
- D's consolidation
- Live stage
- W beside every arm
- W's hypotheses
- Cost
- Verdict
- Limits
- Layout
- What the study package gained
- Reproduction

## Method

`preregistration.md` fixes the run, with three amendments to D's pass made while it ran. In short:
- **Corpus.** #890's, merged in `47a15a5`: C2 50, C3 50, IC2 25, IC3 25, SC 50, UP 50, C1 30, E 30, K 40, KU 30, N 60, NC 30. It is read through the study's new `-corpus` flag, and the evidence is written here.
- **Shared store.** Built from #890's recorded syntheses with no call. S's offline record here equals #890's in every field but timing (`evidence/offline/scores.json`, `S_equals_890: true`).
- **Arms.**
  - **S** is the shipped recall.
  - **W** is S with its live window raised to the newest 6,890 messages, which fit a 180,000-token history (oldest source 2,039). It runs on a stratified sample of 110 probes, seed 900 (`sample.json`).
  - **R** runs a recall step of at most 6 calls before the act loop. The actor sees only the facts whose every cited source the step read with `memory_inspect`.
  - **D** runs one pass over every 8 mids, 279 in all, which returns operations over a document of cited lines. The document is bounded at 16,000 characters and every version is kept; recall is every line, then S's recall.
- **Decision.** #885's rule over R, and D if live: exact McNemar on the primary family (R: C2 and C3; D: UP), Holm, α = 0.05. Guardrails: N 3, K 2, C1 3, E 3, NC 2, KU 2. W is reported beside S and gates nothing.

## Offline stage

`python3 score.py offline`, with #890's reach. D's lines are recall items whose interval spans the mids they cite.

| Family (n) | S | D |
|---|---:|---:|
| C2 (50) | 0 | 1 |
| C3 (50) | 0 | 0 |
| IC2, IC3 (25 each) | 0, 0 | 0, 0 |
| SC (50) | 0 | 0 |
| **UP (50), primary** | **0** | **14** |
| C1 (30) | 27 | 27 |
| E (30) | 26 | 26 |
| K (40) | 35 | 37 |
| KU (30) | 27 | 28 |
| N, near-miss in view (60) | 7 | 14 |
| NC, near-miss in view (30) | 0 | 3 |
| Context characters per turn | 2,943 | 26,258 |
| Stored characters per source character | 0.745 | 0.758 |

**D's admission, margin and control side by side:**

| Test | Required | Measured | Held |
|---|---|---|---|
| UP margin against S | at least 5 of 50 more, D-only > S-only, p < 0.05 | 14 against 0, 14 D-only, p = 1.2e-4 | yes |
| UP coverage control | at least 25 of 50 UP facts stated by a line | 14 | **no** |
| Bound | at most 16,000 characters | 14,821 | yes |
| Lines cite their pass's mids | all | 166 of 166 | yes |
| History complete | one version per applied operation | 1,404 of 1,404 | yes |

- **D is not admitted to the live stage.** Its UP coverage control failed, so under `preregistration.md` §Offline stage it does not go live, although its margin is met. D is reported offline only.
- The margin and the failed control measure the same 14 items. The document states 14 of the 50 standing facts at the end, and those are the items D reaches.
- **The near-miss values in view double.** N has 14 against 7 and NC 3 against 0, though never beside the query's entity.
- **Context grows ninefold:** 26,258 characters per turn against 2,943. That is 166 lines of about 90 characters, each with its label.

## D's consolidation

From `arms/document/evidence/passes-b4-doc16k.jsonl` and `document-b4-doc16k.json`:
- **The document filled early.** It reached the bound at pass 43, within the base's first 344 mids. From then on a pass that added had to drop.
- **Operations over the run:** 761 adds, 48 supersedes, 595 drops, 95 rejected and 343 skipped. Under Amendment 3, a further 396 operations past an answer's first 16 were cut.
  - 113 of 279 passes were applied partially after two refusals, and there were 286 refused attempts in all.
- **Standing facts were written, then pushed out.** A UP fact was stated by some version of a line for 35 of the 50 items, and 14 remained at the end. The stream's 129 passes added 563 lines and dropped 540.
- **The final document** holds 166 lines. 65 cite only base mids and 101 cite a stream mid.
  - It states every hop of no C2 or C3 chain.
  - It states the new holder for 10 of the 30 KU items, and the obsolete one for 7.
  - It states the associated value for 7 of the 60 N items.
- **The failures behind the amendments** were in parsing and bounds:
  - several `document_update` calls in one answer (Amendment 1);
  - supersedes lengthening a partial pass past the bound (Amendment 2);
  - more than 16 operations in all (Amendment 3).
  - After Amendment 3, one pass (232) returned malformed JSON on all three attempts, and the build was restarted once, which sent it again.

## Live stage

<!-- pending -->

## W beside every arm

<!-- pending -->

## W's hypotheses

<!-- pending -->

## Cost

<!-- pending: consolidation 579 calls, 3,188,552 input, 550,397 output -->

## Verdict

<!-- pending -->

## Limits

<!-- pending -->

## Layout

- `preregistration.md`: fixed before the first call, with its amendments.
- `sample.py` and `sample.json`: W's stratified sample, seed 900.
- `arms/full/` (W), `arms/agent/` (R) and `arms/document/` (D): one command each, whose main is `study.Main(New())` over `../../chained-recall/study/`. S is #890's `arms/shipped`, run with `-dir` at this directory.
- `score.py`: `offline`, `controls`, `live` and `tokens`, on #890's matching (`../../memory-encoding/validation/score.py`). `score_test.py` tests what it adds.
- `mutation.py`: each named behaviour removed breaks a test (`evidence/mutation.txt`).
- `evidence/`:
  - `offline/<arm>/<setting>/` and `offline/scores.json`;
  - `live/<arm>/<setting>/` and `live/scores.json`;
  - `controls.json`, `tokens.json`, and `ledger/`, every model call;
  - `arms/document/evidence/`: D's passes and its finished document with every version.

## What the study package gained

Four additive hooks in `../../chained-recall/study/`, each tested and covered by `mutation.py`; no earlier arm's behaviour changes:
- `-corpus`: read `corpus.json` from another study's directory while evidence lands under `-dir`.
- `Windower`: an arm sets its own live window (W).
- `Item.Label`: an item that is not a summary of its interval carries its own label (R's facts, D's lines).
- `LiveResult.Trace`, `WindowFirst` and `WindowMessages`: a live record keeps the memory's trace and the restored window, which R's and W's controls read.

## Reproduction

From the repository root, with Docker, Go 1.26 and Python 3:

```sh
docker run -d --name tei-900 -p 127.0.0.1:8120:80 \
  ghcr.io/huggingface/text-embeddings-inference@sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d \
  --model-id BAAI/bge-base-en-v1.5 --revision a5beb1e3e68b9ab74eb54cfd186867f64f240e1a
C=docs/research/memory-encoding/validation; D=docs/research/memory-paradigms/validation; E=http://127.0.0.1:8120/v1
go run ./docs/research/chained-recall/harness build -batch 4 -dir $C -work <scratch> -embed-url $E    # shared store, no call
go run ./$C/arms/shipped build -dir $D -corpus $C -work <scratch> -embed-url $E                     # then offline
go run ./$D/arms/document build -dir $D -corpus $C -work <scratch> -embed-url $E -proxy http://127.0.0.1:8121   # paid until the passes are recorded; then offline
go run ./$D/arms/full build -dir $D -corpus $C -work <scratch> -embed-url $E                        # likewise arms/agent; no call
python3 $D/score.py offline
python3 $D/score.py controls
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s $D -p '*_test.py'
python3 -B $D/mutation.py
```

- The proxy is `harness proxy -addr 127.0.0.1:8121 -ceiling 40 -dir $D/evidence/ledger -probe`, with `MINIMAX_KEY` in its environment only.
- Every paid phase resumes after its last record, so re-running one against the committed evidence sends nothing.
