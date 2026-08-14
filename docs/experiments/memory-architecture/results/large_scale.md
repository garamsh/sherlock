# Memory Architecture — Large-Scale Test (60k items)

10,000 real items (100 sessions × 100 turns) + 50,000 noise = 60,000 items. M-architectures (new paradigms) vs previous winners (D2, F5, G4, G6, H1, H6).

Note: H1 and H6 ran on a smaller workload (30 sessions × 100 turns = 3,000 real items) due to runtime cost.

| arch | R@5 | Items | KB | qry ms | R/KB |
|----|---|-----|--|------|----|
| M1 | 0.62 | 10000 | 500.0 | 68.9 | 1.260 |
| M2 | 0.08 | 10000 | 500.0 | 1425.9 | 0.158 |
| M3 | 0.62 | 10000 | 500.0 | 325.3 | 1.260 |
| M4 | 0.00 | 10000 | 500.0 | 0.1 | 0.000 |
| M5 | 0.54 | 207 | 10.4 | 0.7 | 53.274 |
| M6 | 0.46 | 150 | 7.5 | 0.4 | 63.015 |
| D2 | 0.69 | 10000 | 500.0 | 213.2 | 1.418 |
| F5 | 0.62 | 79 | 4.0 | 1.8 | 159.533 |
| G4 | 0.69 | 11250 | 562.5 | 238.6 | 1.260 |
| G6 | 0.54 | 79 | 4.0 | 1.6 | 139.591 |
| H1 | 0.62 | 488 | 24.4 | 10.5 | 25.826 |
| H6 | 0.00 | 436 | 21.8 | 0.0 | 0.000 |
