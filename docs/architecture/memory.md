# Memory

Governs the agent's source-linked conversation memory and its lifecycle. Rationale: ADR 0002, ADR 0006, ADR 0011, ADR 0018, ADR 0019.

## Current decisions

- The rolling session keeps the newest visible conversation for current work. It is bounded by the configured message limit; `internal/agent/memory/compaction.go` keeps the leading instruction and trims the oldest window entries. A negative limit disables compaction and restores all saved visible messages after restart.
- Completed turns write each original inbound user message with its gateway ID, successful outbound `message_send` content, and the terminal assistant reply to the agent's SQLite file. System instructions, tool results, hidden reasoning, and credentials are outside the durable source boundary. Credential patterns are redacted before persistence; a redacted original cannot be recovered verbatim (ADR 0006).
- Each stored source has a bounded extractive short summary. Each short summary has a bounded, searchable mid memory. IDs link mid to short to the original source. The three records for a completed turn commit together, so failed maintenance leaves no processed source or advanced cursor (ADR 0011).
- Recall asks the session model for up to four search phrases, then retrieves at most five matching mid memories from SQLite FTS5. Matching candidates are ranked by lexical relevance, with recency breaking ties. Search terms and candidates are hints, not proof of an answer; an empty search injects no evidence (ADR 0019).
- The agent's reserved `memory_inspect` call reads a short summary or the exact saved source by ID. This is the agent's own memory boundary, not an operator-installed tool. The model is instructed to inspect a source for exact wording and to distinguish corrections from older statements (ADR 0018).
- The same SQLite file still contains the addressed tables of the previous design. The runtime refuses startup when those tables hold active or archived rows, with `ErrAddressedStore`; it does not reinterpret or delete them. An explicit export or migration is required to run source-linked recall on that file (ADR 0002).

## Current state

`internal/gateway/runtime.go` opens the store, checks old data, restores the newest persisted visible messages into the rolling session, and wires the search and inspection hooks. A completed turn persists its source records before `turn_finished`. The source archive is durable across a clean restart and does not depend on the bounded session window. The store's FTS5 index is over bounded mid summaries; original source text is reached through IDs after a candidate is found.

The `memory_write` transport still requires `memory_ops` because current provider and turn tests use that response document. The source-linked runtime does not apply those addressed operations to the old tables. The field is a compatibility field pending a separate contract change, and the only active long-term memory authority is the source-linked conversation store.

## Open questions

- General search-phrase quality, correction handling, and grounded answer accuracy remain open. A bounded local live-model A/B replay on one 201-source fixture supports relevance-first ordering for the observed correction loss, not a general quality claim; [ADR 0019](adr/0019-memory-read-step.md#evidence-and-limits) records the evidence and its limits. Lexical mismatch and the five-candidate bound can still omit corrections. The deterministic tests check candidate reachability, source inspection, and scripted answer controls separately.
- Extractive summaries may omit a fact beyond their bounds. A model-generated summarizer would need its own fidelity and cost evaluation; this implementation makes no claim that it has one.
- Credential redaction recognizes common labelled and bearer forms, not every possible secret syntax. Deployments should avoid sending credentials in conversation.
- An in-flight turn that fails before completion has no durable assistant reply. Accepted but undrained gateway messages remain process-local (`gateway.md`).
