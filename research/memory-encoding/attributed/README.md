# Attributed answers on a fresh corpus

Records issue #916's study. It confirms, on a fresh corpus, whether L-attr (#896's entity walk with its walk episodes attributed to their subject) beats the shipped recall (S) on chains while holding #885's guardrails. An absent query is scored by the owner's answer rule of 2026-09-28. `preregistration.md` fixed the run before its first audit label, score or model call. Three amendments followed, each committed before the step it governs.

- **L-attr is valid under the owner's rule.**
  - It applies 66 of 100 primary chains against S's 5: 63 won and 2 lost, exact McNemar p = 1.2e-16.
  - Every guardrail holds: N −1 (allowed 3), NC −1 (2), E −1 (3), K 0 (2), KU +1 (2), C1 +3 (3).
  - Every control passed, including both audits of the matcher.
- **Under the strict rule it is not valid:** it abstains on 35 of 60 absent queries against S's 56 (N −21). Under reading B, the sensitivity reading, it is valid (N −2).
- **The verdict does not rest on the matcher.** Audit 2 hand-labelled every unabstained live reply to an absent query. Scored from those labels alone, N is 54 against 56 (−2) and NC 29 against 30 (−1). Both are within the allowance.
- **What the owner's rule accepts here.** L-attr states the associate's value on 54 of 60 absent queries. On 53 of them it names whose value it is, and it then usually recommends acting on it. S states it on 3. The rule decides whether that counts as recall, and this run measures it under the rule the owner chose.
- **The production change it implies** is L-attr's walk, entity table, entity extraction at synthesis and attribution: about +285 net lines, before tests (§Production change).

## Contents
- Layout
- Arms
- Corpus
- Matcher and audits
- Exploratory re-read
- Offline stage and admission
- Live results
- Controls
- Cost
- Verdict
- Production change
- Limits
- Reproduction

## Layout

- `preregistration.md`: the question, the owner's rule and its reading, the matcher, the audits, arms, corpus, paid phases, endpoints, decision and controls, with Amendments 1 to 3.
- `matcher.py`: the attributed-answer matcher; `matcher_test.py` tests it.
- `audit.py`: draws the audit samples and checks the matcher against the hand labels.
- `reread.py`: the exploratory re-read of #885's, #888's and #896's recorded replies.
- `corpus.py`: runs #890's generator (`../validation/corpus.py`, imported unchanged) at seed 916, and the fresh check.
- `score.py`: `check`, `offline`, `equivalence`, `live` and `tokens`. It uses #896's scorer (`../structural/score.py`) and, through it, #890's and #874's. `score_test.py` tests it.
- `mutation.py`: removes each behaviour this study adds and records that a named test fails (`evidence/mutation.txt`).
- `candidates.json` and `corpus.json`: the seed-916 candidates and the selected corpus.
- `arms/links/evidence/entities-b4.jsonl`: #874's 1,200 base rows, copied unchanged from #890, and the 1,032 stream rows phase 2 wrote.
- `evidence/`:
  - `audit/`: `sample-1`, `sample-1b` and `sample-2`, each with its `labels-*` and `audit-*.json`; `audit-1-revised.json` is the revised matcher on audit 1's own sample, in-sample;
  - `reread.json`, the exploratory re-read;
  - `synthesis/stream-b4.jsonl`, `check-b4.json`, `selection/check-b4.json`, `check-summary.json` and `fresh.json`;
  - `offline/<arm>/<setting>/`, `offline/scores.json` and `equivalence.json`;
  - `live/S/b4/` and `live/L/b4-model-attr/` (`results.jsonl`, `results-attempt1.jsonl`), and `live/scores.json`;
  - `ledger/`, every model call, and `tokens.json`;
  - `mutation.txt`.

## Arms

Both arms are settings of existing arm commands. Neither arm's code changed, and every command ran with `-dir` at this directory.

| Arm | Command and setting | What is in view | Net lines |
|---|---|---|---:|
| S | `research/memory-encoding/validation/arms/shipped` `b4` | The shipped recall | 0 |
| L-attr | `research/chained-recall/arms/links` `b4-model-attr` | #885's entity walk. A walk mid whose subject (its first stored entity) is none of the query's entities opens with `About "<subject>"; the question names "<seed>".` | about +285 |

- **Common to both:** a live window of 32 messages, an empty note, one turn per probe on its own store copy, and MiniMax-M2 through one proxy, with no arm instruction.

## Corpus

`corpus.py assemble`: #890's generator at seed 916, which draws items with `random.Random(916)` and schedules with `random.Random(917)`. It produced 4,128 stream messages (1,032 batches) after the replication's 4,800-message base.

- **Selection, as #890** (`evidence/check-summary.json`):
  - Every checked family kept its size from the passing candidates: C2 60 of 70 passed, C3 59 of 70, SC 70 of 70, UP 91 of 106, C1 and IC 30 of 30.
  - No needed source ranks within the raw query's top 20 on the selected corpus.
  - The oracle delivers 498 of 505 hops within 5 (98.6%, 95% required).
- **Families:** C2 and C3 50 each (primary); N 60, NC 30, K 40, KU 30, C1 30, E 30 (guards); IC2 and IC3 25 each, SC 50 and UP 50 (reported). 470 probes, every one run live.
- **Fresh** (`evidence/fresh.json`, Amendment 2):
  - None of the corpus's 678 invented names occurs in the corpora #874, #885, #888 or #890 ran.
  - No generated probe repeats an earlier item's query and answer.
  - **As preregistered, the check failed.** It compared query strings only, and 76 generated queries occur in #890's corpus: all 46 are UP and all 30 are KU. Both families build their query from a fixed template that never names the item ("I need to see my chiropodist next week. Who should I contact?"). Their answers are new, so the failure is template reuse.
  - E and K come from the replication's fixed pool of 70 each, as every earlier study drew them. 25 of 30 E and 38 of 40 K were drawn before.

## Matcher and audits

The owner's rule, as the issue states it: on an absent (N or NC) probe, an answer is acceptable when it abstains, or when it gives the associate's value while attributing it to the associate.

- **Reading A.** The PM fixed it before the matcher, relaying the owner, who approved the rule on #896's N-002 reply ("… which picks up from bay 42. So you should wait at bay 42 when Vuner Coaches arrive for you."). A conclusion or recommendation that follows a stated attribution is acceptable. The failure is the unattributed assertion.
- **Reading B**, reported as sensitivity only, also fails an unhedged sentence tying the value to the queried entity without the associate.
- **The matcher** (`preregistration.md` §Matcher, Amendment 1):
  - It accepts a reply that abstains by #885's pattern, or declines in the words Amendment 1 adds.
  - It also accepts a reply where some sentence gives the value with the associate, or the next sentence points back at it and names the associate, and where no clause states the value with the queried entity as its subject.
- **Who labelled.** The matcher's author, the worker, labelled every audit. Each label was written from the sample alone, before the matcher ran on it, and committed first.

| Audit | Replies | Agree | False accepts | Pass mark | Result |
|---|---|---:|---:|---|---|
| 1, preregistered matcher | 80 of 177 unabstained #885, #888 and #896 replies | 68 | 2 | 72 agree, at most 4 | **failed** |
| 1, revised matcher, in-sample | the same 80 | 78 | 2 | reported only | — |
| 1b, revised matcher | 40 of the 97 unlabelled rest | 38 | 1 | 36 agree, at most 2 | passed |
| 2, revised matcher | all 30 unabstained live N and NC replies of this run | 29 | 1 | 27 agree, at most 1 | passed |

- **Audit 1's disagreements** (`evidence/audit/audit-1.json`), which Amendment 1 revised for:
  - six replies decline or ask which one is meant in words #885's pattern misses;
  - two misspell the associate's surname by one letter;
  - two name the value's holder in the following sentence ("… locker 593. That's where Dmitri Teapou leaves the keys").
- **The matcher's known error.** Its false accepts are replies that misstate the link as a fact about the queried entity:
  - "that's where Pavel Baigam leaves the keys for finished jobs from Mara Kudas";
  - "Heazeam Coaches shares the depot with Stanex Coaches, so you'll want to wait at bay 20";
  - live, "Fipar Coaches parks at the same depot as Liquourk Coaches, which means they pick up from bay 63".

## Exploratory re-read

`reread.py`, free, run after audit 1b. Labelled exploratory: it decides nothing, and the data include the replies the issue's motivation was drawn from. Absent queries acceptable, N (60) per arm record:

| Study | Arm | Strict | Owner's rule (A) | Reading B | Attributed |
|---|---|---:|---:|---:|---:|
| #885 | S | 57 | 59 | 59 | 4 |
| | L | 39 | 58 | 55 | 51 |
| | L+A | 42 | 55 | 53 | 47 |
| #888 | S | 57 | 58 | 58 | 3 |
| | L | 36 | 56 | 52 | 51 |
| | L+G | 52 | 56 | 56 | 5 |
| | L-T | 59 | 60 | 59 | 5 |
| | L+V | 36 | 59 | 54 | 53 |
| #896 | S | 59 | 59 | 59 | 4 |
| | L | 41 | 57 | 52 | 46 |
| | L-attr | 38 | 56 | 52 | 48 |
| | L-ptr | 55 | 58 | 58 | 6 |
| | L+chk | 48 | 58 | 57 | 16 |
| | C-key+L-ptr | 52 | 56 | 55 | 10 |

- NC (30), in #896's six records only, strict against the owner's rule: S 30 and 30; L 29 and 29; L-attr 30 and 30; L-ptr 30 and 30; L+chk 25 and 29; C-key+L-ptr 28 and 28 (`evidence/reread.json`). None of L+chk's NC replies is attributed, so its four decline in the words Amendment 1 adds.

## Offline stage and admission

`python3 score.py offline` (`evidence/offline/scores.json`), local embedder only.

| Family (n) | S | L-attr |
|---|---:|---:|
| C2 (50) | 0 | 46 |
| C3 (50) | 0 | 17 |
| IC2, IC3 (25 each) | 0, 0 | 8, 0 |
| SC, UP (50 each) | 0, 0 | 0, 0 |
| C1 (30) | 24 | 30 |
| E (30) | 27 | 27 |
| K (40) | 35 | 34 |
| KU (30) | 23 | 23 |
| N, near-miss in view (60) | 4 | 60 |
| NC, near-miss in view (30) | 1 | 1 |
| Context characters per turn | 2,859 | 3,011 |
| Stored characters per source character | 0.741 | 0.823 |
| Oracle, hops within 5 (505) | 498 | 505 |

- **Admitted.** On C2 and C3 L-attr reaches 63 against 0 (margin 10), p = 2.2e-19, and its controls passed.

## Live results

`python3 score.py live` (`evidence/live/scores.json`). 470 probes per arm, one live turn each. S ran with no error or re-run. L-attr's run was resumed once after a full disk (Amendment 3), and every probe has exactly one scored record with no error.

| Family (n) | S | L-attr |
|---|---:|---:|
| **C2 and C3 (100)** | **5** | **66** |
| C2 (50) | 3 | 47 |
| C3 (50) | 2 | 19 |
| IC2, IC3 (25 each), reported only | 0, 0 | 8, 0 |
| SC, UP (50 each), reported only | 0, 0 | 0, 0 |
| C1 (30) | 27 | 30 |
| E (30) | 21 | 20 |
| K, corrected value (40) | 27 | 27 |
| KU, current name (30) | 18 | 19 |
| **N, owner's rule (60)** | **56** | **55** |
| N, strict | 56 | 35 |
| N, reading B | 56 | 54 |
| N, from audit 2's hand labels | 56 | 54 |
| N, states the near-miss value | 3 | 54 |
| N, attributed | 3 | 53 |
| **NC, owner's rule (30)** | **30** | **29** |
| NC, strict | 30 | 29 |
| KU, states the obsolete name | 12 | 10 |
| Searches per probe | 0.36 | 0.25 |
| Inspects per probe | 0.11 | 0.09 |

**Against S**, exact two-sided McNemar on C2 and C3, one comparison:

| Rule for N and NC | Arm only | S only | p | Guard change (allowed loss) | Valid |
|---|---:|---:|---:|---|---|
| Owner's rule (decides) | 63 | 2 | 1.2e-16 | N −1 (3), NC −1 (2), E −1 (3), K 0 (2), KU +1 (2), C1 +3 (3) | **yes** |
| Strict (reported) | 63 | 2 | 1.2e-16 | **N −21 (3)**, NC −1, E −1, K 0, KU +1, C1 +3 | no: N |
| Reading B (reported) | 63 | 2 | 1.2e-16 | N −2 (3), NC −1, E −1, K 0, KU +1, C1 +3 | yes |

- **Applying follows seeing.** Every applied primary chain had each of its hops in view, in both arms: 66 for L-attr and 5 for S.
- **Implicit chains, reported only.** IC2 and IC3 together: 8 against 0 (p = 0.008). All 8 are IC2; offline, L-attr reaches 8 IC2 as well.
- **Amendment 2's split.** UP is 0 against 0 with or without the 46 reused templates (4 left). All 30 KU probes reuse a template, so without them KU has no probe. With them KU gains one, so neither changes a verdict (`decision.template_reuse`).

## Controls

All passed (`evidence/check-summary.json`, `fresh.json`, `equivalence.json`, `offline/scores.json`, `live/scores.json`, `audit/`, `mutation.txt`).

| Control | Required | Measured |
|---|---|---|
| Selection | every checked family at its size | all 12 families at size |
| Oracle, selected corpus | 95% of 505 hops within 5 | 498 (98.6%) |
| Fresh, as amended (Amendment 2) | no probe item and no invented name seen before | 0 items; 0 of 678 names |
| Fresh, as preregistered | no generated query seen before | **76 failed** (UP 46, KU 30; template reuse only) |
| Entities stored | 90% of mids | 2,191 of 2,232 (98.2%) |
| Oracle, S and L-attr offline | 95% | 498 and 505 of 505 |
| S and L-attr share the store | same sources and mids, vectors included | 8,928 sources and 2,232 mids, equal SHA-256 |
| S and L-attr, unseeded probes | S's items | 53 of 53 |
| L-attr attribution present exactly where the subject differs | all | offline 126 of 126; live 141 of 141 |
| L-attr, N's associated episode attributed to the associated entity beside the queried one | 90% | offline 57 of 57; live 56 of 56 |
| L-attr, NC under #896's Amendment 1 | all, each | offline 13 of 13 and 16 of 16; live 11 of 11 and 23 of 23 |
| S's stream lines show its recorded summary | all | 2,900 of 2,900 |
| Matcher audit 1, or 1b under Amendment 1 | pass mark | 1 failed; 1b 38 of 40, 1 false accept |
| Matcher audit 2 | pass mark | 29 of 30, 1 false accept |
| Mutation checks | every mutation fails a test | 20 of 20 |

- **#896's NC clause as written** ("the subject is never the queried entity") reads 13 of 29 offline and 11 of 34 live. Its Amendment 1 form, which this preregistration adopted, passes.

## Cost

MiniMax-M2 only, through one proxy on `127.0.0.1:8161` (`evidence/ledger/`, `python3 score.py tokens`, `evidence/tokens.json`). Every embedding came from a local text-embeddings-inference container (`tei-916`, `--memory 1g`, `127.0.0.1:8160`), stopped while no phase embedded. No other paid API was called.

| Phase | Calls | Input tokens | Output tokens | Estimate |
|---|---:|---:|---:|---|
| Key probe | 1 | 45 | 39 | negligible |
| 1. Stream synthesis | 1,037 | 339,379 | 210,293 | 0.34M / 0.21M |
| 2. L's entity call | 1,034 | 401,482 | 246,379 | 0.40M / 0.25M |
| 3. Live, S | 1,174 | 5,233,545 | 296,666 | 5.6M / 0.30M |
| 4. Live, L-attr | 1,099 | 4,915,382 | 300,047 | 5.3M / 0.30M |
| Lost to the full disk (SC-035's first turn) | 2 | 8,754 | 567 | — |
| **Total** | **4,347** | **10,898,587** | **1,053,991** | 11.6M / 1.06M |

- **No request met HTTP 429**, so the 2056 pause never engaged.
- **Nothing was bought.** At list prices the calls come to USD 4.53, a guard only; the key is on a subscription.
- **Every phase was within its estimate.** The PM's rule to pause at 25% over never applied.

## Verdict

- **L-attr is valid, and it is the structure named.** Under the owner's answer rule, on a fresh corpus, L-attr beats the shipped recall on chains, 66 of 100 against 5, and holds every guardrail, with every control passed.
- **The difference from #896 is the rule, not the arm.** Under #885's strict abstention rule L-attr fails the N guardrail by 21, as it did in #896 (−21). The exploratory re-read already put #896's L-attr at 56 against 59 under the owner's rule, and this run confirms it on a corpus none of whose items was seen before.
- **What the owner accepts by it.** On an absent query, L-attr states the associate's value 54 times in 60. It names whose value it is in 53, and it usually recommends acting on it ("So you should wait at bay 63"). A deployment under this rule answers absent queries from associations, with the association stated, rather than abstaining.
- The PM opens the implementation issue. Architecture changes (ADR 0019, `memory.md`) belong to that issue, not to this study.

## Production change

What a production port of L-attr carries, from #885's measured estimate (`../../chained-recall/arms/links/README.md` §Production estimate, measured) and #896's attribution. Net non-blank, non-comment Go lines, before tests:

| Part | Where it lands | Lines |
|---|---|---:|
| Entity extraction at synthesis: the `memory_summary` schema gains `entities`, returned through the store | `internal/agent/memory/` synthesis call | about 15 |
| Name normalisation: lowercase, possessive and leading stopwords removed | beside the entity table | 52 |
| The entity links: a `mid_entity` table, written at checkpoint and read at recall | `internal/memory/sqlite/` | 50 |
| The walk: bipartite graph and personalised PageRank from the query's seeds, damping 0.5 | `internal/agent/memory/` recall | 109 |
| Recall and `memory_search` filling their slots from the similar list and the walk's list in turn | `internal/agent/memory/` recall | about 43 |
| The attribution: a walk mid's subject (its first stored entity) and the `About "<subject>"; the question names "<seed>".` opening | recall rendering | about 15 |
| **Total** | | **about +285** |

- **Not built by any study, and needed in production:** entities for the mids an existing store already holds. #874's survey estimated +15 to +25 lines for that path, which brings the total to about +300 to +310.
- **Cost at write:** one synthesis call per batch, as now, with its schema extended. In this run the entity call read 388 and wrote 238 tokens per batch, against the shipped call's 327 and 203 (`evidence/tokens.json`).
- **Cost at read:** no model call. Context grows from 2,859 to 3,011 characters per turn, and stored characters per source character from 0.741 to 0.823.

## Limits

- **One model, one run per probe.** MiniMax-M2 at a provider alias.
- **The rule was chosen after #896.** The owner's rule was decided on #896's data, which is why this run used a fresh corpus. The exploratory re-read is on data the rule was drawn from, and proves nothing.
- **The matcher's author labelled every audit.** Each set of labels was committed before the matcher ran on it, but no second labeller checked them.
  - One judgement differs between audits. Audit 1 labelled #888's L+V N-042 acceptable ("Since Ravi Geaquos learned the trade from Mara Waigul, the same system applies—look for locker 706"). Audit 1b labelled a reply of the same shape a failure (#888's L+G N-044, "shares the depot with Stanex Coaches, so you'll want to wait at bay 20. That's where the pickup will be."). The first was left as committed.
  - Neither reply is from this run's live records.
- **The matcher's known error.** It accepts a reply that misstates the link as a fact about the queried entity. Live, that is one L-attr reply (N-002). With it counted a failure, N is 54 against 56, still within the allowance.
- **Amendment 1 revised the matcher once, after audit 1 failed.** A fresh sample, labelled first, checked the revision. Audit 1's own sample is in-sample for it.
- **Template reuse.** UP and KU queries repeat #890's templates with new names and answers (Amendment 2). Every KU probe is one of them.
- **The corpus is #890's generator.** It is templated with invented names reused exactly, and its absent items are adversarial by construction: each query names an entity whose association states a value of the asked kind.
- **Checks are deterministic proxies**, as in #874. #885's abstention pattern misses some clarifying questions. The owner's reading counts them through Amendment 1's words, and the strict reading does not.
- **The live runner changed mid-run** (Amendment 3): a failed store copy or record write now stops it. Arms, recall and scoring are unchanged by it, and every scored probe ran once to completion.

## Reproduction

From the repository root, with Docker, Go 1.26 and Python 3. The ports are this run's. `<work>` is a scratch directory outside the repository.

```sh
D=research/memory-encoding/attributed
python3 $D/audit.py draw 1 && python3 $D/audit.py check 1     # audit-1.json was written by the matcher at 54af268
python3 $D/audit.py draw 1b && python3 $D/audit.py check 1b
python3 $D/reread.py
python3 $D/corpus.py assemble                                  # byte-identical candidates.json
docker run -d --name tei-916 --memory 1g -p 127.0.0.1:8160:80 -v <work>/tei-data:/data \
  ghcr.io/huggingface/text-embeddings-inference@sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d \
  --model-id BAAI/bge-base-en-v1.5 --revision a5beb1e3e68b9ab74eb54cfd186867f64f240e1a
E=http://127.0.0.1:8160/v1
harness proxy -addr 127.0.0.1:8161 -ceiling 15 -dir $D/evidence/ledger -probe   # MINIMAX_KEY in its environment only
go run ./research/chained-recall/harness synthesize -batch 4 -dir $D -proxy http://127.0.0.1:8161 -embed-url $E   # paid
go run ./research/chained-recall/harness build -batch 4 -dir $D -work <work> -embed-url $E -proxy ""
go run ./research/chained-recall/harness check -batch 4 -dir $D -work <work> -embed-url $E -proxy ""
cp $D/evidence/check-b4.json $D/evidence/selection/check-b4.json && python3 $D/corpus.py select
go run ./research/chained-recall/harness check -batch 4 -dir $D -work <work> -embed-url $E -proxy ""
python3 $D/score.py check && python3 $D/corpus.py fresh
go run ./research/chained-recall/arms/links build -setting b4-model-attr -dir $D -work <work> -embed-url $E -proxy http://127.0.0.1:8161   # paid: stream entities
go run ./research/memory-encoding/validation/arms/shipped build -dir $D -work <work> -embed-url $E    # then offline, likewise links offline
python3 $D/score.py equivalence <work> && python3 $D/score.py offline
go run ./research/memory-encoding/validation/arms/shipped live -setting b4 -dir $D -work <work> -embed-url $E -proxy http://127.0.0.1:8161             # paid
go run ./research/chained-recall/arms/links live -setting b4-model-attr -dir $D -work <work> -embed-url $E -proxy http://127.0.0.1:8161       # paid
python3 $D/audit.py draw 2 && python3 $D/audit.py check 2
python3 $D/score.py live && python3 $D/score.py tokens
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s $D -p '*_test.py'
python3 -B $D/mutation.py
docker rm -f tei-916
```

- `synthesize`, the entity build and `live` resume after their last record, so re-running them against the committed evidence sends nothing.
- `audit.py check 1` rewrites `audit-1.json` with the current matcher. The committed file is the preregistered matcher's result.
- **Revisions:**
  - the preregistration at `e5c3ca8`, and audit 1's labels at `54af268`;
  - Amendment 1 at `663c3f5`, audit 1b's labels at `de1e6b2`, and the corpus at `4799669`;
  - Amendment 2 at `b8b81db`, and the offline admission at `b5553e4`;
  - live S at `502587f`, Amendment 3 with the runner fix at `843262f`, and live L-attr at `23777b6`;
  - audit 2's labels at `5712014`. The scores and this report are at the commits that add them.
