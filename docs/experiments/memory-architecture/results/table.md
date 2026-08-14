# Memory Architecture Experiment — Results

Architectures ordered: simple (A0..A2) → retrieval (B1..B3) → time-aware (C3) → curation (D2) → cognitive (E1..E4).

Metrics:
- LoC: lines of code in the architecture class
- items: number of items stored
- KB: storage size of stored contents (KB)
- ins μs: average insert time (microseconds)
- qry μs: average query time (microseconds)
- R@5: Recall@5 (any expected keyword in top-5)
- P@5: Precision@5 (% of top-5 that are relevant)
- time@1: Time-aware top-1 (recency queries return most recent at position 1)
- noise: Noise rejection (% of noise queries that return no relevant content)
- anti-poll: Anti-pollution (recall after 1000 noise turns)
- dedup: Unique items / total items
- accuracy: % of retrieved chunks with content from a known keyword family

| arch | LoC | items | KB | ins μs | qry μs | R@5 | P@5 | time@1 | noise | anti-poll | dedup | accuracy |
| arch | LoC | items | KB | ins μs | qry μs | R@5 | P@5 | time@1 | noise | anti-poll | dedup | accuracy |
|----|---|-----|--|------|------|---|---|------|-----|---------|-----|--------|
| A0 | 28 | 1071 | 36.2 | 0.3 | 0.1 | 0.00 | 0.00 | 0.00 | 1.00 | 0.0 | 1.00 | 0.00 |
| A1 | 35 | 1071 | 36.2 | 0.4 | 0.2 | 0.00 | 0.00 | 0.00 | 1.00 | 0.0 | 1.00 | 0.00 |
| A2 | 34 | 1071 | 36.2 | 0.3 | 62.8 | 0.00 | 0.00 | 0.00 | 1.00 | 0.0 | 1.00 | 0.00 |
| B1 | 49 | 1071 | 36.2 | 690.5 | 4118.2 | 0.50 | 0.15 | 0.00 | 1.00 | 0.0 | 1.00 | 0.35 |
| B2 | 36 | 1071 | 36.2 | 6.2 | 16974.9 | 0.30 | 0.10 | 0.00 | 1.00 | 0.0 | 1.00 | 0.36 |
| B3 | 73 | 1071 | 36.2 | 698.8 | 21713.1 | 0.30 | 0.08 | 0.00 | 1.00 | 0.0 | 1.00 | 0.86 |
| C3 | 70 | 1071 | 36.2 | 717.0 | 21944.2 | 0.30 | 0.08 | 0.00 | 1.00 | 0.0 | 1.00 | 0.86 |
| D2 | 63 | 36 | 2.0 | 55.5 | 700.9 | 0.60 | 0.17 | 0.00 | 1.00 | 0.0 | 1.00 | 0.86 |
| E1 | 63 | 1071 | 36.2 | 735.2 | 22720.5 | 0.50 | 0.15 | 0.00 | 1.00 | 0.0 | 1.00 | 0.35 |
| E2 | 26 | 1071 | 36.2 | 728.2 | 23049.2 | 0.50 | 0.15 | 0.00 | 1.00 | 0.0 | 1.00 | 0.35 |
| E3 | 33 | 1071 | 36.2 | 726.5 | 22591.0 | 0.65 | 0.17 | 0.00 | 1.00 | 1.0 | 1.00 | 0.83 |
| E4 | 33 | 1071 | 36.2 | 728.1 | 22055.1 | 0.65 | 0.17 | 0.00 | 1.00 | 1.0 | 1.00 | 0.83 |
| F1 | 0 | — | 0.0 | 0.0 | 0.0 | 0.00 | 0.00 | 0.00 | 0.00 | — | 0.00 | 0.00 |
| F2 | 0 | — | 0.0 | 0.0 | 0.0 | 0.00 | 0.00 | 0.00 | 0.00 | — | 0.00 | 0.00 |
| F3 | 0 | — | 0.0 | 0.0 | 0.0 | 0.00 | 0.00 | 0.00 | 0.00 | — | 0.00 | 0.00 |
| F4 | 0 | — | 0.0 | 0.0 | 0.0 | 0.00 | 0.00 | 0.00 | 0.00 | — | 0.00 | 0.00 |
| F5 | 0 | — | 0.0 | 0.0 | 0.0 | 0.00 | 0.00 | 0.00 | 0.00 | — | 0.00 | 0.00 |
