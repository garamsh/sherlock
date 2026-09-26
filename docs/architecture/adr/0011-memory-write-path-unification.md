# ADR 0011: Persist sources before atomic summary checkpoints, and keep one bounded note beside them

> Status: accepted
> Date: 2026-08-02 (revised in place 2026-09-26)

## Context

The earlier decision unified model-chosen writes through the reserved `memory_write` response. Source-linked conversational memory instead needs an original visible message and summaries that cannot be orphaned by a failed maintenance step. Issue #823 changes the active write authority while preserving the terminal response transport. Issue #866 adds one model-written layer beside the conversation: a bounded note that keeps standing instructions and the current work beyond the window, as the surveyed agents keep one (`docs/research/agent-context/` §Comparison).

## Decision

A completed response archives its visible source messages in one transaction before model synthesis. Each full cross-turn batch creates an immutable mid episode with its direct source interval. The last committed interval is the only processing checkpoint. Model calls happen outside write transactions; a checkpoint comparison refuses a stale concurrent synthesis. An episode's embedding is written after the episode commits, also outside any transaction; an episode still without one is embedded at the next completion or startup and is left out of similarity ranking until then.

A failure preserves completed sources and any earlier committed episodes. Startup and subsequent completions resume from the persisted intervals without skipping pending input. Gateway IDs identify a replay of the same completed batch; ID/content conflicts and partially overlapping batches fail explicitly. The reserved `memory_write` response carries the reply and the note.

- The note is a required string of at most 2,000 characters that the model restates whole every turn. A longer one is handed back to the model as the call's result, not cut.
- A note the call leaves out or sends as null keeps the saved one.
- It is saved in the agent's SQLite file after the turn's sources commit, with credential patterns redacted. A failed note write fails the turn and leaves the sources durable and the previous note in place.
- The session shows it under the instructions on every turn.
- The addressed `memory_ops` field is removed.

## Consequences

- The note is the one source-free memory the model writes. It is bounded, has one writer, and is replaced whole; everything else the agent recalls is source-linked.
- Old addressed rows remain readable by the legacy store API but block source-linked runtime startup until an explicit migration or export.
- The monitoring stream's `writeback` event carries the saved note instead of `ops`; a consumer that read `ops` reads `note`.

## Evidence and limits

The note was tested with the search call of ADR 0019, as candidate A in `docs/research/agent-context/`, against the shipped structure. All three runs were preregistered and live, on MiniMax-M2:

- **Issue #862.** On 280 primary probes, A applied the evidence in 209 and the shipped structure in 124 (A only 86, shipped only 1).
- **Issue #864.** On a new seed, A applied it in 192 and the shipped structure in 122 (74 to 4). The exact-query, absent-query and continuation guardrails held.
- **Where the note shows.** In #864, A's note stated the obsolete value after a correction in 0 of 70 episodes, and held a standing instruction after it was given in 60 of 70. Implicit-rule application rose from 2 to 17 of 70, with the rule in view in 54.
- **The first two verdicts are undetermined under their own rules.** The oracle control, which asks whether the evidence was reachable through the store at all, failed in both:
  - #862 counted a mid summary holding a key word, and summaries reworded it (S 261, A 264, against 266 required).
  - #864 counted a top-five mid holding the evidence source. Every miss was a mid that states the key but mixes the evidence with a neighbouring exchange, and so ranks lower (257 in both arms).
- **Issue #870 (PR #873): valid.** Each evidence exchange was placed in a batch of its own, so the oracle could pass or fail on ranking alone.
  - Every control passed: oracle reach 277 and 279 of 280, every stored evidence batch unmixed, A's note non-empty after a standing rule in 62 of 70, and every mutation check.
  - A applied the evidence in 212 of 280 against the shipped structure's 171 (A only 42, shipped only 1, p = 1.0e-11), and its guardrails held.
  - Its note stated no obsolete value after a correction in any of 70 episodes.
  - This preregistered verdict is the basis for adopting the note.
- **The first design failed.** A nullable note meant "unchanged" was left null by MiniMax-M2 on every turn, even after a standing instruction, in #862's pilot; the note is required for that reason.

## Rejected alternatives

- A nullable note meaning "unchanged" was left null on every turn in #862's pilot. A structured work record with lifecycle states and attempt logs (PR #812) is larger than any surveyed agent keeps and did not stop cancelled work resuming in its own run.
- Keeping remote synthesis inside the source transaction would lose completed messages on a model failure and hold the write lock during network latency. Persisted pending intervals and bounded pending context make separate source capture recoverable.
- An independent mutable cursor could advance without the summary it describes. Deriving progress from committed summary intervals avoids that failure state.
