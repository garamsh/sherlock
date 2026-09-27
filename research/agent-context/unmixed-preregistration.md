# Agent-context unmixed-batch preregistration

Fixes issue #870's third run of S against A before its first paid call. It follows the #864 [confirmatory preregistration](confirm-preregistration.md), Amendment 1 included, except where this file says otherwise; that file, the #862 [preregistration](preregistration.md) and all earlier evidence are unchanged.

## Question

Does candidate A (a `memory_search` call during work plus a bounded note) make the agent reach and apply the right evidence more often than the shipped recall S, on a corpus where the oracle control can pass or fail on ranking alone? #864 was undetermined because every oracle miss was a mid that mixed the evidence with a neighbouring exchange.

## Arms

| Arm | Commit | Source |
|---|---|---|
| S | `d045c0f` | origin/dev when this file was committed; its production code is `4e84d2c`'s |
| A | `61f4e4c` | branch `sauce-git/feat/agent-context-arm-a`; draft PR #867 carries it with review fixes that change nothing a probe reads |

The harness is `aa56020`. The base store is #862's (SHA-256 `a11edc446e46d0eed64c6cf244bcc0a0a82ea720eb108a223f1abecee1360250`), and the proxy starts with #864's synthesis cache. The model, embedder and proxy rules are #862's.

## Episodes and the unmixed-batch rule

`unmixed-episodes.json`, from `corpus.py unmixed`: the #864 generator at seed 870 in place of 864, with the families and sizes of #864 (F, G, I, K 70 each; E, N, R 30 each). One rule is added: **every evidence statement's exchange fills one four-message batch alone.**

- **Seeded evidence** (F, G, E). The statement pair is followed by a same-topic pair, for example "That's the one detail about X I need you to hold." and its answer. The pair of pairs starts on a batch boundary.
  - G's 21 distractor pairs gain the "thanks" pair F already had, and the evidence block is inserted at an even pair index, so alignment holds.
- **Live evidence** (I's rule, K's correction, R's task start). The statement is live, and the turn stores the user message, any `message_send` text and the reply.
  - An `align` step then seeds as many same-topic messages as close the batch (for example "Please apply that to everything from here on.", its answer and "Thanks."). The next statement therefore opens a batch of its own, however many messages the turn stored.
  - Every other non-probe live turn (K's original statement, R's change or cancellation) is closed the same way. These align steps replace the filler pad pairs that followed live turns in #864.
- **Nothing else changes.**
- **Deterministic checks.**
  - Before collection, `corpus_test.py` walks every episode. It checks that each seeded statement opens a batch whose other pair is its declared follow-up, and that each live statement opens a batch and is followed by an align step. Its control is that the same walk finds #864's live statements unaligned.
  - At run time, the harness checks each primary episode's stored evidence (`harness/run.go` `unmixedBatch`, recorded as `oracle_detail.unmixed`). A mid must span exactly the evidence source and the three messages after it, and each of those must be an assistant message or one of the episode's declared follow-ups.
  - A free smoke run against a stand-in upstream found every sampled family unmixed in both arms before this commit.

## Oracle, metrics, guardrails and decision rule

As #864:
- the source-level oracle, a top-five mid holding the evidence source, required at 266 of 280 in each arm, with every miss classified from the retained store;
- *applied* as the primary endpoint;
- the guardrails;
- one comparison, A against S, by the exact two-sided McNemar test at α = 0.05, scored by `score.py confirm`.

## Controls

Every #864 control holds, and one is added:
- **Oracle reach:** at least 266 of 280 in S and in A.
- **A searches:** `memory_search` called in at least 14 of 70 G probes.
- **The note is written:** A's note non-empty after the I rule turn in at least 35 of 70.
- **Mutations:** all 13 in `mutation.py` break a test (`evidence/unmixed/mutation.txt`, recorded before collection).
- **Unmixed batches:** every primary episode whose evidence was stored has an unmixed evidence batch, in each arm (`unmixed_batches_S`, `unmixed_batches_A`).

A failed control makes the verdict "undetermined", and the report names it.

## Budget and collection

- **Budget.** At most USD 8.00. The ledger starts empty with that ceiling, and every attempt is reserved before HTTP. The projection is about USD 5.2, #864's spend for the same two arms.
- **Workers.** Two episodes at a time per arm.
- **Re-runs, adopted up front from #864's Amendment 1.** An episode whose seeding or turn fails on HTTP 429, 5xx or a transport error is re-run from scratch, as often as the key allows, and scored once from its completed run. Its failed record moves to `results-attempt1.jsonl`, and the report gives the re-run count per arm. Any other failure counts as not applied.
- **Quota.** Before collection one minimal ledgered request probes the key. If it, or collection, meets the usage quota (HTTP 429, code 2056), the key is probed every 15 minutes for at most two hours. If no probe succeeds, collection stops, the run is reported incomplete, and the verdict is undetermined.
- Nothing in the arms, harness, episodes or scorer changes between this commit and the end of scoring.

## Revision

SHA-256 of the inputs:

- `unmixed-episodes.json`: `c2775d3bdc8b64e06e5d0a4d3c67da135170b9700feb55b53e653d63e687a737`
- `score.py`: `8e93a88b38f38f3c87f098607310ac66859d89d4f8b664707fdfe79219aa56d2`
- `corpus.py`: `3b92a240df4332a825bbef4d9df06e7227c0806e21ff792690dc9030e9bfd478`
- harness binaries: S `5823cad02e85ad5b6ea0136a909830665d10be732c9cd3d3ac04bd41011bc52b`, A `b1120235046cfcaa80ffb9777f3a3e8cd6be95eace760ee0227d0e5f8dfb3c05`, built from S `d045c0f` and A `61f4e4c` with `harness/` from `aa56020`.
