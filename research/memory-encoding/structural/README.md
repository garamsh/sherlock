# Structural safeguards for associative recall

Records issue #896's study. On #890's corpus it tests whether a structural safeguard keeps the association gains while holding abstention and corrections. A structural safeguard controls what text is in view; an instructional one asks the model to behave. The gains are L's chains (#885, #888) and C's situated recall (#890). `preregistration.md` fixed the run before its first model call, with one amendment before the first live call.

- **No structure is valid, so none is named, and the shipped recall stands.** Every control passed (L-attr's under Amendment 1), so the null is the rule's answer.
- **Each structure either gives up the chains to hold abstention, or keeps L's chains together with L's losses:**
  - **L-attr** keeps L's chains (69 of 100 applied against S's 7). But it abstains on 38 of 60 absent queries against S's 59, and states the associated value on 48. Naming whom a memory is about does not stop the model answering from it.
  - **L-ptr** hides the walk episodes' text and nearly holds abstention (55). But it keeps only 15 chains (p = 0.13), because the model rarely reads a pointer: 247 of its inspects asked for the pointer's mid ID and 92 for a source in its interval.
  - **L+chk** keeps 57 chains, and its notice holds abstention where it fires (40 of 41). But the check passes the absent query's associated episode as a connected chain in 17 of 60. It empties the block for corrections and exact facts, and fails five guardrails (K 15 against 29, E 13 against 21).
  - **C-key** reaches situated values by ranking alone offline (30 of 50 against 0), but fails its oracle control and was not admitted. Inside C-key+L-ptr it applies 10 of 50 SC items against 0, and fails N, C1 and KU.
- **By the preregistered ranking, the best structure is L-attr** (one guardrail failed). That ranking is a description, not a validation.
- **What the run shows about the cause.** The losses come from associated text in view and from how the model reads what is in view, as the issue predicted. No structure tested here changes that without taking the chains too.

## Contents
- Layout
- Arms
- Offline stage and admission
- Live results
- Controls
- Cost
- Every structure since #874
- Verdict
- Limits
- Reproduction

## Layout

- `preregistration.md`: the question, arms, endpoints, decision rule and controls, fixed before any paid call; Amendment 1 before any live call.
- `score.py`: `offline`, `equivalence`, `live` and `tokens`. It extends #890's scorer (`../validation/score.py`), and through it #874's (`../../chained-recall/score.py`). It adds pointer reach, SC value reach, every arm's controls and the decision. `score_test.py` tests it.
- `mutation.py`: removes each behaviour #896 added and records that a named test fails (`evidence/mutation.txt`).
- **The arms are settings of existing arm commands**, so no reviewed code is copied (`preregistration.md` §Where the arms live):
  - L-ptr, L-attr, L+chk and C-key+L-ptr are settings of `../../chained-recall/arms/links`, in `memory.go`, `walk.go` and `check.go`;
  - C-key is a setting of `../validation/arms/context`.
- **Four symbolic links reach #890's recorded data**, so every command takes `-dir` at this directory and writes nothing under `../validation/`: `corpus.json`, `evidence/synthesis/`, `arms/links/evidence/entities-b4.jsonl` and `arms/context/evidence/context-b4-w8.jsonl`.
- `evidence/`:
  - `offline/<arm>/<setting>/` and `offline/scores.json`;
  - `equivalence.txt`, S and L re-recorded here against #890's records;
  - `live-smoke/`, the four smoke probes, excluded from scoring;
  - `live/<arm>/<setting>/results.jsonl`, `live/L/b4-model-chk/checks.jsonl`, `live/run.json` (each setting's run window) and `live/scores.json`;
  - `ledger/`, every model call, and `tokens.json`;
  - `mutation.txt`.

## Arms

As `preregistration.md` §Arms fixes them, with net production lines before tests:

| Arm | Setting | What is in view | Net lines |
|---|---|---|---:|
| S | `arms/shipped` `b4` | The shipped recall | 0 |
| L, reference | `arms/links` `b4-model` | #885's entity walk: walk mids with their summaries | about +270 |
| L-ptr | `b4-model-ptr` | A walk mid only as a pointer, `Linked through "<entity>"; content not shown, read its sources with memory_inspect.`, with its mid ID and source interval in the label | about +275 |
| L-attr | `b4-model-attr` | A walk mid whose subject (its first stored entity) is none of the query's entities opens with `About "<subject>"; the question names "<seed>".` | about +285 |
| L+chk | `b4-model-chk` | When recall or `memory_search` delivers a walk mid, one narrow call names at most 3 items that connect the asked entity to the asked attribute. The runtime keeps them only if they connect through stored entities; otherwise only `No stored memory states this fact.` | about +345 |
| C-key | `arms/context` `b4-w8-key` | S's summaries; stream mids ranked by C's recorded context line before S's summary | about +55, plus one write call per batch |
| C-key+L-ptr | `arms/links` `b4-model-key-ptr` | L-ptr over C-key's store | about +330, plus one write call per batch |

- **L+chk's form** is the PM's decision on the issue's dispatch channel:
  - the check covers every mid of the block;
  - its reserved schema returns an ordered list of at most 3 IDs;
  - the runtime drops a named set that does not connect (§Controls).
- **C-key reuses two recordings** of #890: S's summaries and C's context fields. A production C-key needs the summary from a call that does not see the context, because #890's C summaries absorbed their neighbours. That is the extra write call.

## Offline stage and admission

`python3 score.py offline` (`evidence/offline/scores.json`), local embedder only.
- A chain hop counts as reached when an item covering its statement states both of its entities, or is a pointer through one of them.
- SC counts the value in the evidence mid. #890's stricter count, subject and value in one item, is 0 for every arm here.
- N and NC count the items putting the near-miss value in view (`preregistration.md` §Offline stage and admission).

| Family (n) | S | L | L-ptr | L-attr | L+chk | C-key | C-key+L-ptr |
|---|---:|---:|---:|---:|---:|---:|---:|
| C2 (50) | 0 | 42 | 41 | 43 | 42 | 0 | 46 |
| C3 (50) | 0 | 21 | 21 | 21 | 18 | 0 | 24 |
| SC (50) | 0 | 0 | 0 | 0 | 0 | **30** | 29 |
| IC2, IC3 (25 each) | 0, 0 | 0, 1 | 0, 0 | 0, 1 | 0, 0 | 0, 0 | 0, 0 |
| UP (50) | 0 | 0 | 0 | 0 | 0 | 1 | 1 |
| C1 (30) | 27 | 30 | 30 | 30 | 30 | 28 | 30 |
| E (30) | 26 | 27 | 26 | 27 | **16** | 27 | 26 |
| K (40) | 35 | 34 | 34 | 34 | **16** | 35 | 34 |
| KU (30) | 27 | 25 | 22 | 25 | 20 | 21 | **18** |
| N, near-miss in view (60) | 7 | 54 | 6 | 54 | 20 | 6 | 3 |
| NC, near-miss in view (30) | 0 | 0 | 0 | 0 | 0 | 14 | 14 |
| Context characters per turn | 2,943 | 3,049 | 2,737 | 3,109 | 827 | 3,080 | 2,836 |
| Stored characters per source character | 0.745 | 0.826 | 0.826 | 0.826 | 0.826 | 0.838 | 0.920 |
| Oracle, hops within 5 (505) | 493 | 505 | 505 | 505 | 487 checked; L's 505 | **478** | 505 |

- **S and L are unchanged.** Re-recorded here, both equal #890's committed records in every field but timing, 6 of 6 files (`evidence/equivalence.txt`).
- **Every L variant keeps L's reach.** Pointers keep it, since the pointer names the entity the walk followed. L+chk keeps 60 of L's 63: its check named a connected set on 43 of 50 C2 and 24 of 50 C3 probes.
- **What each takes out of view on N.** L puts the associated value in view on 54 of 60 absent queries.
  - L-ptr: 6, as S's 7.
  - L+chk: 20.
  - L-attr keeps all 54, with the attribution in front of each. #890's "near-miss beside the entity" measure reads 49 for L-attr, because the attribution line names the queried entity. It does not attribute the value to that entity.
- **What the check costs offline.** L+chk's block carries only the notice wherever the check named nothing or an unconnected set. That takes E from 26 to 16 and K from 35 to 16. On K it named a set in 33 of 35 checks, but 19 did not connect through stored entities.
- **C-key reaches SC by ranking alone**: 30 of 50 against 0, with S's text shown. It costs KU (21 against 27) and its oracle (478 of 505). Its key re-ranks 413 of 470 probes' blocks.

### Admission

#890's rule: the mechanism controls passed, and on the primary family at least 10% of it more than S, with exact McNemar p < 0.05.

| Arm | Primary | Arm vs S | p | Margin | Controls | Admitted |
|---|---|---:|---:|---|---|---|
| L-ptr | C2, C3 | 62 vs 0 | 4.3e-19 | met | passed | yes |
| L-attr | C2, C3 | 64 vs 0 | 1.1e-19 | met | as written: failed; under Amendment 1: passed | as written: no; under Amendment 1: yes |
| L+chk | C2, C3 | 60 vs 0 | 1.7e-18 | met | passed | yes |
| C-key | SC | 30 vs 0 | 1.9e-9 | met | **oracle failed**: 478 of 505 (94.7%), 95% required | no |
| C-key+L-ptr | C2, C3, SC | 99 vs 0 | 3.2e-30 | met | passed | yes |

- **L-attr under Amendment 1.** The preregistered clause, "On NC, the subject is never the queried entity: all of them.", counted every NC walk item with a subject. That includes the queried entity's own episodes, which the arm shows without an attribution by design. It read 27 of 40.
  - Made after these offline records and before any live call, Amendment 1 replaces it with two checks: all 27 attributed NC items name another subject, and all 13 items about the queried entity are shown plain.
  - The PM decided it: the clause measured the episodes' subjects, not the safeguard.
- **C-key is not admitted**, so its situated gain has no live measurement. It goes live only inside C-key+L-ptr, whose walk restores the oracle.

## Live results

`python3 score.py live` (`evidence/live/scores.json`). 420 probes per arm: every family but UP, one live turn each, on its own store copy, through MiniMax-M2 and one proxy. None failed, and none was re-run. S always went live; L-ptr, L-attr, L+chk and C-key+L-ptr went live by admission; L went live as the reference.

*Applied*, as #885 and #890: the reply, messages sent or files written state the answer (chains, SC, E, C1), the new value (K, KU), or abstain by #885's pattern (N, NC).

| Family (n) | S | L, reference | L-ptr | L-attr | L+chk | C-key+L-ptr |
|---|---:|---:|---:|---:|---:|---:|
| **C2 and C3 (100)** | **7** | **65** | **15** | **69** | **57** | **17** |
| C2 (50) | 5 | 47 | 13 | 46 | 42 | 13 |
| C3 (50) | 2 | 18 | 2 | 23 | 15 | 4 |
| **SC (50)** | **0** | 0 | 0 | 0 | 0 | **10** |
| IC2, IC3 (25 each), reported only | 0, 0 | 0, 1 | 0, 0 | 0, 1 | 0, 0 | 0, 0 |
| C1 (30) | 30 | 30 | 26 | 29 | 30 | 25 |
| E (30) | 21 | 22 | 19 | 22 | 13 | 20 |
| K, corrected value (40) | 29 | 29 | 32 | 30 | 15 | 33 |
| KU, current name (30) | 20 | 21 | 17 | 20 | 15 | 11 |
| N, abstained (60) | 59 | 41 | 55 | 38 | 48 | 52 |
| N, states the near-miss value | 4 | 46 | 6 | 48 | 17 | 10 |
| NC, abstained (30) | 30 | 29 | 30 | 30 | 25 | 28 |
| KU, states the obsolete name | 7 | 7 | 12 | 8 | 5 | 14 |
| Searches per probe | 0.44 | 0.30 | 0.91 | 0.27 | 0.13 | 0.89 |
| Inspects per probe | 0.15 | 0.13 | 1.09 | 0.18 | 0.08 | 1.13 |

**Against S**, exact McNemar on the primary family, Holm across the four arms:

| Arm | Primary | Arm only | S only | Holm p | Guard change (allowed loss) | Net lines | Valid |
|---|---|---:|---:|---:|---|---:|---|
| L-ptr | C2, C3 | 15 | 7 | 0.13 | **N −4 (3)**, **C1 −4 (3)**, **KU −3 (2)**, E −2, K +3, NC 0 | +275 | no: not significant, guardrails |
| L-attr | C2, C3 | 64 | 2 | 2.4e-16 | **N −21 (3)**, C1 −1, E +1, K +1, NC 0, KU 0 | +285 | no: guardrail |
| L+chk | C2, C3 | 52 | 2 | 4.9e-13 | **N −11 (3)**, **K −14 (2)**, **E −8 (3)**, **NC −5 (2)**, **KU −5 (2)**, C1 0 | +345 | no: guardrails |
| C-key+L-ptr | C2, C3, SC | 23 | 3 | 1.8e-4 | **N −7 (3)**, **C1 −5 (3)**, **KU −9 (2)**, E −1, NC −2, K +4 | +330 | no: guardrails |

- **Applying follows seeing, again.** In every arm, every applied primary chain had each of its hops in view, counting what `memory_inspect` returned. Every hop was seen in 7 primary chains for S, 65 for L, 15 for L-ptr, 69 for L-attr, 59 for L+chk and 17 for C-key+L-ptr.
- **L-ptr: the pointer is followed badly.**
  - The label after a pointer names the mid ID and its source interval. Of L-ptr's 457 inspects, 247 asked for the pointer's mid ID as if it were a source, 92 for a source inside its interval, and 118 for another source.
  - So most pointed episodes were never read, and the chains stayed unapplied.
  - With the text out of view, abstention nearly holds (55 against 59, and the near-miss stated 6 times against S's 4). The losses are spread over N, C1 and KU.
- **L-attr: attribution does not stop the answer.**
  - The associated value was attributed to its own subject in all 50 N probes where the walk delivered it (§Controls). The model still stated it 48 times and abstained 38 times.
  - Its chains and corrections are L's.
- **L+chk: the check passes the near-miss and blocks corrections.**
  - On N, the check named nothing, or an unconnected set, in 41 of 60 recall blocks; with only the notice in view, the model abstained in 40.
  - In the other 19 it named a connected set, which the structural guard accepts. In 17 of them the set was the joining episode and the associated value's episode, and the model answered in 11 of the 19.
  - On K it named a set in 30 of 34 checks, but 17 did not connect through stored entities; 11 of the 16 K probes lost against S had only the notice in view.
  - Where the block held only the notice, the model sometimes answered the restored chat instead (`K-03`, `NC-025`), or said it has no access to the user's accounts (`K-01`, `K-12`).
  - Of its 5 NC losses, 4 are clarifying questions ("I'm not familiar with ... could you give me more context?") that #885's abstention pattern does not count.
- **C-key+L-ptr:** SC applied 10 of 50, where every SC item's evidence was seen in 32. The model had the value in view without its subject, which C-key's shown summary does not name, and answered it in a third of them. It inherits L-ptr's pointer-reading problem on chains (17), and loses KU by 9, stating the obsolete name 14 times.
- **By the preregistered ranking** (§Comparison and decision: fewest guardrails failed, then fewest probes lost beyond the allowances, then net primary gain, then lines), the order is L-attr, L-ptr, C-key+L-ptr, L+chk.

## Controls

All passed, L-attr's under Amendment 1 (`evidence/offline/scores.json`, `evidence/live/scores.json`).

| Control | Required | Measured |
|---|---|---|
| Oracle, S, L, L-ptr, L-attr, C-key+L-ptr | 95% of 505 hops within 5 | 493, 505, 505, 505, 505 |
| Oracle, C-key | 95% | **478 (94.7%): failed**, not admitted |
| Oracle, L+chk (L's store and pre-check search) | 95% | L's 505; checked search 487 |
| L-ptr, no walk-episode text: offline recall and oracle walk items | all pointers | 1,534 of 1,534 |
| L-ptr, live `(linked)` lines | all pointers | 637 of 637 |
| L-ptr, walk items in the live block of primary probes | half | 100 of 100 |
| C-key+L-ptr, the same three | as L-ptr | 1,546 of 1,546; 637 of 637; 100 of 100 |
| C-key and C-key+L-ptr, stream mids show S's summary | all | offline 2,643 of 2,643 and 2,161 of 2,161; live 2,007 of 2,007. S's own: 2,813 and 2,598 |
| C-key, delivered mids differ from S's | 10% of probes | 413 of 470 |
| L-attr, attribution present exactly where the subject differs | all | offline 146 of 146; live 159 of 159 |
| L-attr, N's associated episode attributed to the associated entity beside the queried one | 90% | offline 49 of 49; live 50 of 50 |
| L-attr, NC as preregistered: "the subject is never the queried entity" | all | offline 27 of 40, live 21 of 41: **failed as written** |
| L-attr, NC under Amendment 1: attributed items name another subject; the queried entity's own items are plain | all, each | offline 27 of 27 and 13 of 13; live 21 of 21 and 20 of 20 |
| L+chk, every walk delivery checked | all | offline 838 checks, 0 unchecked; live 394 checks over 475 records, 0 unchecked |
| L+chk, the ledger holds each probe's checks | every probe | 420 of 420 |
| Mutation checks (`evidence/mutation.txt`) | every mutation fails a test | 32 of 32 |
| S and L re-recorded equal #890's | every field but timing | 6 of 6 files |

- **L-attr as written and as amended.** Under the NC clause as preregistered, L-attr was not admitted, and its live verdict would be undetermined. Under Amendment 1 it was admitted, and it is invalid on the N guardrail. Either way no structure is named.
- **L+chk's check, how often it named a set** (live recall blocks; offline in `evidence/offline/scores.json`):

  | Family | Blocks checked | Named a set | Connected | Notice only |
  |---|---:|---:|---:|---:|
  | C2 | 50 | 45 | 44 | 6 |
  | C3 | 50 | 37 | 29 | 21 |
  | N | 60 | 20 | 19 | 41 |
  | NC | 24 | 1 | 0 | 24 |
  | K | 34 | 30 | 13 | 21 |
  | E | 24 | 19 | 12 | 12 |
  | KU | 11 | 11 | 2 | 9 |
  | C1 | 16 | 16 | 16 | 0 |

  No answer was malformed. The check also ran on 44 `memory_search` results that held walk mids.

## Cost

MiniMax-M2 only, through one proxy on `127.0.0.1:8111` (`evidence/ledger/`, `python3 score.py tokens`, `evidence/tokens.json`); every embedding came from a local text-embeddings-inference container (`tei-896`, `127.0.0.1:8110`). No other paid API was called.

| Phase | Calls | Input tokens | Output tokens |
|---|---:|---:|---:|
| Key probe | 1 | 45 | 64 |
| L+chk's offline check | 855 | 584,783 | 537,525 |
| Live smoke, L+chk and L-ptr, excluded from scoring | 14 | 52,107 | 2,593 |
| Live, S | 1,110 | 4,989,761 | 268,967 |
| Live, L-ptr | 1,693 | 7,744,867 | 318,412 |
| Live, L-attr | 1,040 | 4,697,297 | 269,911 |
| Live, L+chk: turns | 966 | 3,737,749 | 217,976 |
| Live, L+chk: checks | 397 | 327,679 | 238,967 |
| Live, C-key+L-ptr | 1,709 | 7,825,022 | 308,164 |
| Live, L | 1,033 | 4,629,452 | 258,272 |
| **Total** | **8,818** | **34,588,762** | **2,420,851** |

- **No request met HTTP 429**, so the 2056 pause never engaged (`probes.log` holds only the preflight probe).
- **Nothing was bought.** At list prices the calls come to USD 13.28, a guard only; the key is on a subscription.
- **Over the estimate.** The preregistration estimated about 16M input tokens. The pointer arms made about 1.5 times S's act calls, because every inspect is one more turn step.
- **L+chk per turn.** The check ran in 359 live turns, 1.11 calls and 913 input and 666 output tokens per such turn. MiniMax-M2 reasons before it answers, which is most of the output.
- **Ledger routing.** Every `arms/links` setting is ledgered under arm L; `score.py tokens` splits them by the windows in `evidence/live/run.json`.



## Every structure since #874

Quoted from each study's report, not re-scored. The corpora differ, so compare within a study first, against its own S.
- **Chains:** applied, live, on the study's primary chains; offline reach where the study made no live call.
- **Implicit:** IC2 and IC3 applied (#890: reached).
- **Abstention:** absent queries abstained, live; for #890's offline arms, absent queries with the near-miss in view (lower is safer).
- **Corrections:** K given the corrected value, live (#890: reached).
- **Stored:** stored characters per source character.
- **Lines:** net production lines, before tests.

| Study, corpus | Structure | Chains | Implicit | Abstention | Corrections | Stored | Lines |
|---|---|---|---|---|---|---:|---|
| #874 (`../../chained-recall/README.md`) | S | 4 of 114 | 0 of 50 | 15 of 20 | 16 of 20 | 0.705 | 0 |
| | M, chaining instruction | 10 | 0 | 14 | 17 | 0.705 | +2 to +4 |
| | L, entity walk | 47 | 4 | 11 | 14 | 0.782 | about +270 |
| | H, group summaries (offline) | 2 of 150 reached (g8) | — | — | — | 0.943 (g8), 1.107 (g4) | +85 to +125 |
| #885 (`§Confirmation (#885)`) | S | 9 of 100 | 0 of 50 | 57 of 60 | 28 of 40 | 0.731 | 0 |
| | L | 56 | 0 | 39 | 25 | 0.814 | about +270 |
| | L+A, label and instruction | 57 | 1 | 42 | 21 | 0.814 | about +292 |
| #888 (`§Read-time variants (#888)`) | S | 4 of 100 | 0 of 50 | 57 of 60 | 28 of 40 | 0.730 | 0 |
| | L | 60 | 0 | 36 | 25 | 0.813 | about +270 |
| | L+G, gate | 7 | 0 | 52 | 27 | 0.813 | about +284 |
| | L-T, walk only on call | 13 | 0 | 59 | 24 | 0.813 | about +265 |
| | L+V, inspect before stating (enforced) | 11 | 0 | 6 | 27 | 0.813 | about +272 |
| #890 (`../validation/README.md`), offline | S | 0 of 100; SC 0 of 50 | 0 of 50 | near-miss in view 7 of 60 | 35 of 40 | 0.745 | 0 |
| | C, context at synthesis | 0; SC 39 | 0 | 6; NC beside the entity 24 of 30 | 35 | 0.933 | +35 to +65 |
| | P, previous-episode links | 0 | 0 | 6 | 35 | 0.745 | +75 to +115 |
| | F, standing facts | 0; UP 2 of 50 | 0 | 7 | 35 | 0.853 | +90 to +130 |
| | L, reference | 63 | 1 | 54 | 34 | 0.826 | about +270 |
| #896 (this study), live | S | 7 of 100; SC 0 of 50 | 0 of 50 | 59 of 60 | 29 of 40 | 0.745 | 0 |
| | L, reference | 65 | 1 | 41 | 29 | 0.826 | about +270 |
| | L-ptr, pointers only | 15 | 0 | 55 | 32 | 0.826 | about +275 |
| | L-attr, attribution | 69 | 1 | 38 | 30 | 0.826 | about +285 |
| | L+chk, grounding check | 57 | 0 | 48 | 15 | 0.826 | about +345 |
| | C-key, context only in the key (offline) | 0; SC 30 reached | 0 | near-miss in view 6 of 60 | 35 reached | 0.838 | about +55, plus a write call per batch |
| | C-key+L-ptr | 17; SC 10 | 0 | 52 | 33 | 0.920 | about +330, plus a write call per batch |

- **Every L variant chains; none holds abstention and corrections together.**
  - Across four corpora the walk's chain gain replicates: 47 of 114 (#874), then 56 (#885), 60 (#888) and 65 (here) of 100.
  - Every variant that held abstention (L+G, L-T, L-ptr, and L+chk where its notice fired) gave up most of the chains or the corrections.
- **Nothing tested compresses.** Every structure stores more than the shipped recall's 0.705 to 0.745 characters per source character. Batch 8 alone compressed, to 0.466, and lost exact reach (#874).
- **Implicit chains stay unsolved** by every structure, at 0 or 1 of 50.

## Verdict

- **No structure is valid, so none is named for implementation, and the shipped recall stands.** Every control passed, L-attr's under Amendment 1, so this is the rule's null and not an undetermined run. Under L-attr's clause as written, L-attr alone would be undetermined, and the answer is the same.
- **Measured against the issue's goal, keeping the association gains while holding abstention and corrections:**
  - **Hiding the associated text (L-ptr)** holds most abstention but gives up the chains, because MiniMax-M2 does not follow a pointer to its sources. It inspects the mid ID in 247 of 457 inspects.
  - **Labelling whom the text is about (L-attr)** keeps every chain and none of the abstention. It is L+A's result (#885) without the instruction.
  - **A runtime grounding check (L+chk)** keeps most chains. But the model that answers from associated text also judges it: the check accepted the associated episode as a connected chain on 17 absent queries. The structural guard cannot tell that chain from a real one, since both connect through stored entities.
    - Where the check named nothing, the notice held abstention and emptied the block for corrections and exact facts.
    - It fails five guardrails, the most of any arm here.
  - **Context only in the key (C-key)** reaches situated values offline by ranking alone, 30 of 50, but lowers the oracle below its threshold. With pointers it applies 10 of 50 SC items and loses N, C1 and KU.
- **What decides the next step.** An absent query's associated episode and a chain's later hop sit at the same distance from the query's entity, and are linked the same way. #888 found this for a walk-score gate; this run finds it for text, labels, pointers and a model check.
  - A safeguard that holds abstention must tell the two apart by something they do not share. Nothing tested since #874 has.
  - The measured candidates are exhausted within the rule, so whether to pursue another is the owner's decision.

## Limits

- **One model, one run per probe.** MiniMax-M2 at a provider alias, which also served as L+chk's checker.
- **The corpus is #890's,** templated with invented names reused exactly. Absent items are adversarial by construction, as #885's were: each query names an entity whose association states a value of the asked kind.
- **The pointer's form.** L-ptr's pointer names the entity in its text and the mid ID and source interval in the study's label, as the issue specified. A pointer that printed the source IDs in its text might be read differently. That was not measured.
- **The check's form.** L+chk's list form, its bound of 3 and its connectivity rule are the PM's fixing of the issue's arm. A single-item check would reject every chain by construction.
- **Checks are deterministic proxies,** as in #874. #885's abstention pattern misses a clarifying question phrased "could you give me more context", which is 4 of L+chk's 5 NC losses. The guardrail counts them as preregistered.
- **L-attr's control was amended after the offline data** (Amendment 1), and the report gives it both ways.
- **C-key was never live on its own.** Its situated gain is measured live only through C-key+L-ptr.
- **UP was not probed live,** being no arm's family.
- **A production C-key needs a separate write call** for its plain summary. This run reused two recordings and did not measure that call.

## Reproduction

From the repository root, with Docker, Go 1.26 and Python 3. The ports are this run's.

```sh
D=research/memory-encoding/structural
docker run -d --name tei-896 -p 127.0.0.1:8110:80 \
  ghcr.io/huggingface/text-embeddings-inference@sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d \
  --model-id BAAI/bge-base-en-v1.5 --revision a5beb1e3e68b9ab74eb54cfd186867f64f240e1a
E=http://127.0.0.1:8110/v1
go run ./research/chained-recall/harness build -batch 4 -dir $D -work <scratch> -embed-url $E -proxy ""   # free
go run ./research/memory-encoding/validation/arms/shipped build -dir $D -work <scratch> -embed-url $E       # then offline
go run ./research/memory-encoding/validation/arms/context build -setting b4-w8-key -dir $D -work <scratch> -embed-url $E   # before b4-model-key-ptr
go run ./research/chained-recall/arms/links build -setting b4-model-ptr -dir $D -work <scratch> -embed-url $E   # and b4-model, b4-model-attr, b4-model-chk, b4-model-key-ptr; then offline
python3 $D/score.py equivalence
harness proxy -addr 127.0.0.1:8111 -ceiling 20 -dir $D/evidence/ledger -probe                                   # MINIMAX_KEY in its environment only
go run ./research/chained-recall/arms/links offline -setting b4-model-chk -dir $D -work <scratch> -embed-url $E -proxy http://127.0.0.1:8111   # paid
python3 $D/score.py offline
go run ./research/memory-encoding/validation/arms/shipped live -setting b4 -dir $D -work <scratch> -embed-url $E -proxy http://127.0.0.1:8111 -only <every probe but UP>   # likewise each admitted links setting, then b4-model
python3 $D/score.py live
python3 $D/score.py tokens
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s $D -p '*_test.py'
python3 -B $D/mutation.py
docker rm -f tei-896
```

- Every build reads recordings that are complete, so it makes no call and needs no `-proxy`.
- `live` resumes after the probes already recorded, so re-running it against the committed evidence sends nothing new. `offline` rewrites its records, and for L+chk that re-sends the check calls.
- `evidence/live/run.json` holds each setting's window. The smoke run's records were moved to `evidence/live-smoke/` after it ended, before collection began.
- **Revisions.** The preregistration and code are at `4b64a53`, Amendment 1 at `e7657e2` and the offline records at `282a979`. The live records and this report are at the commit that adds them.

