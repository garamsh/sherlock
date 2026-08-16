# Architecture

Current architecture of the system and the decisions behind it. This file is the index; workers read it before touching code, the PM keeps it honest.

One pod per agent, two containers (ADR 0017): the agent container is the agent's self (gateway, reasoning loop, memory, mouth, tool client); the sidecar container is the agent's workspace — the computer: the gRPC data plane serving files and process execution. Tools are scripts the operator installs under `tools/<name>/` with a `manifest.json` and an executable; the platform owns the manifest format, the stdio call protocol, the boundary validation, and the drift pin. The code ships one binary with two subcommands (`gagent agent`, `gagent workspace`); first-party and third-party tools ship the same way, as `tools/<name>/` directories. Deployment artifacts live at the repo-root `Dockerfile` (agent/workspace targets) and `deploy/local/`.

## Contents
- Structure
- Rules
- Index

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

- `environment.md` — the environment splits into workspace (gRPC files and exec, the data plane) and tools (scripts with manifests, stdio contract): topology, execution, permissions
- `agent.md` — the agent is the self: reasoning loop, organs, platform tool client, model access
- `memory.md` — long-term memory: the agent's organ — store, search, lifecycle
- `gateway.md` — the agent's ears and eyes: ingress, outbound, monitoring
- `security.md` — trust domains, secret custody, egress, tool-trust posture

### Decision log

- `0001` — Go stack and single-module layout
- `0002` — per-agent memory in a single SQLite file
- `0003` — secret-free execution domain
- `0004` — LLM access behind interfaces, mock for MVP
- `0005` — tools own security; guard models are layers, not boundaries
- `0006` — memory lifecycle: recall, act, writeback
- `0007` — gateway I/O: asynchronous ingress, tool-based outbound, separate monitoring
- `0008` — agent-influenced sub-processes run dropped to the unprivileged UID
- `0009` — the tool definition shape: a script with a manifest
- `0010` — one execution boundary: agent-influenced effects under the unprivileged UID
- `0011` — memory write-path unification: the writeback is the single write path
- `0012` — gateway authentication via mTLS
- `0013` — external egress is tool-routed
- `0014` — scope boundaries: authorization above the platform, no mid-run intervention
- `0015` — the turn drains the queue
- `0016` — the identity is the agent ID
- `0017` — the agent is the self; the workspace is the data plane; tools are scripts with manifests (pod-per-agent)
- `0018` — a fact is held at the address of the question it answers, and the write states the operation

### Modules

| Path | Responsibility | Topic file |
|---|---|---|
| `cmd/gagent/` | CLI entry point (`gagent agent`, `gagent workspace`): resolve config and wire concrete implementations; nothing else. | — |
| `internal/gateway/` | Accept normalized inbound messages, serialize the one agent's runs through a single queue, and publish the monitoring event stream. | `gateway.md` |
| `internal/agent/` | Run the reasoning loop (frame, recall, act, writeback) for the agent's one rolling session. | `agent.md` |
| `internal/tool/` | The platform tool client: read manifests from the operator's `tools/` directory, spawn the executable, validate inputs and outputs against the manifest's JSON Schemas, and pin manifest hashes to refuse drift at startup (ADR 0017, ADR 0009). | `agent.md`, `environment.md` |
| `internal/tool/tooltest/` | Manifest conformance guards shared by tool tests. | — |
| `internal/memory/` | Own the agent's long-term memory: the `memory.db` store, hybrid search, and the memory service behind the lifecycle hooks. | `memory.md` |
| `internal/workspace/` | The gRPC data plane: proto, generated stubs, server wrapping the workspace filesystem backend for Read/Write/List and `internal/workspace/shell.Executor` for Exec (ADR 0017). No tool hosting. | `environment.md` |
| `internal/workspace/shell/` | The workspace's exec backend behind the Exec RPC (ADR 0008): fork+exec+setuid dropped to the exec UID, rlimits, scrubbed child environment, per-request network isolation, fail-closed `agent_uid unavailable`. | `environment.md` |
| `internal/workspace/client/` | The typed workspace gRPC client wrapper — the boundary tools that need files or exec import: one helper (`client.New(ctx, addr)`) that dials the listener and returns the typed `WorkspaceClient`. Tools own the wire to the workspace. | `environment.md` |
| `internal/workspace/files/` | The workspace's filesystem backend: an FS rooted at the workspace directory, with the lexical confinement the gRPC server enforces (single-consumer of the sidecar; lives here per the layout rules). | `environment.md` |
| `tools/` | The tool set the operator installed. Each tool is a directory `tools/<name>/` containing `manifest.json` (the contract) and an executable (the implementation). First-party tools ship here. | `agent.md`, `environment.md` |
| `tests/` | End-to-end scenarios wiring multiple modules together. | — |
