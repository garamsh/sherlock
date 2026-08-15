# Experiments

Structured experiments that produced evidence for architectural decisions. Each subdirectory is one experiment: a report, its harness, and its raw results. An experiment record answers "why is this architecture the current one" with reproducible numbers; it is not a responsibility document (current state lives in `docs/architecture/`).

## Index

- `memory-architecture/` — the memory-store experiment behind ADR 0006's 2026-08-14 revision: 32 candidate store/retrieval architectures, four workloads, five rounds. Report, harness, and raw results included.
- `memory-cycle/` — the memory-cycle experiment: write path, read path, and store policy tested as one system. Established that the voluntary writeback is the root failure (0.00 context-recall) and that structured output is the minimal structural fix.
- `memory-creation/` — the memory-creation experiment: who decides what to write, with what vocabulary, and when maintenance happens. Four rounds; the fourth was built to falsify the third and did. Established that no similarity threshold can decide supersession, that what the decider is shown is the structure (a retrieval offer beats a recency window 0.76 to 0.01 on corrections), and that key-addressed replacement relocates the failure rather than removing it.
