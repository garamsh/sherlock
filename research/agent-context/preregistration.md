# Agent-context experiment preregistration

Fixes the arms, episodes, metrics, controls, decision rule and budget of issue #862's stage 2 before its first paid call. The figures it rests on come from the stage-1 pilot, whose results, ledger, scores and projection are under `evidence/pilot/`.

## Question

Does a search call during work with a bounded note (A), or a pre-turn understanding call with the same note (B), make the agent reach and apply the right evidence more often than the shipped recall (S)?

## Arms

Each arm is its own source tree, built with the harness copied in (`harness/`), and runs its own production session, store, recall and model client through `gateway.Factory`.

| Arm | Commit | What differs from S |
|---|---|---|
| S | `4e84d2c` (origin/dev) | nothing: raw-text recall before the act loop, `memory_ops` in the terminal call |
| A | `61f4e4c` | reserved `memory_search` call in the act loop; a note of at most 2,000 characters, restated whole in every terminal call in place of `memory_ops`, shown under the instructions every turn |
| B | `b5bc7ed` | A's note, but written by one understanding call before recall that sees the note, the last 16 visible messages and the input and returns a restatement, one to three queries and the note; recall ranks by the restatement and queries as one text; no search call; the terminal call carries the reply only |

- Candidate C is excluded: its running summary is the recursive synthesis ADR 0006 rejects, re-opening that decision is the owner's, and it is the most expensive arm.
- B's recall embeds the restatement and the queries joined as one text. The study's plan named the maximum cosine over them; the recall interface takes one text, and changing it would change the store for one arm.

## Model, embedder and proxy

- Chat model `MiniMax-M2` at `https://api.minimax.io/v1/chat/completions`, through the production OpenAI-compatible client of each arm.
- Embedder `BAAI/bge-base-en-v1.5` at revision `a5beb1e3e68b9ab74eb54cfd186867f64f240e1a`, served by text-embeddings-inference 1.9.4 (`ghcr.io/huggingface/text-embeddings-inference@sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d`), as in #858.
- Every chat request passes through the harness proxy (`harness/proxy.go`, commit `1cf6751`):
  - it adds `max_tokens`: 4,096 for act calls, 2,048 for understanding and synthesis calls;
  - it reserves each attempt's worst case against the ceiling before sending it, at USD 0.30 per million input tokens (one token per three request bytes) and USD 1.20 per million output tokens, and settles at reported usage;
  - it retries HTTP 429, 5xx and transport failures up to six attempts, waiting 5, 10, 20, 40 and 60 s;
  - it re-requests a synthesis that does not carry exactly one `memory_summary` call, up to the same six attempts, as production retries a failed synthesis at the next completion;
  - it answers a synthesis request byte-identical to one already answered from its cache. Stage 2 starts with the pilot's cache (`evidence/pilot/synthesis-cache.jsonl`), because the filler blocks are the same.

## Episodes

`episodes.json` (SHA-256 in §Revision), produced by `corpus.py` at seed 862. Every episode starts from a copy of the base store: the #856 replication's 4,800 messages and its 1,200 recorded batch-4 syntheses, replayed without a paid call (`harness base`).

- Seeded pairs enter through `RecordTurn` as completed turns do; filler blocks are 40 messages from replication filler chats no base chat uses.
- Every live input runs in a runtime built for it, so each starts from what a restart restores: the 32 newest visible messages (the study's window, in place of the default 128, in every arm), the note, and the store.
- The last live input is the probe.

| Family | n | Role | Shape |
|---|---:|---|---|
| F, follow-up in window | 70 | primary | 21 pairs stating the same attribute of other events, pushed beyond the window; the referent pair, three filler pairs and a cue ("Let's get back to X") inside it; probe "What <attribute> did I give you for it?" |
| G, follow-up beyond window | 70 | primary | the referent among 21 same-attribute pairs, then a filler block; the cue in the window; the same probe |
| I, implicit need | 70 | primary | a standing rule stated in a live turn, pushed beyond the window by a filler block; the probe is a task the rule governs (10 rules × 7 tasks) |
| K, correction | 70 | primary | an original statement and its correction, each a live turn followed by a filler block; probe "What is <subject>?" |
| E, exact | 30 | guard | an unused replication fact pushed beyond the window; its exact query |
| N, absent | 30 | guard | the replication's 30 absent queries after a filler block |
| R, continuation | 30 | guard | #810's cases: an RSVP task started live, then (changed or cancelled) a live change, then a filler block; the neutral probe "Two more RSVPs: … That's everyone." 10 continue, 10 changed, 10 cancelled |

## Metrics

Per probe, from `score.py` (SHA-256 in §Revision):

- **Applied (primary endpoint).** The probe's visible output meets the item's check. Visible output is the messages sent, else the reply; value and code checks also read the reply and the files written.
  - F, G, E: the value is stated: every token with a digit, every capitalized word and every month appears as a whole word, a number may carry an ordinal suffix and drop thousands commas, and a 24-hour time may appear on the 12-hour clock.
  - K: the current value is stated; naming the old value as the one corrected does not fail it.
  - I: the rule's check (Roman-numeral list, Celsius only, ISO dates and no month names, all lowercase, tab-indented code, starts with "Ahoy", € amounts and no $, "TL;DR:" first, 24-hour times and no am/pm, ends with "Juniper").
  - R: continue writes the named file with all four names; changed writes the second file with all four and not the first; cancelled writes no file holding two or more of the names.
- **Reached.** An act call of the probe turn had the evidence key in view, by the same matching, outside the model's own output: before the input, the input, or a tool result after it (`harness/record.go`).
- **Inspected.** A `memory_inspect` result in the probe turn holds the key.
- **Abstained (N).** The visible output matches the abstention pattern in `corpus.py`.
- **Layer fidelity (K).** Whether the note after the correction turn states the old value without the new one.
- **Cost.** Calls, tokens and USD per arm and per live turn, from the proxy ledger.
- A turn that ends in an error counts as not applied.

Intervals are Wilson 95%.

## Controls

Required before any verdict. If one fails the verdict is "undetermined" and the report names it.

- **Oracle reach.** For at least 95% of the 280 primary episodes in every arm, a search with the evidence statement's own words ranks a mid holding the key among the top five, or the restored window holds it.
- **A searches.** A calls `memory_search` in at least 20% of G probes.
- **The note is written.** In A and B, the note after the I rule turn is non-empty in at least half of the I episodes.
- **B understands.** B's understanding call answers through its reserved call on at least 90% of B's live turns.
- **Mutations.** Every mutation in `mutation.py` breaks a test (`evidence/mutation.txt`, recorded before stage 2).

## Decision rule

1. A and B are each compared with S on *applied* over the 280 paired primary probes, by the exact two-sided McNemar test, Holm-corrected across the two comparisons at family-wise α = 0.05.
2. A candidate is valid when its difference is positive and its Holm-adjusted p is below 0.05, and every guardrail holds:
   - no primary family loses more than 7 of 70 against S;
   - E and N each lose no more than 3 of 30;
   - no cancelled R case is resumed where S did not resume it.
3. If both are valid, A is taken, having the smaller production diff (−91 against +14 net lines against S), unless B beats A directly on *applied* over the primary probes by the exact McNemar test at α = 0.05.
4. If neither is valid and every control passed, the shipped structure stands, and that is an answer at this size.

## Power

The pilot's discordance against S on *applied* was 0.34 for A and 0.44 for B (32 primary pairs each). At 280 pairs and Holm's first-step level of 0.025, a true 10-point gain is detected with probability 0.71 for A and 0.59 for B (`score.py project`, `evidence/pilot/projection.json`). So 280 does not reach 80% power for a 10-point gain; the families stay at 70 each as the issue requires, and a null result below about a 12-point gain is not a strong answer. The pilot's observed differences were 34 and 38 points.

## Budget

- The pilot spent USD 1.5911 (`evidence/pilot/calls.jsonl`, every attempt, including the runs the pilot then discarded).
- Projected stage 2: USD 8.90, from the pilot's cost per episode in each family and arm scaled to the stage-2 sizes. Projected total USD 10.49, under the issue's USD 30.
- Stage 2's ledger ceiling is USD 20.00, which keeps the total at most USD 21.60. A refused request fails its turn; if the ceiling is reached, collection stops and the report says which episodes are missing.

## Collection

- The three arms run at once, four episodes at a time each. Results are appended per episode; a stopped run resumes after the last recorded episode.
- An episode whose seeding or turn fails with a transport error, or with HTTP 429 or 5xx after all six attempts, is re-run once from scratch after collection; a second such failure counts as not applied. Any other failure counts as not applied without a re-run.
- Nothing is changed between this commit and the end of collection: not the arms, the harness, the episodes nor the scorer. A defect found during collection is reported, not fixed in place.

## Revision

Committed before the first stage-2 request. The commit that adds this file is the stage-2 revision; the SHA-256 of its inputs is:

- `episodes.json`: `fc6421a638c772d334aa81a515fde028435d3d056deafacd487eceb1fa03635f`
- `score.py`: `311f1e8ffb9274c5a5d9aa60338b60f4f508ec1be03a0856eff5802d7033b987`
- `corpus.py`: `636362ce81dc63e476b56aa628e7549296fd5e343707154531b6889da7f36c61`
- harness binaries were built from S `4e84d2c`, A `61f4e4c` and B `b5bc7ed`, each with `harness/` from `1cf6751`.
