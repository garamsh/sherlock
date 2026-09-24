# Memory

Governs the agent's source-linked conversation memory and its lifecycle. Rationale: ADR 0002, ADR 0006, ADR 0011, ADR 0018, ADR 0019.

## Current decisions

- The rolling session keeps the newest visible conversation for current work. It is bounded by the configured message limit; `internal/agent/memory/compaction.go` keeps the leading instruction and trims the oldest window entries. A negative limit disables compaction and restores all saved visible messages after restart.
- Completed turns write each original inbound user message with its gateway ID, successful outbound `message_send` content, and the terminal assistant reply to the agent's SQLite file. System instructions, tool results, hidden reasoning, and credentials are outside the durable source boundary. Credential patterns are redacted before persistence; a redacted original cannot be recovered verbatim (ADR 0006).
- The recent rolling window is short memory. There is no separate short-summary tier. A full bounded batch of completed visible messages across turns produces one immutable mid episode, synthesized by the session model independently of other episodes. Partial batches remain durably pending (ADR 0006).
- Source capture commits before synthesis. Each mid episode commits its direct source interval atomically; the end of the latest committed interval is the processing checkpoint. Failed synthesis leaves pending sources for the next completion or startup to retry, without holding a database write transaction across the model call (ADR 0011).
- Recall combines bounded recent mid episodes with relevant older FTS5 candidates, deduplicated by ID. A bounded page of pending source previews remains visible even with a small rolling window. Unusable generated phrases fall back to task text; usable phrases that match nothing retain that result (ADR 0019).
- The reserved `memory_inspect` call reads an exact source message by ID directly from an episode's inclusive source interval. Summary text is a retrieval hint, not verified evidence. Corrections can supersede assertions within an episode; across episodes, chronology and original evidence distinguish current from historical claims (ADR 0018).
- The same SQLite file still contains the addressed tables of the previous design. The runtime refuses startup when those tables hold active or archived rows, with `ErrAddressedStore`; it does not reinterpret or delete them. An explicit export or migration is required to run source-linked recall on that file (ADR 0002).

## Current state

`internal/gateway/runtime.go` opens the store, checks old data, restores the newest persisted visible messages into the rolling session, retries pending full batches, and wires synthesis, search, and inspection. A completed response persists its source records before summary maintenance and `turn_finished`; maintenance failure reports an error while retaining those sources. The source archive is durable across a clean restart and does not depend on the bounded session window. The store's FTS5 index is over bounded mid summaries; original source text is reached through IDs after a candidate is found.

The `memory_write` transport still requires `memory_ops` because current provider and turn tests use that response document. The source-linked runtime does not apply those addressed operations to the old tables. The field is a compatibility field pending a separate contract change, and the only active long-term memory authority is the source-linked conversation store.

## Existing databases

- Populated extractive source-linked databases are refused before modification. Their one-source summaries are not silently reinterpreted as episodic summaries. Empty extractive schemas upgrade automatically; populated legacy addressed tables retain their startup refusal.
- Stop the old process cleanly and retain the old binary and the complete database, including any WAL/SHM sidecars if shutdown was interrupted. Back up with SQLite's backup facility or while no process is writing; copying only the main file during an active WAL write is not a backup.
- To start episodic memory without rewriting old evidence, select a different, empty `--memory-path`. Keep the original file with the previous binary for historical inspection. The new runtime does not search that separate archive. Carrying all old history into the new runtime requires an explicit migration; none is provided here. Do not delete or relabel the old summary tables to bypass the refusal.
- `TestOpenConversation_ExtractiveDatabaseRequiresExplicitNewPath` verifies refusal leaves the populated file unchanged and that the fresh-path and empty-schema controls work.

## Open questions

- The current pre-turn phrase call is a baseline. Direct search is reusable, but the act loop has only source inspection and cannot repeat retrieval. Implicit associations and iterative search policy require a separate discriminating evaluation (ADR 0019).
- Synthesis can omit facts or mishandle corrections; an episode summary is model output, not verified truth. Lexical mismatch and bounded retrieval can also omit evidence. Deterministic tests exercise persistence, correction fixtures, linking, and context bounds; they do not establish general model fidelity.
- Pending source previews are bounded; unusually long messages require exact source inspection. A persistent model failure prevents startup catch-up until the provider recovers, while completed sources remain intact.
- Gateway source IDs make retries of the same completed input batch idempotent. One-shot calls without IDs are distinct turns, so an uncertain acknowledgement cannot be deduplicated from text alone. Mixed previously committed and new IDs are refused rather than silently skipped.
- Credential redaction recognizes common labelled and bearer forms, not every possible secret syntax. Deployments should avoid sending credentials in conversation.
- An in-flight turn that fails before source capture has no durable assistant reply. Accepted but undrained gateway messages remain process-local (`gateway.md`).
