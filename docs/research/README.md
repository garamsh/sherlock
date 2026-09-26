# Research

Research records the questions, methods, evidence, conclusions, and limits behind this repository's studies. Current system responsibilities and decisions live in [architecture](../architecture/README.md); study reports link to them where relevant.

## Studies

- [Agent context](agent-context/README.md) — source survey of how Claude Code, Codex CLI, Hermes Agent, OpenClaw and opencode understand input and compose context, and three candidate structures for sherlock; the preregistered test of two (#862) applies the evidence in 209 (search during work with a note) and 214 (understand first with a note) of 280 primary probes against the shipped recall's 124, but is undetermined because the oracle-reach control failed for the shipped arm and the search arm; a confirmatory run (#864) with a source-level oracle repeats the gain (192 against 122) and is undetermined again, every oracle miss being a ranking miss of a mid that states the key; a third run on unmixed evidence batches (#870) passes every control and finds A valid, 212 against 171 (A only 42, S only 1, p = 1.0e-11).
- [Memory architecture](memory-architecture/README.md) — 38 candidate memory-store and retrieval architectures measured across four workloads.
- [Memory correction](memory-correction/README.md) — a recorded correction and reversal run.
- [Memory creation](memory-creation/README.md) — creation, correction, and retirement of memory entries.
- [Memory cycle](memory-cycle/README.md) — the write path, read path, and store policy measured together; the voluntary writeback is the root failure (0.00 context-recall).
- [Memory frames](memory-frames/README.md) — memory structures measured over a decade of turns.
- [Memory read policy](memory-read-policy/README.md) — direct, rewritten and iterative retrieval on two implicit tasks and exact/absent controls; source inspection and application remain limiting.
- [Memory retrieval](memory-retrieval/README.md) — retrieval and supersession behavior of the former store.
- [Mid-memory retrieval](mid-memory-rag/README.md) — shipped phrase-call FTS recall against separate embedding-similarity and recency lists over 300 synthesized mids; 29 against 27 of 40 primary queries, undetermined at p = 0.69; a powered replication over 280 queries finds the owner's structure (+9.3 points) and a hybrid with shipped recall (+12.5 points) valid, and both grow the code; the production recall #858 built from it delivers 212 of 280.
- [Mutation survey](mutation-survey/README.md) — behavior that the repository's tests did or did not catch.
- [Recall specificity](recall-specificity/README.md) — the former agent's answers to questions at three levels of specificity.
- [Task continuation](task-continuation/README.md) — continuation across live history, process restart, and artifacts alone.

## Recording a study

Give each study its own directory and Markdown report beside its evidence and any study-specific harness. State the question, method and reproduction steps, evidence and results with links to raw files, conclusion, and limits. Identify the source revision and runtime measured so historical results are not read as measurements of the current system. Keep original evidence intact when updating a report, and link to current decisions in `docs/architecture/` rather than copying them.

Place a utility under `docs/research/tools/` when its documented inputs and outputs can serve future studies without study-specific changes and it has a credible long-term use beyond one study. Keep study-specific harnesses beside their reports. Paths beginning `docs/experiments/` in historical report descriptions and raw output name the layout at the time of those runs; active commands and links use `docs/research/`.
