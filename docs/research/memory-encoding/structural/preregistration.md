# Structural safeguards for associative recall: preregistration

Fixes issue #896's run before its first model call. The run tests whether a structural safeguard keeps the association gains while holding abstention and corrections. A structural safeguard controls what text is in view; an instructional one asks the model to behave. The gains are L's chains (#885, #888) and C's situated recall (#890). The run names the best valid structure, or none. It reuses #890's corpus, recorded syntheses, contexts, entities, harness and scorer (`../validation/`), except where this file says otherwise.

## Question

With the model acting, do the arms below make the agent apply more often than the shipped recall (S) does, on their primary family, while holding the absent, correction, single-hop and exact guards? The arms are L-ptr, L-attr, L+chk, C-key and C-key+L-ptr.

## Where the arms live

Each arm is a setting of an arm command that already exists, so no reviewed code is copied. This directory holds the study: this file, the scorer, its tests, the evidence and the report.

- **L's variants are settings of `../../chained-recall/arms/links`,** as #885's and #888's variants were. They reuse its entity graph, walk and slot filling.
- **C-key is a setting of `../validation/arms/context`.** It reuses C's recorded contexts and its re-embedding.
- **C-key+L-ptr is a setting of `arms/links`,** built from a copy of C-key's store rather than the shared one. It adds no embedding code.
- **This directory reaches #890's data through four committed symbolic links.** `study.Main` reads the corpus, recorded syntheses and recordings from its `-dir`, and writes evidence under the same `-dir`. The links let every command take `-dir docs/research/memory-encoding/structural`, so no record lands under `../validation/`:
  - `corpus.json`;
  - `evidence/synthesis/`;
  - `arms/links/evidence/entities-b4.jsonl`;
  - `arms/context/evidence/context-b4-w8.jsonl`.
- **No build calls the model.** Those recordings are complete, so every build runs without `-proxy`, and a build with a missing batch fails instead of sending it. Only L+chk's offline records and every live turn call the model.

## Arms

All arms run at batch 4 over the shared store `harness build -batch 4` rebuilds from #890's recorded summaries. The base's 1,200 mids keep their recorded summaries, as in #890.

| Arm | Setting | What changes from its parent | Net production lines, before tests |
|---|---|---|---:|
| S | `arms/shipped` `b4` | The shipped recall | 0 |
| L, reference only | `arms/links` `b4-model` | Nothing: #885's L | about +270 |
| L-ptr | `arms/links` `b4-model-ptr` | A walk mid is shown as a pointer only | about +275 |
| L-attr | `arms/links` `b4-model-attr` | A walk mid is shown with its subject beside the query's entities | about +285 |
| L+chk | `arms/links` `b4-model-chk` | A grounding check the runtime runs before any walk mid goes on as evidence | about +345 |
| C-key | `arms/context` `b4-w8-key` | C's context only in the embedding key; S's summary shown | about +55, and one write call per batch |
| C-key+L-ptr | `arms/links` `b4-model-key-ptr` | L-ptr over C-key's store | about +330, and one write call per batch |

- **L-ptr.** Recall and `memory_search` fill their slots from the similar list and the walk's list in turn, as L's do. A walk-delivered mid's text is only `Linked through "<entity>"; content not shown, read its sources with memory_inspect.` (`pointerText`). The study's label after it names the mid ID and the source interval. Its content is reachable only through `memory_inspect`. Lines: L's +270, plus about 5 to render the pointer.
- **L-attr.** As L, except for a walk-delivered mid whose subject is none of the query's entities.
  - The subject is the first entity the mid's synthesis wrote (`graph.subject`). The query's entities are the walk's seeds.
  - That mid's text opens with `About "<subject>"; the question names "<seed>" and ... .` (`attribution`). The names are the stored, normalised ones.
  - Lines: L's +270, plus about 15 for the subject lookup and the attribution.
- **L+chk.** As L, plus a runtime-enforced grounding check, the Self-RAG / CRAG pattern, in the form the PM fixed on the issue:
  - **When it runs.** A recall block or `memory_search` result that holds a walk mid triggers the check. Nothing else does.
  - **The call.** One extra call per such block or result, through the study proxy under phase `check`. It gives the question and every mid item of the block, recent, similar and walk, with their IDs and text (`checkPrompt`).
  - **The answer.** A reserved schema, `memory_grounding` with `items`, returns an ordered list of at most 3 item IDs that together connect the asked entity to the asked attribute, or an empty list. For a search, the question is the search query.
  - **The structural guard.** The runtime keeps the named items only when the set is connected under stored entities: the first item names one of the query's entities (a walk seed), and each next item shares a stored entity with the one before it (`graph.connects`).
  - **What goes on.** Kept items go on in the check's order, as L shows them, and every other mid item is dropped. Otherwise, whether the list is empty or disconnected, the block or result carries only `No stored memory states this fact.`, rendered without a memory label. The pending source previews production appends stay.
  - **Malformed answers.** An answer that is not one `memory_grounding` call naming at most 3 distinct delivered IDs is asked again, up to 3 attempts. After the third it counts as empty: the check fails closed.
  - **Errors.** A transport or provider error fails the turn, and the live runner re-runs the probe (§Paid phases).
  - It is not a contract line: the runtime makes the call and applies its answer.
  - Lines: L's +270, plus about 75: the prompt and schema, the call and its parsing with retries, the connectivity rule, the notice, and applying it to recall and search.
- **C-key.** The shared store, with every stream mid re-embedded from C's recorded context line, a blank line, then S's recorded summary. The context comes first, as C embedded it. The shown text is S's summary, and recall and search are S's (`study.ShippedMemory`).
  - It reuses S's recorded summaries (`evidence/synthesis/stream-b4.jsonl`) and C's recorded context fields (`arms/context/evidence/context-b4-w8.jsonl`), so it makes no write-time call here.
  - A batch C recorded as failed keeps its shared mid.
  - Lines: the survey's C (`../README.md` §C, +35 to +65) without the recall label. Production cannot reuse two recordings: C's context line absorbed its neighbours when the summary came from the same call (#890). So a plain summary needs its own call, and C-key costs one extra write call per batch.
- **C-key+L-ptr.** C-key's store, copied, with L's entity table added from the same recorded entities, opened as L-ptr. Lines: C-key's +55 and L-ptr's +275.
- **L** runs live as the reference for how much of L's gain each variant keeps on this corpus. It is not compared in the decision.
- **Common to every arm**, as #890 §Live stage fixes it:
  - a live window of 32 messages and an empty note;
  - each probe one turn on its own copy of the arm's build;
  - MiniMax-M2 through the study proxy, no arm instruction.

## Corpus

#890's selected corpus, unchanged (`../validation/corpus.json`, SHA-256 below), with its recorded syntheses, contexts and entities. No new seed: every arm here is a read-time or key-only change over recordings that were fixed before #890's selection, so the corpus is as independent of these arms as it was of #890's.

| Family | n | Role here |
|---|---:|---|
| C2, C3 | 50 each | primary for L-ptr, L-attr and L+chk; with SC, for C-key+L-ptr |
| SC | 50 | primary for C-key; with C2 and C3, for C-key+L-ptr |
| N | 60 | guard |
| NC | 30 | guard |
| K | 40 | guard |
| KU | 30 | guard |
| C1 | 30 | guard |
| E | 30 | guard |
| IC2, IC3 | 25 each | reported only |
| UP | 50 | offline only, reported; not probed live, being no arm's family |

## Offline stage and admission

Every arm records `study.Offline` over the corpus into `evidence/offline/<arm>/<setting>/`, and `score.py offline` scores them. It uses #890's matching (`../validation/score.py`), with two changes in what counts as reached. They apply to every arm, S included, because two of the arms show text #890's rule cannot count:

- **A pointer reaches its hop.** A chain hop is reached when a delivered item covers the hop's statement and either states both of the hop's entities, as #890, or is a pointer whose linking entity is one of the hop's two entities. A pointer shows no text, so #890's rule could never count it. The hop's content is one `memory_inspect` away.
- **SC counts the value.** SC is reached when a delivered item covers the evidence exchange and states the value. C-key shows S's summary, which never names the subject (`../validation/README.md` §Offline results), so #890's rule would count C-key 0 by construction. #890's stricter count, subject and value in one item, is reported beside it.
- **Everything else is #890's.** E, K, KU and C1 reach, the N and NC near-miss measures, context characters per turn, stored characters per source character, vectors, and the oracle.

**Admission to the live stage** is #890's rule with its offline margin. An arm's setting goes live when:
- its mechanism controls passed (§Controls);
- on its primary family it reaches at least 10% of the family more than S does: C2 and C3, 10 of 100; SC, 5 of 50; C2, C3 and SC together, 15 of 150;
- and the exact McNemar test against S gives p < 0.05.

L+chk's offline items are those after the check, so its offline records make check calls.

## Paid phases

MiniMax-M2 at `https://api.minimax.io/v1/chat/completions` only, through one proxy: `harness proxy` on a free local port, with its ledger in `evidence/ledger/`.

- `MINIMAX_KEY` is in the proxy's environment only.
- The list-price ceiling is USD 20.00, a guard only. The key is on a subscription, and nothing is bought.
- No other paid API is called.
- Embeddings come from a local text-embeddings-inference 1.9.4 container, started for this run on a free port. It serves `BAAI/bge-base-en-v1.5` at revision `a5beb1e3e68b9ab74eb54cfd186867f64f240e1a`, the image digest #890 names.

| Phase | Calls | Estimate (input / output tokens) |
|---|---:|---|
| Key probe | 1 | negligible |
| Live smoke, L+chk and L-ptr: 2 probes each, excluded from scoring | 4 | 0.03M / 0.002M |
| L+chk's offline check (phase `check`, episode `b4-model-chk`): recall blocks and oracle searches holding a walk mid | at most 1,000 | 1.0M / 0.3M |
| Live, S, L-ptr, L-attr, L+chk, C-key, C-key+L-ptr and L: 420 probes each (phase `live`) | about 4,000 | 14M / 0.9M, from #888's 4,400 input and 270 output tokens per probe |
| Live check calls of L+chk (phase `check`, episode the probe) | about 600 | 0.6M / 0.2M |

- **Pacing.** Two requests at a time. Arms go live one after another in the order of the table, never together. A survey worker shares the key but makes no model calls.
- **Usage limit.** On HTTP 429 with code 2056 the proxy pauses every request and probes the key at most every 30 minutes (`../../chained-recall/harness/quota.go`). Callers wait, and a pause uses up no attempt.
- **Re-runs.** A live probe that fails on the provider's limits, a server fault or the network runs once more, and its first record is kept in `results-attempt1.jsonl`. Any other failure counts as not applied.
- **Tokens** are reported per phase from the ledger (`score.py tokens`).
  - The ledger routes every `arms/links` setting under arm L. Their live calls are told apart by each setting's run window, recorded in `evidence/live/run.json`.
  - L+chk's check calls carry phase `check`, with the probe as episode.
- **Dropping L.** If the key's usage limit stops the run for more than a day before L's live pass, L is not run live and the report says so. L is only the reference.

## Endpoints

Scored by `score.py` as committed here. It calls `../validation/score.py` and `../../chained-recall/score.py` for matching, answer checks, abstention and McNemar.

- **Applied, the primary endpoint.** As #885 and #890: the turn's reply, messages sent and files written state the chain's or the SC item's answer.
- **Guards**, as #890:
  - E: the value;
  - K and KU: the corrected value;
  - C1: the answer;
  - N and NC: an abstention by #885's pattern, which counts a clarifying question.
- **Reported beside them:**
  - for N and NC, whether the output states the near-miss value;
  - for KU, whether it states the obsolete name;
  - every hop seen, counting `memory_inspect` results, through which a pointer's content is read;
  - searches and inspects per probe;
  - IC2 and IC3 applied.
- **L+chk's own record:**
  - its check calls and tokens per turn;
  - per family, how often the check ran, named a set, named a connected set, and kept nothing. Reported for C2, C3, N, NC and K, and for every family.

## Comparison and decision

#885's decision rule and guardrails, with #890's guard families:

- **Test.** Each admitted arm against S on its primary family: the exact two-sided McNemar test on applied, Holm-corrected across the arms compared, at family-wise α = 0.05.
- **Guardrails.** An arm may lose against S at most:
  - N 3 of 60 and NC 2 of 30;
  - K 2 of 40 and KU 2 of 30;
  - C1 3 of 30 and E 3 of 30.
- **Valid.** An arm is valid when its difference is positive and significant, every guardrail holds, and every control passed. IC2 and IC3 never gate.
- **Decision.** The best valid structure is the valid arm with the fewest net production lines (`score.py` `NET_LINES`), as #885 and #890 decide.
- **Best structure when none is valid.** The report still ranks the arms that went live by the first of these that separates them, as #888 did:
  1. the fewest guardrails failed;
  2. the fewest probes lost beyond the allowances;
  3. the largest net primary gain (arm only minus S only);
  4. the fewest net lines.

  That ranking is a description, not a validation.
- **Null.** If no arm is valid and every control passed, the shipped recall stands, and no arm is named for implementation.
- **Comparison table.** The report tables every structure measured since #874 by chains, implicit association, abstention, corrections, compression and code cost. It quotes earlier studies' reports and does not re-score them.

## Controls

A failed control makes that arm's verdict undetermined, and the report names it. Where a control has a live form, it is read from the live records too.

- **Oracle.** For each arm, each hop's own statement delivers the hop's mid within the top 5 for at least 95% of hops. L+chk's store and its search before the check are L's, so its control is L's oracle. Its checked oracle is reported.
- **L-ptr and C-key+L-ptr: no walk-episode text.**
  - Every walk item in every offline recall and oracle search is exactly the pointer text for its entity.
  - Every `(linked)` line of every live recall block is exactly a pointer and its label.
  - This must hold for all of them.
  - Walk items are in the live recall block of at least half the primary chain probes.
- **L-attr: attributions present and correct on N and NC**, checked by the scorer from the recorded trace and the live recall blocks.
  - **Present.** Every walk item whose recorded subject is none of the query's entities opens with its attribution, and every other walk item carries none: all of them, offline and live.
  - **Correct.** Where the walk delivers the N item's associated-value episode, its attribution names the associated entity as subject and the queried entity as named: at least 90% of such N items, offline and live. On NC, the subject is never the queried entity: all of them.
- **C-key: shown text S's, ranking not.**
  - Every delivered stream mid's text equals S's recorded summary for its batch, offline and live, all of them. S's own records pass the same comparison, so the comparison can fail.
  - C-key's delivered mid list differs from S's for at least 10% of offline probes.
- **L+chk: the check ran.**
  - **Every walk delivery was checked.** Every recall block and search result that held a walk mid ran the check: offline, from the trace; live, from `evidence/live/L/b4-model-chk/checks.jsonl`, all of them.
  - **The ledger agrees.** For every live probe, the ledger holds at least as many `check` calls as the probe's final attempt made checks.
  - **Counts reported.** Named-but-disconnected sets and fail-closed malformed answers.
- **Mutation checks.** `mutation.py` shows the tests fail with each behaviour #896 added removed:
  - the pointer text;
  - the attribution, and the difference that triggers it;
  - C-key's kept summary, and its key;
  - C-key+L-ptr's start from C-key's store;
  - the check's call, its filtering, its connectivity rule, its notice, its application to search, and failing closed;
  - the notice rendering;
  - the scorer's pointer reach, SC value reach, admission margin and each control.

  `evidence/mutation.txt` records it before the first paid call.
- **Existing settings.** L's earlier settings and C's `b4-w8` behave as before. S and L `b4-model` are re-recorded offline into this study, and `score.py equivalence` compares them field by field, timing excepted, with #890's committed records.

## Collection

1. This file, the arm settings, the scorer, its tests and the mutation record, committed.
2. The TEI container; the shared store (`harness build`); C-key's build before C-key+L-ptr's. Every build runs without `-proxy`.
3. Offline records of S, L, L-ptr, L-attr, C-key and C-key+L-ptr, then the equivalence check.
4. The proxy, with its preflight probe; L+chk's offline records.
5. `score.py offline`: the admission.
6. A smoke run of the first C2 and the first N probe for L+chk and for L-ptr, if admitted, into `evidence/live-smoke/`.
   - They are excluded from scoring.
   - A harness defect they reveal is fixed and committed before collection. None of the endpoints, controls or rules above changes.
7. Live, one admitted arm after another: S, L-ptr, L-attr, L+chk, C-key, C-key+L-ptr, then L. Only the families of §Corpus, with UP left out, through `live -only`.
8. `score.py live`, `tokens` and the controls.

Every probe's store copy is kept outside the repository until scoring. Nothing is written under `../validation/` or any other earlier study's evidence.

## Revision

- Harness, study, arms and scorer at the commit that adds this file.
- SHA-256 of `../validation/corpus.json`: `63e5c3df2772bd39ff8ad8967bf2eb69884fd3b7839b12fccc46005a2f88d718`.
