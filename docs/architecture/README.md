# Architecture

Current architecture of the system and the decisions behind it. This file is the index; workers read it before touching code, the PM keeps it honest.

The system is three components — Gateway / Agent / Tool. Every capability is a tool behind one contract (`internal/tool/`); no fourth organizing layer exists (ADR 0001, ADR 0010, ADR 0013; code-level structural rules live in `docs/convention/simplicity.md`).

## Structure

- **Responsibility documents** (one `.md` per domain or concern, e.g. `memory.md`, `gateway.md`) — the single source of truth for how the system is shaped *now*. To know the current state, read only these.
- **`adr/`** — the record of individual decisions: direction taken, context, rejected alternatives.

## Rules

1. **Responsibility document skeleton**: current decisions (consolidated) / rationale summary with links to the relevant ADRs / open questions. Do not copy ADR content — synthesize the present state.
2. **ADRs are append-only.** Once merged, the body is frozen. Exceptions: updating the status field (`accepted` → `superseded by ADR-XXXX`), and fixing typos or broken links. A changed decision means a new ADR that supersedes the old one — never an edit.
3. **Decisions land in pairs.** A PR that adds or supersedes an ADR must update the affected responsibility documents in the same PR. A PR with only one of the two is rejected — the final state must always live in the responsibility documents.
4. Keep this index current: every responsibility document and notable ADR is listed here.

## Index

### Responsibility documents

- `environment.md` — environment model: body, topology, trust domains
- `memory.md` — long-term memory: store, search, lifecycle
- `security.md` — trust domains, secret custody, egress
- `gateway.md` — ingress, routing, outbound, monitoring
- `agent.md` — reasoning loop, tool use, model access
- `adr/0001`–`0016` — design decision log (see topic files for current truth)

### Modules

> Status: all listed modules exist.

| Path | Responsibility | Topic file |
|---|---|---|
| `cmd/gagent/` | CLI entry point (`gagent gateway`, `gagent agent`): resolve config and wire concrete implementations; nothing else. | — |
| `internal/gateway/` | Accept normalized inbound messages, route them to environments, and publish the monitoring event stream. | `gateway.md` |
| `internal/agent/` | Run the reasoning loop (frame, recall, act, writeback) for one rolling session per environment. | `agent.md` |
| `internal/tool/` | Define the Tool contract (name, description, input schema, output schema) and the registry every tool joins. | — |
| `internal/environment/` | Own the environment data layout: create-on-first-use and teardown of `data/envs/<env-id>/`. | `environment.md` |
| `internal/memory/` | Own one environment's long-term memory: the `memory.db` store, hybrid search, and the five memory operations. | `memory.md` |
| `internal/files/` | Read and write files inside the environment's workspace. | `environment.md` |
| `internal/message/` | Deliver agent-decided outbound messages to channels and peers (`message_send`). | `gateway.md` |
| `internal/shell/` | Run tool child processes as sub-processes dropped to the agent UID — `shell_exec` in a fresh network namespace, script tools with network access (ADR 0014). | `security.md` |
| `internal/tool/script/` | Universal subprocess adapter: run any executable as a tool through the shell executor (ADR 0013, ADR 0014). | `gateway.md` |
| `internal/tool/tooltest/` | Build-time schema/struct conformance guards shared by tool tests. | — |
| `internal/tool/mocks/` | Generated mocks of the tool contract for consumer tests. | — |
| `tools/playwright/` | Example script tool (browser automation) registered via the config's `tools:` section. | `gateway.md` |
| `tests/` | End-to-end scenarios wiring multiple modules together. | — |
