# ADR 0011: Memory write-path unification — the writeback is the single write path

> Status: accepted
> Date: 2026-08-02

## Context

The memory system had two write paths: the memory tool's `store` op and the writeback block's `add` op. QA observed the model storing the same fact two or three times in one turn through both paths, and the platform contract needed a full section just to explain when to use which — which the model still mixed up. Meanwhile the maintenance-mimicking ops (`compress`, `forget`) are the decisions the model performs worst at: every prior-art system the lifecycle record (ADR 0006) surveyed runs consolidation as a background or threshold-triggered process, not a per-turn agent call. Patching the prompt per incident is guard-stacking at the contract level.

## Decision

The model decides content; the platform runs the mechanics.

- The writeback block is the single write path: `keep` / `update` / `delete` / `add` over recalled IDs plus new facts.
- The model's memory surface is read-only search — and in the pod topology memory stops being a tool at all (ADR 0018): recall injection covers the need.
- Maintenance is platform-owned and threshold-triggered: eviction archives by score threshold; consolidation runs on store-size threshold or shutdown, never as a model-invoked op.
- The model can still consolidate deliberately through the writeback itself: `delete` the parts, `add` the summary. No dedicated op is needed.

## Consequences

- Duplicate writes from dual paths become structurally impossible; the contract shrinks to one line (record only verified facts).
- The lifecycle (frame, recall, act, writeback) is unchanged — the writeback was already the per-turn write protocol.
- The founding "agent-driven memory management" principle narrows: the agent decides what to remember, correct, and retract; the platform decides when to consolidate and evict.

## Rejected alternatives

- **Full-array sync each turn** (the model receives the store and returns it) — O(store size) tokens both ways per turn, omission becomes deletion, and LLMs cannot reproduce long lists verbatim. The writeback delta is the same idea scoped to the recalled subset, with ops as the diff language.
- **Keep the five-op tool and strengthen the contract** — prompt patching failed repeatedly; the confusion is structural (two paths), not instructional.
- **Automatic per-turn consolidation** — token cost and unpredictable recall degradation; threshold triggers are the prior-art standard.
