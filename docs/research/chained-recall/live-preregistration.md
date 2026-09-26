# Chained-recall live preregistration

Fixes issue #874's live stage before its first live call. It follows `preregistration.md`, both amendments included, except where this file says otherwise. At commit time only S's offline records exist; L's and H's do not.

## Question

With the model acting, does an arm make the agent reach and apply every hop's evidence of a chain more often than the shipped structure does?

## Arms

- **S.** The shipped structure over the batch-4 shared store (`arms/shipped`), with the model free to chain `memory_search`.
- **M.** S with one instruction appended to the default ego (`arms/chain-contract`, `Instruction`). Production would carry it in the contract.
- **L or H**, one of them, chosen when both offline records exist, by this rule fixed now:
  - **Setting.** Each arm is read at its setting whose mean context characters per turn is at most 1.25 times S at batch 4's (3,498, `evidence/offline/scores.json`).
    - Where more than one setting qualifies, the one with the higher clean k ≥ 2 chain reach is used; where none does, the setting with the fewest context characters.
    - This is how "at equal context characters" is read.
  - **Choice.** The arm with the higher offline chain reach on the clean k ≥ 2 chains (preregistration.md Amendment 2) goes live. A tie goes to the one with fewer context characters.
  - **H's margin.** H goes live only if its reach there trails S at batch 4 by no more than 5 percentage points (the PM's margin).
  - **Its live setting** is that setting.
- **Common to every arm:**
  - The live window is 32 messages.
  - Each probe runs one turn on its own copy of the arm's build: what a restart restores, the arm's recall and search, and production's session, inspection, note and synthesis (`study/live.go`).
  - The chat model is MiniMax-M2 through the shared proxy.

## Probes

- All 180 chain queries and all 60 guard queries, for every live arm: 240 probes per arm.
- **The primary set is the 114 clean chains of k ≥ 2** (Amendment 2; `score.py` `clean_chains`).
- The remaining chains are reported beside it: C1, and the 36 whose check leaked.

## Endpoints

Scored by `score.py live` as committed here.

- **Applied, the primary endpoint.** The turn's reply, messages sent and files written state the chain's answer: the value for explicit chains, the required reference for implicit ones. Guards use their own checks: E the value, K the corrected value, N an abstention.
- **Every hop seen.** Each hop's two entities are stated in the turn's recall block or in one of its memory tool results.
- **Searches per probe,** and **probes following a name:** at least one `memory_search` whose query names a capitalized word the probe's query does not, which an earlier view in the turn showed.
- **Tokens** per arm, from the ledger (`score.py tokens`).

## Comparison and decision

- **Test.** Each live arm against S on the primary set: the exact two-sided McNemar test, Holm-corrected across the two comparisons, at family-wise α = 0.05.
- **Valid.** An arm is valid when its difference is positive and significant, and every guardrail holds. A guardrail holds when C1, E, K and N each lose no more than 3 probes against S.
- **Decision.** Among valid arms the smallest measured net production diff is taken (M's instruction is the smallest), unless a larger valid arm beats it directly on the primary set by the same test at α = 0.05.
- **Null.** If no arm is valid and every control passed, the shipped structure stands for chaining.

## Controls

A failed control makes the live verdict undetermined, and the report names it.

- **M follows names:** at least 12 of the 114 primary probes (about 10%) follow a name.
- **The arms differ in what the model sees:** S's and M's recall blocks are identical for the same probe, since they share a store. So any difference between them comes from the instruction, and a mismatch in more than 5 probes fails the control.
- **L or H's mechanism reaches the live context:** the arm's own item levels or kinds (linked or group items) appear in the recall block of at least half the primary probes.
- **Mutation checks:** `study` and `harness` tests are seen to fail with their behaviour removed, and `evidence/mutation.txt` records it before collection.

## Collection

- **Smoke run.** First, 4 probes of M (`C2-01`, `C3-01`, `IC2-01`, `E-01`) run into `evidence/live-smoke/`.
  - They are excluded from scoring.
  - A harness defect they reveal is fixed and committed before collection. None of the endpoints, controls or rules above changes.
- **Workers.** Two probes at a time per arm. The key is shared with the L and H workers.
- **Re-runs.** A probe that fails on the provider's limits, a server fault or the network is run again once, after any pause, and scored from that run. Its first record is kept in `results-attempt1.jsonl`. Any other failure counts as not applied.
- **Usage limit.** On HTTP 429 code 2056, the proxy pauses and probes at most every 30 minutes until a probe succeeds.
- **Stores.** Every probe's store copy is kept until scoring.
- **Budget.** The proxy's list-price guard rises from USD 3.00 to 12.00 for this stage. The estimate is 720 probes at about three act calls of 9,000 input tokens each: about 20M input and 2M output tokens, or USD 8.4 at list price. The key's Token Plan refuses rather than bills.

## Revision

- Harness, study and arms at the commit that adds this file.
- SHA-256: `score.py` `3d41fcf7960948b663133a20f93f6414527a3cc8402bec2c71dc60b5378525cc`; `corpus.json` `034439bafec98e2462e91a0d4ce731aa1a9b3a2d4dd366d3c13cd437ba7c8e47`, as in `preregistration.md`.

## Amendments

- **1.** Committed after the smoke run and before collection.
  - **Smoke run.** It ran 4 probes of M (`evidence/live-smoke/M/b4/`). It revealed one defect, fixed in `9cfa1a1` before the smoke run: copying a store refused the empty write-ahead log a closed reader leaves.
    - What the model did is not a defect and changes nothing here. In `C2-01` M named the contact from recall, then answered that it had no number without searching for the name.
  - **Budget.** The shared proxy on port 8091 serves the L and H workers' synthesis and cannot be restarted under them. So the live stage runs through its own proxy on `127.0.0.1:8092`, with its own ledger (`evidence/ledger-live/`) and the USD 12.00 list-price guard.
    - Its 2056 pause is the same code.
    - `score.py tokens` reads both ledgers.
- **2.** Committed after S's collection and before M's results existed.
  - As committed, `score.py live` took every k ≥ 2 chain as its primary set, against this file's primary set of the 114 clean chains.
  - The code now takes the clean chains as primary, reports every k ≥ 2 chain beside them, and counts M's name-following control over the primary probes.
  - The endpoints, test and rules above are unchanged. S's figures were read only on the old set, and no comparison had been computed.
