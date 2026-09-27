# Entity profile cards: preregistration

Fixes issue #906's free screen before it is scored. The screen asks whether per-entity profile cards could give association gains without the abstention and correction losses of the read-time association arms. A card is short, owned by one entity and overwritten on correction. It is attached through the entities of the episodes recall already delivered, never through the query's words. The screen builds cards from #890's recorded data and attaches them to S's recorded deliveries (`../validation/`), with no model call and no embedding.

## Question

If recall attached the cards of the entities its delivered episodes name, how often would an attached card state the gold value? Measured on the chain and user-profile families, with the correction and owner-confusion risks beside it. Does that ceiling clear the margin #890 used, so that a paid phase is worth proposing?

## Recorded inputs

All read, none written. Every path is under `../validation/`, at `f6bedf3`.

| Input | Path | What it holds |
|---|---|---|
| Facts | `arms/facts/evidence/facts-b4.jsonl` | F's facts per stream batch: 1,032 batches, 1,739 facts, each a `kind` (`user` or `other`) and a `text` |
| Entities | `arms/links/evidence/entities-b4.jsonl` | L's entity names per mid, for all 2,232 mids of the batch-4 store, base and stream |
| Deliveries | `evidence/offline/S/b4/probes.jsonl` | S's delivered items per probe, 470 probes, 7 mids each |
| Corpus | `corpus.json` | #890's families, gold values, KU and N/NC fields |

- **Entities path.** The issue names `../../chained-recall/arms/links/evidence/entities-b4.jsonl`. That file is #874's corpus: 1,624 mids over sources 1 to 6,496. #890's corpus has 2,232 mids over sources 1 to 8,928. The recording for it is `../validation/arms/links/evidence/entities-b4.jsonl`, which L wrote as #890's reference arm. It is byte-identical to `../structural/arms/links/evidence/entities-b4.jsonl`, and the screen uses it.
- **No facts for the base.** F synthesised only the stream (`../validation/arms/facts/README.md`), so every card comes from stream facts. Base mids still attach cards through their entities.
- **What was looked at before this file was written:**
  - the record formats and first lines of each input;
  - counts: facts per kind (1,013 `user`), mean fact length (66.1 characters), entities per mid (3.43; one record is null), mids whose entities include "user" (136), and S's delivered characters per probe (2,943.1 mean);
  - F's published controls (`../validation/arms/facts/evidence/controls.json`).

  No card was built and no measure below was computed.

## Card construction

Implemented in `screen.py` and fixed here.

- **Names.**
  - An entity's name is lowercased, its whitespace collapsed, and a leading "the " dropped.
  - A name matches a text where it occurs with no letter or digit on either side, case-insensitively.
  - Names shorter than 3 characters are ignored.
- **Participants.** `user`, `users`, `assistant`, `me`, `i` and `you` are participants, not entities. They never own a card through the entity list.
- **Owner.** Each fact has one owner or none:
  - the candidates are the non-participant entities of the fact's own mid (same source interval) that the fact names, and the user, where the text names `user`, `user's`, `i`, `me` or `my`;
  - the candidate named earliest in the text owns the fact, the longer name winning a tie;
  - a fact naming no candidate is owned by the user if its kind is `user`, and has no owner otherwise;
  - a fact with no owner is dropped and counted.
- **Attribute key.** The set of content words that remains of the fact's text after removing:
  - every entity name of the fact's mid, and the user words;
  - every token holding a digit, and every capitalized token except the first that remains;
  - stop words and bookkeeping words (`screen.py` `STOP`, `BOOKKEEPING`).
- **Overwrite.** Facts join their owner's card in stream order.
  - A newer fact replaces every older fact on the same card whose key overlaps its own by at least half of the smaller key: |A ∩ B| / min(|A|, |B|) ≥ 0.5.
  - An empty key never overlaps.
  - Each replacement is logged.
- **Bound.** A card keeps its newest facts, whole, while their texts joined by "; " fit in 400 characters. That is about five of F's facts. The facts are shown in stream order.
- **Rendering.** `<owner>: <fact>; <fact>`. The owner is the entity's name as first recorded, or "The user". The rendered characters are the size measure.
- **Time.** Every probe is asked after the whole stream, so every probe sees the final cards.

## Attach rule

- For each probe, S's recorded delivered items, in delivered order.
- For each item, the entities of the mid with the same source interval. The cards of the non-participant entities are attached, each once, in first-named order.
- **The user's card** is attached on every probe that delivered at least one episode. The user takes part in every episode, and 136 of 2,232 entity lists name "user", so tying the card to that word would attach it by the synthesis call's accident of naming.
- The query's text is never read.
- There is no cap on the number of cards. The ceiling is an upper bound, and size is reported.

## Measures

All computed by `python3 screen.py score` into `evidence/screen.json`. Values are matched by `states()` of `../../agent-context/score.py`, which #890 uses.

- **Reach, the ceiling.** For UP, C2, C3, IC2 and IC3: the share of probes where an attached card states the gold answer (`answer`).
  - Reported beside it: the probes where S's delivered items already state the answer.
- **KU:**
  - an attached card states the current value (`key`);
  - an attached card states the superseded value (`obsolete`);
  - stale: an attached card holds a fact stating the superseded value that does not also state the current one.
- **N and NC, owner confusion:**
  - an attached card whose owner is the queried entity (`entity`, by name) states the near-miss value;
  - that card exists in the store, attached or not;
  - any attached card states the near-miss value.
- **Size:** attached card characters per probe (mean, median, maximum) per family and over all 470 probes, against S's delivered characters (`context_chars`).
- **Looser ceilings,** reported and gating nothing. Each holds at least the facts of the one before, so each can only reach more:
  - *unbounded*: the same owners, with no overwrite and no bound;
  - *any mention*: every fact that names a non-participant entity of a delivered mid anywhere in its text, plus every fact the user owns, with no owner rule, overwrite or bound.

  If the gate fails on these as well, the cut does not rest on the owner rule, the overwrite or the bound.
- **Counts:** cards, facts per card, dropped facts, replacements, facts cut by the bound.

## Gate

The margin is #890's (`../validation/preregistration.md` §Offline stage and admission): 10 of 100 chains, or 5 of 50 UP items. S's carried and strict reach on C2, C3 and UP are 0 in #890's committed records, so a card count is also the most a card could add over S.

- **The paid phase may be proposed** when the reach ceiling on C2 and C3 together is at least 10 of 100, or on UP at least 5 of 50. Either family may admit it, because each is a primary family of its own in #890 (L's and P's chains, F's UP).
- **Otherwise no paid phase runs.** The report says why, and the PR closes #906.
- **On a pass,** the PM is sent the proposed paid phase with its token estimate, and nothing is spent before the PM's go. The paid phase is not designed here.
- **A failed owner audit** (§Controls) voids a pass. Nothing is proposed for a construction whose owners are wrong. A failed gate stands either way, and the report names the failed control.
- IC2 and IC3 are reported and never gate.

## Controls

- **Owner audit.**
  - `python3 screen.py sample` draws 50 owned facts with `random.Random(906)` into `evidence/owner-audit.jsonl`. They are drawn and labelled by hand before `score` runs.
  - A fact is labelled correct when the owner is the entity the fact is chiefly about: its grammatical subject, or the user for a fact about the user's own attribute, preference, rule or possession.
  - The audit passes at 45 of 50 or more.
- **Overwrite on KU.** For each KU item:
  - the *first fact* is a fact of the batch holding `sources[0]` that states `obsolete`;
  - the *correction fact* is a fact of the batch holding `sources[1]` that states `key`.

  The control passes when, in at least 27 of 30 items, some first fact was replaced by overwrite. The report lists the items where it was not, and why: no first fact, no correction fact, different owners, or keys that do not overlap.
- **Inputs.** Every stream batch has a facts record and an entities record with the same interval, and every corpus probe has one S delivery. If any is missing, the screen stops.
- **Mutation checks.** `mutation.py` removes each rule above, one at a time, and shows that a named test of `screen_test.py` fails. It covers:
  - the owner's earliest-named rule, its tie, the participant exclusion, the user fallback, and dropping an unowned `other` fact;
  - the key's removals and the overlap threshold;
  - the newest-first bound;
  - attaching through delivered mids only, and the user's card;
  - the reach, KU, stale and owner-confusion matchers;
  - the gate's two margins, its either-family rule, and the audit voiding a pass.

  `evidence/mutation.txt` records the result before `score` runs.

## Cost

- No model call, no embedding, no TEI container, no network. Python 3 over committed files.
- A paid phase, if the gate ever admits one, is MiniMax-M2 only, within the subscription, through one study proxy with #890's 2056 pause. No OpenAI or other paid API.

## Collection

1. This file, `screen.py`, `screen_test.py`, `mutation.py` and `evidence/mutation.txt`, committed and pushed.
2. `python3 screen.py sample`, then the hand labels, committed.
3. `python3 screen.py score`, committed with `evidence/screen.json`, `evidence/cards.jsonl` and `evidence/probes.jsonl`.
4. The report, `README.md`, and one line in `../../README.md` §Studies.

Nothing is written under `../validation/` or any other earlier study.

## Amendments

- **1. The paid phase is cut.** Made after `score` (`evidence/screen.json`, at `da3a915`) and before any paid call. The PM decided it on the dispatch channel.
  - **The gate passed on reach.** C2 and C3 together reach 34 of 100, above the 10-of-100 margin, and the owner audit passed (49 of 50).
  - **Why it is cut anyway:**
    - the overwrite control failed (23 of 30 against 27). The mechanical attribute key replaces facts across attributes, so a live run would measure a broken card rather than the structure;
    - an associate's value is attached on 36 of 60 N probes, correctly owned. That is the exposure that cost L-attr its abstention in #896 (38 of 60 against S's 59).
  - **What does not run:** the proposed live phase (about 5.4M input and 0.27M output tokens) and any decision. No other part of this file changes.
