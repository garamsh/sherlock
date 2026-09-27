# Memory Architecture — Large-Scale Test (60k items)

10,000 real items (100 sessions × 100 turns) + 50,000 noise = 60,000 items. M-architectures (new paradigms) vs previous winners (D2, F5, G4, G6, H1, H6).

Note: H1 and H6 ran on a smaller workload (30 sessions × 100 turns = 3,000 real items) due to runtime cost.

| arch | R@5 | Items | modelled KB | qry ms | R/modelled-KB |
|----|---|-----|-----------|------|-------------|
| M1 | 0.69 | 10000 | 500.0 | 72.5 | 0.001385 |
| M2 | 0.08 | 10000 | 500.0 | 1550.4 | 0.0001538 |
| M3 | 0.54 | 10000 | 500.0 | 355.6 | 0.001077 |
| M4 | 0.00 | 10000 | 500.0 | 0.1 | 0 |
| M5 | 0.77 | 207 | 10.4 | 0.6 | 0.07432 |
| M6 | 0.54 | 150 | 7.5 | 0.3 | 0.07179 |
| D2 | 0.85 | 10000 | 500.0 | 281.5 | 0.001692 |
| F5 | 0.69 | 83 | 4.2 | 2.5 | 0.1668 |
| G4 | 0.85 | 11250 | 562.5 | 260.4 | 0.001504 |
| G6 | 0.69 | 83 | 4.2 | 1.7 | 0.1668 |
| H1 | 0.77 | 485 | 24.2 | 10.6 | 0.03172 |
| H6 | 0.00 | 431 | 21.6 | 0.0 | 0 |
