# Memory structures compared

Sets every memory structure measured from #854 to #916 side by side, with each figure linked to the committed file it was read from, and states what the results establish and what they leave open. It re-scores nothing and measures nothing new. Every figure was read from the files at `f669f8b`, except #913's, read at `bdd3c37`, and #916's, read at `8cf217b`.

## Sources

A figure's label names its study and stage, and links the file it was read from. Studies on different corpora are not comparable figure for figure: compare a structure with the S (or A, or B4) of the same study, which each cell gives in brackets.

| Label | Study | Corpus | Stage | Scores | Report |
|---|---|---|---|---|---|
| 874 live, 874 off | #874 | #874's (`chained-recall/corpus.json`): 180 chains, and 20 each of E, K, N; its chain check failed | live applied; offline reached | [874 live], [874 off] | [#874](../chained-recall/README.md) |
| 885 live, 885 off | #885 | seed 885 (`chained-recall/confirm/corpus.json`): N is 60 adversarial absent items | live applied; offline reached | [885 live], [885 off] | [#885](../chained-recall/README.md#confirmation-885) |
| 888 live, 888 off | #888 | seed 888 (`chained-recall/readtime/corpus.json`), built as #885's | live applied; offline reached | [888 live], [888 off] | [#888](../chained-recall/README.md#read-time-variants-888) |
| 890 off | #890 | #890's (`memory-encoding/validation/corpus.json`): adds SC, UP, KU and NC | offline reached; no arm went live | [890 off] | [#890](../memory-encoding/validation/README.md) |
| 896 live, 896 off | #896 | #890's | live applied; offline reached | [896 live], [896 off] | [#896](../memory-encoding/structural/README.md) |
| 900 live, 900 off | #900 | #890's | live applied (S, R); offline reached (D) | [900 live], [900 off] | [#900](../memory-paradigms/validation/README.md) |
| 902 screen | #902 | #890's | free screen of the search at write; no arm built | [902 screen] | [#902](../memory-encoding/retrieved-context/README.md) |
| 906 screen | #906 | #890's recorded facts, entities and deliveries | free screen; no call | [906 screen] | [#906](../memory-encoding/profiles/README.md) |
| 854 | #854 | seed 854 (`mid-memory-rag/corpus.json`): 40 primary queries over 300 mids | offline delivery | [854] | [#854](../mid-memory-rag/README.md) |
| 856 | #856 | the replication (`mid-memory-rag/evidence/replication/corpus.json`): 280 primary queries | offline delivery | [856] | [#856](../mid-memory-rag/README.md#replication-issue-856) |
| 901 | #901 | the replication plus four other studies' recorded mids as distractors | offline delivery at 1× and 3.67× | [901] | [#901](../memory-scaling/README.md) |
| 913 screen | #913 | #901's, at 3.67× | free screen of delivery and stated; no call | [913 screen] | [#913](../memory-scaling/live/README.md) |
| 916 live, 916 off | #916 | seed 916 (`memory-encoding/attributed/corpus.json`), #890's generator; no item or invented name seen before | live applied; offline reached | [916 live], [916 off] | [#916](../memory-encoding/attributed/README.md) |

Verdicts are quoted from each study's report. Production lines link the report or survey that estimated them.

## Reading the table

- **Chains.** C2 and C3 together, of 100. *Live*: applied, the answer stated in the reply, messages or files. *Off*: reached, every hop's mid delivered stating both entities. #874's preregistered primary set was 114 clean chains that also hold IC, so its figures here are not its reported primary.
- **Implicit association.** IC2 and IC3 together, of 50, applied or reached; and SC, of 50, only on #890's and #916's corpora. #896 counts SC offline by the value in the evidence mid, which is looser than #890's rule.
- **Abstention.** *Live*: N and NC probes abstained on, of 60 and 30 (#874: N of 20). #916's are scored by the owner's attributed-answer rule, which also accepts the associate's value attributed to the associate, and are labelled *owner's rule*, with the strict count beside them. *Off*: probes with a near-miss value in view; there, lower is safer, and it is not an abstention count.
- **Corrections.** KU, the current holder given, of 30, on #890's corpus. #874, #885 and #888 have no KU, so their K (the corrected value, of 20 or 40) is given, labelled K. #854 and #856 give their correction family, the newer source delivered, of 10 and 70; #901 the same family of 70 at 3.67×.
- **Standing user facts.** UP, of 50, only on #890's and #916's corpora. #896 did not probe UP live.
- **Compression.** Stored characters per source character. #901's figure is for the replication part only.
- **Retrieval.** Primary queries whose evidence a delivered unit holds: of 280 for #856 and #901, of 40 for #854. #901 gives 1× and 3.67×. #913 adds *stated*: a delivered text states the gold value, of 280 at 3.67×.
- **Lines.** Net production lines before tests, as the study's report or survey estimated them. Nothing was implemented.
- **Verdict.** The study's own words, shortened. *Not admitted*: the arm did not pass the offline gate to the live stage. *Cut*: the paid phase was cut before any call. *Undetermined*: a control failed, so the study's rule gives no answer.
- **—** means not measured.

## Comparison

| Structure | Chains | Implicit association | Abstention | Corrections | UP | Compression | Retrieval | Lines | Verdict |
|---|---|---|---|---|---|---|---|---|---|
| S, the shipped recall (B4 similarity and recency since #858) | [874 live] 10; [885 live] 9; [888 live] 4; [890 off] 0; [896 live] 7; [900 live] 7 | IC: 0 in [874 live], [885 live], [888 live], [890 off], [896 live], [900 live]. SC: 0 in [890 off], [896 live], [900 live] | [874 live] N 15 of 20; [885 live] N 57; [888 live] N 57; [896 live] N 59, NC 30; [900 live] N 58, NC 30; [890 off] in view N 7, NC 0 | [874 live] K 16 of 20; [885 live] K 28 of 40; [888 live] K 28 of 40; [890 off] KU 27; [896 live] KU 20; [900 live] KU 20 | [890 off] 0; [900 live] 0 | [874 off] 0.705; [885 off] 0.731; [888 off] 0.730; [890 off] 0.745; [901] 0.684 | [901] S4: 212 at 1×, 201 at 3.67× | 0 | Stands in every study |
| M, an instruction to follow names | [874 live] 20 (S 10) | [874 live] IC 0 (S 0) | [874 live] N 14 of 20 (S 15) | [874 live] K 17 of 20 (S 16) | — | S's store ([874 report]) | — | +2 to +4 ([874 report]) | Not valid: not significant (Holm p 0.11 on the 114) |
| L, entity links and a PageRank walk | [874 live] 66 (S 10); [885 live] 56 (S 9); [888 live] 60 (S 4); [890 off] 63 (S 0); [896 live] 65 (S 7) | IC: [874 live] 4; [885 live] 0; [888 live] 0; [890 off] 1; [896 live] 1 (S 0 in each). SC: [890 off] 0; [896 live] 0 (S 0) | [874 live] N 11 of 20 (S 15); [885 live] N 39 (S 57); [888 live] N 36 (S 57); [896 live] N 41 (S 59), NC 29 (S 30); [890 off] in view N 54 (S 7) | [874 live] K 14 (S 16); [885 live] K 25 (S 28); [888 live] K 25 (S 28); [890 off] KU 25 (S 27); [896 live] KU 21 (S 20) | [890 off] 0 (S 0) | [874 off] 0.782; [885 off] 0.814; [888 off] 0.813; [890 off] 0.826 | — | about +270 ([links estimate]) | Not valid in #874 (N), #885 and #888 (N, K); reference in #890 and #896 |
| L+A, L with an association label and an abstention instruction | [885 live] 57 (S 9) | [885 live] IC 1 (S 0) | [885 live] N 42 (S 57) | [885 live] K 21 (S 28) | — | [885 off] 0.814 | — | about +292 ([874 report]) | Not valid: N, K |
| L+G, walk mids gated on the query's entities | [888 live] 7 (S 4) | [888 live] IC 0 (S 0) | [888 live] N 52 (S 57) | [888 live] K 27 (S 28) | — | [888 off] 0.813 | — | about +284 ([874 report]) | Not valid: not significant, N |
| L-T, the walk only as a tool | [888 live] 13 (S 4) | [888 live] IC 0 (S 0) | [888 live] N 59 (S 57) | [888 live] K 24 (S 28) | — | [888 off] 0.813 | — | about +265 ([874 report]) | Undetermined: its call control failed; also not significant, K |
| L+V, inspect before stating, enforced in scoring | [888 live] 11 enforced, 59 unenforced (S 4) | [888 live] IC 0 (S 0) | [888 live] N 6 enforced, 36 unenforced (S 57) | [888 live] K 27 (S 28) | — | [888 off] 0.813 | — | about +272 ([874 report]) | Not valid: not significant, N, C1 |
| H, a second level of group summaries | [874 off] groups of 4: 0, of 8: 2 (S 0) | [874 off] IC 0 (S 0) | — | [874 off] K 16 of 20 (S 16) | — | [874 off] 1.107 (groups of 4), 0.943 (of 8) | — | +85 to +125 ([levels estimate]) | Not live: does not chain; #874's offline stage is undetermined |
| C, context at synthesis | [890 off] 0 (S 0) | [890 off] IC 0; SC 39 (S 0) | [890 off] in view N 6 (S 7); NC 24, all beside the queried entity (S 0) | [890 off] KU 20 (S 27) | [890 off] 1 (S 0) | [890 off] 0.933 | — | +47 to +63 ([C estimate]) | Undetermined: not admitted, two controls failed |
| P, an episode linked to the previous one | [890 off] 0 (S 0) | [890 off] IC 0; SC 0 (S 0) | [890 off] in view N 6 (S 7); NC 17 (S 0) | [890 off] KU 26 (S 27) | [890 off] 1 (S 0) | [890 off] 0.745 | — | +84 to +112 ([P estimate]) | Not valid: not admitted, margin not met with its control passed |
| F, standing facts written by the synthesis call | [890 off] 0 (S 0) | [890 off] IC 0; SC 0 (S 0) | [890 off] in view N 7 (S 7); NC 0 (S 0) | [890 off] KU 29 (S 27) | [890 off] 2 (S 0) | [890 off] 0.853 | — | +90 to +130 ([encoding survey]) | Undetermined: not admitted, its control failed |
| L-ptr, walk mids shown as pointers | [896 live] 15 (S 7) | [896 live] IC 0; SC 0 (S 0) | [896 live] N 55 (S 59), NC 30 (S 30) | [896 live] KU 17 (S 20) | — | [896 off] 0.826 | — | about +275 ([896 report]) | Not valid: not significant; N, C1, KU |
| L-attr, walk mids attributed to their subject | [896 live] 69 (S 7); [916 live] 66 (S 5) | [896 live] IC 1; SC 0 (S 0). [916 live] IC 8; SC 0 (S 0) | [896 live] N 38 (S 59), NC 30 (S 30); [916 live] owner's rule N 55 (S 56), NC 29 (S 30); strict N 35 (S 56), NC 29 (S 30) | [896 live] KU 20 (S 20); [916 live] K 27 of 40 (S 27), KU 19 (S 18) | [916 live] 0 (S 0) | [896 off] 0.826; [916 off] 0.8225 (S 0.7412) | — | about +285 ([896 report], [916 report]) | Valid under the owner's rule ([916 live]), shipped in #922; not valid under the strict rule: N in #896 (admitted under Amendment 1) and #916 |
| L+chk, a runtime grounding check | [896 live] 57 (S 7) | [896 live] IC 0; SC 0 (S 0) | [896 live] N 48 (S 59), NC 25 (S 30) | [896 live] KU 15 (S 20) | — | [896 off] 0.826 | — | about +345 ([896 report]) | Not valid: N, K, E, NC, KU |
| C-key, context only in the embedding key | [896 off] 0 (S 0) | [896 off] IC 0; SC 30 (S 0) | [896 off] in view N 6 (S 7); NC 14 (S 0) | [896 off] KU 21 (S 27) | [896 off] 1 (S 0) | [896 off] 0.838 | — | about +55, plus a write call per batch ([896 report]) | Not admitted: its oracle control failed |
| C-key+L-ptr | [896 live] 17 (S 7) | [896 live] IC 0; SC 10 (S 0) | [896 live] N 52 (S 59), NC 28 (S 30) | [896 live] KU 11 (S 20) | — | [896 off] 0.920 | — | about +330, plus a write call per batch ([896 report]) | Not valid: N, C1, KU |
| RW-key, retrieved context in the embedding key | [902 screen] at most 7 linkable at write (S reaches 0) | [902 screen] IC at most 1 linkable; SC 0 of 50 introductions found | — | [902 screen] the search at write found the superseded statement for 30 of 30 KU | — | — | — | about +130 ([902 preregistration]) | Cut: its ceiling of 7 is below the 10 admission needs |
| RW, retrieved context in the summary | [902 screen] its message half as RW-key's; its mid half not bounded | as RW-key, message half | — | as RW-key: the superseded statement retrieved for 30 of 30 KU | — | — | — | about +75 ([902 preregistration]) | Cut: the shared ceiling and the correction risk |
| Entity profile cards | [906 screen] an attached card states the answer for 34 (S's items 0) | [906 screen] IC 1 (S 0); SC — | [906 screen] the associate's value on an attached card for N 36, NC 0 | [906 screen] KU current value on a card 7, superseded 9; overwrite control 23, 27 required | [906 screen] 0, 48 unbounded (S 0) | — | — | — | Cut: overwrite control failed, and the N exposure |
| W, the full-context ceiling | — | — | — | — | — | — | — | — | Not run (#900 Amendment 4) |
| R, a recall sub-agent with checked citations | [900 live] 13 (S 7) | [900 live] IC 0; SC 0 (S 0) | [900 live] N 52 (S 58), NC 28 (S 30) | [900 live] KU 11 (S 20) | [900 live] 1 (S 0) | — | — | +120 to +190 ([paradigms survey]) | Not valid: not significant; N, K, KU |
| D, an idle-time memory document | [900 off] 1 (S 0) | [900 off] IC 0; SC 0 (S 0) | [900 off] in view N 14 (S 7); NC 3 (S 0) | [900 off] KU 28 (S 27) | [900 off] 14 (S 0) | [900 off] 0.758 | — | +135 to +215 ([paradigms survey]) | Not admitted: its UP coverage control failed |
| A, the recall before #858: FTS5 with a phrase call | — | — | — | [854] 10 of 10; [856] 58 of 70 | — | — | [854] 29 of 40; [856] 168 | 0 at the time | The baseline of #854 and #856; replaced by #858 |
| B4, similarity over batch-4 summaries | — | — | — | [856] 65 of 70 (A 58); [901] 66 of 70 | — | [901] 0.684 | [856] 209 (A 168); [901] budgeted: 211 at 1×, 203 at 3.67× | as B8 against A ([856 report]) | Valid in #856 (secondary); shipped by #858; #901's baseline |
| B8, similarity over batch-8 summaries | [874 off] S at batch 8: 2 (S 0) | [874 off] IC 0 (S 0) | — | [874 off] K 14 of 20 (S 16); [854] 9 of 10 (A 10); [856] 62 of 70 (A 58); [901] 58 of 70 (B4 66) | — | [874 off] 0.466; [901] 0.450 | [854] 27 of 40 (A 29); [856] 194 (A 168); [901] 176 at 1×, 170 at 3.67× (B4 211, 203) | +100 to +190 against A ([856 report]) | Undetermined in #854; valid in #856; delivers less than B4 in #901 |
| B16, similarity over batch-16 summaries | — | — | — | [856] 55 of 70 (A 58); [901] 46 of 70 (B4 66) | — | [901] 0.280 | [856] 164 (A 168); [901] 114 at 1×, 112 at 3.67× (B4 211, 203) | — | Not valid in #856 (exact guardrail); delivers less in #901 |
| H of #856, FTS5 and B8 alternated | — | — | — | [856] 65 of 70 (A 58) | — | — | [856] 203 (A 168) | +160 to +265 ([856 report]) | Valid in #856 |
| D of #854, similarity over raw source messages | — | — | — | [854] 8 of 10 (A 10) | — | — | [854] 23 of 40 (A 29, B8 27) | — | Outside #854's decision, which compared B8 with A; trails B8 by 4 |
| T, an aged tier: B16 for older messages | — | — | — | [901] 46 of 70 (B4 66) | — | [901] 0.472 at 1×, 0.280 from 2.08× | [901] 172 at 1×, 112 at 3.67× (B4 211, 203) | — | Delivers less than B4 |
| K, B4 keys over B16 values | — | — | — | [901] 66 of 70 (B4 66) | — | [901] 0.280 | [901] 168 at 1×, 197 at 3.67× (B4 211, 203); [913 screen] with batch-4 recency (Kr), 197 (S4 201, B4 203), stated 187 (S4 197, B4 201) | — | No worse than B4 on delivery in #901; cut after the stated screen in #913 |
| X, FTS5 and cosine by reciprocal rank fusion | — | — | — | [901] 63 of 70 (B4 66) | — | [901] 0.684 | [901] 161 at 1×, 168 at 3.67× (B4 211, 203) | — | Delivers less than B4 |
| DR-rel, raw messages from the top 5 mids, by score | — | — | — | [901] 66 of 70 (B4 66) | — | [901] 1.684 | [901] 206 at 1×, 199 at 3.67× (B4 211, 203) | — | Secondary: no significant difference from B4 (p 0.27 and 0.34) |
| DR-new, the same, newest mid first | — | — | — | [901] 66 of 70 (B4 66) | — | [901] 1.684 | [901] 186 at 1×, 188 at 3.67× (B4 211, 203) | — | Secondary: delivers less than B4 |
| The reranker of #901 | — | — | — | — | — | — | — | — | Not run: its server could not load within the host's spare memory |

Left out: #874's L with rule entities (#874 reads only its model-entity setting as L's result), #854's C and #854's and #856's A0, which test the phrase call rather than batch size or the embedding, and #901's exploratory and bias-estimate arms (Kr, B8m, B8c), which appear under Findings only where a finding rests on them.

## Findings

Stated only as far as the table carries them. The chain and absent figures below are all live, on 100 C2 and C3 chains and 60 N items, except #874's N of 20.

- **Under strict abstention, every read-time arm trades association for abstention.** Against its own study's S:
  - The arms that gained 40 chains or more lost at least 11 N abstentions of 60: L (#885 −18, #888 −21, #896 −18), L+A −15, L-attr (#896 −21, #916 −21) and L+chk −11. #874's L lost 4 of 20.
  - The two arms that stayed within the N allowance of 3 gained 10 chains or fewer, and neither was significant in its study: M (+10, #874) and L-T (+9, #888).
  - The rest gained 10 chains or fewer and still lost more than 3 N abstentions: L+G (+3, N −5), L-ptr (+8, −4), C-key+L-ptr (+10, −7) and R (+6, −6).
  - L+V shows the trade inside one arm: unenforced it applies 59 chains and abstains on 36; enforced, 11 and 6.
- **Under the strict rule, no structure measured breaks it.** None has a significant association gain with its guardrails held.
  - R is the one live arm whose N losses are not associated values stated: it stated the associated value on 2 of 60 N probes, against S's 3 ([900 live]). But its chain gain is not significant (13 against 7) and it loses N, K and KU.
  - The write-time and screened structures that gained did so offline only, and each put near-miss values in view or failed a control: C (SC 39; NC near-miss beside the entity 24 of 30), C-key (SC 30; oracle control failed), profile cards (34 chains; the associate's value on a card for 36 of 60 N), D (UP 14; N near-miss in view 14 against 7; coverage control failed). None of them was measured live alone.
  - P, F and RW-key reached no chain; RW-key's screen caps what writing can carry at 7 of 100.
- **Under the owner's rule, L-attr keeps both, and #922 shipped it.** #916 ran it against S live on a fresh corpus ([916 live], [916 report]):
  - Chains 66 against 5 of 100, 63 won and 2 lost, exact McNemar p = 1.2e-16. Implicit IC2 and IC3 8 against 0, reported only.
  - Every guardrail held under the owner's rule: N 55 against 56, NC 29 against 30, K 27 against 27, KU 19 against 18, C1 30 against 27, E 20 against 21. Every control passed.
  - Under reading B, the sensitivity reading, N is −2 and the arm is valid. Under the strict rule N is −21 (35 against 56) and it is not valid, as in #896.
  - What the rule accepts: L-attr states the associate's value on 54 of 60 N probes, naming whose value it is on 53; S states it on 3.
  - The matcher that scores the rule was audited by hand: audit 1 failed, 68 of 80 agreeing, and the matcher was revised once; audits 1b and 2 passed, 38 of 40 and 29 of 30 ([916 audits]).
  - PR #922 (`ff383ed`) made the walk with attribution the production recall. ADR 0019 is revised in place, and [memory.md] states the current read step.
- **K was the last retrieval-level candidate, and #913 cut it after the stated screen.**
  - #901: at 3.67×, K delivers 197 of 280 against B4's 203, interval [−0.042, +0.008] inside the −0.05 bound, and stores 0.280 characters per source character against 0.684 ([901]). Every other #901 arm that ran delivers less than B4 or, for DR-rel, stores 1.684; the reranker did not run. At 1× it depends on recency: with its own B16 recency K delivers 168 against B4's 211, and with B4-sized recency (Kr, exploratory) 202.
  - #913, Kr at 3.67×: delivery passes, 197 against S4's 201 and B4's 203, both inside −0.05. The stated screen fails: a delivered text states the value for 187, against 197 and 201, B4 interval [−0.081, −0.009], and the exact-stated guardrail loses 5 net against B4, beyond the allowance of 3 ([913 screen]).
  - Why: the B16 summaries drop the value. Kr delivers the evidence without stating the value for 19 primary queries, against 9 each for S4 and B4 ([913 screen] `per_query_s4`). No paid call was made, and the shipped recall stood until #922.
- **What remains untested.**
  - The full-context ceiling W: no run has measured whether MiniMax-M2 answers absent questions from associations in view in their original wording.
  - K live: #913 cut the run, so K has no live figure and none on a chain, abstention or correction family. A batch-16 synthesis that keeps values is a new study, not a re-read of #913 ([#913](../memory-scaling/live/README.md#limits)).
  - The reranker, and any hosted embedder.
  - C's context line with S's summaries kept, which #890 names as its next step, and C, F, D or profile cards live on their own.
  - RW's compounding, and a search at write with another k or query.
  - A profile card with a model-written attribute key, or a user card that keeps old standing facts.
  - Implicit chains: no structure applied more than 8 of 50 IC live, or reached more than 8 offline (#916's L-attr, IC2 in both).
  - UP live beyond R's, S's and L-attr's (1, 0 and 0 of 50).
  - The walk's cost as the store grows: recall reads every episode and entity link each turn, so its cost grows linearly with stored episodes, and no index bounds it ([memory.md] §Open questions).
  - Seeds that need an exact name match: a query naming an entity differently from its stored name seeds nothing, and the studied corpora reused every name exactly, so this was not measured ([ADR 0019] §Read-policy limits).
  - Compression at or beyond B4: K stored 0.280 against B4's 0.684 and was cut on stated values (#913); L-attr stores more than S, 0.8225 against 0.7412 ([916 off]).
  - Every live figure is one run per probe on one model, MiniMax-M2, over templated corpora of invented names.
- **The owner's attributed-answer rule, and what #916 established under it.** Decided on 2026-09-28: on an absent probe, an answer that gives the associate's value while attributing it to the associate is acceptable.
  - #916 confirmed L-attr under it on a fresh corpus, and it decides #916's verdict. #916's N and NC are given under both rules; every other N and NC figure above is scored under the strict rule, and those recorded figures do not change.
  - The PM's re-read that motivated the rule, 18 of L-attr's 22 unabstained N replies naming the associate (the count as #916 states it; N 38 of 60 in [896 live]), is a name-mention count on data already seen, not a result. #916's exploratory re-read of the earlier records is likewise labelled exploratory and decides nothing.

[874 live]: ../chained-recall/evidence/live/scores.json
[874 off]: ../chained-recall/evidence/offline/scores.json
[874 report]: ../chained-recall/README.md
[885 live]: ../chained-recall/confirm/evidence/live/scores.json
[885 off]: ../chained-recall/confirm/evidence/offline/scores.json
[888 live]: ../chained-recall/readtime/evidence/live/scores.json
[888 off]: ../chained-recall/readtime/evidence/offline/scores.json
[890 off]: ../memory-encoding/validation/evidence/offline/scores.json
[896 live]: ../memory-encoding/structural/evidence/live/scores.json
[896 off]: ../memory-encoding/structural/evidence/offline/scores.json
[896 report]: ../memory-encoding/structural/README.md#arms
[900 live]: ../memory-paradigms/validation/evidence/live/scores.json
[900 off]: ../memory-paradigms/validation/evidence/offline/scores.json
[902 screen]: ../memory-encoding/retrieved-context/evidence/screen.json
[902 preregistration]: ../memory-encoding/retrieved-context/preregistration.md
[906 screen]: ../memory-encoding/profiles/evidence/screen.json
[854]: ../mid-memory-rag/evidence/scores.json
[856]: ../mid-memory-rag/evidence/replication/scores.json
[856 report]: ../mid-memory-rag/README.md#code-size-estimate-revised-for-the-valid-arms
[901]: ../memory-scaling/evidence/scores.json
[913 screen]: ../memory-scaling/live/evidence/scores.json
[916 live]: ../memory-encoding/attributed/evidence/live/scores.json
[916 off]: ../memory-encoding/attributed/evidence/offline/scores.json
[916 audits]: ../memory-encoding/attributed/evidence/audit/
[916 report]: ../memory-encoding/attributed/README.md
[memory.md]: ../../docs/architecture/memory.md
[ADR 0019]: ../../docs/architecture/adr/0019-memory-read-step.md
[links estimate]: ../chained-recall/arms/links/README.md#production-estimate-measured
[levels estimate]: ../chained-recall/arms/levels/README.md#production-estimate
[C estimate]: ../memory-encoding/validation/arms/context/README.md#production-estimate
[P estimate]: ../memory-encoding/validation/arms/prev/README.md#net-production-lines
[encoding survey]: ../memory-encoding/README.md
[paradigms survey]: ../memory-paradigms/README.md#candidates-for-a-validation-run
