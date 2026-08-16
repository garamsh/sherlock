# Experiments

Structured experiments that produced evidence for architectural decisions. Each subdirectory is one experiment: a report, its harness, and its raw results. An experiment record answers "why is this architecture the current one" with reproducible numbers; it is not a responsibility document (current state lives in `docs/architecture/`).

## Index

- `memory-architecture/` — the memory-store experiment behind ADR 0006's 2026-08-14 revision: 38 candidate store/retrieval architectures, four workloads, six recorded runs. Report, harness, and raw results included.
- `memory-cycle/` — the memory-cycle experiment: write path, read path, and store policy tested as one system. Established that the voluntary writeback is the root failure (0.00 context-recall) and that structured output is the minimal structural fix.
- `memory-retrieval/` — what the read path returns for a task statement, and what the choice of embedder costs it, measured against `internal/memory` itself rather than a reimplementation of it. Established that the shipped ranking returns 0.037 to 0.092 of the planted answers in its top ten while the same ranking over the same corpus returns 0.799 to 0.875 with salience and age held constant; that a real embedder is worth a great deal to relevance and nothing to what the read path returns; and that no supersession threshold separates corrections from coexisting facts on the shipped embedder either, where the mechanism retires every coexisting pair and 6 corrections in 250.
- `memory-creation/` — which structure creates, corrects and retires a memory entry. Established that no similarity threshold can decide supersession, that what the decider is shown decides the outcome (a retrieval offer scores 0.71 on corrections against a recency window's 0.01, the two differing in nothing else), and that key-addressed replacement relocates the failure rather than removing it.
