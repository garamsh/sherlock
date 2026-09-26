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

- `go run ./docs/research/chained-recall/arms/links build -work <scratch> -setting b4` builds one setting. Build `b4` and `b8` after `harness build` has written the shared stores. They make no model call.
- `b4-model` calls the model for every batch whose entities are not yet recorded. It needs `-proxy` and the phase in `preregistration.md`. With `evidence/entities-b4.jsonl` complete, it makes no call.
- `... offline -work <scratch>` then records the probes. `python3 docs/research/chained-recall/score.py offline` scores them.
- `python3 docs/research/chained-recall/arms/links/controls.py [<ledger calls.jsonl>]` computes the controls into `evidence/controls.json`. Given the ledger, it also pairs the added synthesis tokens (`preregistration.md` §Measures). `controls_test.py` checks it.

## Results

Pending: the shared stores need #874's stream synthesis.

## Limits

- **Rule entities are not the model's.** At `b4` and `b8` the entities come from capitalization in the recorded summaries. A name the summary writes in lowercase is missed. So is a sentence-initial word that is not a stopword, which starts a spurious entity. `b4-model` measures the model's own list at batch 4 only.
- **Names must match exactly.** A query that names an entity differently from the summary seeds nothing. This is the failure the survey predicts from HippoRAG. The corpus reuses each entity's exact name across hops and query, so this study cannot measure that failure.
- **Equal context by item count.** L delivers S's seven mids, not a budget of characters. Its context characters are reported beside S's.
- **New mids stay unlinked.** A mid the live session writes during a probe has no entities, as the survey's existing-database path describes.
