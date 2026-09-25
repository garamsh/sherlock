# Agent

Governs the agent: the reasoning loop, memory, tool use, and model access. Rationale: ADR 0017, ADR 0009, ADR 0004, ADR 0006, ADR 0019.

## Current decisions

- The agent is the self: one continuous rolling session per agent, running in its own container (ADR 0017). Its organs:
  - **Gateway** — the ears and eyes; message ingress and the monitoring stream (governed by `gateway.md`).
  - **Reasoning loop** — model call, tool-call decision, result incorporation, repeat.
  - **Memory** — the source-linked conversation organ, whose search and inspection calls are reserved inside the agent (governed by `memory.md`).
  - **Mouth** — outbound messaging; the agent's own speech to channels and peers (the `message_send` tool, ADR 0017).
  - **Hands** — the platform tool client (`internal/tool`): reads tool manifests from the operator's `tools/` directory, spawns each tool's executable, validates inputs and outputs at the boundary against the manifest's JSON Schemas, and holds each manifest's hash against the operator's pin set at startup, refusing a tool that drifted from its pin or that the set does not name (ADR 0017, ADR 0009). The pin set is the operator's, through config; an agent configured without one loads unpinned and says so at startup. There is no compile-time registry, no runtime plugin loader, no wire protocol, no remote tool registry.
- Tools are scripts with manifests (ADR 0017, ADR 0009). The platform specifies only the contract shape and the call protocol; the tool owns its description, its validation, and its behavior. The operator's `tools/` directory is the installation boundary: a tool is present if and only if the operator dropped its directory, and the platform validates required tools at startup (a missing required tool fails closed).
- Process exec is the workspace's capability (ADR 0017), surfaced to the model as the `shell_exec` tool — an independent tool server calling the workspace's Exec RPC (ADR 0008). The agent executes nothing: the shell_exec tool calls the workspace gRPC client, and the workspace runs the command in its own directory dropped to the configured exec UID, so exec'd programs see the same files the file tools serves.
- The reasoning loop is unchanged in shape; what changed is where tools come from. The loop as implemented today is restated below.
- Provider tool schemas constrain the reserved terminal response and memory inspection calls when the provider supports that mode. The terminal response still carries the legacy `memory_ops` field, but the source-linked runtime does not apply those operations to old addressed tables (ADR 0011).

## Current state (the loop as implemented)

- One rolling session per agent process; each turn frames the current task, recalls bounded older summaries by embedding similarity, acts with recent context, and ends through the reserved response call. The runtime restores recent visible messages from SQLite on restart (ADR 0006, ADR 0019).
- Two per-turn guards keep a stalled model from consuming the iteration budget: an identical-failed-call guard (same name, input, and error three times) and a per-tool call budget (six per turn); each substitutes a synthetic result and emits a failed `tool_call` event, and the iteration cap remains the backstop.
- The agent's tool set is built at startup from the operator's tool list. The `message_send` tool is required; tools that need the workspace (file_read, file_write, file_list, shell_exec) hold the workspace gRPC client themselves. The tool surface the model sees is the JSON Schema descriptors each manifest declares, re-validated at the boundary, projected into the accepted subset where a provider's API constrains the shape — the OpenAI provider's strict mode has no optional property, so it promotes each one to required and nullable at the wire. The projection is the provider's and stops there: the boundary keeps holding calls to the manifest, reading the null that promotion invites as the absence the manifest already permits.
- Memory inspection is a reserved agent call, not an operator-installed tool: the session intercepts it and reads exact visible source messages directly from recalled episode intervals. The terminal response call is reserved in the same way (ADR 0018).
- **A turn without the reserved terminal response does not finish.** The session asks again if a model stops without the call; a malformed reply fails the turn. A successful reply and its visible task are persisted before the terminal event (ADR 0011).
- Chat model access goes through one consumer-owned interface and embedding through a second (ADR 0004). Mid-episode synthesis uses the session model; recall makes no model call and uses the embedder configured in the `embedding:` section. `mock` remains the offline scripted stand-in, with an offline embedder beside it, and configured OpenAI- or Anthropic-compatible clients supply the live chat endpoint beside an OpenAI-compatible embeddings endpoint.
- Instructions compose the operator ego with the platform contract. The contract is never overridden by the ego file; it tells the model to inspect sources for exact wording and to treat a retrieved candidate as a lead, not an established answer.

## Open questions

- Whether the `anthropic-compatible` client and the live Messages API agree. The client is written to the protocol's documented contract and tested against a local fake, and no request has yet gone to the real endpoint, so the tests establish what the client sends and not what the API accepts (`README.md` §Endpoints, and how far each is verified). The first live call is what settles it.
- Progressive disclosure for large tool sets (a `search_tools`-style tool that returns descriptors on demand) — when the operator's tool list first warrants it.
