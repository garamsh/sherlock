# Agent

Governs the agent: the reasoning loop, memory, tool use, and model access. Rationale: ADR 0018, ADR 0009, ADR 0004, ADR 0006.

## Current decisions

- The agent is the self: one continuous rolling session per agent, running in its own container (ADR 0018). Its organs:
  - **Gateway** — the ears and eyes; message ingress and the monitoring stream (governed by `gateway.md`).
  - **Reasoning loop** — model call, tool-call decision, result incorporation, repeat.
  - **Memory** — the long-term organ; a layer, never a tool (governed by `memory.md`).
  - **Mouth** — outbound messaging; the agent's own speech to channels and peers (the `message_send` tool, ADR 0018).
  - **Hands** — the platform tool client (`internal/tool`): discovers installed tool servers through the platform protocol, dispatches calls, validates inputs and outputs at the boundary, applies permission policy, and pins descriptor hashes to refuse drift at startup (ADR 0018, ADR 0009). There is no compile-time registry, no runtime plugin loader, no remote tool registry.
- Tools are independent servers behind the platform protocol (ADR 0018, ADR 0009). The platform specifies only the protocol; the tool owns its description, its validation, and its behavior. The operator's tool list is the installation boundary: a tool is present if and only if the operator installed it, and the platform validates required tools at startup (a missing required tool fails closed).
- Process exec is the workspace's capability (ADR 0018), surfaced to the model as the `shell_exec` tool — an independent tool server calling the workspace's Exec RPC (ADR 0008). The agent executes nothing: the shell_exec tool calls the workspace gRPC client, and the workspace runs the command in its own directory dropped to the configured exec UID, so exec'd programs see the same files the files tool serves.
- The reasoning loop is unchanged in shape; what changed is where tools come from. The loop as implemented today is restated below.
- Strict schema enforcement at the model API layer (constrained decoding when the provider supports it, with `parallel_tool_calls: false` because strict mode is incompatible with parallel calls) prevents the model from emitting arguments the schema rejects.

## Current state (the loop as implemented)

- One rolling session per agent process; each turn follows the memory lifecycle: frame, recall, act, writeback.
- Two per-turn guards keep a stalled model from consuming the iteration budget: an identical-failed-call guard (same name, input, and error three times) and a per-tool call budget (six per turn); each substitutes a synthetic result and emits a failed `tool_call` event, and the iteration cap remains the backstop.
- The agent's tool set is built at startup from the operator's tool list. The `message_send` tool is required; tools that need the workspace (files, shell_exec) hold the workspace gRPC client themselves. The tool surface the model sees is exactly the JSON Schema descriptors each tool serves — fetched and re-validated at the boundary.
- Memory is a layer, never a tool: the read-only memory search tool is gone; recall injection covers reads and the writeback block is the single write path (ADR 0011, ADR 0018).
- All LLM access goes through consumer-owned interfaces (model, embedder); the scripted mock model and hashing embedder keep the full pipeline runnable offline (the mock exercises `shell_exec`, then `message_send`, then a writeback block — the full tool surface).
- Instructions compose an operator-owned ego file (`--ego-file`, flag-only) with the platform contract (`agent.Contract`), which is never overridable, so a custom prompt cannot break the memory loop.

## Open questions

- Non-OpenAI chat providers (Anthropic, …) — deferred; the interface is stable.
- Progressive disclosure for large tool sets (a `search_tools`-style tool that returns descriptors on demand) — when the operator's tool list first warrants it.