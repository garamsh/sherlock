# Round 5 harness

What round 5 changes in the memory-creation dataset and scoring, and what it holds fixed. Numbers a run produces belong in the experiment record, not here.

## What changes

- The coexist subjects come from the seeded generator instead of the first four persons, and each subject's values are drawn from its family's pool, so a second seed measures a second coexist set rather than the same one twice.
- Eight coexist families, 64 coexist probes and 144 coexist statements per seed. Every subject carries every family and every family draws a fixed number of values, so each family is an eighth of the coexist column under any seed.
- No coexist family names or answers a topic in the corrected and control set. Round 4's spoken-`languages` family sat beside a `language` category holding programming languages; it is gone.
- `score()` reports the preserved fraction per coexist family beside `coexist_preserved`, and the runner writes it into the result JSON.

## Why

Three bullets in the experiment record's Limits section describe one defect in the coexist column:

- The coexist set does not vary with the seed: 12 probes and the 28 fact turns behind them are identical under every seed, so a structure scored on two seeds is one coexist measurement reported twice.
- One family holds 12 of the 28 values, so a single prompt example moves the column by 0.43, and one probe is 8.3% of a column that carries a third of `net`.
- That column is also the noisiest in the round — 0.167 of it moved between two seeds on identical data.

At 64 probes one probe is 1.6% of the column and one family 12.5%, and the sampling error the record computes for `net` falls from ±0.083 to ±0.047 (`harness/round4/precision.py` holds the formula and the counts it assumes).

## What stays fixed

- `harness/round4/` is untouched. Round 5 imports its `api.py`, `structures.py`, `structures_a.py` and `structures_b.py`, and takes the persons, categories, chatter, probe shape and metric definitions from its `dataset.py` and `scoring.py`.
- The corrected, control and distractor assignments are round 4's for the same seed, so the coexist set is the only difference between the two datasets. `dataset_check.py` asserts this.
- The store grows rather than shrinks: 645 turns and 504 fact turns per seed against round 4's 488 and 388. `n_distractor` stays at 120 and the coexist statements are added to the stream, not traded against it.

## Files

- `dataset.py` — the turn stream and the probes.
- `scoring.py` — round 4's metrics plus `coexist_by_family`.
- `runner.py` — runs the named structures and writes `results_round5/<structure>_seed<seed>.json`.
- `dataset_check.py` — the invariants above, on seeds 7, 11 and 23.
- `round4.py` — loads round 4's modules by path, because both rounds name a module `dataset` and a module `scoring`.

## Running

```
cd docs/experiments/memory-creation
python3 harness/round5/dataset_check.py
python3 harness/round5/runner.py DeciderNeighbors
SEED=11 python3 harness/round5/runner.py DeciderNeighbors
```

The check makes no API call and prints the composition of each seed. The runner needs `OPENAI_API_KEY`; embeddings and decider replies cache beside round 4's `api.py`, as round 4's runners do.
