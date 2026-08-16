# Memory cycle experiment

One question, answered with numbers: **which memory *system* — write path, read path, and store policy together — actually keeps facts in the model's context?** This is the second experiment in the series; the first (`memory-architecture/`) ranked stores assuming the write happens. QA pass #8 measured that it does not: gpt-4o emitted zero voluntary writeback blocks in 15/15 responses. This experiment tests the whole cycle.

## The question

The memory-architecture experiment answered "how should the store rank and forget". It left the cycle untested: how a fact gets written, and how it gets back into the model's context. A store's recall@5 is meaningless when nothing is ever written. The candidate systems below differ in the write path and read path, not the store.

## The method

Each candidate is a simulated agent loop over a scripted 20-turn conversation (a user plants preferences, identity facts, and corrections; checkpoint probes ask for them later). The score is **context-recall**: the fraction of checkpoint probes whose answer is present in the model's effective context at probe time.

Model behavior is simulated with measured or explicitly-marked compliance rates, not hopeful ones:

| Behavior | Rate | Basis |
|---|---|---|
| Voluntary writeback block | 0% | measured: gpt-4o-mini 0/7 (QA #5–#7), gpt-4o 0/15 (QA #8) |
| Structured output shape | 100% | API constrained decoding guarantees the shape |
| Structured output content (write the facts) | 50% / 80% | assumption — the shape is enforced, the content follows the model's engagement; both rates tested |
| Platform extraction | 100% | deterministic code |
| Forced narrow call | 95% | assumption — a one-sentence forced call is far easier to comply with than a per-response fenced block |
| On-demand search usage | 30% | assumption — a model often does not know what it does not know |

The store behind every candidate is the same (the adopted G6 scoring: salience + relevance + salience-scaled recency, supersession at cosine ≥ 0.6).

## The systems, in plain terms

- **N1 Voluntary writeback** (current production) — the contract asks the model to end every response with a fenced memory block; the platform parses it if present. Per-turn recall injection on the read side.
- **N2 Structured writeback** — the response itself is an API-enforced schema `{reply, memory_ops}`; the memory field cannot be absent. Two willingness rates tested (80%, and a lazy 50% variant N2s).
- **N3 Platform extraction** — no model involvement in writes; the platform pattern-matches fact-shaped statements from the user turn and stores them deterministically.
- **N4 Rolling narrative** — one living "what I know about the user" document, rewritten by a forced narrow model call each turn; the whole document is injected every turn.
- **N5 Log + on-demand search** — every turn stored verbatim; nothing injected; the model has a search tool and decides when to use it.
- **N6 Forced notes** — a forced narrow call writes a one-sentence note per turn; per-turn recall of notes.
- **N7 Bounded context** — no memory system; the window holds the last 10 turns and drops older ones.
- **N8 Structured + core block** — N2 plus the top-3 salience memories always injected regardless of the query.

## The results

| system | context-recall | store items | extra calls/turn | tokens/turn |
|---|---|---|---|---|
| N1 voluntary writeback | **0.00** | 0 | 0.00 | 0 |
| N2 structured writeback (80%) | **0.67** | 15 | 0.00 | 32 |
| N2s structured, lazy model (50%) | 0.39 | 11 | 0.00 | 29 |
| N3 platform extraction | 0.17 | 9 | 0.00 | 30 |
| N4 rolling narrative | 0.67 | 0 | 1.00 | 76 |
| N5 log + on-demand | 0.61 | 19 | 0.00 | 16 |
| N6 forced notes | 0.67 | 17 | 1.00 | 33 |
| N7 bounded context | 0.83 | 0 | 0.00 | 60 (grows, forgets old) |
| N8 structured + core block | 0.67 | 15 | 0.00 | 56 |

## What the numbers establish

1. **The write path is the whole game.** N1 scores 0.00 not because its store or recall is bad but because its write never fires. Every architectural investment behind a voluntary write path is wasted.
2. **Structured output is the smallest structural fix.** N2 moves recall from 0.00 to 0.67 with zero extra calls and modest tokens — the write becomes un-skippable because the response schema has no hole for "no memory this turn".
3. **Shape-enforcement is not content-enforcement.** N2s (lazy model, 50% fill) drops to 0.39. Structured output guarantees the field exists; the model's willingness to fill it is the residual variable. That willingness is guided by the contract text and is observable (empty ops on a fact-rich turn are visible in the writeback event).
4. **The read path is not the bottleneck here.** N4, N6, N8 all tie N2 at 0.67 despite costing more calls or tokens. The 0.67 ceiling is the write path's willingness, not recall quality.
5. **Bounded context (N7) wins the short game and loses the long one.** 0.83 at 20 turns, but it forgets everything older than the window — over a 10-year life that is almost everything. It is the reason long-term memory exists, not a candidate for replacing it.
6. **Platform extraction (N3) is too shallow.** Deterministic pattern-matching caught 0.17 — the right facts rarely look like the patterns.

## Adopted direction

The write path moves from a prompt convention to an API-enforced response schema (`response_format: json_schema` on the chat-completions call): the model's reply and its memory ops are one structured response. The store (ADR 0006, G6) and the per-turn recall injection are unchanged. The lazy-model residual (N2s) is mitigated by contract guidance and is observable through the writeback event stream.

## Reproducing

```
cd docs/experiments/memory-cycle
python3 harness/system_runner.py     # rewrites results_table.md and results_raw.json
python3 harness/claim_sources.py     # checks this document against both
```

The runner rewrites `results_table.md` and `results_raw.json` beside this document, whatever directory it is called from; the simulation is offline and neither script makes an API call. `claim_sources.py` reads the figures here against the run behind them and the compliance rates in `harness/systems.py`, and fails when a figure stops matching its source. The QA compliance measurements the method table cites are behavioural passes against a live model rather than runs of this harness, so it reports them as carried by no file here.
