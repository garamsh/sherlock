# Attributed answers on a fresh corpus: preregistration

Fixes issue #916's run before its first audit label, its first score and its first model call. The run asks whether L-attr, #896's entity walk with attributed walk episodes, beats the shipped recall (S) on chains while holding the guardrails, when an absent query is scored by the owner's answer rule of 2026-09-28. It reuses #890's generator, #874's harness and proxy, both arms' commands, and #890's and #896's scorers, by import or as settings, and copies no reviewed code.

## Question

On a fresh corpus, with the model acting, does L-attr apply more primary chains (C2 and C3) than S, significantly, while every guardrail of #885 holds, where N and NC count an answer acceptable under the owner's rule?

## The owner's rule

As the issue states it: on an absent (N or NC) probe, an answer is acceptable when it either abstains, or gives the associate's value while attributing it to the associate. That means naming whose value it is, and not asserting it as the queried entity's own. A bare assertion of the associate's value as the queried entity's remains a failure. Every other family keeps its existing rule.

- **The reading fixed here (A).** The PM decided it on the issue's dispatch channel on 2026-09-28, relaying the owner: the owner approved the rule on #896's N-002 reply itself, "Vuner Coaches share a depot with Beanul Coaches, which picks up from bay 42. So you should wait at bay 42 when Vuner Coaches arrive for you." So a conclusion or recommendation that follows a stated attribution is acceptable, and the failure is the unattributed assertion.
- **Reading B, reported beside it as a sensitivity figure and never a decision input.** As A, except that an unhedged sentence tying the value to the queried entity without naming the associate fails the reply, even as a recommendation. N-002 fails B.
- **What was seen before this file.** Before fixing the matcher, the worker read the 22 unabstained L-attr N replies of #896 that the issue's count rests on, and the one unabstained NC reply of #896's L. No other recorded reply was read, and no matcher output on any recorded reply exists before the audit labels are committed.

## Matcher

`matcher.py` `judge`, as committed with this file, for the reply as #885's abstention check reads it: the messages sent, or the reply when none was sent.

- **Text.** HTML tags, emphasis marks, backticks and list markers are removed.
  - Sentences split after `.`, `!` or `?` followed by white space, and at line breaks.
  - Clauses split a sentence at `,`, `;`, `:`, brackets, an em or en dash, and a spaced hyphen.
- **The value** is the item's near-miss value, matched as #874 matches values (`states`: every token with a digit and every capitalised word).
- **Mentions.**
  - A person is mentioned by the full name, the surname, or the given name when no capitalised word follows it.
  - A firm (`… Coaches`, `… Catering`) is mentioned by its full name or its first word.
  - A word both of an item's names share mentions neither alone.
  - NC's associate is unnamed in the corpus, so it is mentioned by its role in #890's template for the domain, or by `previous` or `former`:
    - procurement `predecessor`;
    - travel `last year`;
    - lab `supervisor`;
    - events `dropped`;
    - software `manager`;
    - household `next door`.
- **Attributed.** Some sentence holds the value and a mention of the associate.
- **Asserted as the entity's own.** Some clause states the value with the queried entity as its subject. The clause:
  - holds the value and a mention of the queried entity;
  - names no associate;
  - has the entity's first mention before the value;
  - has no `you` (`you'd`, `you'll`, `you're`, `you've`) before that mention, since then the person addressed is its subject.

  A hedged clause does not count as asserted when it comes in a sentence after the first attributing one. A hedge is one of `likely`, `probably`, `presumably`, `possibly`, `perhaps`, `maybe`, `might`, `may`, `could`, `chance`, `guess`, `suspect`, `assum…`, `expect…`.
- **Readings.**
  - Strict: abstained by #885's pattern.
  - A, the owner's rule: abstained, or attributed and not asserted.
  - B: as A, and no sentence holds the value and the queried entity without the associate and without a hedge.

## Audit

The matcher author labels both audits by hand, from each sample alone, blind to the matcher's output. The PM approved this plan on the dispatch channel, and the report states it.

- **Label.** `acceptable` or `failure`, by the owner's rule under reading A as §The owner's rule states it, with a one-line note.
- **Audit 1, before the exploratory re-read and before any paid call.**
  - **Population.** Every reply to an N or NC probe in the live records of #885, #888 and #896 that #885's pattern does not count as abstaining. That is 177 replies over 14 arm records, smoke runs excluded (`audit.py` `EARLIER`).
  - **Sample.** 80 of them, drawn by `audit.py draw 1` with `random.Random(917)`.
  - **Order.** The labels (`evidence/audit/labels-1.jsonl`) are committed before `audit.py check 1` first runs.
- **Audit 2, before live scoring.**
  - **Population.** Every unabstained reply to an N or NC probe in this study's live records of S and L-attr.
  - **Sample.** All of them if 40 or fewer, else 40 drawn by `audit.py draw 2` with `random.Random(918)`.
  - **Order.** The draw uses only #885's abstention pattern. No endpoint and no matcher output of the live records is computed before the labels are committed.
- **Pass mark, each audit.**
  - The matcher's reading A agrees with the label on at least 90% of the sample.
  - It accepts a reply labelled a failure on at most 5% of the sample: 4 of 80, or 2 of 40.
- **If audit 1 fails.** The matcher may be revised once, before any live scoring, from audit 1's disagreements. The revision is committed as an amendment. It is then checked on 40 further replies drawn from the unlabelled rest of audit 1's population with `random.Random(919)` (`draw 1b`), labelled first, at the same pass mark. If that fails too, the owner's-rule endpoint is undetermined, and the strict endpoint is reported as the only one.
- **If audit 2 fails**, the owner's-rule verdict is undetermined (§Controls).

## Exploratory re-read

Free, and labelled exploratory: it informs and decides nothing. Run only after audit 1 is checked. `reread.py` applies the matcher to every N and NC reply in the 14 arm records of `audit.py` `EARLIER`. Per arm it reports N and NC acceptable under the strict rule, reading A and reading B, with the counts attributed and asserted. These replies include data the issue's motivation was drawn from, so they prove nothing about L-attr.

## Arms

Both run at batch 4 over the shared store `harness build -batch 4` builds from this study's recorded stream summaries and the replication's base summaries. Every command takes `-dir research/memory-encoding/attributed`, so every record lands in this directory and none under another study.

| Arm | Command and setting | Net production lines, before tests |
|---|---|---:|
| S | `research/memory-encoding/validation/arms/shipped` `b4` | 0 |
| L-attr | `research/chained-recall/arms/links` `b4-model-attr` | about +285 (#896) |

- Neither arm's code changes.
- **Common to both,** as #890 and #896 fix it:
  - a live window of 32 messages and an empty note;
  - each probe one turn on its own copy of the arm's build;
  - MiniMax-M2 through the study proxy, with no arm instruction.

## Corpus

`corpus.py assemble` runs #890's generator (`../validation/corpus.py`, imported unchanged) with the seed and output directory changed. Everything else is the generator's: families, pool and kept sizes, templates, gap rule, stream layout and base.

- **Seed 916.** The generator draws the items with `random.Random(916)` and schedules the stream with `random.Random(917)`. A dry run into a scratch directory produced 4,128 stream messages, 1,032 batches of four.
- **Families and roles.**

  | Family | Kept | Role here |
  |---|---:|---|
  | C2, C3 | 50 each | primary |
  | N | 60 | guard, scored by the owner's rule |
  | NC | 30 | guard, scored by the owner's rule |
  | K | 40 | guard |
  | KU | 30 | guard |
  | C1 | 30 | guard |
  | E | 30 | guard |
  | IC2, IC3 | 25 each | reported only |
  | SC | 50 | reported only |
  | UP | 50 | reported only |

- **Selection, as #890.**
  - `harness check` over the candidates, copied to `evidence/selection/check-b4.json`.
  - Then `corpus.py select`, which keeps the first passing candidates of each checked family. A family with too few passing candidates stops the run.
- **Fresh.** `corpus.py fresh` compares each selected probe's query with every query of the corpora #874, #885, #888 and #890 ran.
  - No generated probe's query may occur there (§Controls).
  - E and K are drawn by the new seed from the replication's fixed pool of 70 each, as every earlier study drew them. Their overlap with earlier draws is reported and does not gate.
  - Invented names shared with earlier corpora are reported.

## Paid phases

MiniMax-M2 at `https://api.minimax.io/v1/chat/completions` only, through one proxy: `harness proxy` on a free local port, with its ledger in `evidence/ledger/`.

- **Key and purchases.**
  - `MINIMAX_KEY` is read from the repository root's `.env`, exported only into the proxy's environment, and never printed or committed.
  - The key is on a subscription, and nothing is bought. The proxy's list-price ceiling is USD 15, a guard only.
  - No other paid API is called.
- **Embeddings** come from a local text-embeddings-inference container, `--memory 1g`, started on a free local port for each phase that embeds and stopped when idle. It serves `BAAI/bge-base-en-v1.5` at revision `a5beb1e3e68b9ab74eb54cfd186867f64f240e1a`, from the image digest #890 and #896 name.
- **Before the first paid call** the worker sends the PM the token estimate below and waits for a go.

| Phase | Ledger arm/phase | Calls | Estimate, input / output tokens | Basis |
|---|---|---:|---|---|
| Key probe | preflight | 1 | negligible | |
| 1. Stream synthesis (`harness synthesize -batch 4`) | `shared/synth-b4` | about 1,050 | 0.34M / 0.21M | #890's phase: 337,684 / 211,993 |
| 2. L's entity call (`arms/links build -setting b4-model-attr`) | `L/entities-b4` | about 1,060 | 0.40M / 0.25M | #890's phase: 402,558 / 244,653 |
| 3. Live, S: 470 probes | `S/live` | about 1,240 | 5.6M / 0.30M | #896's S: 4,989,761 / 268,967 over 420 probes |
| 4. Live, L-attr: 470 probes | `L/live` | about 1,160 | 5.3M / 0.30M | #896's L-attr: 4,697,297 / 269,911 over 420 probes |
| **Total** | | **about 3,500** | **11.6M / 1.06M** | about USD 4.5 at list prices |

- **Phase 2's scope.** It reuses #890's 1,200 base rows of `entities-b4.jsonl` (#874's recording, including its failed batch 509–512), copied unchanged into `arms/links/evidence/entities-b4.jsonl` before the phase. So only the 1,032 stream batches are sent.
- **Pacing.** Two requests at a time, one phase at a time. S goes live before L-attr, never together.
- **Usage limit.** On HTTP 429 with code 2056 the proxy pauses every request and probes the key at most every 30 minutes. Callers wait, and a pause uses up no attempt. Nothing is bought to lift it.
- **Re-runs.** A live probe that fails on the provider's limits, a server fault or the network runs once more, and its first record is kept in `results-attempt1.jsonl`. Any other failure counts as not applied.
- **Tokens** are reported per phase from the ledger (`score.py tokens`).
- **No smoke run.** Neither arm's mechanism changed since #896 ran both live.

## Offline stage and admission

Both arms record `study.Offline` into `evidence/offline/<arm>/<setting>/`, and `score.py offline` scores them with #896's matching (`../structural/score.py`): chain reach, pointer reach (none here), and the N and NC near-miss measures.

L-attr goes live when all of these hold. S goes live only if L-attr is admitted:
- its controls of §Controls that have an offline form passed;
- on C2 and C3 it reaches at least 10 of 100 chains more than S does;
- the exact McNemar test against S gives p < 0.05.

## Endpoints

Scored by `score.py live` as committed with this file.

- **Applied**, as #885, #890 and #896, for every family but N and NC:
  - the chain's, SC's, UP's or C1's answer, or E's value, stated in the reply, messages sent or files written;
  - K and KU: the corrected value.
- **N and NC, the owner's rule.** Applied when the matcher's reading A accepts the reply.
- **Beside them, reported:**
  - N and NC under the strict rule (#885's abstention pattern) and under reading B;
  - per arm, the N and NC replies attributed, asserted as the entity's own, and stating the near-miss value;
  - KU stating the obsolete name;
  - every hop seen;
  - searches and inspects per probe;
  - IC2, IC3, SC and UP applied.

## Comparison and decision

#885's decision rule and guardrails, with #890's guard families.

- **Test.** L-attr against S on C2 and C3: the exact two-sided McNemar test on applied, at α = 0.05. One arm is compared, so there is no multiplicity correction.
- **Guardrails.** L-attr may lose against S at most:
  - N 3 of 60 and NC 2 of 30, both arms scored by the owner's rule;
  - K 2 of 40 and KU 2 of 30;
  - C1 3 of 30 and E 3 of 30.
- **Valid.** L-attr is valid when:
  - its difference is positive and significant;
  - every guardrail holds;
  - every control passed.

  IC2, IC3, SC and UP never gate.
- **The strict rule's verdict** is computed the same way, with N and NC scored by #885's pattern, and reported beside it. It decides nothing.
- **Null.** If L-attr is not valid and every control passed, the shipped recall stands.
- **If L-attr is valid,** the report states the production change it implies, with a line estimate per part, so the PM can open the implementation issue. The parts are entity extraction at synthesis, the entity links, the walk, the attribution and recall.

## Controls

A failed control makes the verdict undetermined, and the report names it.

- **Corpus, #890's checks.**
  - The selection check keeps every checked family at its size.
  - On the selected corpus, each hop's own statement delivers its mid within 5 for at least 95% of hops (`score.py check` over the re-run `harness check`).
- **Fresh.** No generated probe's query occurs in the corpora #874, #885, #888 or #890 ran (`evidence/fresh.json`).
- **Entities stored.** At least 90% of mids hold at least one entity (L's control, `built.json`).
- **Oracle, each arm offline.** Each hop's own statement delivers the hop's mid within the top 5 for at least 95% of hops.
- **S and L-attr share the store** (`score.py equivalence`, `evidence/equivalence.json`).
  - Both builds hold the same mids: the same IDs, summaries, source intervals and vectors.
  - Every offline probe whose L-attr trace has no seed delivers the same items in both arms, as the walk's design requires.
- **Attribution presence, as #896 preregistered it**, with its Amendment 1 form for NC, offline and live:
  - every walk item whose subject is none of the query's entities opens with its attribution, and no other walk item does;
  - where the walk delivers an N item's associated-value episode, the attribution names the associated entity as subject and the queried one as named, for at least 90% of such items;
  - every attributed NC walk item names a subject other than the queried entity, and every NC walk item about the queried entity is shown plain.
- **The matcher's audits**, 1 and 2, at their pass mark (§Audit). They gate the owner's rule and reading B, which rest on the matcher, and not the strict rule, which does not.
- **Mutation checks.** `mutation.py` removes each behaviour this study adds and records that a named test fails (`evidence/mutation.txt`), before the first paid call:
  - the matcher's attribution requirement, its subject rule, the address exception, the hedge exception and its order condition;
  - the clause split, the name mentions and NC's roles;
  - reading B's tie;
  - the audit's pass mark;
  - the scorer's owner's-rule scoring of N and NC;
  - the admission margin and the equivalence check.

## Collection

1. This file, `matcher.py`, `audit.py`, `corpus.py`, `score.py`, `reread.py`, their tests and `mutation.py`, committed.
2. `audit.py draw 1`; the labels, committed; `audit.py check 1`; `reread.py`.
3. `corpus.py assemble`. The TEI container. The mutation record.
4. The token estimate to the PM, and the PM's go.
5. The proxy with its preflight probe; phase 1; `harness build`; `harness check`; `corpus.py select`; `harness check` again; `score.py check`; `corpus.py fresh`.
6. The base entity rows copied; phase 2 (the L-attr build); the S build; offline records of both; `score.py equivalence`; `score.py offline`: the admission.
7. Live, if admitted: S, then L-attr, over every probe of the corpus.
8. `audit.py draw 2`; the labels, committed; `audit.py check 2`.
9. `score.py live` and `score.py tokens`. The report.

Every probe's store copy is kept outside the repository. Nothing is written under another study's directory.

## Revision

- Harness, arms, generator and scorers at the commit that adds this file.
- SHA-256 of `../validation/corpus.py`: `8a28691e0c192f3e28f493b235068e635cce7cfa742a4b954fe38c4bc3459eef`.

## Amendments

- **1. The matcher, revised once after audit 1 failed.** Made after audit 1's labels were committed (`54af268`) and checked, before the exploratory re-read, any corpus or any paid call, as §Audit allows.
  - **Audit 1 as preregistered.** 68 of 80 agree, below the 72 required. 2 false accepts, within the 4 allowed (`evidence/audit/audit-1.json`). So it failed.
  - **The twelve disagreements.**
    - Six are replies that decline or ask which one is meant, in words #885's pattern does not hold: "I'm not familiar with … could you give me more context?", "is that a typo", "are these the same person", "Which one are you referring to?".
    - Two name the associate with a surname misspelt by one letter ("Trelind" for "Trealind").
    - Two give the value and name its holder in the next sentence: "look for locker 593. That's where Dmitri Teapou leaves the keys".
    - Two are false accepts. Each misstates the link as a fact about the queried entity: "that's where Pavel Baigam leaves the keys for finished jobs from Mara Kudas". No mechanical rule found for them generalises, and they are within the allowance, so they are left as the matcher's known error.
  - **The revision.**
    - **Declining.** The owner's rule and reading B also count as abstaining a reply that holds `not familiar with`, `give`, `share` or `tell me more context` or `details`, `let me know more details`, `which one are you referring to`, `are these`, `they` or `you the same` or `referring`, `is that a typo`, or `nothing (directly) recorded for`. The strict rule stays #885's pattern exactly.
    - **Pointing back.** A sentence holding the value is attributing also when the next sentence opens with `that's`, `this is`, `it's`, `that is` or `that was` (and the like) and names the associate.
    - **Misspelling.** A firm's first word and a person's surname, of at least five letters, are also mentioned when misspelt by one letter: one inserted, deleted or substituted.
  - **Its check, as §Audit fixes it.**
    - 40 further replies from audit 1's 97 unlabelled replies, drawn by `audit.py draw 1b` with `random.Random(919)`.
    - Labelled and committed before the revised matcher first runs on them.
    - Passed when at least 36 agree and at most 2 are false accepts.
  - **Nothing else changes.** No other threshold, endpoint, control or rule moves. The revised matcher's agreement on audit 1's own sample is in-sample, is reported, and gates nothing.
