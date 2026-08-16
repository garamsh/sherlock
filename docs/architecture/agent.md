# Agent

Governs the agent: the reasoning loop, memory, tool use, and model access. Rationale: ADR 0017, ADR 0009, ADR 0004, ADR 0006.

## Current decisions

- The agent is the self: one continuous rolling session per agent, running in its own container (ADR 0017). Its organs:
  - **Gateway** — the ears and eyes; message ingress and the monitoring stream (governed by `gateway.md`).
  - **Reasoning loop** — model call, tool-call decision, result incorporation, repeat.
  - **Memory** — the long-term organ; a layer, never a tool (governed by `memory.md`).
  - **Mouth** — outbound messaging; the agent's own speech to channels and peers (the `message_send` tool, ADR 0017).
  - **Hands** — the platform tool client (`internal/tool`): reads tool manifests from the operator's `tools/` directory, spawns each tool's executable, validates inputs and outputs at the boundary against the manifest's JSON Schemas, and holds each manifest's hash against the operator's pin set at startup, refusing a tool that drifted from its pin or that the set does not name (ADR 0017, ADR 0009). The pin set is the operator's, through config; an agent configured without one loads unpinned and says so at startup. There is no compile-time registry, no runtime plugin loader, no wire protocol, no remote tool registry.
- Tools are scripts with manifests (ADR 0017, ADR 0009). The platform specifies only the contract shape and the call protocol; the tool owns its description, its validation, and its behavior. The operator's `tools/` directory is the installation boundary: a tool is present if and only if the operator dropped its directory, and the platform validates required tools at startup (a missing required tool fails closed).
- Process exec is the workspace's capability (ADR 0017), surfaced to the model as the `shell_exec` tool — an independent tool server calling the workspace's Exec RPC (ADR 0008). The agent executes nothing: the shell_exec tool calls the workspace gRPC client, and the workspace runs the command in its own directory dropped to the configured exec UID, so exec'd programs see the same files the files tool serves.
- The reasoning loop is unchanged in shape; what changed is where tools come from. The loop as implemented today is restated below.
- Strict schema enforcement at the model API layer (constrained decoding when the provider supports it, with `parallel_tool_calls: false` because strict mode is incompatible with parallel calls) prevents the model from emitting arguments the schema rejects. It covers the response as well as each tool call: a response is a document carrying the visible reply and the turn's memory writes, so the memory field has no shape in which it is absent (ADR 0018).

## Current state (the loop as implemented)

- One rolling session per agent process; each turn follows the memory lifecycle: frame, recall, act, writeback.
- Two per-turn guards keep a stalled model from consuming the iteration budget: an identical-failed-call guard (same name, input, and error three times) and a per-tool call budget (six per turn); each substitutes a synthetic result and emits a failed `tool_call` event, and the iteration cap remains the backstop.
- The agent's tool set is built at startup from the operator's tool list. The `message_send` tool is required; tools that need the workspace (files, shell_exec) hold the workspace gRPC client themselves. The tool surface the model sees is the JSON Schema descriptors each manifest declares, re-validated at the boundary, projected into the accepted subset where a provider's API constrains the shape — the OpenAI provider's strict mode has no optional property, so it promotes each one to required and nullable at the wire. The projection is the provider's and stops there: the boundary keeps holding calls to the manifest, reading the null that promotion invites as the absence the manifest already permits.
- Memory is a layer, never a tool: the read-only memory search tool is gone; recall injection covers reads and the writeback is the single write path (ADR 0011, ADR 0017).
- All LLM access goes through consumer-owned interfaces (model, embedder); the scripted mock model and hashing embedder keep the full pipeline runnable offline (the mock exercises `shell_exec`, then `message_send`, then a memory write — the full tool surface).
- Instructions compose an operator-owned ego file (`--ego-file`, flag-only) with the platform contract (`agent.Contract`), which is never overridable, so a custom prompt cannot break the memory loop. The contract's writeback section is the prose half of the response schema the provider enforces; the schema is what makes the field present, the contract what teaches the model to fill it.

## Open questions

- Non-OpenAI chat providers (Anthropic, …) — deferred; the interface is stable.
- Progressive disclosure for large tool sets (a `search_tools`-style tool that returns descriptors on demand) — when the operator's tool list first warrants it.