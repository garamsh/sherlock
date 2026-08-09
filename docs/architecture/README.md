# Architecture

Current architecture of the system and the decisions behind it. This file is the index; workers read it before touching code, the PM keeps it honest.

One pod per agent, two containers (ADR 0018): the agent container is the agent's self (gateway, reasoning loop, memory, mouth, native process exec, tools); the sidecar container is the agent's workspace — the filesystem-only gRPC data plane. Tools are agent-side Go packages the agent imports at compile time; the plugin concept is dropped. The code ships one binary with two subcommands (`gagent agent`, `gagent workspace`); deployment artifacts live at the repo-root `Dockerfile` (agent/workspace targets) and `deploy/local/`.

## Structure

- **Responsibility documents** (one `.md` per domain or concern) — the single source of truth for how the system is shaped *now*. To know the current state, read only these.
- **`adr/`** — the decision log: one record per current decision, numbered continuously from `0001` (`0000` is the template).

## Rules

1. **Responsibility document skeleton**: current decisions (consolidated) / rationale summary with links to the relevant ADRs / open questions. Do not copy ADR content — synthesize the present state.
2. **ADR records are living.** Each record states one current decision. When a decision evolves, update its record in place; when a decision stops carrying meaning, delete the record. History lives in git, not in the log. A record's date is its original decision date.
3. **Numbering is continuous.** A deletion or insertion renumbers the log, and references to renumbered records in other records are updated in the same PR.
4. **Decisions land in pairs.** A PR that adds or changes a record must update the affected responsibility documents in the same PR — the final state must always live in the responsibility documents.
5. Keep this index current: every responsibility document and ADR is listed here.

## Index

### Responsibility documents

- `environment.md` — the environment splits into workspace (filesystem-only gRPC, the data plane) and tools (agent-side Go packages imported at compile time): topology, execution, permissions
- `agent.md` — the agent is the self: reasoning loop, organs, compile-time tool imports, model access, native process exec
- `memory.md` — long-term memory: the agent's organ — store, search, lifecycle
- `gateway.md` — the agent's ears and eyes: ingress, outbound, monitoring
- `security.md` — trust domains, secret custody, egress

### Decision log

- `0001` — Go stack and single-module layout
- `0002` — per-agent memory in a single SQLite file
- `0003` — secret-free execution domain
- `0004` — LLM access behind interfaces, mock for MVP
- `0005` — tools own security; guard models are layers, not boundaries
- `0006` — memory lifecycle: recall, act, writeback
- `0007` — gateway I/O: asynchronous ingress, tool-based outbound, separate monitoring
- `0008` — agent-influenced sub-processes run dropped to the unprivileged UID
- `0009` — the tool definition shape: descriptor plus Go implementation, compile-time import
- `0010` — one execution boundary: agent-influenced effects under the unprivileged UID
- `0011` — memory write-path unification: the writeback is the single write path
- `0012` — platform auto-capture of inbound messages
- `0013` — gateway authentication via mTLS
- `0014` — external egress is tool-routed
- `0015` — scope boundaries: authorization above the platform, no mid-run intervention
- `0016` — the turn drains the queue
- `0017` — the identity is the agent ID
- `0018` — the agent is the self; the workspace is the data plane (pod-per-agent)

### Modules

| Path | Responsibility | Topic file |
|---|---|---|
| `cmd/gagent/` | CLI entry point (`gagent agent`, `gagent workspace`): resolve config and wire concrete implementations; nothing else. | — |
| `internal/gateway/` | Accept normalized inbound messages, serialize the one agent's runs through a single queue, and publish the monitoring event stream. | `gateway.md` |
| `internal/agent/` | Run the reasoning loop (frame, recall, act, writeback) for the agent's one rolling session. | `agent.md` |
| `internal/tool/` | Define the Tool contract (name, description, input schema, output schema) and the registry every tool joins. Tools join the registry in-process. | — |
| `internal/memory/` | Own the agent's long-term memory: the `memory.db` store, hybrid search, and the memory service behind the lifecycle hooks. | `memory.md` |
| `internal/files/` | The workspace's filesystem backend: an FS rooted at the workspace directory, with the lexical confinement the gRPC server enforces. | `environment.md` |
| `internal/workspace/` | The filesystem-only gRPC data plane: proto, generated stubs, server wrapping `internal/files.FS` for Read/Write/List (ADR 0018). No tool hosting, no process exec. | `environment.md` |
| `internal/workspace/client/` | The typed workspace gRPC client wrapper — the boundary tools import: one helper (`client.New(ctx, addr)`) that dials the listener and returns the typed `WorkspaceClient`. The agent dials it once at startup to construct the tools; tools own the wire from there (ADR 0018). | `environment.md` |
| `internal/message/` | Deliver agent-decided outbound messages to channels and peers (`message_send`). | `gateway.md` |
| `internal/tool/tooltest/` | Build-time schema/struct conformance guards shared by tool tests. | — |
| `internal/tool/mocks/` | Generated mocks of the tool contract for consumer tests. | — |
| `tools/files/` | The agent's files tool: a `tool.Tool` that reads, writes, and lists files through the workspace gRPC client. Imported at compile time by the agent. | `environment.md` |
| `tools/playwright/` | The agent's browser-automation tool: a Go package the agent imports at compile time. No subprocess wrapping, no script adapter (ADR 0018). | `environment.md` |
| `tests/` | End-to-end scenarios wiring multiple modules together. | — |
