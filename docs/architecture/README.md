# Architecture

Current architecture of the system and the decisions behind it. This file is the index; workers read it before touching code, the PM keeps it honest.

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
- `adr/0001`–`0009` — initial design decision log (see topic files for current truth)

### Modules

> Status: all listed modules exist.

| Path | Responsibility | Topic file |
|---|---|---|
| `cmd/gagent/` | CLI entry point (`gagent gateway`, `gagent agent`): resolve config and wire concrete implementations; nothing else. | — |
| `internal/gateway/` | Accept normalized inbound messages, route them to environments, and publish the monitoring event stream. | `gateway.md` |
| `internal/agent/` | Run the reasoning loop (frame, recall, act, writeback) for one rolling session per environment. | `agent.md` |
| `internal/tool/` | Define the Tool contract (name, input schema, output schema) and the registry every tool joins. | — |
| `internal/environment/` | Own the environment data layout: create-on-first-use and teardown of `data/envs/<env-id>/`. | `environment.md` |
| `internal/memory/` | Own one environment's long-term memory: the `memory.db` store, hybrid search, and the five memory operations. | `memory.md` |
| `internal/sandbox/` | Execute agent-supplied commands inside the agent's own environment (`sandbox_exec`). | `environment.md` |
| `internal/files/` | Read and write files inside the environment's workspace. | `environment.md` |
| `internal/message/` | Deliver agent-decided outbound messages to channels and peers (`message_send`). | `gateway.md` |
| `tests/` | End-to-end scenarios wiring multiple modules together. | — |
