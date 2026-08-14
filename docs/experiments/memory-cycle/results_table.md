# System-level memory experiment — the full cycle

Question: which memory *system* — write path, read path, store policy together — keeps facts in the model's context over a multi-week conversation? The previous experiments ranked stores assuming the write happens; QA measured that it does not (0/15 voluntary writeback on gpt-4o).

Compliance inputs are measured or marked assumptions: voluntary writeback 0% (QA #5-#8), structured output 100% (API-enforced), platform extraction 100% (deterministic), forced narrow call 95% (assumption), on-demand search 30% (assumption).

| system | context-recall | store items | extra calls/turn | injected tokens/turn |
|------|--------------|-----------|----------------|--------------------|
| N1-voluntary-writeback | 0.00 | 0 | 0.00 | 0 |
| N2-structured-writeback | 0.67 | 15 | 0.00 | 32 |
| N2s-structured-lazy | 0.39 | 11 | 0.00 | 29 |
| N3-platform-extraction | 0.17 | 9 | 0.00 | 30 |
| N4-rolling-narrative | 0.67 | 0 | 1.00 | 76 |
| N5-log-ondemand | 0.61 | 19 | 0.00 | 16 |
| N6-forced-notes | 0.67 | 17 | 1.00 | 33 |
| N7-full-context | 0.83 | 0 | 0.00 | 60 |
| N8-structured-coreblock | 0.67 | 15 | 0.00 | 56 |
