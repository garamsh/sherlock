# ADR 0010: Tool is the only abstraction — remove MCP

> Status: accepted
> Date: 2026-07-30

## Context

PLAN §1–§2 establishes that every capability joins through `tool.Tool`; ADR 0001 lists the tool implementations under `internal/<domain>/`. PR #37 (one day after the bootstrap ADRs) added `internal/tool/mcp/` — a fourth concept (subprocess-based tool loader) that violated "don't add layers."

The user has confirmed MCP should not stay.

## Decision

The Tool contract is the only abstraction. Removing MCP:

- Delete `internal/tool/mcp/`, `cmd/gagent/mcp.go`, `mcp.servers` config, MCP SDK dependency.
- Drop `Factory.Tools` and `BuiltinToolNames` from `internal/gateway/runtime.go`; with no external tool source the factory registers built-ins only.
- Drop the corresponding tests.

What stays: `Tool`, `Registry`, `Invoke`, `schema`, `mocks/Tool.go` (the test mock is untouched because the interface doesn't change). The four built-in tools keep their trust-domain classifications under ADR 0007.

## Consequences

- One source for "how does the agent act": Go code in this repo, reviewed in PRs.
- Simpler: ~350 LOC, ~200 LOC of tests, the MCP SDK, the subprocess lifecycle, the env allowlist, the schema sanitizer, the `namedTool` placeholder, and `Factory.Tools` all go away.
- ADR 0004 / 0007 stand unchanged; the env-allowlist paragraph and the MCP-discovered-schema sanitizer are removed from `security.md` / `agent.md`.

## Rejected alternatives

- **Keep MCP experimental** — still pays the dependency and subprocess cost.
- **Hand-rolled plugin loader (`internal/<tool>/<tool>.so`)** — more complex loader for no gain; deferred indefinitely.
