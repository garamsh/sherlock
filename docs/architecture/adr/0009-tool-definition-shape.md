# ADR 0009: The tool definition shape — a self-describing server behind a platform protocol

> Status: accepted
> Date: 2026-08-02 (revised in place 2026-08-13)

## Context

Capabilities must join the agent without a platform-owned plugin mechanism: browser automation, a GitHub client, a one-off utility. ADR 0018 settled where tools live: independent servers the platform reaches through one protocol. This record states what a tool is in that shape.

## Decision

A tool is a self-describing server that speaks the platform protocol. The platform specifies only the protocol; everything else is the tool's choice.

- **The tool serves its own descriptor.** The platform fetches the tool's name, description, input/output JSON Schemas, and usage instructions from the tool itself at discovery time. The tool owns its public contract.
- **The tool validates its own inputs and conforms its own outputs.** Input validation against the input schema and output conformance against the output schema are the tool's primary responsibility; the platform re-validates at the boundary and quarantines any tool that fails (ADR 0018 §Tool contract).
- **The tool owns its behavior.** External API calls, local compute, workspace calls, error handling, and any other domain logic are the tool's implementation choice. Some tools need an external API; some do not. The platform does not assume either.
- **The tool holds its own secrets.** A tool that needs credentials reads them from its own process environment, scoped by operator config. The platform does not dispatch secrets.
- **No code in the agent binary implements a tool.** The agent binary holds the protocol client (`internal/tool`) and nothing tool-shaped. Adding a capability is installing a tool server in the operator's tool list, not a Go import.

A first-party tool ships in this repository as a Go binary under `cmd/<tool>/` (sibling of `cmd/gagent/`) that the agent process spawns (stdio) or reaches (HTTP) via the protocol client. A third-party tool installs the same way with no structural difference; nothing in the platform's treatment distinguishes them.

## Consequences

- Adding a capability is an installation, not a rebuild. The agent binary evolves independently of the tool set; the platform and the capability move at different cadences.
- The descriptor the platform sees is whatever the tool returns; the platform pins a hash of the fetched descriptor at startup and refuses to load a tool whose schema drifts from the pin. The tool's self-description is untrusted input at the boundary.
- The contract surface for the model — JSON Schema descriptors over the chat-completions tool-calling layer — is the same shape the platform already exposes to the LLM API. The tool's JSON Schema is the model-facing contract and the platform-fetched contract at once.
- `tools/playwright/`, `tools/files/`, `tools/shell_exec/`, and `internal/message/` (the mouth) become independent tool binaries; the compile-time registry they replaced becomes a runtime tool list. The agent binary loses the tool imports and the in-process registry it grew around them.
- The shape replaces the in-process tools-as-packages model; that history lives in git.