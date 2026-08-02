# ADR 0015: Memory write-path unification — the writeback is the single write path

> Status: accepted
> Date: 2026-08-02
> Supersedes: the memory tool's write ops (`store`, `update`, `compress`, `forget`) from ADR 0003's five-operation vocabulary. The `search` op stands.

## Context

The memory system had two write paths: the memory tool's `store` op and the writeback block's `add` op. QA observed the model storing the same fact two or three times in one turn through both paths (mid-project sweep after #140), and the platform contract needed a full section just to explain when to use which — which the model still mixed up. Meanwhile the maintenance-mimicking ops (`compress`, `forget`) are the decisions the model performs worst at: every prior-art system ADR 0008 surveyed (MemGPT, Mem0, Generative Agents) runs consolidation as a background or threshold-triggered process, not a per-turn agent call. Patching the prompt per incident (#112, #137, #144) is guard-stacking at the contract level.

## Decision

The model decides content; the platform runs the mechanics.

- The writeback block is the single write path: `keep` / `update` / `delete` / `add` over recalled IDs plus new facts. The memory tool becomes read-only (`search`).
- Maintenance is platform-owned and threshold-triggered: `forget` already archives by score threshold; consolidation (episodic clusters → semantic parents) runs on store-size threshold or environment shutdown per `memory.md`, never as a model-invoked op.
- The model can still consolidate deliberately through the writeback itself: `delete` the parts, `add` the summary. No dedicated op is needed.

## Consequences

- Duplicate writes from dual paths become structurally impossible; the contract's "tool call vs writeback" section is deleted and the remaining rules (record only verified facts) shrink to one line.
- ADR 0003's five-operation vocabulary reduces to one tool op (`search`) plus the writeback vocabulary; ADR 0008's lifecycle (frame, recall, act, writeback) is unchanged — the writeback was already the per-turn write protocol.
- The memory tool's schema, typed structs, and conformance tests shrink to `search`; `memory.Service`'s write methods remain — the writeback path and platform maintenance call them.
- The founding "agent-driven memory management" principle narrows: the agent decides what to remember, correct, and retract; the platform decides when to consolidate and evict.

## Rejected alternatives

- **Full-array sync each turn** (the model receives the store and returns it) — O(store size) tokens both ways per turn, omission becomes deletion, and LLMs cannot reproduce long lists verbatim. The writeback delta is the same idea scoped to the recalled subset, with ops as the diff language.
- **Keep the five-op tool and strengthen the contract** — the prompt-patch path already failed twice (#112, #142); the confusion is structural (two paths), not instructional.
- **Automatic per-turn consolidation** — token cost and unpredictable recall degradation; threshold triggers are the prior-art standard.
