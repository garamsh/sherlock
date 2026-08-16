# Memory Architecture Experiment — Results

Architectures:<br/>A0..A2 = simple & baseline; B1..B3 = retrieval; C3 = time-aware; D2 = curation; E1..E4 = cognitive; F1..F5 = efficiency.

Metrics:
- LoC: lines of code in the architecture class
- items: number of items stored
- KB: storage size of stored contents (KB)
- ins μs / qry μs: average insert / query time (microseconds)
- R@5: Recall@5 (any expected keyword in top-5)
- P@5: Precision@5 (% of top-5 that are relevant)
- time@1: Time-aware top-1 (recency queries return most recent at position 1)
- noise: Noise rejection (% of noise queries that return no relevant content)
- anti-poll: Anti-pollution (recall after 1000 noise turns)
- dedup: Unique items / total items
- accuracy: % of retrieved chunks with content from a known keyword family
- R/KB: Recall per KB of storage (effective memory density)
- supers: Supersession accuracy (latest version of fact appears in top-5)
- stale: Stale recall rate (% of retrieved items containing old/superseded content)
- rep-keep: Repetition-session items kept (lower = more compression)

| arch | LoC | items | KB | ins μs | qry μs | R@5 | P@5 | time@1 | noise | anti-poll | dedup | acc | R/KB | supers | stale | rep-keep |
| arch | LoC | items | KB | ins μs | qry μs | R@5 | P@5 | time@1 | noise | anti-poll | dedup | acc | R/KB | supers | stale | rep-keep |
|----|---|-----|--|------|------|---|---|------|-----|---------|-----|---|----|------|-----|--------|
| A0 | 28 | 1071 | 36.2 | 3.0 | 0.3 | 0.00 | 0.00 | 0.00 | 1.00 | 0.0 | 1.00 | 0.00 | 0.000 | 0.00 | 0.00 | 0 |
| A1 | 35 | 1071 | 36.2 | 0.7 | 0.3 | 0.00 | 0.00 | 0.00 | 1.00 | 0.0 | 1.00 | 0.00 | 0.000 | 0.00 | 0.00 | 0 |
| A2 | 34 | 1071 | 36.2 | 0.5 | 560.8 | 0.00 | 0.00 | 0.00 | 1.00 | 0.0 | 1.00 | 0.00 | 0.000 | 0.00 | 0.00 | 0 |
| B1 | 49 | 1071 | 36.2 | 6670.9 | 45168.8 | 0.50 | 0.15 | 0.00 | 1.00 | 0.0 | 1.00 | 0.35 | 0.000 | 0.33 | 0.10 | 0 |
| B2 | 36 | 1071 | 36.2 | 74.3 | 125286.9 | 0.30 | 0.10 | 0.00 | 1.00 | 0.0 | 1.00 | 0.36 | 0.000 | 0.00 | 0.10 | 0 |
| B3 | 73 | 1071 | 36.2 | 5319.8 | 247829.3 | 0.30 | 0.08 | 0.00 | 1.00 | 0.0 | 1.00 | 0.86 | 0.000 | 0.67 | 0.20 | 0 |
| C3 | 70 | 1071 | 36.2 | 7540.0 | 299948.7 | 0.30 | 0.08 | 0.00 | 1.00 | 0.0 | 1.00 | 0.86 | 0.000 | 0.67 | 0.20 | 0 |
| D2 | 63 | 36 | 2.0 | 501.4 | 4780.8 | 0.60 | 0.17 | 0.00 | 1.00 | 0.0 | 1.00 | 0.86 | 0.000 | 1.00 | 0.20 | 0 |
| E1 | 63 | 1071 | 36.2 | 7279.7 | 187360.6 | 0.50 | 0.15 | 0.00 | 1.00 | 0.0 | 1.00 | 0.35 | 0.000 | 0.33 | 0.10 | 0 |
| E2 | 26 | 1071 | 36.2 | 8723.5 | 205282.4 | 0.50 | 0.15 | 0.00 | 1.00 | 0.0 | 1.00 | 0.35 | 0.000 | 0.33 | 0.10 | 0 |
| E3 | 33 | 1071 | 36.2 | 7074.3 | 200823.4 | 0.65 | 0.17 | 0.00 | 1.00 | 1.0 | 1.00 | 0.83 | 0.000 | 0.67 | 0.20 | 0 |
| E4 | 33 | 1071 | 36.2 | 5568.6 | 252840.7 | 0.65 | 0.17 | 0.00 | 1.00 | 1.0 | 1.00 | 0.83 | 0.000 | 0.67 | 0.20 | 0 |
| F1 | 71 | 36 | 2.0 | 1226.4 | 9268.7 | 0.80 | 0.21 | 0.00 | 1.00 | 1.0 | 1.00 | 0.77 | 0.000 | 1.00 | 0.20 | 0 |
| F2 | 78 | 33 | 1.9 | 3317.6 | 6639.1 | 0.60 | 0.15 | 0.00 | 1.00 | 0.0 | 1.00 | 0.61 | 0.000 | 0.67 | 0.20 | 0 |
| F3 | 88 | 16 | 2.1 | 1969.6 | 2938.9 | 0.50 | 0.10 | 0.00 | 1.00 | 1.0 | 1.00 | 0.67 | 0.000 | 1.00 | 0.20 | 0 |
| F4 | 82 | 36 | 2.0 | 610.4 | 11813.8 | 0.80 | 0.21 | 0.00 | 1.00 | 1.0 | 1.00 | 0.76 | 0.000 | 1.00 | 0.20 | 0 |
| F5 | 103 | 33 | 1.9 | 4851.5 | 7971.5 | 0.80 | 0.20 | 0.00 | 1.00 | 1.0 | 1.00 | 0.75 | 0.000 | 1.00 | 0.20 | 0 |
