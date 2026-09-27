# Write-time encoding validation: preregistration

Fixes issue #890's run before its first model call. The run measures the memory-encoding survey's write-time candidates against the shipped recall, offline and live, under the survey's test plan (`../README.md` §Test plan), which stands as written. It uses #874's harness, proxy, study package and matching (`../../chained-recall/`), except where this file says otherwise. Arms C and P add their own preregistrations under `arms/context/` and `arms/prev/` before their paid phases; they may add controls, not change anything this file fixes.

## Question

Does context at synthesis (C), an episode linked to the episode it continues (P) or standing facts written by the synthesis call (F) make the agent reach and apply what its primary family needs more often than the shipped recall (S), while holding the absent, correction, single-hop and exact guards?

## Arms

Every arm runs at batch 4 over the shared store (`study.Env.SharedStore(4)`), copied, and adds to it only for the stream. The base's 1,200 mids keep the replication's recorded summaries and nothing else, as #885's L kept #874's base entities: the test plan has each arm synthesise the stream once. So an arm's effect on E and K, whose evidence is in the base, is only what its stream additions displace or add.

- **S** (`arms/shipped`, `b4`): the shipped structure, `study.ShippedArm`.
- **C** (`arms/context`, `b4-w8`): context at synthesis as `../README.md` §C defines it, with W = 8 and without the note, which is empty offline and live (§Test plan).
- **P** (`arms/prev`, `b4-prev` and `b4-k5`): episodes chained on the previous episode, and on 5 candidates, as `../README.md` §P defines it.
- **F** (`arms/facts`, `b4`): standing facts written by the synthesis call, as `../README.md` §F defines it:
  - The `memory_summary` schema gains `facts`: at most 5 statements per batch, each at most 200 characters, each typed `user` or `other`, stated only from the batch. It is the same one call.
  - As L did in #874, the mid keeps its recorded summary. Only the facts are kept from F's call, so F and S differ in the facts alone.
  - Recall is S's recall, then the newest `user` facts up to 1,000 characters as the profile block (kind `profile`), then the top 3 facts by cosine to the task not already delivered (kind `fact`). Each fact item carries its mid's source interval. `memory_search` is S's.
  - A fact is embedded by the local embedder, as a mid is.
- **L** (`arms/links`, `b4-model` of `../../chained-recall/arms/links`, run with `-dir` at this study): the offline reference for chain reach. Its base entities are #874's recording; the stream's are written by a new entity call. It never goes live.

## Corpus

`corpus.py assemble`, seed 890, #874's templates and #885's rules, then `corpus.py select` after the check.

| Family | Candidates | Kept | Role |
|---|---:|---:|---|
| C2, C3 | 70 each | 50 each | primary for P |
| IC2, IC3 | 30 each | 25 each | secondary for every arm |
| SC | 70 | 50 | primary for C |
| UP | 106 | 50 | primary for F |
| C1 | 30 | 30 | guard |
| E | — | 30 | guard, replication, as #885 |
| K | — | 40 | guard, replication, as #885 |
| KU | 30 | 30 | guard |
| N | 60 | 60 | guard, #885's construction |
| NC | 30 | 30 | guard |

- **Every candidate is in one stream** after the base: 4,128 messages, 1,032 batches of four. Every summary any arm writes of it is fixed before selection.
- **Every exchange fills its batch alone.** Two exchanges of one item sit at least 16 exchanges apart, except an SC or NC item's introduction, which is the exchange right before its second, and a KU item's change, which follows its first statement.
- **UP facts** come as an aside after an unrelated opening sentence (`corpus.py` `OPENINGS`), each in a slot of its own, and were pre-screened on raw exchange text before any call (`evidence/screen.json`).
- **N** is #885's construction, drawn at its size. #885 kept the absent items whose walk delivered the associated episode; that selection needs L's records and is not repeated, so every N item is kept.
- **Check and select** (`corpus.py select`, over `evidence/selection/check-b4.json`, `harness check` on the candidates). A candidate is kept when the raw query ranks none of the sources its family needs within the top 20 of S at batch 4: every hop but the first of a chain, the evidence of SC, the fact of UP. `select` keeps the first passing candidates of each family in candidate order. A family with too few passing candidates stops the run.
- **Oracle.** On the selected corpus, each hop's own statement delivers its mid within 5 for at least 95% of hops (`score.py check` on `evidence/check-b4.json`). A failed oracle stops the run.

## Paid phases, in order

Each is MiniMax-M2 through the one study proxy, run by this issue's worker with `MINIMAX_KEY` in its environment only.

1. **Stream synthesis** (`harness synthesize -batch 4`): the shipped call over the 1,032 stream batches, for the shared store. About 1,032 calls.
2. **F's facts** (`arms/facts build`): F's extended call over the same batches. About 1,032 calls.
3. **L's stream entities** (`arms/links build -setting b4-model`): about 1,032 calls. It runs only after phases 1, 2 and the C and P workers' synthesis are done, and is dropped if the key's usage limit stops it for more than a day; the report then says L was not run.
4. **C's and P's syntheses**, under their own preregistrations.
5. **Live**, only after every arm's offline stage is scored.

Estimates: #885's stream synthesis read 328 and wrote 201 tokens per call. The extended calls add up to about 100 tokens each side, as L's did (61 and 63). Phases 1 to 3 are about 1.2M input and 0.8M output tokens.

## Offline stage

Every arm records `study.Offline` for its settings, and `score.py offline` scores them.

- **Reach.** A chain, as #874: every hop's mid delivered stating both of its entities. SC: the evidence mid delivered in an item that states both the subject and the value. UP: the fact's mid delivered in an item that states the fact's word. KU: the change's mid delivered stating the new name. E and K: as #874.
- **Risk measures, not gates.** For N and NC, whether an item states the near-miss value, and whether one item states it together with the query's entity.
- **Compression.** Stored characters per source character, context characters per turn and vectors, beside S's.
- **Admission to the live stage.** An arm's setting goes live when its mechanism control passed, and on its primary family it reaches at least 10% of the family more than S and the exact McNemar test against S gives p < 0.05. The families: C, SC; P, C2 and C3; F, UP. Of P's two settings, each that meets the margin goes live.

### F's controls

Fixed before F's first call.

- **User facts.** Of the selected UP items, at least 90% have a fact typed `user` that states the fact's word in the batch holding the fact (`arms/facts/controls.py`).
- **Facts stored.** At least 90% of stream batches return a well-formed facts array, empty allowed.
- **Oracle.** As every arm.
- **Mutation checks.** Removing the profile block, the fact list, the `user` type filter or the character bound breaks a named test (`mutation.py`, `evidence/mutation.txt`).

## Live stage

- S always, and each admitted arm setting.
- Every chain and guard probe, one live turn each, on its own copy of the arm's build, with a 32-message window and an empty note, through MiniMax-M2 and the study proxy, two probes at a time. A probe that fails on infrastructure is re-run once (`study.Live`).
- *Applied*, as #874 and #885: the reply, messages sent or files written state the answer (chains, SC, UP), the new value (K, KU), or abstain, counting a clarifying question (N, NC).

## Decision

- Each live arm against S on its primary family, exact McNemar on applied, Holm across the arms compared, α = 0.05.
- **Guardrails,** probes lost against S at most: N 3 of 60, K 2 of 40, C1 3 of 30, E 3 of 30, NC 2 of 30, KU 2 of 30.
- IC2 and IC3 are reported with their pair counts and p, and gate nothing.
- An arm is valid when significant with its guardrails held. Among valid arms, the one with the fewest net production lines is named, as its README states them before the live stage (`score.py` `NET_LINES`). If none is valid and every control passed, the shipped structure stands.

## Cost rules

- MiniMax only, through the one proxy, on the subscription; nothing is bought. The key is shared with #888, so at most two requests at a time per phase, and one paid phase at a time from this worker.
- On HTTP 429 with code 2056 the proxy pauses every request and probes at most every 30 minutes; callers wait on `/budget`.
- The proxy's ceiling is a list-price guard of USD 30.
- Tokens are reported per phase (`score.py tokens`).
