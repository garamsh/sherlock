# Agent-context confirmatory preregistration

Fixes the arms, episodes, oracle, metrics, controls, decision rule and budget of issue #864 before its first paid call. It follows the #862 [preregistration](preregistration.md) except where this file says otherwise; that file and its evidence are unchanged.

## Question

Does candidate A — a `memory_search` call during work plus a bounded note — make the agent reach and apply the right evidence more often than the shipped recall S? #862 answered "undetermined" because its oracle control, which asked for a key word in a mid summary, failed on reworded summaries. This run defines the oracle at the source.

## Arms

| Arm | Commit | Source |
|---|---|---|
| S | `2e28064` | origin/dev when this file was committed; its production code is `4e84d2c`'s |
| A | `61f4e4c` | branch `sauce-git/feat/agent-context-arm-a` |

B and candidate C are not run.

## Harness, model and embedder

As #862's preregistration §Model, embedder and proxy, with the harness at `8cb0b1b` (`harness/`). The one change is the oracle below.

- Each arm's binary is built from `git archive <commit>` with that `harness/` copied in.
- The base store is #862's, SHA-256 `a11edc446e46d0eed64c6cf244bcc0a0a82ea720eb108a223f1abecee1360250`: the replication's 4,800 messages and 1,200 replayed batch-4 syntheses.
- The proxy starts with #862's synthesis cache (`evidence/stage2/ledger/synthesis-cache.jsonl`), because the filler blocks are the same.

## Episodes

`confirm-episodes.json`, from `corpus.py confirm`: the #862 generator at seed 864 in place of 862. Families and sizes are unchanged.

| Family | n | What the new seed changes |
|---|---:|---|
| F, G | 70 each | new events, attributes, values, distractors and filler order |
| I | 70 | the generator has exactly 70 rule-task pairs, so the probes repeat #862's; the filler blocks and pads around them are drawn anew |
| K | 70 | new subjects' values and filler order |
| E | 30 | a new draw of unused replication facts; 12 of the 30 facts recur from #862 |
| N | 30 | the replication's 30 absent queries, as in #862; filler drawn anew (7 episodes coincide with #862's) |
| R | 30 | new names and filler order, 10 per case |

Every other rule of #862's §Episodes holds: seeded pairs through `RecordTurn`, a 32-message window, and every live input starting from what a restart restores.

## Oracle

For each primary episode, in each arm, after its probe, through the arm's own store:

1. **Evidence source.** The stored user messages whose content is exactly the episode's evidence statement (`oracle` in the episode file): the referent statement (F, G), the rule statement (I), the correction (K), the fact (E) or the task start (R).
2. **Ranking.** Every embedded mid ranked by the arm's own `Search` for that statement.
3. **Reached** when a mid among the top 5 has a source interval holding an evidence source.

A miss is classified from the same store:
- **Unsummarized:** no mid holds the source yet.
- **Reworded summary:** the holding mid ranks below 5 and its summary does not state the evidence key by the scorer's matching.
- **Ranking miss:** the holding mid's summary states the key and still ranks below 5.

The harness records the source IDs, the holding mid, its rank, its summary and the class (`oracle_detail`). Each episode's store is copied aside after its probe (`-keep-stores`) and kept until scoring ends, so every miss can be checked against the store. Stores are not committed.

## Metrics and guardrails

As #862's §Metrics and §Decision rule guardrails, scored by `score.py confirm`:
- *applied* is the primary endpoint;
- *reached*, *inspected*, *abstained* (N), the R cases and layer fidelity (K) are reported;
- guardrails: no primary family loses more than 7 of 70 against S, E and N each lose no more than 3 of 30, and no cancelled R case is resumed where S did not resume it.

## Controls

Required before any verdict; a failed control makes the verdict "undetermined", and the report names it:

- **Oracle reach:** at least 266 of the 280 primary episodes reached, in S and in A.
- **A searches:** `memory_search` called in at least 14 of the 70 G probes.
- **The note is written:** A's note non-empty after the I rule turn in at least 35 of 70.
- **Mutations:** every mutation in `mutation.py` breaks a test (`evidence/confirm/mutation.txt`, recorded before collection). This includes one that removes the oracle's interval check.

## Decision rule

One comparison: A against S on *applied* over the 280 paired primary probes, by the exact two-sided McNemar test at α = 0.05.

- **Valid** when the difference is positive, p < 0.05, every control passes and every guardrail holds.
- **Not valid** when the controls pass but the difference is not positive, p ≥ 0.05, or a guardrail fails.
- **Undetermined** when a control fails.

## Budget

- At most USD 8.00 in total. The proxy ledger starts empty with an 8.00 ceiling, and every attempt is reserved before HTTP.
- The projection is about USD 5.0: S and A together cost USD 4.96 in #862's stage 2 with the same synthesis cache (`evidence/stage2/scores.json` `cost.per_family_usd`).
- If the ceiling is reached, collection stops and the report names the episodes missing.

## Collection

As #862's §Collection: both arms at once, four episodes at a time each, and resumable.
- An episode that fails on a transport error, or on HTTP 429 or 5xx after six attempts, is re-run once from scratch.
- Any other failure counts as not applied.
- Nothing in the arms, harness, episodes or scorer changes between this commit and the end of scoring.

## Revision

Committed before the first paid call of this run. SHA-256 of the inputs:

- `confirm-episodes.json`: `a730863de48bbbcdbd3f5cbe1bfed16dd5154d89c5f6a8173ace987ffa4fa69e`
- `score.py`: `b359466ad07c5d8dd30ea2322baf4154c3b166e001698f2351be214ae7c3a6cd`
- `corpus.py`: `34eab91fa60042b5de4be530becc6ffc5b9f3c68bb60e423d0fe9815391d529a`
- harness binaries: S `a825c8d1330884e8c8a474f818a6f3d9c84e46d0c18a940e5b4fba4612447def`, A `1573391ab96f808c8a02cbf80a4a5f9e66eb95622177ce2733374c26ce074869`, built from S `2e28064` and A `61f4e4c` with `harness/` from `8cb0b1b`.
