# Research

Research records the questions, methods, evidence, conclusions, and limits behind this repository's studies. Current system responsibilities and decisions live in [architecture](../architecture/README.md); study reports link to them where relevant.

## Studies

- [Memory architecture](memory-architecture/README.md) — 38 candidate memory-store and retrieval architectures measured across four workloads.
- [Memory correction](memory-correction/README.md) — a recorded correction and reversal run.
- [Memory creation](memory-creation/README.md) — creation, correction, and retirement of memory entries.
- [Memory cycle](memory-cycle/README.md) — the write path, read path, and store policy measured together; the voluntary writeback is the root failure (0.00 context-recall).
- [Memory frames](memory-frames/README.md) — memory structures measured over a decade of turns.
- [Memory retrieval](memory-retrieval/README.md) — retrieval and supersession behavior of the former store.
- [Mutation survey](mutation-survey/README.md) — behavior that the repository's tests did or did not catch.
- [Recall specificity](recall-specificity/README.md) — the former agent's answers to questions at three levels of specificity.
- [Task continuation](task-continuation/README.md) — continuation across live history, process restart, and artifacts alone.

## Recording a study

Give each study its own directory and Markdown report beside its evidence and any study-specific harness. State the question, method and reproduction steps, evidence and results with links to raw files, conclusion, and limits. Identify the source revision and runtime measured so historical results are not read as measurements of the current system. Keep original evidence intact when updating a report, and link to current decisions in `docs/architecture/` rather than copying them.

Shared utilities, when one has a stable use across studies, belong in [tools](tools/README.md). Paths beginning `docs/experiments/` in historical report descriptions and raw output name the layout at the time of those runs; active commands and links use `docs/research/`.
