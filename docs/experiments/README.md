# Experiments

Structured experiments that produced evidence for architectural decisions. Each subdirectory is one experiment: a report, its harness, and its raw results. An experiment record answers "why is this architecture the current one" with reproducible numbers; it is not a responsibility document (current state lives in `docs/architecture/`).

## Index

- `memory-architecture/` — the memory-store experiment behind ADR 0006's 2026-08-14 revision: 32 candidate store/retrieval architectures, four workloads, five rounds. Report, harness, and raw results included.
- `memory-cycle/` — the memory-cycle experiment: write path, read path, and store policy tested as one system. Established that the voluntary writeback is the root failure (0.00 context-recall) and that structured output is the minimal structural fix.
- `memory-creation/` — which structure creates, corrects and retires a memory entry. Established that no similarity threshold can decide supersession, that what the decider is shown decides the outcome (a retrieval offer scores 0.71 on corrections against a recency window's 0.01, the two differing in nothing else), and that key-addressed replacement relocates the failure rather than removing it.
