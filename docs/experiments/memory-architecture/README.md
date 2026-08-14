# Memory architecture experiment

The experiment that produced the evidence for ADR 0006's curation-only, salience-tiered memory model (revision of 2026-08-14). Report, harness, and raw results are all in this directory; `python3 runner.py` (or any of the other runners) reproduces the numbers offline.

## Question

Which memory architecture should a long-running agent use, when the agent is expected to live for years and accumulate memory continuously? The constraint from the owner: the structure must be intuitive and self-working — the structure itself does the work, not rules bolted on — and strong on the numbers.

## Method

- 32 candidate architectures implemented as small Python classes behind one `Memory` interface (`harness/architectures.py`). Each candidate encodes one structural idea.
- Four workloads (`harness/workload_spec.py`, `harness/diverse_workload.py`, plus generated scale workloads):
  - small — 5 sessions + 1000 noise turns
  - long-horizon — 9 sessions spanning a simulated 10-year agent life + 5000 noise
  - diverse — 35 sessions across 8 cognitive categories (person, event, routine, opinion, procedure, spatial, emotion, relationship) + 5000 noise
  - large-scale — 10,000 real items + 50,000 noise
- Metrics per workload: recall@5, precision@5, time-aware top-1, noise rejection, anti-pollution, dedup ratio, content accuracy, recall per KB (density), query latency, storage size, supersession accuracy, stale-recall rate, lines of code.
- Embeddings are simulated with deterministic bag-of-words hashing so the experiment runs offline; the structure comparison is unaffected.

## Architecture map

| Tier | IDs | Structural idea |
|---|---|---|
| Baseline | A0–A2 | no memory; uniform auto-capture; verbatim log |
| Retrieval | B1–B3 | keyword-only; semantic-only; hybrid RAG (the pre-experiment baseline) |
| Time | C3 | flat Ebbinghaus decay |
| Curation | D2 | store only what the model wrote back |
| Cognitive | E1–E4 | working-memory tier + LTM; + reconsolidation; + salience; + procedural |
| Storage | F1–F5 | salience-scaled Ebbinghaus; supersession; compression; tiered retention; combined |
| Research | G1–G9 | Generative Agents scoring (Park et al. 2023); MemoryBank Ebbinghaus; MemGPT hierarchy; reflection; cognitive index; decade-stable hybrid; recursive retrieval (2-hop, 3-hop); spreading activation (Collins & Loftus) |
| Hybrids | H1–H6 | explicit combinations of the proven mechanisms |
| Structural paradigms | M1–M6 | Hopfield associative memory; Kanerva sparse distributed memory; HNSW graph; LSH tables; working-memory + LRU; log-structured merge |

## Results

### Small workload (1k items)

Winner on density and recall: F5 (combined supersession + tiered retention). R@5 0.80, 91.7 recall/KB, 138 items. Auto-capture baselines (A1, A2) score 0.00 recall — the noise drowns everything.

### Long-horizon workload (10-year life)

Winner: G6 (decade-stable: Generative Agents scoring + salience-scaled Ebbinghaus + tiered retention + supersession). R@5 0.90, 292.6 recall/KB, 63 items. F5 regresses here (0.70) because uniform half-life cannot discriminate a 10-year-old preference from yesterday's chatter.

### Diverse workload (8 cognitive categories)

Winner on recall and precision: G4 (reflection — periodic summaries). R@5 0.87, P@5 0.39, accuracy 1.00. Winner on density: F5 (91.7 recall/KB). Recursive retrieval (G7–G9) matches single-hop (0.79) — multi-hop needs a connected corpus, and independent sessions are not one.

### Large-scale workload (60k items)

Winners on density with stable recall: F5 (159.5 recall/KB, 79 items) and G6 (139.6, 79 items). The pure-structure paradigms (M-series) hold up: LSM (M6) at 63.0 recall/KB with 150 items, working-memory+LRU (M5) at 53.3 with 207 items — both with no scoring formula at all. The triple hybrid H6 collapses (0.00) — three mechanisms interacting destructively, confirming that piling mechanisms on is the opposite of the goal.

## Conclusions

1. **Auto-capture is the root failure.** Every auto-capture architecture loses on every workload. This measurement is what retired ADR 0012.
2. **Curation-only is the floor.** Storing only model-written content is the precondition for every other win.
3. **Salience tier + salience-scaled decay + tiered retention + supersession** is the minimal set that holds recall across all four workloads while keeping the store two orders of magnitude smaller than the naive baselines.
4. **More mechanisms do not compose for free.** The single-combination hybrids (H1–H5) are middling; the triple hybrid (H6) breaks. The adopted model is the smallest mechanism set with the full evidence base.
5. **Recursive retrieval is real but conditional.** It helps when the corpus is connected (a codebase, a long conversation); the experiment's fragmented sessions gave it nothing to traverse.

## Adopted

ADR 0006 (2026-08-14 revision) records the model this evidence selected: curation-only storage, platform-derived salience, salience-scaled Ebbinghaus recency, tiered retention, supersession on insert. The implementation landed in #322.

## Harness

- `harness/interface.py` — the `Memory` ABC every candidate implements
- `harness/architectures.py` — the 32 implementations
- `harness/workload_spec.py`, `harness/diverse_workload.py` — the workloads
- `harness/metrics.py` — the metric functions
- `harness/runner.py`, `scale_runner.py`, `long_horizon_runner.py`, `diverse_runner.py`, `hybrid_runner.py`, `large_runner.py` — the six runners
- `results/` — every table and raw JSON from every round

Reproduce: `cd harness && python3 ../harness/runner.py` (or any other runner) from this directory's parent; output lands in `results/`.
