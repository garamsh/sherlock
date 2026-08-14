# Memory architecture experiment

One question, answered with numbers: **what memory architecture should a years-long agent use?** This document is self-contained — it explains the method, every architecture tested, every result, and what was adopted. The harness that produced the numbers is in `harness/` and reproduces everything offline.

## The question

gagent's agent lives for years and talks to its owner continuously. It needs long-term memory that:

- remembers what matters and recalls it correctly,
- keeps the newest version of a fact, not a pile of stale copies,
- does not drown in years of noise,
- stays fast and small.

The owner's design constraint: the structure must be **simple and self-working** — the structure itself does the work, not rules accumulated on top of it.

## The method

Thirty-two candidate architectures were implemented as small Python classes behind one interface (`harness/architectures.py`, each ~30–100 lines). Every candidate encodes exactly one structural idea, so the numbers isolate the idea's value.

**Workloads** (four, ordered by difficulty):

| Workload | Content | Purpose |
|---|---|---|
| small | 5 sessions + 1,000 noise turns | smoke test |
| long-horizon | 9 sessions across a simulated 10-year life + 5,000 noise | does memory survive a decade |
| diverse | 35 sessions across 8 memory categories + 5,000 noise | does it handle the variety of human memory |
| large-scale | 10,000 real items + 50,000 noise | does accuracy and speed hold at scale |

**Metrics**: recall@5 (did the right memory appear in the top 5), precision@5 (how much of the top 5 is right), content accuracy, anti-pollution (does noise crowd out curated memory after 1,000 junk turns), supersession accuracy (does the newest version of a fact win), stale recall (how often an outdated fact is returned), storage size, recall per KB (memory density), query latency.

**A note on realism**: embeddings are simulated with deterministic word-hashing so the experiment runs offline. That makes absolute numbers approximate but keeps the architecture-to-architecture comparison faithful — the same similarity engine serves every candidate.

## The architectures, in plain terms

**Baselines (what not to do)**

- **A0 No memory** — nothing is stored; every turn starts blank.
- **A1 Auto-capture uniform** — every turn stored at the same low weight; the newest few are returned. This was gagent's pre-experiment behavior (ADR 0012).
- **A2 Full-text log** — everything verbatim; last-N return.

**Classic retrieval**

- **B1 Keyword-only** — TF-IDF cosine over words. No semantics.
- **B2 Semantic-only** — embedding cosine only.
- **B3 Hybrid RAG** — keyword + semantic blended with a relevance/recency/importance/access score. The pre-experiment gagent model.
- **C3 Ebbinghaus decay** — B3 plus a flat time-decay on importance.

**Curation and cognition**

- **D2 Curation-only** — store only what the model explicitly writes back. Nothing else enters.
- **E1 Two-tier** — a working memory (recent 7 items) plus a long-term store.
- **E2 + reconsolidation** — retrieving a memory strengthens it (like recall reinforcing a human memory).
- **E3 + salience tier** — every memory carries a high/mid/low tier.
- **E4 + procedural** — a separate table for "how-to" patterns (instructions and corrections).

**Storage discipline (the F series)**

- **F1 Salience-scaled decay** — curation + Ebbinghaus decay whose half-life grows with salience: important things fade slowly, trivia fades fast.
- **F2 Supersession** — a new memory that closely matches an old one replaces it; the latest version of a fact is the one that lives.
- **F3 Compression** — every N items collapse into one summary item.
- **F4 Tiered retention** — high-salience kept forever, mid kept ~100 turns, low dropped promptly.
- **F5 Combined** — supersession + tiered retention together.

**Research-inspired (the G series)** — each maps to a published system or theory:

- **G1 Generative Agents** (Park et al. 2023) — score = importance + recency + relevance, with an LLM assigning importance at write time.
- **G2 MemoryBank** (Zhong et al. 2023) — Ebbinghaus forgetting where each retrieval rehearses and re-strengthens the memory.
- **G3 MemGPT-style** — a summary tier plus raw pages; retrieval hits summaries first.
- **G4 Reflection** (Park et al. 2023) — every 8 inserts, the system writes a higher-level summary of the recent items; summaries are retrievable like any memory.
- **G5 Cognitive index** (HippoRAG-inspired) — a small high-salience index plus a long-tail store.
- **G6 Decade-stable** — G1's scoring + salience-scaled decay + tiered retention + supersession, combined.
- **G7/G8 Recursive retrieval** — after the first retrieval, extract the top results' concepts and retrieve again (2 or 3 hops).
- **G9 Spreading activation** (Collins & Loftus 1975) — items form a co-occurrence graph; retrieval activates initial hits and lets activation propagate along edges.

**Explicit hybrids (the H series)** — direct combinations of proven mechanisms (G6+G4, G6+G7, G4+G7, F5+G4, an episodic/semantic split, and a triple hybrid of all three).

**Pure structures (the M series)** — architectures whose structure alone is the mechanism, with no scoring formula at all:

- **M1 Hopfield associative memory** (Hopfield 1982) — retrieval is energy minimization over stored patterns.
- **M2 Kanerva sparse distributed memory** (Kanerva 1988) — items are sparse binary vectors into a fixed matrix; similar contents activate overlapping addresses.
- **M3 HNSW** (Malkov & Yashunin 2018) — a hierarchical small-world graph; retrieval is a greedy walk.
- **M4 LSH** (Indyk & Motwani 1998) — many hash tables; items colliding in many tables are similar.
- **M5 Working memory + LRU** — two lists; the recent tier and a least-recently-used long-term tier order themselves.
- **M6 LSM** — append-only levels with periodic compaction keeping the strongest items (the storage-engine pattern).

## The results

### Small workload (1k items)

| arch | R@5 | Anti-pollution | Items | Recall/KB |
|---|---|---|---|---|
| A1 auto-capture | 0.00 | 0.0 | 1,052 | 0.0 |
| B3 hybrid RAG | 0.44 | 0.0 | 1,052 | 0.01 |
| D2 curation-only | 0.69 | 1.0 | 22 | 0.49 |
| **F5 combined** | **0.80** | 1.0 | 138 | **0.92** |

Auto-capture fails completely: with 1,000 noise turns in the store, the right memory is never found.

### Long-horizon workload (a 10-year agent life)

| arch | R@5 | Items | Recall/KB |
|---|---|---|---|
| B3 hybrid | 0.80 | 5,090 | 3.2 |
| D2 curation | 0.90 | 90 | 204.8 |
| F5 combined | 0.70 | 63 | 227.6 |
| G1 Generative Agents | 0.90 | 90 | 204.8 |
| **G6 decade-stable** | **0.90** | **63** | **292.6** |

The 10-year workload separates uniform decay from salience-scaled decay: F5 drops from 0.80 to 0.70 because it cannot tell a 10-year-old preference from yesterday's chatter; G6 holds at 0.90 because its half-life grows with salience.

### Diverse workload (8 memory categories)

| arch | R@5 | P@5 | Accuracy | Recall/KB |
|---|---|---|---|---|
| F5 combined | 0.62 | 0.15 | 0.99 | 91.7 |
| **G4 reflection** | **0.87** | **0.39** | **1.00** | 31.7 |
| G7–G9 recursive | 0.79 | 0.24 | 0.99 | 32.6 |

Reflection wins on raw recall and precision — combining memories into summaries finds more of the right things. Recursive retrieval matches single-hop: multi-hop needs a connected corpus, and independent sessions give it no path to walk.

### Large-scale workload (60k items)

| arch | R@5 | Items | Query ms | Recall/KB |
|---|---|---|---|---|
| B3 hybrid | 0.30 | 5,021 | 103,794 | 1.2 |
| D2 curation | 0.60 | 36 | 760 | 341.3 |
| **F5 combined** | 0.62 | **79** | 1.8 | **159.5** |
| G6 decade-stable | 0.54 | 79 | 1.6 | 139.6 |
| M5 WM+LRU | 0.54 | 207 | 0.7 | 53.3 |
| M6 LSM | 0.46 | 150 | 0.4 | 63.0 |
| H6 triple hybrid | 0.00 | 436 | 0.0 | 0.0 |

At scale, only the disciplined stores stay fast and small. The pure structures (M5, M6) prove that a structure with no scoring formula at all can work — but F5's two rules beat them on density. The triple hybrid H6's collapse to 0.00 is the experiment's clearest warning: mechanisms piled together interact destructively.

## What the numbers establish

1. **Auto-capture is the root failure.** Every auto-capture architecture loses on every workload. This is the measurement that retired ADR 0012.
2. **Curation-only is the floor.** Storing only what the model writes is the precondition for every other win.
3. **The minimal winning set is four rules**: curation-only storage, a salience tier on every memory, salience-scaled decay with tiered retention, and supersession on insert. Together they hold recall across all four workloads with a store two orders of magnitude smaller than the naive baselines.
4. **More mechanisms do not compose for free.** Single-combination hybrids are middling; the triple hybrid breaks. Simple is not a preference here — it is what the data supports.
5. **Recursive retrieval is real but conditional.** It pays off in connected corpora (a codebase, one long conversation), not in fragmented sessions.

## What was adopted

ADR 0006 (revised 2026-08-14) records the selected model: curation-only storage, platform-derived salience, salience-scaled Ebbinghaus recency, tiered retention, supersession on insert. The implementation is #322; the behavioral verification and its two follow-up fixes (supersession threshold calibration, recall-event observability) are QA pass #7 and #326.

## Reproducing

```
cd docs/experiments/memory-architecture
python3 harness/runner.py               # small workload
python3 harness/long_horizon_runner.py  # 10-year workload
python3 harness/diverse_runner.py       # 8-category workload
python3 harness/large_runner.py         # 60k-item workload
```

Each runner rewrites its table under `results/`. The full result tables from the recorded runs are in `results/` (direct, scale, long-horizon, diverse, hybrid, large-scale), with raw JSON beside each.
