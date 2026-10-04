# ADR 0002: Per-agent memory in a single SQLite file

> Status: accepted
> Date: 2026-07-25 (revised in place 2026-10-04)

## Context

An agent needs durable memory that stays with its own data. The earlier addressed schema and the source-linked schema both use the agent container's volume, and a restart must recover records without another service. Issue #823 changed what the file holds, not who owns it. SQLite's own locking lets two processes open one file and interleave their writes, so ownership has to be enforced before the file opens, not inside it (#928).

## Decision

Each agent owns one configured `memory.db` SQLite file. The source archive, mid episodes, and each episode's embedding share it. The store interface keeps SQLite behind the memory boundary. Completed sources commit before synthesis; each mid episode and its processing interval commit atomically (ADR 0011). The old addressed tables remain in existing files; the source-linked runtime refuses a file with active or archived addressed rows instead of deleting or treating them as source-backed evidence. Populated extractive source-linked schemas likewise require an explicit operator decision; `memory.md` records the fresh-path procedure that preserves the old archive.

One process on a host writes a store. Before it opens `memory.db`, the store takes an exclusive, non-blocking `flock` on a lock file named after the file's fully resolved real path plus `.lock`, so a relative, `..` or symlinked spelling of the same store contends for the same lock. A held lock refuses the opener with `ErrStoreLocked`, which names another process as the store's owner; a serving agent and a one-shot `sherlock agent --message` run open the store the same way. The lock is released after the database closes, or when the process exits. It guards one host and one file: fencing a copy of the volume on another host belongs to the lifecycle authority, not to Sherlock.

## Consequences

- A single volume backup contains conversation sources and their search links.
- A second agent, or a one-shot run, pointed at a store a running agent holds fails at startup instead of writing beside it. The lock file stays on disk after the holder exits; only a held lock refuses an opener.
- Two hosts that each mount the store, or a copy of it, are not fenced from each other by this lock.
- A populated addressed store requires explicit export or migration before this runtime starts. The old rows stay untouched on refusal.
- Similarity ranking reads the stored embeddings in process, so no external search service or vector index is needed. Only the embedding of new text needs the configured endpoint (ADR 0004).
