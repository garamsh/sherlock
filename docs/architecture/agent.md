# Agent

Governs the agent: the reasoning loop, tool use, and model access. Rationale: ADR 0005, ADR 0008.

## Current decisions

- The agent runs one continuous rolling session per environment — a person model, not a task-runner model. No automatic resets.
- Context is organized around the current task. Each turn follows the memory lifecycle: frame, recall, act, writeback (see `memory.md`).
- The reasoning loop is model call → tool-call decision → execution → result incorporation → repeat. The agent reads the registry's tool schemas and decides on its own when to call which tool.
- Memory decisions (store, update, compress, forget) are the agent's own, made inside the reasoning loop via the memory tool and the writeback block. No separate memory-manager component exists.
- Outbound replies are the agent's decision through the `message_send` tool; default reply conventions live in the agent's instructions.
- All LLM access goes through narrow interfaces owned by the consumer: a model interface (agent loop) and an embedder interface (memory). MVP ships deterministic local implementations — a scripted mock model and a hashing-based embedder — so the full pipeline runs offline.
- The loop, session, `Model` interface, lifecycle hook interfaces (`Recaller`, `WritebackSink`, `Compactor`, `EventSink`), and the scripted mock model live in `internal/agent/`. The writeback block is a trailing ` ```memory ` fenced JSON block carrying `keep` / `update` / `delete` / `add` operations; malformed blocks are typed errors.

## Open questions

- LLM provider selection — deferred; interfaces are stable before providers arrive.
- Mid-run steering of the loop — deferred (see `gateway.md`).
