# Paradigm validation

Records issue #900's run: the full-context ceiling W, the recall sub-agent R and the idle-time memory document D of `../README.md` §Candidates for a validation run, measured against the shipped recall S on #890's corpus under `preregistration.md`.

- **No arm is named, and the shipped recall stands.** R is not valid, and D did not go live.
- **R, the recall sub-agent with citations checked in code**, applied 13 of 100 primary chains against S's 7 (13 R only, 7 S only, p = 0.26). It lost 6 absent (N), 4 correction (K) and 9 update (KU) probes against S, past the allowances of 3, 2 and 2. Every R control passed:
  - the recall step ran in 470 of 470 probes;
  - the citation check dropped a fact once;
  - 167 of the 195 facts it kept are stated by their cited sources.
- **D, the idle-time memory document**, met the offline margin on standing facts: UP 14 against 0, p = 1.2e-4. But it failed its preregistered UP coverage control, 14 of 50 against 25, so it was not admitted to the live stage and is reported offline only. Its document filled at pass 43 of 279. Of the 35 standing facts it ever wrote, it kept 14.
- **W, the full-context ceiling, was not run.** The PM dropped it before any live call (Amendment 4). The ceiling rests on #899's survey (`../README.md` §Full context as memory) and not on a measurement here.
- **Cost:** 5,166 calls, 16.1M input and 1.3M output tokens, all on MiniMax-M2 through one proxy. There was no 429, and the list-price guard stood at USD 6.45.

## Contents
- Method
- Offline stage
- D's consolidation
- Live stage
- What R's recall step did
- W, not run
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

`python3 score.py live` (`evidence/live/scores.json`). S and R answered all 470 probes, one live turn each, with no error and no re-run. D was not admitted, and W was not run.

| Family (n) | S | R |
|---|---:|---:|
| **Primary, C2 and C3 (100)** | **7** | **13** |
| C2 (50) | 1 | 11 |
| C3 (50) | 6 | 2 |
| IC2, IC3 (25 each), reported only | 0, 0 | 0, 0 |
| SC (50) | 0 | 0 |
| UP (50) | 0 | 1 |
| C1 (30) | 29 | 26 |
| E (30) | 20 | 20 |
| K, corrected value (40) | 31 | 27 |
| KU, new holder (30) | 20 | 11 |
| KU, obsolete holder stated | 7 | 11 |
| N, abstained (60) | 58 | 52 |
| N, associated value stated | 3 | 2 |
| NC, abstained (30) | 30 | 28 |
| Searches per probe (actor) | 0.39 | 0.09 |

| Arm against S | Arm only | S only | Holm p | Guard change (allowed loss) | Valid |
|---|---:|---:|---:|---|---|
| R | 13 | 7 | 0.26 | **N −6 (3)**, **K −4 (2)**, **KU −9 (2)**, C1 −3 (3), NC −2 (2), E 0 | no: not significant, N, K, KU |

- **The chain gain is small and not significant.** R applied 11 C2 chains where S applied 1, and 2 C3 chains where S applied 6.
- **R loses updates, corrections and abstentions, but not by showing associated values.**
  - On N it stated the associated value in 2 probes, against S's 3. Of its 60 N probes, one kept fact stated an associated value.
  - Its N losses are answers the abstention pattern does not count. In `N-001` the actor answered as if the task were about a résumé; in `N-006` it asked what game the name belonged to.
- **The actor often set a delivered fact aside.**
  - In `K-02` it had the kept fact "Your current WiFi network name is HomeNetwork5G." and answered that it has no WiFi.
  - In `KU-006` it answered that the question "seems to be from a different conversation context".
  - Of the 8 K probes R lost, 4 had the corrected value in a kept fact.

## What R's recall step did

From each live record's `trace` (`score.py controls`, `evidence/controls.json`):

| Family | Step ended with a result | Probes with a kept fact | Step calls per probe | Searches per probe | Sources read per probe |
|---|---:|---:|---:|---:|---:|
| C1 (30) | 30 | 27 | 3.13 | 0.90 | 1.27 |
| C2 (50) | 31 | 20 | 4.88 | 1.56 | 2.58 |
| C3 (50) | 25 | 18 | 5.02 | 1.84 | 2.46 |
| K (40) | 33 | 31 | 5.03 | 1.05 | 2.98 |
| KU (30) | 20 | 19 | 4.93 | 1.53 | 2.57 |
| N (60) | 20 | 5 | 5.53 | 2.58 | 2.30 |
| NC (30) | 7 | 0 | 5.47 | 2.17 | 2.70 |
| UP (50) | 30 | 5 | 4.96 | 3.22 | 0.98 |
| All (470) | 269 | | 4.97 | | |

- **The step ran out more often than it reported.** In 201 of 470 probes it made no valid `recall_result` call by its sixth call, although the last call offered nothing else, so the actor was told no stored evidence was found. On N and NC that is the right outcome, and the step kept a fact in only 5 of 90 probes.
- **The citation check was exercised once.** The step returned 196 facts and cited only sources it had read for 195 of them. So the check held nothing back, and the facts' quality rested on the step itself.
  - 167 of the 195 kept facts (86%) are stated by their cited sources, by #874's matching.
  - One N fact attributed the associated value to the query's entity.
- **The chain hop was seldom reached.** A kept fact stated a primary chain's answer in 8 of 100 probes. The step read 2.5 sources per chain probe, and a C3 chain needs three hops.
- **Per turn, R added one step of about 5 calls.** Each call took 1.6 s at p50 and 2.7 s at p90. The probe's model time rose from 10.0 s to 18.3 s at p50, and from 15.2 s to 23.0 s at p90. The actor's own act calls fell from 2.57 to 2.21 per probe, and its searches from 0.39 to 0.09.

## W, not run

Amendment 4 dropped W before any live call. W's code, tests, sample and mutation check are in place (`arms/full/`, `sample.json`), and running W would take one live command with `-only` over the sample.
- **None of `preregistration.md` §W's hypotheses was read:** not chains in view, not abstention in view, not corrections, not implicit chains.
- **For the ceiling, `../README.md` §Full context as memory stands as the evidence.**
  - Full context matches or beats memory systems up to about 26K tokens.
  - At 100K to 115K it loses overall on 7 of the 8 models read.
  - It keeps single-session-assistant and often knowledge-update.
  - This corpus's 180,000-token window is longer than any of those.
- **What stays open is #899's own question.** No run has measured whether MiniMax-M2 answers absent questions from associations when they are in view in their original wording. That is the reading W was preregistered to give.

## Cost

MiniMax-M2 only, through one proxy on `127.0.0.1:8121` (`evidence/ledger/`, `python3 score.py tokens`, `evidence/tokens.json`). Embeddings came from local text-embeddings-inference (`tei-900`).

| Phase | Calls | Input tokens | Output tokens |
|---|---:|---:|---:|
| Key probes | 2 | 90 | 111 |
| D's consolidation, 279 passes | 579 | 3,188,552 | 550,397 |
| S live | 1,208 | 5,405,845 | 305,227 |
| R live, the recall step | 2,338 | 3,626,764 | 232,433 |
| R live, the actor | 1,039 | 3,910,467 | 251,475 |
| **Total** | **5,166** | **16,131,718** | **1,339,643** |

- No request met HTTP 429, and the 2056 pause never engaged. Nothing was bought. At list prices the calls come to USD 6.45, a guard only.
- D's consolidation took 3.19M input tokens against the 1.3M estimated: after the bound, most passes took three attempts.
- The proxy was stopped once by host memory pressure while the run waited, and restarted with a fresh preflight probe (`evidence/ledger/probes.log`). `tei-900` was recreated on port 8140, because another study had taken 8120. It was the same image and model revision, capped at 1.5 GB.

## Verdict

- **Under the preregistered rule, the shipped recall stands.**
  - R is not valid: its gain is not significant, and it breaks three guardrails. Its controls passed, so for R the null is the rule's answer.
  - D was not admitted: it failed its UP coverage control. So its null is not clean.
- **What the run establishes outside the rule:**
  - A recall step that must read before it reports rarely puts associated values in front of the actor (1 of 60 N probes). But on this model it ends without a result in 43% of probes and reaches few chain hops.
  - The actor sometimes sets its facts aside as not belonging to the conversation (`K-02`, `KU-006`). That costs corrections and updates that S's summaries kept.
  - An always-in-view document bounded at 16,000 characters fills within the first 344 mids. After that it trades old lines for new ones, and it holds 14 of 50 standing facts at the end. It raises context per turn ninefold.
- **For #899's candidates:** neither non-retrieval paradigm tested here beat retrieve-into-context on this corpus and model, and the ceiling reading is still owed.

## Limits

- **One model, one run per probe, a templated corpus.** These are MiniMax-M2 at a provider alias, and invented names reused exactly, as in #874, #885 and #890.
- **R's design is one of many.** Its bound, instruction and output schema are this run's. Nothing here shows how a larger bound, or a step that must answer before its budget ends, would do.
- **Checks are deterministic proxies.** The abstention pattern misses off-topic replies, and support is token matching.
- **D was measured offline only.** Its offline reach counts lines stating values, and whether the actor would apply them was not measured.
- **W was not run,** so no structure here is read against a measured ceiling.
- **D's pass changed under three amendments while it ran,** each committed before the pass that needed it. Passes recorded before an amendment replay unchanged.

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
C=research/memory-encoding/validation; D=research/memory-paradigms/validation; E=http://127.0.0.1:8120/v1
go run ./research/chained-recall/harness build -batch 4 -dir $C -work <scratch> -embed-url $E    # shared store, no call
go run ./$C/arms/shipped build -dir $D -corpus $C -work <scratch> -embed-url $E                     # then offline
go run ./$D/arms/document build -dir $D -corpus $C -work <scratch> -embed-url $E -proxy http://127.0.0.1:8121   # paid until the passes are recorded; then offline
go run ./$D/arms/full build -dir $D -corpus $C -work <scratch> -embed-url $E                        # likewise arms/agent; no call
go run ./$C/arms/shipped live -setting b4 -dir $D -corpus $C -work <scratch> -embed-url $E -proxy http://127.0.0.1:8121   # paid
go run ./$D/arms/agent live -setting b4-agent -dir $D -corpus $C -work <scratch> -embed-url $E -proxy http://127.0.0.1:8121   # paid
python3 $D/score.py offline
python3 $D/score.py controls
python3 $D/score.py live
python3 $D/score.py tokens
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s $D -p '*_test.py'
python3 -B $D/mutation.py
```

- The proxy is `harness proxy -addr 127.0.0.1:8121 -ceiling 40 -dir $D/evidence/ledger -probe`, with `MINIMAX_KEY` in its environment only.
- Every paid phase resumes after its last record, so re-running one against the committed evidence sends nothing.
