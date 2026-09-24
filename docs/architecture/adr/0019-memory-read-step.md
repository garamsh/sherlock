# ADR 0019: Search older conversation with bounded model-guided phrases

> Status: accepted
> Date: 2026-08-17 (revised in place 2026-09-24)

## Context

The prior read step resolved a question to a model-written address and widened to a subject on a miss. The running measurement in `docs/research/recall-specificity/` found no addressed hits in 30 probes; every answer that carried the planted value came from widening. Source-linked summaries need a read that can find several topics and survive paraphrase without claiming that a candidate is an answer.

## Decision

The session model supplies up to four search phrases, including alternate wording and separate topics. Recall offers the two most recent mid episodes and up to five distinct older candidates ranked by FTS5 lexical relevance, with recency breaking ties. Phrase usability and FTS tokenization use the same policy: when the bounded phrase list contains no searchable tokens, task text is the fallback. A usable phrase list that matches nothing retains its own empty search result.

Recent episodes remain available even when lexical search finds nothing. Up to eight pending source previews keep the unsummarized interval reachable with small rolling windows. Each summary and preview is bounded in length and labelled as such, with an inspection reference. A candidate is not proof that a requested fact exists; original wording requires source inspection.

## Evidence and limits

- The bounded local live-model A/B replay recorded in [PR #834's review at bfbc756](https://github.com/garamsh/gagent/pull/834#pullrequestreview-5304675916) used the same 201-source correction fixture from [issue #832](https://github.com/garamsh/gagent/issues/832): two generic-query baseline failures and two correct relevance-first answers. This supports replacing newest-first ordering for the observed crowding failure.
- That single fixture does not establish general search-phrase quality, summary fidelity, correction handling, or grounded answer accuracy. The issue #821 prototype's 7/7 candidate reachability used hand-authored summaries and cannot establish these properties either.
- FTS5 lexical matching and the older-candidate bound can still omit evidence. Recent episodes can also omit facts through synthesis. Neither ranking nor recency establishes truth; the agent must inspect the linked evidence.

- `TestConversationRecall_RecentCorrectionAndOlderRelevantEpisode` compares the two-set selection against relevance-only and recent-only controls: a weak-overlap recent correction survives while an older discriminative episode remains reachable past generic intervening episodes. This is deterministic reachability, not model answer quality.

## Consequences

- Recall adds one model call before the act loop, using the session's configured model.
- Candidate count and preview length are bounded. Recent and pending material can be unrelated to the question and is not presented as a matching result.
- Recent context remains in the rolling session; search is for older information that need not fit there.

## Read-policy limits

- The runtime currently asks for phrases once before the act loop. Its reusable store search accepts explicit terms, and recall without a model searches task text directly. The act loop can inspect sources but cannot issue another memory search.
- Neither task text nor one model reinterpretation reliably exposes implicit needs such as writing methods or coding conventions. Comparing direct retrieval, reinterpretation, and iterative model-directed search remains separate experimental work; this baseline settles none of those policy choices.

## Rejected alternatives

- Resolving to the old question address retains the mismatch measured in the running system.
- Putting the entire source archive into each prompt removes the context bound.
