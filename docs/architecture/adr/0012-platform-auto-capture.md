# ADR 0012: Platform auto-capture of inbound messages

> Status: accepted
> Date: 2026-08-02

## Context

The writeback unification (ADR 0011) made the writeback the single write path, and a series of contract fixes plus platform guards tried to make "remember X" requests reliable with a small chat model. Repeated QA verification rounds showed each fix works when its path triggers, yet about a third of runs kept surfacing a new variance: empty writeback, file redirect, false success claim. Basic remembering is a reliability requirement living in the wrong layer — model compliance. The prior art the lifecycle record (ADR 0006) surveyed already works differently: Generative Agents stores every observation automatically and lets retrieval ranking, reflection, and consolidation do the filtering.

## Decision

The platform stores every inbound user message as an episodic memory — verbatim, source `user-turn`, low default importance — with no model involvement. The writeback block remains for model-curated facts (distilled knowledge, corrections, retractions), not transcripts.

## Consequences

- "Remember X" is satisfied by construction; the failure family of empty or misdirected writebacks becomes moot.
- The contract's remember-request rules collapse into one line: every message is remembered automatically; the writeback carries distilled facts, not transcripts.
- Captured messages enter recall ranking at low default importance so curated facts outrank raw turns; threshold eviction and consolidation (ADR 0011) absorb the added volume.
- Auto-captured rows hold whatever the user typed, including secrets. The memory file stays the agent's own asset and recall surfaces it only to the same agent — no trust-domain change, but capture is now systematic rather than selective.
- The founding "agent-driven memory" principle narrows once more: the agent curates, the platform remembers.

## Rejected alternatives

- **Turn-final intent validation** (detect remember-request + empty writeback, then retry or fail) — requires classifying the user message's intent, and empty writebacks are often legitimate; text-matching guard-stacking with false-positive risk.
- **More contract lines** — consecutive QA rounds proved each new clause leaves a reachable variance.
- **Capture full exchanges (message + reply)** — doubles volume for little recall gain; curated writeback adds already carry what mattered in the reply. Arrives with a use case if one appears (simplicity.md).
