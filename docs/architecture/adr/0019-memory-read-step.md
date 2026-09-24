# ADR 0019: Search older conversation with bounded model-guided phrases

> Status: accepted
> Date: 2026-08-17 (revised in place 2026-09-24)

## Context

The prior read step resolved a question to a model-written address and widened to a subject on a miss. The running measurement in `docs/research/recall-specificity/` found no addressed hits in 30 probes; every answer that carried the planted value came from widening. Source-linked summaries need a read that can find several topics and survive paraphrase without claiming that a candidate is an answer.

## Decision

The session model supplies up to four search phrases, including alternate wording and separate topics. The store tokenizes those phrases and searches bounded mid summaries through SQLite FTS5, returning at most five candidates ranked by lexical relevance, with recency breaking ties. The intended fallback when the model supplies no usable phrases is the task text; currently, an empty list preserves it, but a nonempty list containing only unusable phrases suppresses it ([issue #836](https://github.com/garamsh/gagent/issues/836)). An empty result injects no claimed evidence. The agent can inspect linked short summaries and sources during its act loop.

## Evidence and limits

- The bounded local live-model A/B replay recorded in [PR #834's review at bfbc756](https://github.com/garamsh/gagent/pull/834#pullrequestreview-5304675916) used the same 201-source correction fixture from [issue #832](https://github.com/garamsh/gagent/issues/832): two generic-query baseline failures and two correct relevance-first answers. This supports replacing newest-first ordering for the observed crowding failure.
- That single fixture does not establish general search-phrase quality, summary fidelity, correction handling, or grounded answer accuracy. The issue #821 prototype's 7/7 candidate reachability used hand-authored summaries and cannot establish these properties either.
- FTS5 lexical matching and the five-candidate bound can still omit a correction. Relevance rank does not establish current truth; the agent must inspect the linked evidence.

## Consequences

- Recall adds one model call before the act loop, using the session's configured model.
- Candidate count is bounded, and the read never fabricates a hit for an unsupported question.
- Recent context remains in the rolling session; search is for older information that need not fit there.

## Rejected alternatives

- Resolving to the old question address retains the mismatch measured in the running system.
- Putting the entire source archive into each prompt removes the context bound.
