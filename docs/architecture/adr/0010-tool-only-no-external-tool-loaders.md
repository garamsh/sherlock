# ADR 0010: Tool is the only abstraction — remove MCP

> Status: accepted
> Date: 2026-07-30

## Context

PLAN.md §1–§2 establishes the principle that drives this project's shape:

> **Tool이 서비스를 구성한다**: 외부 서비스든, 실행 샌드박스든, 메모리 관리든 전부 Agent가 호출하는 "Tool" 하나로 취급한다. 특별 취급되는 서브시스템을 따로 만들지 않는다.

The three-component model (Gateway / Agent / Tool) follows from that principle: every capability, no matter its source, joins the registry through the same `tool.Tool` interface; nothing sits above or beside Tool as its own concept.

PLAN.md §5 names two required built-in tools (`sandbox_exec`, `memory`) and "1–2 example service connectors (web search, file processing — chosen based on project nature)" as **examples** of additional tools. The original PLAN does not name MCP, does not describe a subprocess-based tool source, and does not anticipate any tool registration mechanism beyond the `tool.Tool` contract.

ADR-0001 codifies the layout: `internal/<domain>/` for built-in tool implementations (`internal/memory/`, `internal/sandbox/`, `internal/files/`). MCP is not part of this list.

PR #37 ("feat: MCP-based dynamic tool loading", merged 2026-07-26, one day after the bootstrap ADRs were accepted) introduced `internal/tool/mcp/`, `mcp.servers` config, and a subprocess-based tool source adapter. That addition created a fourth concept (MCP server processes) alongside Gateway / Agent / Tool, in tension with PLAN's "don't add layers" rule and ADR-0001's tool-implementation list. The justification at the time — operator extensibility without a platform rebuild — has not been validated against the cost of a separate tool source: the MCP SDK dependency, the subprocess lifecycle, the scrubbed-env allowlist, the OpenAI-subset sanitizer, and the `namedTool` placeholder that re-implements the full `Tool` interface just to reserve a name.

The user has confirmed MCP should not stay. The decision is to remove it and align the codebase with the original PLAN.

## Decision

The Tool contract is the only abstraction through which the agent acts. There is no other tool source. The set of tools is the set of Go packages under `internal/<domain>/`; adding a tool means adding Go code, reviewing it, and shipping it in a normal PR.

1. **Remove `internal/tool/mcp/`** in full. No subprocess tool loader, no JSON-RPC adapter, no scrubbed-env allowlist, no `mcpServer.IsNotFound` check.
2. **Remove the `mcp.servers` config block** and the `MCPConfig`/`MCPServerConfig` types in `cmd/gagent/config.go`.
3. **Remove `cmd/gagent/mcp.go`** (the collision-check helper) and `cmd/gagent/mcp_test.go`. The corresponding `namedTool` placeholder type goes with them.
4. **Remove `Factory.Tools` and `BuiltinToolNames`** from `internal/gateway/runtime.go`. Without MCP, no external tools join the registry; the factory registers built-ins only.
5. **Remove the MCP-related tests** (`TestFactory_ExtraToolsJoinTheRegistry`, `TestFactory_ExtraToolNameCollisionFails`, `TestFactory_MCPStyleToolIsSanitizedForOpenAI`) from `internal/gateway/runtime_test.go`.
6. **Remove `github.com/modelcontextprotocol/go-sdk`** from `go.mod`/`go.sum`.

The `tool.Stub` helper proposed in earlier design notes is no longer needed: with no external tool source, name-collision reservation has no use case. `tool.Stub` is dropped from the plan.

What stays:

- `internal/tool/` — the `Tool` interface, `Registry`, `Invoke`, `schema` validation, and the generated `internal/tool/mocks/Tool.go` (used by `internal/agent/service_test.go` for unit tests that exercise the session loop with a fake tool).
- The four built-in tools (`memory`, `files`, `sandbox_exec`, `message_send`) and their conformance with the `tool.Tool` contract.
- The `tool.Descriptor` struct, which is the model-facing subset of a tool's spec; it continues to flow through `Registry.List()` → `agent.Model.Chat`.

## Consequences

- **One source for "how does the agent act."** Tools are Go code in this repository, reviewed in PRs, version-controlled, and tested with the rest of the platform. The agent's capability surface equals the set of tool implementations under `internal/<domain>/`.
- **Simpler architecture.** Deletion removes a dependency (MCP SDK), a subprocess lifecycle, an env-scrubbing allowlist, a schema sanitizer for external schemas, the `namedTool` placeholder, and the `Factory.Tools` field. Roughly 350 lines of code, 200 lines of tests, and ~40 lines of MCP config and yaml docs go away.
- **Adding tools is a code change.** Operators who want a new capability write Go code. The value MCP offered — runtime extensibility without rebuild — is replaced by the standard CI loop. This matches the project's "conventions beat preferences" stance: code review is the integration point, not a JSON-RPC boundary.
- **ADR-0004 and ADR-0007 stand without modification.** ADR-0004's "secret-free execution domain" applies to the four built-in tools; ADR-0007's "trust-domain invariant" (CodeExecuting vs SecretHolding) still classifies each built-in tool at design review. The MCP-specific paragraphs that appeared in `docs/architecture/security.md` (the env-allowlist scrubbing) and `docs/architecture/agent.md` (the external-tool schema sanitization) are removed because they describe a feature that no longer exists.
- **Mocks keep working.** `internal/tool/mocks/Tool.go` is regenerated by mockery only when the `Tool` interface changes; this ADR changes no interfaces, so the mock is untouched. `mocks.MockTool` continues to be used by `internal/agent/service_test.go` for session-loop unit tests.
- **PLAN.md is unchanged.** The original Korean directive remains the historical record. This ADR supersedes the implicit decision in PR #37 that introduced MCP.

## What this ADR does NOT change

- **ADR-0001** — the layout decision (`internal/<domain>/` per tool implementation) stands and is strengthened by removing the `internal/tool/mcp/` exception.
- **ADR-0004** — secret-free execution domain applies to the four built-in tools; the env-allowlist paragraph is removed because there are no MCP server processes.
- **ADR-0006** — single-container UID trust domains; unchanged.
- **ADR-0007** — tools own security, trust-domain invariant; the invariant now has four tools to classify instead of four + MCP. Each built-in tool's trust domain is documented in its own source file.

## Rejected alternatives

- **Keep MCP but mark it experimental.** It still adds a dependency, a subprocess path, and a separate configuration surface; the cost is paid by every build and every review. The user has confirmed the direction.
- **Replace MCP with a hand-rolled plugin loader (e.g. `internal/<tool>/<tool>.so` shared objects).** Adds a more complex loader (symbol resolution, ABI versioning) without removing the conceptual cost. Deferred indefinitely; the `Tool` interface already provides everything a Go-implemented tool needs.
- **Move MCP behind a feature flag.** Code paths that are always off are dead code. Removal is cleaner.

## Open questions

- None. The four built-in tools plus any future built-in additions are sufficient for the MVP; further connectors are added when their use case arrives, as Go code.
