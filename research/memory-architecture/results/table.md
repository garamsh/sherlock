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
- R/stored-KB: Recall per KB of the bytes actually stored. Not the
  R/modelled-KB the other result files report.
- supers: Supersession accuracy (latest version of fact appears in top-5)
- stale: Stale recall rate (% of retrieved items containing old/superseded content)
- rep-keep: Repetition-session items kept (lower = more compression)

| arch | LoC | items | KB | ins μs | qry μs | R@5 | P@5 | time@1 | noise | anti-poll | dedup | acc | R/stored-KB | supers | stale | rep-keep |
| arch | LoC | items | KB | ins μs | qry μs | R@5 | P@5 | time@1 | noise | anti-poll | dedup | acc | R/stored-KB | supers | stale | rep-keep |
|----|---|-----|--|------|------|---|---|------|-----|---------|-----|---|-----------|------|-----|--------|
| A0 | 28 | 1071 | 36.2 | 0.3 | 0.1 | 0.00 | 0.00 | 0.00 | 1.00 | 0.0 | 1.00 | 0.00 | 0.000 | 0.00 | 0.00 | 10 |
| A1 | 35 | 1071 | 36.2 | 0.4 | 0.1 | 0.00 | 0.00 | 0.00 | 1.00 | 0.0 | 1.00 | 0.00 | 0.000 | 0.00 | 0.00 | 10 |
| A2 | 34 | 1071 | 36.2 | 1.8 | 67.3 | 0.00 | 0.00 | 0.00 | 1.00 | 0.0 | 1.00 | 0.00 | 0.000 | 0.00 | 0.00 | 10 |
| B1 | 49 | 1071 | 36.2 | 708.8 | 4249.0 | 0.50 | 0.15 | 0.00 | 1.00 | 0.0 | 1.00 | 0.35 | 0.014 | 0.33 | 0.10 | 10 |
| B2 | 36 | 1071 | 36.2 | 6.2 | 16741.8 | 0.30 | 0.10 | 0.00 | 1.00 | 0.0 | 1.00 | 0.36 | 0.008 | 0.00 | 0.10 | 10 |
| B3 | 73 | 1071 | 36.2 | 738.6 | 24153.4 | 0.30 | 0.08 | 0.00 | 1.00 | 0.0 | 1.00 | 0.86 | 0.008 | 0.67 | 0.20 | 10 |
| C3 | 70 | 1071 | 36.2 | 681.2 | 21708.2 | 0.30 | 0.08 | 0.00 | 1.00 | 0.0 | 1.00 | 0.86 | 0.008 | 0.67 | 0.20 | 10 |
| D2 | 63 | 36 | 2.0 | 70.1 | 905.6 | 0.60 | 0.17 | 0.00 | 1.00 | 0.0 | 1.00 | 0.86 | 0.304 | 1.00 | 0.20 | 10 |
| E1 | 63 | 1071 | 36.2 | 704.2 | 21690.9 | 0.50 | 0.15 | 0.00 | 1.00 | 0.0 | 1.00 | 0.35 | 0.014 | 0.33 | 0.10 | 10 |
| E2 | 26 | 1071 | 36.2 | 716.9 | 24290.2 | 0.50 | 0.15 | 0.00 | 1.00 | 0.0 | 1.00 | 0.35 | 0.014 | 0.33 | 0.10 | 10 |
| E3 | 33 | 1071 | 36.2 | 733.7 | 22203.4 | 0.65 | 0.17 | 0.00 | 1.00 | 1.0 | 1.00 | 0.83 | 0.018 | 0.67 | 0.20 | 10 |
| E4 | 33 | 1071 | 36.2 | 715.3 | 23365.7 | 0.65 | 0.17 | 0.00 | 1.00 | 1.0 | 1.00 | 0.83 | 0.018 | 0.67 | 0.20 | 10 |
| F1 | 71 | 36 | 2.0 | 57.2 | 925.1 | 0.80 | 0.21 | 0.00 | 1.00 | 1.0 | 1.00 | 0.77 | 0.406 | 1.00 | 0.20 | 10 |
| F2 | 78 | 33 | 1.9 | 347.4 | 655.6 | 0.60 | 0.15 | 0.00 | 1.00 | 0.0 | 1.00 | 0.61 | 0.323 | 0.67 | 0.20 | 7 |
| F3 | 88 | 16 | 2.1 | 181.8 | 361.5 | 0.50 | 0.10 | 0.00 | 1.00 | 1.0 | 1.00 | 0.67 | 0.239 | 1.00 | 0.20 | 2 |
| F4 | 82 | 36 | 2.0 | 60.9 | 731.8 | 0.80 | 0.21 | 0.00 | 1.00 | 1.0 | 1.00 | 0.76 | 0.406 | 1.00 | 0.20 | 10 |
| F5 | 103 | 33 | 1.9 | 353.1 | 674.7 | 0.80 | 0.20 | 0.00 | 1.00 | 1.0 | 1.00 | 0.75 | 0.430 | 1.00 | 0.20 | 7 |
