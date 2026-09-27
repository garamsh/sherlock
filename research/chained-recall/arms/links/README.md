# Arm L: entity links and a PageRank walk

Records arm L of issue #874's chained-recall study (#880): survey candidate L (`../../../associative-memory/README.md` §Candidates for sherlock), built on the shared interface (`../../study/arm.go`) and measured offline on the shared chain corpus.

## Method

- **Store.** Each setting copies the shared store at its batch size (`study.Env.SharedStore`). It adds one table, `mid_entity`, which links each mid to the normalized names of the entities its synthesis names (`main.go`).
- **Entities.** They come from one of two sources, and a setting says which:
  - **by rule** (`entities.go` `ruleEntities`), at `b4` and `b8`. Each maximal run of capitalized words in the recorded summary becomes one entity, with leading stopwords removed. The rule stands in for names the synthesis call would write, and no model is asked.
  - **by the model** (`synthesis.go`), at `b4-model` only. The synthesis call, with its schema extended by an `entities` array, writes each batch's entities (`preregistration.md`). The mid keeps its recorded summary.
- **Names.** A name is stored as its words, lowercased, with a possessive and leading stopwords removed. "The Suvun Hotel's" is stored as `suvun hotel`.
- **Graph.** The graph is bipartite: a mid is joined to each entity it names (`walk.go`).
- **Seeds.** A stored entity is a seed when all of its words occur, contiguously, among the query's words. Seed matching makes no model call. Each seed is weighted by one over the number of mids that name it, as HippoRAG weights its seeds.
- **Walk.** Personalized PageRank from the seeds, at damping 0.5, HippoRAG's setting. It iterates until the scores change by less than 1e-12 in total, or 100 iterations. Every mid with a positive score is ranked by score, the newer first on a tie.
- **Recall** (`memory.go`):
  - It delivers the two newest mids first, as the shipped recall does.
  - Then it fills the shipped recall's five historical slots, taking in turn from the similar list and the walk's list. It skips mids already delivered, and an exhausted list gives its turns to the other.
  - A mid the walk delivers has the kind `linked`.
  - So L delivers as many mids as S does, and its context is S's at equal item count. When the query names no stored entity, recall is S's recall exactly.
- **Search.** `memory_search` alternates the two lists the same way over its `limit` slots.
- **Trace.** Every call records a trace (`study.Tracer`):
  - the seeds;
  - the iterations taken;
  - how many mids the walk reached;
  - which delivered mids came from the walk;
  - which of those were outside the similar list;
  - which of those name no seed, so that reaching them took more than one entity step.
- **Settings:**

  | Setting | Batch | Entities |
  |---|---:|---|
  | `b4` | 4 | by rule |
  | `b8` | 8 | by rule |
  | `b4-model` | 4 | by the synthesis call |

## Controls

Fixed before the first offline run. A setting whose control fails is reported as not having run its mechanism, and its reach is not read as L's result.

- **Entities stored.** At least 90% of mids hold at least one entity (`built.json` `notes.share_with_entities`).
- **The walk ran.** On the 150 chains of k ≥ 2, both of the following hold:
  - on at least 90% of chain queries, recall has at least one seed and the walk takes more than one iteration;
  - on at least 50% of them, recall delivers at least one mid from the walk that lies outside the similar list.

  The trace also counts how many of those name no seed, and it is reported beside them.
- **Oracle.** For at least 95% of the 405 hops, `Search` over the hop's own statement delivers the hop's mid within the top 5 (`oracle.jsonl`). This is the threshold #874 sets for S.
- **Mutation checks.** Each of the following breaks the named tests:
  - Removing the entities (`ruleEntities` returns none) breaks `TestRuleEntities_*` and `TestRecall_DeliversEveryHopWithinTheShippedBudget`.
  - Removing the walk (one iteration, so only the seeds' own mids score) breaks `TestLinked_FollowsTheChainPastTheSeeds` and `TestRecall_DeliversEveryHopWithinTheShippedBudget`.
  - Removing the seed matching (no query word matches) breaks `TestSeeds_*`, `TestLinked_*` and `TestRecall_DeliversEveryHopWithinTheShippedBudget`.

## Running it

- `go run ./research/chained-recall/arms/links build -work <scratch> -setting b4` builds one setting. Build `b4` and `b8` after `harness build` has written the shared stores. They make no model call.
- `b4-model` calls the model for every batch whose entities are not yet recorded. It needs `-proxy` and the phase in `preregistration.md`. With `evidence/entities-b4.jsonl` complete, it makes no call.
- `... offline -work <scratch>` then records the probes. `python3 research/chained-recall/score.py offline` scores them.
- `python3 research/chained-recall/arms/links/controls.py [<ledger calls.jsonl>]` computes the controls into `evidence/controls.json`. Given the ledger, it also pairs the added synthesis tokens (`preregistration.md` §Measures). `controls_test.py` checks it.

## Results

Measured on 2026-09-27 at the revision this file is committed with.

- **Stores.** The shared stores were built with `harness build -batch 4|8` into a scratch directory, from #874's recorded stream syntheses. They deliver S's committed records exactly: `diff` of `evidence/offline/S/b{4,8}/{probes,oracle}.jsonl` at `4590259` against S run on these stores, without the `seconds` field, shows no difference.
- **Records.** They are in `evidence/offline/<setting>/`, as `study.Offline` writes them under the study's `evidence/offline/L/`. The figures below are from `score.py offline` run over them beside S's committed records, and from `controls.py` (`evidence/controls.json`).

### Controls

| Setting | Mids with entities | Seeded and walked | Walk delivered beyond similar | Also beyond the seeds' mids | Oracle | Passed |
|---|---:|---:|---:|---:|---:|---|
| `b4` (rule) | 88.1% | 150/150 | 150/150 | 28 | 405/405 | no: entities under 90% |
| `b8` (rule) | 96.3% | 150/150 | 150/150 | 31 | 401/405 | yes |
| `b4-model` | 97.4% | 150/150 | 142/150 | 108 | 405/405 | yes |

- The `b4` setting fails its entity control, so its reach is not read as L's result. It is reported below for comparison with `b4-model`.
- `b4-model`: batch 509–512 has no entities. Its one real call returned `entities` as a string. The shared proxy caches a well-formed `memory_summary` call even when this study's `entities` field has the wrong type, so every retry returned the same answer and no retry could recover it. The ledger rows are arm `L`, episode `b4-00128`, 2026-09-26 15:44:35Z to 16:29:26Z. The #874 harness fix at `367d0b1` covers only the summary's bound. The mid stays unlinked, which is 1 of 1,624. The batch lies in the base, and no chain hop or guard evidence is in it (`corpus.json`).

### Chain reach, offline

A chain is reached when every hop's mid is delivered with both of the hop's entities stated (`../../score.py`). "Clean" is the 114-chain set #874's live preregistration fixes.

| Arm/setting | k ≥ 2 | Clean | k = 1 | k = 2 | k = 3 | vs S b4: arm only / S only, Holm p |
|---|---:|---:|---:|---:|---:|---|
| S b4 | 0/150 | 0/114 | 29/30 | 0/75 | 0/75 | — |
| S b8 | 2/150 | 0/114 | 26/30 | 2/75 | 0/75 | 2 / 0, 0.5 |
| L `b4` (rule) | 23/150 | 18/114 | 30/30 | 23/75 | 0/75 | 23 / 0, 7.2e-7 |
| L `b8` (rule) | 10/150 | 5/114 | 30/30 | 10/75 | 0/75 | 10 / 0, 0.0039 |
| L `b4-model` | 74/150 | 51/114 | 30/30 | 53/75 | 21/75 | 74 / 0, 4.2e-22 |

The Holm correction here runs over these four comparisons only. #874's scoring of all arms together replaces it.

### Per hop and per family (hops reached, in order)

| Family | S b4 | L `b4` | L `b8` | L `b4-model` |
|---|---|---|---|---|
| C1 (30) | 29 | 30 | 30 | 30 |
| C2 (50) | 46, 0 | 47, 23 | 50, 10 | 50, 50 |
| C3 (50) | 43, 0, 2 | 50, 4, 1 | 50, 5, 1 | 50, 37, 17 |
| IC2 (25) | 10, 0 | 25, 0 | 21, 0 | 25, 3 |
| IC3 (25) | 21, 0, 0 | 25, 0, 0 | 12, 0, 0 | 25, 17, 4 |

Guards, reached of 20: E 17 and K 16 at batch 4 for S and every L setting. At batch 8, S reaches E 11 and K 14, and L reaches E 12 and K 14. When a guard query names no stored entity, L's recall is S's recall.

### Compression and context

| Arm/setting | Stored chars per source char | Context chars per turn |
|---|---:|---:|
| S b4 | 0.705 | 3,498 |
| S b8 | 0.466 | 4,009 |
| L `b4` | 0.740 | 3,840 |
| L `b8` | 0.497 | 4,379 |
| L `b4-model` | 0.782 | 3,576 |

- L delivers the same number of mids as S. Its context differs only by which mids fill the slots.
- It stores the entity names on top of S's summaries: 4,245 links over 2,335 names at `b4`, and 6,419 links over 4,694 names at `b4-model` (`built.json`).

### Added synthesis tokens (`b4-model`)

- Paired with each batch's recorded synthesis over all 1,624 batches, the extended call wrote a mean of 62.8 more output tokens (median 54) and read 61.0 more input tokens (median 61) (`controls.py` over the proxy's `evidence/ledger/calls.jsonl`, 2,753 rows, SHA-256 `4eb364c0…211f`).
- The phase used 773,840 input and 568,626 output tokens over 1,661 ledgered attempts, 1,632 of them answered by the provider.

### Where rule entities lose hops

At `b4` the trace's seeds show common words as entities: "build" and "rodend" for C3-01, and "kidrar", "note", "office" and "write" for IC2-01 (`evidence/offline/b4/probes.jsonl`). A sentence-initial common word in a summary becomes a name. A seed on such a word takes one of the walk's two slots with an unrelated mid. For hop 1 of C3-01, `b4-model` recorded only "Rodend" and "Gebind Components" (`evidence/entities-b4.jsonl`, first source 6301). Its walk seeds on "rodend" alone and reaches three mids, and hop 2 reach on C3 rises from 4 to 37. C3-01 still misses hop 3, because the walk has only two slots and hop 1 takes one of them.

### Production estimate, measured

Non-blank, non-comment Go lines in this arm that a production port carries, with the study's tracing and the rule extractor left out:

| Part | Lines |
|---|---:|
| The walk and graph (`walk.go` without `namesAny`) | 109 |
| Name normalization (`entities.go` without `ruleEntities`) | 52 |
| The entity table, written and read (`main.go` `entitySchema`, `writeEntities`, `readEntities`) | 50 |
| Recall and search filling slots from both lists (`memory.go` `list`, `next`, `take`, with `alternate` untraced) | about 43 |
| The extended synthesis call, over production's `Summarize` (37 against 33), plus returning entities through the store | about 15 |

- The measured total is about +270 lines, against the survey's +135 to +210.
- The survey's existing-database path, +15 to +25, is not built here.

## Limits

- **Preview stores.** The shared stores were built in this worker's scratch directory, not taken from #874's. They are shown equal only through S's records.

- **Rule entities are not the model's.** At `b4` and `b8` the entities come from capitalization in the recorded summaries. A name the summary writes in lowercase is missed. So is a sentence-initial word that is not a stopword, which starts a spurious entity. `b4-model` measures the model's own list at batch 4 only.
- **Names must match exactly.** A query that names an entity differently from the summary seeds nothing. This is the failure the survey predicts from HippoRAG. The corpus reuses each entity's exact name across hops and query, so this study cannot measure that failure.
- **Equal context by item count.** L delivers S's seven mids, not a budget of characters. Its context characters are reported beside S's.
- **New mids stay unlinked.** A mid the live session writes during a probe has no entities, as the survey's existing-database path describes.
