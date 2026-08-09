# Agent

Governs the agent: the reasoning loop, memory, tool use, and model access. Rationale: ADR 0018, ADR 0004, ADR 0006.

## Current decisions

- The agent is the self: one continuous rolling session per agent, running in its own container (ADR 0018). Its organs:
  - **Gateway** — the ears and eyes; message ingress and the monitoring stream (governed by `gateway.md`).
  - **Reasoning loop** — model call, tool-call decision, result incorporation, repeat.
  - **Memory** — the long-term organ; a layer, never a tool (governed by `memory.md`).
  - **Mouth** — outbound messaging; the agent's own speech to channels and peers.
  - **Hands** — the tool registry; the agent imports tool packages at compile time and constructs them at startup with the workspace gRPC client. There is no runtime discovery, no plugin loader, no remote tool registry (ADR 0018).
- Process exec is the agent's native capability, not a tool the model invokes (ADR 0018). No tool in the current build execs, so the exec backend lands with its first real caller. The model never sees `shell_exec`.
- Tools are imported, not registered: `cmd/gagent/agent.go` adds the tool packages it needs (today: `tools/files`, `tools/playwright`) and the constructed `tool.Tool` values join the runtime registry. Adding a capability is a Go-package change in the agent's build; removing it is a Go-import removal. There is no sidecar config involved.
- The reasoning loop itself is unchanged by the pivot — what changes is where tools come from. The loop as implemented today is restated below.

## Current state (the loop as implemented)

- One rolling session per agent process; each turn follows the memory lifecycle: frame, recall, act, writeback.
- Two per-turn guards keep a stalled model from consuming the iteration budget: an identical-failed-call guard (same name, input, and error three times) and a per-tool call budget (six per turn); each substitutes a synthetic result and emits a failed `tool_call` event, and the iteration cap remains the backstop.
- The agent's tool registry holds the mouth (`message_send`, in-process) plus every tool the agent imports — today, `files` (a `tools/files.Tool` constructed with the workspace gRPC client) and `playwright` (a `tools/playwright.Tool`). The registry is fixed at process start; no runtime discovery.
- Memory is a layer, never a tool: the read-only memory search tool is gone; recall injection covers reads and the writeback block is the single write path (ADR 0011, ADR 0018).
- All LLM access goes through consumer-owned interfaces (model, embedder); the scripted mock model and hashing embedder keep the full pipeline runnable offline (the mock exercises `message_send` and a writeback block — no `shell_exec` tool call; the model never invokes process exec directly).
- Instructions compose an operator-owned ego file (`--ego-file`, flag-only) with the platform contract (`agent.Contract`), which is never overridable, so a custom prompt cannot break the memory loop.

## Open questions

- Non-OpenAI chat providers (Anthropic, …) — deferred; the interface is stable.
