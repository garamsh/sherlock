# Memory read-policy comparison

Records the bounded, preregistered comparison for issue #842; it makes no production read-policy decision.

The Go bridge `store.go` was deleted by #858, which removed the FTS5 search and phrase recall it called; `git log -- docs/research/memory-read-policy/store.go` reaches it. Its last working revision is `b898867`. It last matched the [manifest](evidence/manifest.json) hash at `ad7a4d8`, the study's merge, before #850 renamed the module path inside it. The evidence and figures below are unchanged.

## Question and method

Can task-text retrieval, one pre-turn model reinterpretation, or repeatable search during work recover implicit requirements and apply verified originals in a finished artifact?

| Policy | Query selection | Available during work |
|---|---|---|
| A, direct | User task text | Inspect retrieved sources |
| B, rewritten | One model call supplies up to four phrases | Inspect retrieved sources |
| C, iterative | User task text initially | Inspect sources and repeat model-directed search |

- Source revision: `a39315d0fe11e73fc29960fb51ab400a1fd513e8`, after #843. The study inputs were committed as `e131226313ebe0e61e2f2a338b0371959aac87b7` before the first HTTP attempt on 2026-09-24 at 14:29:28 UTC. [Manifest](evidence/manifest.json) hashes still match all five collection inputs. No paid retry or post-start collection change occurred.
- Runtime: Python 3.13.5 study loop, Go bridge to production `ConversationRecall`, `Search`, `RecentMid`, and `Source`. Each bridge invocation reconstructs the same temporary SQLite database through `RecordTurn`. This measures the real store with an experimental loop, not the shipped agent session.
- Model: requested and returned `MiniMax-M2` through `https://api.minimax.io/v1/chat/completions`, temperature zero. The provider exposes no immutable weight revision; the response identifier is recorded per call.
- Same [corpus](corpus.json), [tasks](tasks.json), source visibility rules, and 14,000-byte serialized request bound for all policies. Twelve authored mid episodes link to 96 original messages; each has one substantive source and seven acknowledgements. The newest two are irrelevant controls. No live synthesis is measured.
- Two implicit tasks require a writing method/disclosure rule and a serialization/privacy contract respectively. Exact capacity and absent launch-date controls distinguish retrieval/verification from missing evidence. Capacity and header corrections occur in later episodes.
- [Preregistration](preregistration.md) fixes one run per policy/task, task-major A/B/C order, four calls per cell including B's rewrite, 1,600 output tokens per cell, at most 800 per act call and 400 per rewrite. Global ceilings: 48 calls, 120,000 input tokens, 20,000 output tokens and USD 0.08. Reservations precede HTTP; no retries occur on refusal, truncation or failure.

## Deterministic checks before payment

[`run.py preflight`](run.py) produced [preflight evidence](evidence/preflight.json):

- All seven required facet-source paths are reachable through targeted queries, with exact original-source equality. All policies share this candidate universe; successful query choice is the measured variable.
- Direct input retrieval misses two of three required facets on each implicit task. Exact retrieval exposes the correction; the Zephyr query returns only the two recent irrelevant episodes.
- Inspection refuses a source outside retrieved intervals and accepts a source inside a recent interval. Removing the Lantern summary loses its required source, proving reachability checks can fail.
- Stop-word-only and punctuation-only phrases fail production `SearchTokens` usability; a substantive phrase passes. A coordinator found the initial regex approximation before collection; it was replaced with the production helper before preregistration was committed.
- Two budget/context tests pass with accepted HTTP controls and both fail when the reservation guard is removed in a temporary copy; [mutation output](evidence/budget-mutation.txt) records the failures. No mutation run contacted the model. The offline scorer also passes correct/broken export controls and its test fails with privacy scoring removed; [scorer mutation output](evidence/scorer-mutation.txt) records that check.

## Results

All twelve scheduled cells ran. [Raw calls](evidence/calls.json) retain exact prompts, tool schemas, response bodies, usage and latency; [results](evidence/results.json) retain every tool trace and answer. [Extracted answers](evidence/answers/) remove only returned `<think>` blocks for artifact scoring; originals remain intact. [Scores](evidence/scores.json) combine executable checks with [author judgments](judgments.json) under the preregistered rubric.

| Policy | Fully successful tasks | Applied facets, of 7 | Applied and source-verified facets | Calls | Input tokens | Output tokens | USD, uncached | HTTP seconds |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| A | 2/4 | 3 | 2 | 14 | 11,683 | 2,509 | 0.0065157 | 37.02 |
| B | 1/4 | 0 | 0 | 14 | 8,228 | 2,600 | 0.0055884 | 39.57 |
| C | 2/4 | 1 | 1 | 10 | 8,784 | 2,277 | 0.0053676 | 33.61 |

A and C succeed on the exact and absent controls; B succeeds only on the absent control. B gives the correct capacity but omits the original `Correction:` prefix, failing the preregistered whole-original quotation requirement. Neither implicit task achieves full success under any policy. An absent answer is scored on abstention, not on a fabricated supporting quotation. Facet counts exclude the absent control.

| Cell | Applied facets | Original sources inspected for required facets | Completed artifact and failures |
|---|---:|---|---|
| A essay | 1/3 | Capacity | 88 words including title; correct 28, no supporting quotation. Misses method and health disclosure; invents a water-management meaning for Lantern. |
| B essay | 0/3 | None | 95 words including title; uses obsolete 40. Method never exposed; disclosure and correction exposed but not inspected. |
| C essay | 0/3 | None | 103 whitespace-separated words including title, salutation and count annotation. No tools used; no capacity value, method or disclosure; invents Lantern planning meaning. |
| A export | 1/3 | Header correction | Runnable CSV with correct header and quotation; preserves order/escaping, but emits CRLF and includes private rows. Required contract and privacy sources never exposed. |
| B export | 0/3 | None | Runnable CSV with obsolete header, CRLF and private rows. Contract source absent; privacy and correction exposed but not inspected. |
| C export | 0/3 | None | Two follow-up searches expose all required sources, but none is inspected. Invents JSON serialization, filters fields instead of rows, lacks required function, has a SyntaxError, and truncates the final example. |
| A exact | 1/1 | Capacity correction | Current 28 and full exact correction, source 25. |
| B exact | 0/1 | Capacity correction | Current 28 and exact factual substring, source 25, but omits the original `Correction:` prefix; fails the required whole correction quotation. |
| C exact | 1/1 | Capacity correction | Current 28 and full exact correction after another search, source 25. |
| A/B/C absent | N/A | A inspects recent irrelevant sources; B/C inspect none | All explicitly report unavailable; none invents a date. |

[`score.py`](score.py) executes the two parseable export functions on ordered tuple rows containing a comma, quote, positive-count private label and zero count. It compares decoded rows, header and line endings. C's code fails parsing at the extra `]` on line 60, before execution. These are completed-answer checks, not candidate-ID scores. The scorer checks each claimed quotation against both the final answer and original source and requires recorded inspection. Semantic judgments are by the study author, independently of the measured model's self-assessment, not by an independent human reviewer or blinded judge.

## Failure interpretation

- **Retrieval misses:** A's implicit tasks each miss two rule sources. B's broader queries additionally expose disclosure/privacy but still miss the writing/serialization method. The corpus makes these reachable; the observed queries do not always reach them.
- **Failure to search again:** C essay ends immediately without inspection or further search. Tool availability alone does not establish willingness to use it.
- **Failure after retrieval:** C export searches twice and exposes all needed sources, yet never obtains originals. B sees correction candidates but inspects only older facts for its implicit artifacts. These are not store reachability failures.
- **Tool-use errors:** Six inspection calls are refused: four request 16 sources despite the eight-source limit (A/B essay and A/B export); two supply episode IDs outside exposed source intervals (C export and A absent). B/C exact initially request episode-number-like IDs that happen to be valid source IDs; later inspection recovers the correction. The common tool boundary works as declared, but its shape consumes scarce calls.
- **Unsupported inference/obsolete use:** A/C essay turn an uninspected composition method into reservoir technology. B essay applies obsolete capacity and B export applies the obsolete header. No policy invents a Zephyr date.
- **Reasoning/application:** B exact inspects the right source and answers the correct number but drops the correction prefix from its quotation. Initial scoring accepted the factual substring; coordinator review enforced the preregistered whole-original quotation criterion before this report was finalized. Raw responses remain unchanged.
- **Resource truncation:** C export's final response reaches the per-call 800-token cap. Its syntax and contract errors precede truncation, so a longer answer alone cannot be assumed to repair it. No global spending or context reservation stops collection.
- **Bad summary:** Not measured as a model failure here: all summaries are authored hints, and targeted inspection reaches the planted facts. This cannot establish synthesis fidelity. The absent control is an unavailable fact, not a recall failure.

## Cost and limits

[Spend](evidence/spend.json): **38 HTTP calls, 28,695 input tokens, 7,386 output tokens, USD 0.0174717** at [MiniMax's standard rates](https://platform.minimax.io/docs/guides/pricing-paygo), checked 2026-09-24: input USD 0.30/M, output USD 1.20/M. All calls returned usage. The total is a usage-priced estimate, not a verified invoice; caching discounts are not credited. Summed HTTP latency is 110.21 seconds; it excludes local store reconstruction and scoring. [Per-cell scores](evidence/scores.json) retain the finer breakdown.

- One synthetic run per cell cannot rank policies reliably or estimate variance. Fixed order is not randomized, and model alias revisions/server conditions remain uncontrolled.
- Equal total call ceilings give B fewer act calls because its rewrite consumes one. Inspection refusals further reduce its opportunity to reach corrections. The result does not isolate rewrite quality from this resource tradeoff.
- The source-inspection schema bounds batches at eight, while summaries expose eight-message ranges; four 16-ID requests waste calls. Episode/source ID confusion is another interface limitation. These are observed model/tool interactions, not grounds for discarding runs.
- C's cheaper aggregate includes premature finalization and an incomplete artifact; it is not an efficiency win. C exact uses four calls where A exact uses two; its extra search produces no additional task success. C export buys broader candidate coverage without an executable result.
- Source verification requires both inspection and exact quotation. Several answers apply or invent claims without either. Artifact completion alone does not imply grounded completion.
- An additional test with a clearer source selector and enough fixed act budget could distinguish search-policy performance from inspection friction. That is a future experiment, not permission to rerun these cells. No deployment, `agent-test` mutation, production code, PR #841 or PR #812 changes are included.

## Conclusion

This collection provides no measured benefit for a mandatory reinterpretation call and no successful implicit artifact from merely offering repeatable search. It establishes that follow-up queries can reach otherwise missed sources, while revealing failure to inspect and apply them. The next design to test is a small same-model search/inspection loop with clearer source selection; these results do not justify a planning agent or select a production read policy. Current responsibilities and the unresolved policy question remain in [memory architecture](../../architecture/memory.md) and [ADR 0019](../../architecture/adr/0019-memory-read-step.md).

## Reproduction and verification

At head, from the repository root, only the unit tests run:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s docs/research/memory-read-policy -p '*_test.py' -v
```

`score.py` checks every manifest hash, including `store.go`'s, and preflight and collection call the bridge. They run from a checkout of `ad7a4d8`:

```sh
go build -o /tmp/recall-842-store ./docs/research/memory-read-policy
python3 docs/research/memory-read-policy/run.py preflight /tmp/recall-842-preflight
PYTHONDONTWRITEBYTECODE=1 python3 docs/research/memory-read-policy/score.py
PATH=/home/sauce/go/bin:$PATH make ci
```

- Scoring is offline and regenerates scores and extracted answers from immutable raw evidence. Preflight writes to the requested destination; use a fresh temporary destination to preserve historical evidence.
- Paid reproduction requires `MINIMAX_KEY` in the process environment and `run.py collect` with a fresh output directory. It is a new study run requiring its own authorization and budget, not part of this collection. The collector refuses an existing `calls.json`. The original launch passed only `MINIMAX_KEY` from the root `.env`; it did not source that file or inherit GitHub credentials.
- `make ci` runs lint, format, test and build. The initial bare-path attempt could not find `golangci-lint`; the installed `/home/sauce/go/bin` tool was then added to PATH. Final verification results are recorded in [verification evidence](evidence/verification.txt).
