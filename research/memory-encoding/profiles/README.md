# Entity profile cards

Records issue #906's study: whether per-entity profile cards give the association gains without the abstention and correction losses of the read-time association arms. A card is short, owned by one entity and overwritten on correction. It is attached through the entities of the episodes recall already delivered. The study ran only its free screen, over #890's recorded data (`../validation/`), and fixed it in `preregistration.md`.

- **The gate passed on reach, and the paid phase was cut anyway.** An attached card states the gold answer for 34 of 100 primary chains, all of them C2. That clears the 10-of-100 margin. S states none of those answers.
- **Two findings cut it** (`preregistration.md` §Amendments, 1):
  - **The overwrite control failed**, 23 of 30 against the 27 required. The mechanical attribute key replaces facts across attributes, so a live run would have measured a broken card.
  - **An associate's value is in view on 36 of 60 absent queries**, on the associate's own correctly owned card. #896 showed this exposure costs abstention even when the owner is named.
- **UP reaches 0 of 50.** The user's card shows 4 facts under the 400-character bound. The unbounded variant reaches 48 of 50.
- **No model call and no embedding were made.** The shipped recall stands.

## Question

If recall attached the cards of the entities its delivered episodes name, how often would an attached card state the gold value? What would it bring into view on corrections and absent queries? Does the reach ceiling clear #890's margin: 10 of 100 chains, or 5 of 50 UP items?

## Method

- **Inputs,** all recorded, at `f6bedf3`:
  - F's facts per stream batch (`../validation/arms/facts/evidence/facts-b4.jsonl`: 1,032 batches, 1,739 facts);
  - L's entities per mid (`../validation/arms/links/evidence/entities-b4.jsonl`: all 2,232 mids);
  - S's delivered items per probe (`../validation/evidence/offline/S/b4/probes.jsonl`);
  - #890's corpus.

  The issue named `../../chained-recall/arms/links/evidence/entities-b4.jsonl`, but that file is #874's corpus. The file used is the one for #890's corpus (`preregistration.md` §Recorded inputs).
- **Cards** (`screen.py`, `preregistration.md` §Card construction):
  - **Owner.** Each fact's owner is the entity of its own mid that the fact names earliest, or the user when the user is named first. An unnamed `user` fact goes to the user. An unnamed `other` fact is dropped (17 of 1,739).
  - **Overwrite.** A newer fact replaces an older fact on the same card when their attribute keys overlap by half the smaller key. A key is the content words left after removing names, values, stop words and bookkeeping words.
  - **Bound.** A card shows its newest facts, whole, within 400 characters.
- **Attach.** For each probe, the cards of every entity of every delivered mid, and the user's card, each marked with its owner. The query's text is never read.
- **Looser ceilings,** for context:
  - *unbounded*: the same owners, with no overwrite and no bound;
  - *any mention*: every fact naming a delivered entity anywhere.
- **Controls:**
  - a hand audit of 50 seeded owners (`evidence/owner-audit.jsonl`), drawn and labelled before scoring. It passed with 49 correct; the wrong one is "Pick-ups are made from bay 55.", owned by its value;
  - the KU overwrite control;
  - 29 mutation checks (`evidence/mutation.txt`), all failing a named test.
- **Order.** Preregistration at `b178d89`, audit at `39bec08`, scores at `da3a915`.

## Reach

`evidence/screen.json`. Probes where an attached card states the gold answer.

| Family (n) | Cards as preregistered | Unbounded | Any mention | S's items state it |
|---|---:|---:|---:|---:|
| C2 (50) | 34 | 36 | 36 | 0 |
| C3 (50) | 0 | 7 | 7 | 0 |
| IC2 (25) | 1 | 10 | 12 | 0 |
| IC3 (25) | 0 | 10 | 10 | 0 |
| UP (50) | 0 | 48 | 48 | 0 |

- **C2 works as designed.** An example is C2-006. S delivers the first hop's mid, whose entities include Ravi Requaind. Ravi Requaind's card attaches, and it states "leaves keys of finished jobs in locker 228". S reached no C2 chain (#890).
- **C3 does not.** The attach is one level. The answer sits on the card of the third hop's entity, which a delivered mid rarely names. The unbounded cards reach 7, so overwrite and the bound remove those.
- **UP fails on the user's card.** The user owns 920 facts. Overwrite leaves 162, and the bound shows the newest 4. This is F's failure again (#890: the ten newest user facts, UP 0 of 50), with a smaller window.
- **Counts.** 713 cards, 1.11 facts shown per card on average, 772 replacements, 158 facts cut by the bound.

## Abstention risk

| Family (n) | Queried entity's own card states the near-miss, attached | Stored, attached or not | Near-miss on any attached card |
|---|---:|---:|---:|
| N (60) | 0 | 0 | 36 |
| NC (30) | 0 | 0 | 0 |

- **Owner confusion is 0.** No card of a queried absent entity carries the associate's value.
- **The associate's value is in view on 36 of 60 N queries.**
  - N-003 asks for Ravi Dezeand's room. The delivered mid "Ravi Dezeand trained under Sofia Cesairk" attaches Sofia Cesairk's card: "works out of room 838".
  - The card is correctly owned. But #896's L-attr named the subject of associated text and still abstained on 38 of 60 N queries against S's 59, stating the associated value on 48 (`../structural/README.md`).
  - Owner marking is the same safeguard, so this exposure predicts the same loss.

## Corrections

| KU (30) | Count |
|---|---:|
| An attached card states the current value | 7 |
| An attached card states the superseded value | 9 |
| Stale: an attached fact states the superseded value and not the current | 7 |
| The first statement's fact was replaced by overwrite (control, 27 needed) | 23 |

The control failed. The seven items where the first statement was not replaced:
- **Different owners, 4** (KU-013, KU-014, KU-021, KU-024). The first fact is owned by the old holder ("Aiko Haistas is the user's letting agent.") and the correction by the user.
- **Keys that do not overlap, 3.**
  - KU-002: the correction's key is empty, because "doctor" is one of its mid's entity names.
  - KU-007: "vet" against "veterinarian".
  - KU-028: both first facts have empty keys.

Where overwrite did fire, it also fired across attributes. Most corrections sit on the user's card, with generic words such as "replacing", "previous", "named" and "changed" in their keys:
- In 15 items a correction fact was later replaced by a fact that does not name the item's role. For example, "The user changed their dentist from Nadia Bogea to Sofia Cukais." was replaced by "The user's babysitter is Pavel Watas, having changed from Dmitri Tazaind."
- In KU-023 a therapist correction was replaced by a physiotherapist correction.
- In KU-004 and KU-030 the current holder's fact was replaced by the fact naming the previous holder. That fact was then replaced across attributes.

The basis is the KU listing command under §Reproduction.

## Gate

- **Result,** from `evidence/screen.json` `gate`:
  - C2 and C3 together reach 34 of 100, against a margin of 10;
  - UP reaches 0 of 50, against 5;
  - the owner audit passed;
  - so the preregistered rule allowed the paid phase to be proposed.
- **Proposed:** a live phase with no synthesis call. It would have run 420 probes of #890's families with the cards attached, against S, under #885's decision rule, at about 5.4M input and 0.27M output tokens (from #896's S live ledger: 1,110 calls, 4.99M and 0.27M).
- **Cut by the PM** before any call, for the failed overwrite control and the predicted N guardrail loss (`preregistration.md` §Amendments, 1).
- **Cost:** no MiniMax call, no other paid call, no TEI container. `python3 screen.py score` took 75 seconds on this host.
- **Size:** cards add 964.0 characters per probe on average (median 947, maximum 1,395) over the 320 scored probes, against S's 2,763.4 delivered characters.

## Limits

- **A screen, not a run.** A card stating the answer bounds reach from above. It does not show a live agent would use the card, or would not answer from it on an absent query.
- **The key is mechanical.** A working card needs an attribute key that never overwrites across attributes, which means a key the synthesis call writes, and a correction whose owner is the same card as the statement it corrects. The screen cannot say what such a key would reach.
- **The bound drops old user facts.** A working user card needs a bound that keeps old standing facts, not only the newest. Unbounded, UP reaches 48 of 50, but the user's card would then hold 162 facts after overwrite.
- **The facts are F's.** F wrote at most 5 facts per batch and none for the base. A card writer with another prompt would hold other facts.
- **One level of attach.** C3 needs the card of an entity no delivered mid names. Following cards from cards would be the walk again, with its abstention cost.
- **A templated corpus.** Hops are single sentences about invented names, so the facts restate them almost verbatim.

## Reproduction

- `preregistration.md`: the construction, attach rule, measures, gate and controls, fixed before scoring. Amendment 1 records the cut.
- `screen.py`: `sample` draws the owner audit, and `score` writes the evidence. `screen_test.py` tests it on synthetic records.
- `mutation.py`: removes each rule, one at a time. `evidence/mutation.txt` records that a named test fails for each of the 29.
- `evidence/`:
  - `screen.json`: per-family measures, the gate, the controls and the counts;
  - `probes.jsonl`: per probe, the attached owners and every measure;
  - `cards.jsonl`: every final card;
  - `owner-audit.jsonl`: the labelled sample.

From the repository root, with Python 3 and nothing else:

```sh
D=docs/research/memory-encoding/profiles
python3 $D/screen.py score
(cd $D && PYTHONDONTWRITEBYTECODE=1 python3 -m unittest screen_test)
python3 $D/mutation.py
```

The KU listing behind §Corrections. For each KU item it prints the facts of the batches holding `sources`, and which replacements removed them:

```sh
cd docs/research/memory-encoding/profiles && PYTHONDONTWRITEBYTECODE=1 python3 -c '
import screen as S
r, e, _, p = S.inputs()
_, rep, _ = S.build(r, e)
for g in (x for x in p if x["family"] == "KU"):
    print(g["id"], g["role"], S.ku_overwrite(g, rep, r))
    for o, n in rep:
        if any(o["first"] <= s <= o["last"] for s in g["sources"]):
            print("   ", o["text"], "->", n["text"])'
```
