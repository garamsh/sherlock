# ADR 0011: Memory write-path unification — the writeback is the single write path

> Status: accepted
> Date: 2026-08-02 (revised in place 2026-08-17)

## Context

The memory system had two write paths: the memory tool's `store` op and the writeback block's `add` op. QA observed the model storing the same fact two or three times in one turn through both paths, and the platform contract needed a full section just to explain when to use which — which the model still mixed up. Meanwhile the maintenance-mimicking ops (`compress`, `forget`) are the decisions the model performs worst at: every prior-art system the lifecycle record (ADR 0006) surveyed runs consolidation as a background or threshold-triggered process, not a per-turn agent call. Patching the prompt per incident is guard-stacking at the contract level.

## Decision

The model decides content; the platform runs the mechanics.

- The writeback is the single addressed-fact write path. Current work has a separate lifecycle under ADR 0006: checkpoints and terminal writeback replace the same account, never add a second fact-writing surface. What a write carries is an address, a value, and — where the sentence adds a value that leaves the question alone — an operation (ADR 0018). It does not name a recalled ID: a write computes the address it lands on, so it lands whether or not a read ever returned it.
- The model's memory surface is read-only search — and in the pod topology memory stops being a tool at all (ADR 0017): recall injection covers the need.
- Maintenance is platform-owned and threshold-triggered: eviction archives by score threshold; consolidation runs on store-size threshold (evaluated after a completed turn, never mid-turn) or shutdown, never as a model-invoked op.
- The model can still correct deliberately through the writeback itself: write the current value at the same address, stating `replace`. No dedicated op is needed.

## Consequences

- Duplicate writes from dual paths become structurally impossible; the contract shrinks to one line (record only verified facts).
- The lifecycle (frame, recall, act, writeback) is unchanged — the writeback was already the per-turn write protocol.
- The write path stops depending on the prompt for its shape. The reserved writeback tool is what makes the memory field present, and the session's refusal to end a turn without it is what makes the write happen at all; the contract text still teaches what to put in it, and is no longer the only thing holding the write open. It is a tool rather than a response format because a provider can accept a response format and ignore it (#615).
- The founding "agent-driven memory management" principle narrows: the agent decides what to remember, correct, and retract; the platform decides when to consolidate and evict.

## Rejected alternatives

- **Full-array sync each turn** (the model receives the store and returns it) — O(store size) tokens both ways per turn, omission becomes deletion, and LLMs cannot reproduce long lists verbatim. The writeback states only what the turn asserted, and the address each value computes is what says where it lands.
- **Keep the five-op tool and strengthen the contract** — prompt patching failed repeatedly; the confusion is structural (two paths), not instructional.
- **Automatic per-turn consolidation** — token cost and unpredictable recall degradation; threshold triggers are the prior-art standard.
