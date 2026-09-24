# ADR 0019: Search older conversation with bounded model-guided phrases

> Status: accepted
> Date: 2026-08-17 (revised in place 2026-09-24)

## Context

The prior read step resolved a question to a model-written address and widened to a subject on a miss. The running measurement in `docs/research/recall-specificity/` found no addressed hits in 30 probes; every answer that carried the planted value came from widening. Source-linked summaries need a read that can find several topics and survive paraphrase without claiming that a candidate is an answer.

## Decision

The session model supplies up to four search phrases, including alternate wording and separate topics. The store tokenizes those phrases and searches bounded mid summaries through SQLite FTS5, returning at most five candidates newest first. If the model supplies no usable phrases, the task text is the fallback. An empty result injects no claimed evidence. The agent can inspect linked short summaries and sources during its act loop.

## What is not measured

The model's phrase selection, summary fidelity, and grounded answer quality have no live-model result for this design. The issue #821 prototype's 7/7 candidate reachability used hand-authored summaries and cannot establish these properties. FTS5 lexical matching and a bounded result set can miss a relevant source.

## Consequences

- Recall adds one model call before the act loop, using the session's configured model.
- Candidate count is bounded, and the read never fabricates a hit for an unsupported question.
- Recent context remains in the rolling session; search is for older information that need not fit there.

## Rejected alternatives

- Resolving to the old question address retains the mismatch measured in the running system.
- Putting the entire source archive into each prompt removes the context bound.
