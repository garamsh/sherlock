# Agent

Governs the agent: the reasoning loop, memory, tool use, and model access. Rationale: ADR 0018, ADR 0004, ADR 0006.

## Current decisions

- The agent is the self: one continuous rolling session per agent, running in its own container (ADR 0018). Its organs:
  - **Gateway** — the ears and eyes; message ingress and the monitoring stream (governed by `gateway.md`).
  - **Reasoning loop** — model call, tool-call decision, result incorporation, repeat.
  - **Memory** — the long-term organ; a layer, never a tool (governed by `memory.md`).
  - **Mouth** — outbound messaging; the agent's own speech to channels and peers.
  - **Hands** — the tool contract client; generic operation of whatever tool servers are reachable, with nothing pre-registered.
- The agent executes no code in its own container; every capability outside its organs is reached through the tool contract.
- Tools are discovered, not registered: the agent lists descriptors from the tool servers it can reach (its sidecar, others) at runtime. A tool's descriptor lives with its implementation, so adding a capability never configures the agent (ADR 0018).
- The reasoning loop itself is unchanged by the pivot — what changes is where tools come from. The loop as implemented today is restated below.

## Current state (the loop as implemented)

- One rolling session per agent process; each turn follows the memory lifecycle: frame, recall, act, writeback.
- Two per-turn guards keep a stalled model from consuming the iteration budget: an identical-failed-call guard (same name, input, and error three times) and a per-tool call budget (six per turn); each substitutes a synthetic result and emits a failed `tool_call` event, and the iteration cap remains the backstop.
- No built-in tools: the registry holds the mouth (`message_send`, in-process) plus every tool discovered at startup from the configured `tool-servers:` endpoints (default the pod-local sidecar). Startup discovery retries with a bound — a sidecar may still be starting — and a server that stays unreachable fails startup with an error naming it; a server disappearing mid-run surfaces as ordinary tool execution errors. Tool schemas double as function-calling parameter documents and must stay inside the OpenAI top-level subset; a runtime test guards the constraint.
- Memory is a layer, never a tool: the read-only memory search tool is gone; recall injection covers reads and the writeback block is the single write path (ADR 0011, ADR 0018).
- All LLM access goes through consumer-owned interfaces (model, embedder); the scripted mock model and hashing embedder keep the full pipeline runnable offline (the mock exercises a discovered tool, `message_send`, and a writeback block).
- Instructions compose an operator-owned ego file (`--ego-file`, flag-only) with the platform contract (`agent.Contract`), which is never overridable, so a custom prompt cannot break the memory loop.

## Open questions

- Non-OpenAI chat providers (Anthropic, …) — deferred; the interface is stable.
