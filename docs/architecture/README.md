# Architecture

What the system is made of and why it is shaped this way. Workers read this before touching code; the PM keeps it honest.

## Contents

- `overview.md` — module list and entry point
- Topic files (`environment.md`, `memory.md`, `security.md`, `gateway.md`, `agent.md`) — current truth per responsibility
- `adr/` — decision records, immutable once merged

## Rules

- Describe structure with lists of paths and short sentences. No diagrams.
- Topic files hold current truth: what exists, the rules, rejected alternatives, open questions. ADRs hold the decision context at the time. Never record *how* — that is the code's job.
- A PR that changes a decision updates the topic file **and** adds a superseding ADR in the same PR. One without the other is rejected.
- ADRs are never edited after merge.
- Reading order for workers: `overview.md`, then the topic file for each module being touched. ADRs are optional background.
