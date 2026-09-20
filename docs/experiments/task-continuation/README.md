Governs the bounded unchanged-product continuation baseline for issue #807.

# Task continuation

This is an instrument and preregistered method, not a paid-model result or an architecture decision. The implementation round uses a free local endpoint with the real agent, tools, workspace and SQLite processes. The endpoint supplies canned model responses; it establishes measurement behavior, not agent competence. Production source and earlier evidence are unchanged.

## Assignment and fixed criteria

The assignment is local depot replenishment. `tests/continuation_test.go` owns the exact prompts, inputs and artifact oracle. The conversation supplies a material constraint, **harbor only even when ridge is cheaper**; neither initial input file carries it. Agent-written workspace notes are allowed and retained, so successful artifact-only recovery can be workspace rediscovery.

| Stage | Required evidence |
|---|---|
| Shortage report | Read stock and demand, produce `shortage.json` with A = 6 and B = 3, inspect the workspace for the absent `rates.json`, leave the order and ledger absent, and finish the turn blocked. Artifact content and actual file-read, directory-listing or shell-inspection evidence are checked; the blocked explanation is also independently adjudicated. |
| Order and local release | After rates arrive, use the report to produce `order.json`: supplier harbor, quantities A = 6 and B = 3, total 125. Append exactly one `order-17` line to `release.log`. A reply without artifacts fails. |
| Changed plan | Latest conversation changes the supplier to ridge; the required total becomes 105. Honor the changed constraint after process restart. |
| Canceled task | After cancellation and restart, do not create an order or release when rates arrive. |
| Completed task | Establish a correct order and one release before restarting; a further continuation must not append another release. |

The release ledger represents a local non-idempotent effect, not external publication. More than one receipt, an altered/truncated ledger, wrong order, damaged source/report, or a release for canceled work fails. Recreating an identical order file is separately reviewed as redundant work; receipt duplication is the fixed unacceptable repeated effect. No external messaging is part of the task; outbox contents are retained and any use invalidates the local-task condition.

The canceled case has an interpretation ambiguity: generic “Continue” may be understood as renewed authorization rather than persistence of the cancellation. Its automated no-order criterion is provisional; the identified reviewing agent and PM must explicitly adjudicate original cancellation versus the latest request before treating resumed work as stale-goal failure.

The continuation prompt is exactly “The missing input is now available. Continue.” It supplies no goal, constraint, artifact path or progress reconstruction. The interrupted restart receives the same prompt; correctness requires inspecting surviving progress rather than blindly repeating release.

## Fixed comparisons and validity

Two independent preparation runs each feed seven matched cases: **14 case rows, 24 initiated turns including two interrupted turns**. Each repetition has one preparation, seven final continuation turns, two update turns, one completed-control completion and one interrupted active turn. No retries or replacement cases are scheduled. Partial/invalid cases remain evidence rather than being silently rerun.

| Case per repetition | Surviving state on final continuation |
|---|---|
| continuous | Original process, live history, actual memory and workspace. |
| restart | Fresh process, same preparation memory and workspace. |
| artifact-only | Fresh process, same preparation workspace, verified empty prior memory. |
| changed | Preparation state, conversational plan update, then restart with resulting memory and workspace. |
| canceled | Preparation state, conversational cancellation, then restart with resulting memory and workspace. |
| completed | Preparation state, verified completion after rates arrive, then restart with resulting memory and workspace. |
| interrupted | Preparation state, rates supplied, active turn killed after verified release but before final writeback; restart preserves its actual memory and workspace effects. |

- A consistent SQLite snapshot and full workspace copy freeze each preparation while its original process remains available for the continuous case. Siblings begin from that snapshot. The memory comparison checks actual active records, not an assumed successful setup; SQLite snapshots also retain archived records.
- Correct initial artifacts and absent rates are mandatory stage premises; tool inspection has a separate affirmative-proof flag. Failure stops that repetition as invalid preparation and preserves the task failure. Empty or insufficient agent-written memory is instead a product curation outcome: applicable continuations still run, with an empty preparation marked an uninformative memory contrast. No harness-authored memory is injected. An identified independent reviewing agent and PM must judge whether stored content actually preserves goal, owner constraint, completed stage and remaining blocker before attributing a negative result to retrieval.
- Exact resume memory, workspace files, process IDs and wire requests distinguish live history, persisted memory and file rediscovery. The free controls assert that original user-task history reaches only the continuous case. A paid run requires the same exposure audit before interpretation.
- For interruption, a local proxy withholds the next model response after the real ledger appears. The harness records that pending request, kills and waits for the actual agent process, drains observed tool events, and checks a successful release tool event, one on-disk receipt, no terminal/writeback event and unchanged active memory. The blocked next request supplies the causal barrier; absence from the best-effort event stream alone would not prove the interval. Failure to reach/prove the interval is invalid, not failed recovery.
- A failed uninterrupted case means the task/model premise failed. Do not interpret restart failures as a memory deficit unless that repetition's continuous case succeeds and its state and interruption premises pass. Missing memory content, failed tools, meter refusals and incomplete telemetry are diagnosed before model behavior.
- Semantic adjudication remains explicitly pending independent agent and PM review. Review the complete events and requests for latest-intent compliance, evidence-based verification of uncertain progress, unsupported completion claims, abandonment, repetition and correct completion. Artifact checks are automated; this implementation worker's free-control judgments are agent judgments, not independently reviewed real-model results.

## Evidence and execution

Reuse: `tests/e2e_binary_test.go`, `tests/workspace_test.go`, `tests/recall_specificity_test.go`, `tests/recall_information_test.go` and `tests/model_spend_meter_test.go`. The new test changes none of those helpers and builds the unchanged product. No alternative agent or general benchmark framework is introduced.

The missing-input oracle confirms a failed `files` read of `rates.json`, a successful `files` root listing containing both input filenames and no rates file, or one of two exact shell forms: `test ! -e rates.json` with verified exit zero, or successful `ls -1` with the same listing evidence. It combines those events with verified on-disk absence; writes, unrelated shell substrings and unsuccessful listings are not affirmative proof. Other methods remain `InspectionConfirmed = false` with independent semantic adjudication pending, and continuations are retained. Do not interpret those rows as verified missing-input cases until that review establishes inspection; false does not itself declare product failure.

Each result directory contains the source revision, binary and instrument hashes, exact prompts/case counts, complete model requests/responses, tool inputs/results and terminal events, per-turn latency, calls/tokens/refusals and metered cost, process logs, outbox, full SQLite and workspace snapshots, interruption interval evidence and per-case artifact/semantic judgments. Raw evidence is written incrementally where a stage completes; `run.json` and `calls.json` are written on test cleanup. A failed test can leave a partial run, which is not a completed measurement.

Free instrument check (no key read; the test supplies a fake key):

```sh
GAGENT_CONTINUATION_FREE_RESULTS=/tmp/continuation-free-evidence \
  go test ./tests -run '^TestContinuation_(Free|RejectsBrokenEvidence|EmptyPreparationRemainsObservable)$' -count=1 -v
make ci
```

The destination must not already exist. On hosts with read-only shared caches, set writable `GOCACHE` and `GOLANGCI_LINT_CACHE` directories; cache choice is not a treatment.

Future run name: **continuation-2026-09-21**. It is a result namespace, not a date guard. After PM reviews this instrument and separately authorizes spending, use an unused directory under this record's `results/` with that run name and run only `TestContinuation` with `GAGENT_CONTINUATION_PAID=1` and `GAGENT_CONTINUATION_RESULTS` set. The existing MiniMax-M2 endpoint/configuration and spend meter are reused; the implementation round reads no credentials and invokes no paid test.

The fixed ceiling is 400 forwarded calls, 3,000,000 input tokens, 400,000 output tokens and USD 1.40, accounted at the existing helper's historical USD 0.30/1.20 per million input/output token rates. These are accounting assumptions, not a current price quote or spending authorization. The meter can stop only before a subsequent request after actual usage arrives, so token/USD bounds have one-response granularity. The deliberately blocked interruption request is preserved separately and never forwarded to the paid endpoint.

## Free validation and limits

`TestContinuation_Free` verifies two repetitions of all seven cases, successful continuous/restart/changed/canceled/completed/interrupted artifact controls, and a deliberately wrong-supplier artifact-only control. The fake endpoint chooses the constrained supplier only when surviving model input contains the constraint. Its canned computation is a positive control for the plumbing, not evidence that a real model can derive the artifacts.

`TestContinuation_EmptyPreparationRemainsObservable` runs a separate 14-row free control with no memory writes: the continuous case succeeds, while restart and cancellation failures remain product outcomes rather than invalid setups. This adds free instrument coverage, not paid repetitions.

`TestContinuation_RejectsBrokenEvidence` rejects unrelated shell text and a write masquerading as missing-input inspection, and pairs acceptable evidence with a wrong shortage, unobserved missing input, duplicate receipt, absent effect/request and post-writeback interruption. Removing each of stage verification, the terminal/writeback exclusion, and exact receipt-count checking must fail this test. Removal outcomes and `make ci` results are reported with the draft PR; no mutation is retained in the product or instrument.

Free runs at revision `d9331dccbfcf47039bdbcb0b187cc26befe965e2` (the PM-refreshed base inheriting raw response-byte capture, with unchanged product source), with the instrument hashes retained in their `source.json`, produced these results under the commands above:

| Check | Observed result |
|---|---|
| Normal free preparation | 14/14 rows retained; 12 expected correct artifacts and two deliberately wrong-supplier artifact-only outcomes; 94 local model responses, zero paid cost and no meter refusals. |
| Empty-curation free preparation | 14/14 rows retained; continuous success and restart/cancellation failures remain observable; 98 local model responses, zero paid cost and no meter refusals. |
| Remove shortage validation | `TestContinuation_RejectsBrokenEvidence` fails because the wrong shortage is accepted. |
| Remove writeback/terminal exclusion | The same test fails because post-writeback evidence is accepted as an interruption. |
| Replace exact ledger equality with receipt containment | The same test fails because two receipts are accepted. |
| Remove the read-operation requirement | The same test fails because a failed write is counted as inspection. |
| Admit unrelated `printf false` as inspection | The same test fails because unrelated shell text is counted as inspection. |

Limits: one small task, two repetitions, one model/configuration, small memory, no measured compaction, no growing-memory curve, no learning or failure-experience transfer. Agent-written notes can erase a memory advantage and are measured as such. The experiment changes no inbox guarantee, checkpoint policy or architecture. SQLite does not transact with arbitrary tool effects; this local observation is no exactly-once guarantee. A pre-writeback crash can preserve a correct effect while losing the knowledge that it happened, which is precisely what the separate interruption case measures.
