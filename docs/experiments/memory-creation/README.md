# Memory-creation experiment

One question, answered with numbers: **how should a memory entry come to exist — who decides, with what vocabulary, and when does maintenance happen?** This is the third experiment in the series. The cycle experiment (`memory-cycle/`) established that the write path must be shape-enforced; this experiment compares the structures that can sit behind that enforcement.

## The question

The adopted memory model (ADR 0006) writes through an ops vocabulary (`keep`/`update`/`delete`/`add` over recalled IDs) emitted by the main model inside its response. The cycle experiment showed the voluntary form scores 0.00 — but even with the shape enforced (structured output), the ops vocabulary is one choice among many. The owner asked for the alternatives to be measured: everything-as-update, a dedicated memory model, a journal with background distillation, freeform notes, entity records, temporal claims, and append-only with read-time latest-wins.

## The method

Each candidate is a small simulated write path over the same scripted 20-turn conversation used in the cycle experiment (identity facts, preferences, corrections planted; checkpoint probes ask for them). The store's supersession path (cosine ≥ 0.6 replaces) is shared. Scores: checkpoint recall (is the right fact in the store at probe time), correction latest-wins (after "I changed my mind", is the new fact present and the old one gone or superseded), store size, extra model calls per turn, and class size (LoC).

Compliance inputs are the same measured or marked rates as the cycle experiment: voluntary 0% (measured), structured main-response field 80%, dedicated narrow call 95%, deterministic platform paths 100%.

## The structures, in plain terms

- **C1 Ops vocabulary** (current production) — the main model writes `keep`/`update`/`delete`/`add` ops over recalled IDs, in a fenced block at the end of each response. Voluntary.
- **C2 Everything-is-update** — the store is a set of topic documents; every write is an update to a topic (created if missing). No delete op; stale topics decay out. Structured field on the main response.
- **C3 Dedicated memory model** — a separate narrow model call after each turn reads the turn and decides what to write. The main model never sees the memory protocol at all. This is the "subconscious" shape: conversation and memory are separate layers with separate deciders.
- **C4 Journal + distillation** — the platform appends every turn to a verbatim journal (deterministic); a background call periodically distills the journal into facts.
- **C5 Freeform notes** — the main model writes unstructured "worth remembering" text in a structured field; the platform splits, embeds, and supersedes by similarity.
- **C6 Entity records** — the store is keyed by entity (Lisa, Otto, Postgres); writes upsert entity attributes, like a CRM.
- **C7 Claims with temporal ordering** — every write is a timestamped claim; there is no update op; at read time the newest claim about a subject wins.
- **C8 Append-only** — never update, never delete; every write appends; read-time latest-wins across similarity clusters.

## The results

| system | checkpoint recall | latest-wins | items | calls/turn | LoC |
|---|---|---|---|---|---|
| C1 ops-voluntary (current) | **0.00** | 0.00 | 0 | 0.00 | 16 |
| C2 everything-update | 0.79 | 0.00 | 17 | 0.00 | 28 |
| **C3 dedicated model** | **0.83** | 0.00 | 17 | 1.00 | 23 |
| C4 journal+distill | 0.29 | 0.00 | 24 | 0.20 | 31 |
| C5 freeform notes | 0.79 | 0.00 | 15 | 0.00 | 19 |
| C6 entity records | 0.79 | 0.00 | 18 | 0.00 | 31 |
| C7 claims-temporal | 0.79 | 0.00 | 18 | 0.00 | 27 |
| C8 append-only | 0.79 | 0.00 | 18 | 0.00 | 26 |

## What the numbers establish

1. **The decider matters more than the vocabulary.** Everything from C2 to C8 ties at 0.79 with zero extra calls; C3's dedicated call edges them out at 0.83 at the price of one extra call per turn. The ops vocabulary (C1) is not the problem — the voluntary decider is.
2. **The dedicated memory model is the clearest layer separation.** With C3, the main model never sees the memory protocol: it talks; the memory model remembers. The responsibilities stop overlapping — which is exactly the owner's direction ("레이어의 책임과 역할을 명확히"). The extra call is the cost of that separation.
3. **Everything-is-update (C2) is the simplest vocabulary that performs.** No ops, no delete, no keep — every write is "the document for this topic is now this." Zero extra calls, 0.79 recall, smallest read-side surface.
4. **Journal + distillation (C4) loses the timing.** Batched distillation forgets what mattered when — 0.29 recall. Deferred writes are worse than per-turn writes.
5. **A simulation limit, stated honestly:** latest-wins is 0.00 everywhere because the harness's lexical similarity cannot detect a semantic correction ("I drink coffee in the mornings" vs "I changed my mind — I drink tea now" share almost no words). With a real embedder (production), the supersession path fires; in this harness it cannot. The correction-handling comparison is therefore deferred to a run with a real embedder, not marked as a failure of every structure.

## Layer responsibilities, measured

| Layer | Responsibility | Decider options ranked by evidence |
|---|---|---|
| Main model | Talks; writes memory only through a structured field (if that shape is chosen) | structured field (0.79), never voluntary (0.00) |
| Memory decider | Decides what is worth keeping, corrects, retracts | dedicated narrow call (0.83) > main-model field (0.79) |
| Platform | Store policy: supersession, retention, decay — never the content decision | unchanged across candidates |

## Adopted direction

The candidate ranking supports two structurally clean options, both shape-enforced:

- **Main-model structured field** (C2's everything-is-update vocabulary) — no extra calls, the simplest read/write surface.
- **Dedicated memory model** (C3) — the cleanest layer separation, one extra call per turn.

The choice between them is a cost/boundary preference, not a performance one; the evidence separates them by 0.04 recall. The write-path enforcement (structured output) is the common precondition either way.

## Reproducing

```
cd docs/experiments/memory-creation
python3 harness/creation_runner.py
```

The runner rewrites `results_table.md`; raw per-system numbers are in `results_raw.json`.
