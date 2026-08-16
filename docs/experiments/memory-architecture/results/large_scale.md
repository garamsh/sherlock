# Memory Architecture — Large-Scale Test (60k items)

10,000 real items (100 sessions × 100 turns) + 50,000 noise = 60,000 items. M-architectures (new paradigms) vs previous winners (D2, F5, G4, G6, H1, H6).

Note: H1 and H6 ran on a smaller workload (30 sessions × 100 turns = 3,000 real items) due to runtime cost.

| arch | R@5 | Items | KB | qry ms | R/KB |
|----|---|-----|--|------|----|
| M1 | 0.69 | 10000 | 500.0 | 91.6 | 1.418 |
| M2 | 0.08 | 10000 | 500.0 | 1857.1 | 0.158 |
| M3 | 0.54 | 10000 | 500.0 | 457.4 | 1.103 |
| M4 | 0.00 | 10000 | 500.0 | 0.1 | 0.000 |
| M5 | 0.77 | 207 | 10.4 | 0.7 | 76.106 |
| M6 | 0.54 | 150 | 7.5 | 0.5 | 73.518 |
| D2 | 0.85 | 10000 | 500.0 | 242.3 | 1.733 |
| F5 | 0.69 | 83 | 4.2 | 1.9 | 170.825 |
| G4 | 0.85 | 11250 | 562.5 | 458.5 | 1.540 |
| G6 | 0.69 | 83 | 4.2 | 3.9 | 170.825 |
| H1 | 0.77 | 485 | 24.2 | 13.5 | 32.482 |
| H6 | 0.00 | 431 | 21.6 | 0.0 | 0.000 |
