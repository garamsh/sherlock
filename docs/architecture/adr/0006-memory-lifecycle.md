# ADR 0006: Preserve recent context and source-linked older conversation

> Status: accepted
> Date: 2026-07-25 (revised in place 2026-09-24)

## Context

The addressed-only lifecycle dropped old conversation when the rolling context compacted. A model's curated writeback did not preserve the original user wording, so later exact-detail questions had no source to inspect. Issue #823 chose a bounded recent window together with durable linked summaries of older visible conversation.

## Decision

- Keep the newest session messages up to the configured context bound. Restore recent visible user and assistant messages from the store after restart.
- Persist each completed inbound user message with its gateway ID and visible assistant outputs as source records. System instructions, tool results, hidden reasoning, and credentials do not become memory sources.
- Create bounded short summaries linked to source IDs and searchable mid memories linked to short IDs. These are extractive and bounded; no extra model call writes them.
- Recall bounded candidates before the act loop. The agent may inspect linked summaries and original messages through its reserved memory boundary when detail matters.

## Consequences

- The legacy addressed service still has a bounded window of wall-clock time — 7 days in its retention implementation, but the source-linked runtime does not run that pass.

- Compaction removes old messages from the current prompt but not from the durable source archive.
- The archive and summaries have one SQLite owner. There is no separate long-term memory writer competing with source capture.
- The live-model quality of summary selection and answer grounding remains unverified. The offline prototype in issue #821 measured reachability on hand-authored summaries, not model answer quality.

## Rejected alternatives

- Keeping every source message in the rolling prompt would remove the context bound.
- Using the old addressed writeback as another active authority would let an unlinked value compete with inspectable source evidence.
