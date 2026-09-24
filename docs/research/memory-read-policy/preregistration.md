# Memory read-policy preregistration

Defines the fixed collection and scoring plan for issue #842 before model calls.

## Question and design

- Compare A: task-text retrieval, B: one model rewrite before retrieval, C: task-text retrieval followed by optional repeated model-directed search during work.
- Each uses the same MiniMax-M2 endpoint, temperature zero, task prompts, corpus, source inspection, and 14,000-byte serialized request bound. The underlying production store and `ConversationRecall` provide two recent plus up to five older candidates per search. All arms may inspect sources from retrieved intervals; only C may change queries after work starts.
- Twelve cells, in task order essay/export/exact/absent, each A/B/C once. No retries, including truncated responses. A transport or accounting failure stops collection and leaves the remaining cells unrun. No selection by result.
- `corpus.json` contains twelve authored mid episodes with eight original messages each. These are synthetic source-linked fixtures, not measured model synthesis. The last two episodes are irrelevant recent controls. Both implicit tasks need three facets; explicit project words cannot name all required rule memories. The policy registry and topic memories provide bridges to follow-up search.
- Corrections occur in different episodes: Alder capacity 40 to 28; Kestrel header count to total. The exact control requests the capacity correction. Zephyr has no supporting source.
- This is a study-local act loop, not the shipped session. It measures retrieval timing with real production storage, not deployment, file execution, or general autonomous work.

## Limits and evidence

- Maximum 48 HTTP calls, 120,000 input tokens, 20,000 output tokens, USD 0.08. Each cell gets four calls including B's rewrite and 1,600 output tokens; each act call reserves at most 800 output tokens, rewrite 400. Unused allowances do not permit additional runs. A final-call reminder requests completion.
- Before each request reserve serialized UTF-8 body bytes plus 1,024 as a conservative input-token upper bound, and the full requested output maximum. Refuse any request exceeding the global limits or shared context bound. Unknown usage is charged at its reservation and ends collection; provider overrun ends collection. These bounds assume provider token limits are honored.
- USD is calculated at standard uncached input USD 0.30/M and output USD 1.20/M, verified on 2026-09-24 against [MiniMax pricing](https://platform.minimax.io/docs/guides/pricing-paygo). Cache discounts may lower the invoice; the experiment cannot inspect billing. No paid synthesis calls.
- Record complete request/response JSON without authentication headers, request timestamps, latency, usage, returned model identifier, tool trace and final answer, plus source revision and input hashes. MiniMax-M2 is a provider alias; no immutable model revision is available.
- `run.py preflight` must prove all seven required facet-source paths using the real store, exact source equality, direct-query behavior, absent-query behavior, rejected unexposed inspection with an accepted recent-source control, loss of reachability when a summary is removed, and production SearchTokens usability for stop-word-only versus substantive phrases. Before collection, coordinator inspection identified and removed a Python regex approximation of that fallback; the Go bridge now calls the production helper. No paid call may precede this check.

## Scoring fixed before collection

- Score final artifacts independently of candidate IDs and of the measured model's self-assessment. The author applies this rubric after collection; no second human or paid judge is implied.
- Essay: (1) concrete household opening, mechanism, final limitation, residents addressed, forbidden adjective absent; (2) no measured pilot health outcomes stated without health-benefit inference; (3) current 28-megalitre capacity, no obsolete 40 as current. Finished prose must be at most 120 words excluding citations.
- Export: (1) complete runnable `render_export(rows)` using standard-library csv.writer and io.StringIO, preserves order, LF records; (2) filters underscore-prefixed labels; (3) header label,total with values from count. Execute extracted code locally on rows exposing quoting, ordering and privacy. A small example must show output. No deployment or external effects.
- Exact: current 28 and the exact original correction quotation. Absent: explicit unavailability with no invented date.
- For each relevant facet record inspected original source, exact supporting quotation in the final response, and semantic application separately. Full success requires a completed artifact, all facets, and source verification; retrieved-but-uninspected summaries alone never count as verified.
- Classify misses: retrieval miss (source never exposed), failure to search again (C stops with reachable needed evidence absent), reasoning/application (source inspected but incorrectly used), bad summary (source fact omitted/distorted by summary), unavailable fact (absent control), tool/wiring failure, or resource truncation. Overlapping retrieval and no-follow-up labels are permitted. Report obsolete/false assertions separately.
- Compare task and policy success, facets, misses, calls, input/output tokens, cost and summed HTTP latency. One synthetic run per cell provides no uncertainty estimate, no causal separation of extra calls from search timing, and no production read-policy decision.
