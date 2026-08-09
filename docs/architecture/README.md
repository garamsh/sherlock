# Architecture

Current architecture of the system and the decisions behind it. This file is the index; workers read it before touching code, the PM keeps it honest.

One pod per agent, two containers (ADR 0018): the agent container is the agent's self (gateway, reasoning loop, memory, mouth); the sidecar container is the agent's computer (workspace, execution, tool servers, permission enforcement). A tool is a contract with the outside — defined where its implementation lives and discovered by the agent at runtime. The code ships one binary with two subcommands (`gagent agent`, `gagent sidecar`); deployment artifacts live at the repo-root `Dockerfile` (agent/sidecar targets) and `deploy/local/`.

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

- `environment.md` — the environment is the agent's computer: topology, workspace, execution, permissions
- `agent.md` — the agent is the self: reasoning loop, organs, tool discovery, model access
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
- `0008` — `shell_exec`: sub-process in the unprivileged UID
- `0009` — script tools: the tool definition shape
- `0010` — one execution boundary: agent-influenced code in the unprivileged UID
- `0011` — memory write-path unification: the writeback is the single write path
- `0012` — platform auto-capture of inbound messages
- `0013` — gateway authentication via mTLS
- `0014` — external egress is tool-routed
- `0015` — scope boundaries: authorization above the platform, no mid-run intervention
- `0016` — the turn drains the queue
- `0017` — the identity is the agent ID
- `0018` — the agent is the self; the sidecar is the computer (pod-per-agent)

### Modules

| Path | Responsibility | Topic file |
|---|---|---|
| `cmd/gagent/` | CLI entry point (`gagent agent`, `gagent sidecar`): resolve config and wire concrete implementations; nothing else. | — |
| `internal/gateway/` | Accept normalized inbound messages, serialize the one agent's runs through a single queue, and publish the monitoring event stream. | `gateway.md` |
| `internal/agent/` | Run the reasoning loop (frame, recall, act, writeback) for the agent's one rolling session. | `agent.md` |
| `internal/tool/` | Define the Tool contract (name, description, input schema, output schema), the registry every tool joins, and the wire protocol that serves a registry over HTTP and calls it back (ADR 0018). | — |
| `internal/memory/` | Own the agent's long-term memory: the `memory.db` store, hybrid search, and the memory service behind the lifecycle hooks. | `memory.md` |
| `internal/files/` | Read and write files inside the sidecar's workspace. | `environment.md` |
| `internal/message/` | Deliver agent-decided outbound messages to channels and peers (`message_send`). | `gateway.md` |
| `internal/shell/` | Run tool child processes as sub-processes dropped to the agent UID — `shell_exec` in a fresh network namespace, script tools with network access (ADR 0010). | `security.md` |
| `internal/tool/script/` | Universal subprocess adapter: run any executable as a tool through the shell executor (ADR 0010); owns the script-tool config declaration shape (ADR 0009). | `gateway.md` |
| `internal/tool/tooltest/` | Build-time schema/struct conformance guards shared by tool tests. | — |
| `internal/tool/mocks/` | Generated mocks of the tool contract for consumer tests. | — |
| `tools/playwright/` | Example script tool (browser automation) registered via the sidecar config's `tools:` section. | `gateway.md` |
| `tests/` | End-to-end scenarios wiring multiple modules together. | — |
