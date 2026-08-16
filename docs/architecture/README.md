# Architecture

Current architecture of the system and the decisions behind it. This file is the index; workers read it before touching code, the PM keeps it honest.

One pod per agent, two containers (ADR 0017): the agent container is the agent's self (gateway, reasoning loop, memory, mouth, tool client); the sidecar container is the agent's workspace — the computer: the gRPC data plane serving files and process execution. Tools are scripts the operator installs under `tools/<name>/` with a `manifest.json` and an executable; the platform owns the manifest format, the stdio call protocol, the boundary validation, and the drift pin. The code ships one binary with two subcommands (`gagent agent`, `gagent workspace`); first-party and third-party tools ship the same way, as `tools/<name>/` directories. Deployment artifacts live at `build/agent.Dockerfile` and `build/workspace.Dockerfile` (one image each, built by `make build-images`) and `deploy/local/`.

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
5. Keep this index current: every responsibility document and ADR is listed here, and §Modules carries one row per Go package under `cmd/` and `internal/`. `tools/` and `tests/` take one row each, because a single statement holds for every package beneath them; the experiment harnesses under `docs/experiments/` stay out, being no part of the shipped system. A package with no row is a defect in this file, not a judgement about the package.

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
| `cmd/gagent/` | CLI entry point (`gagent agent`, `gagent workspace`): build the command tree, resolve settings through `internal/config/`, and hold the provider wiring that turns them into concrete implementations. Behaviour that the layout rules place in `internal/` has accumulated here; #507 owns that. | — |
| `internal/config/` | Resolve the settings both entry points run on, in one precedence — flags, `GAGENT_*` environment, config file, defaults — and register the flags that are its own top layer, so a caller cannot reach the resolution with the wrong flag set. Holds the resolved types the commands pass down, the ego file's reading, the gateway's TLS material (ADR 0012), and the two startup gates gating on a resolved value: the owner-only rule for operator files, and the loopback-bind predicate both listeners refuse a non-loopback address with. | — |
| `internal/gateway/` | Accept normalized inbound messages over HTTP, serialize the one agent's runs through a single queue that drains a batch per turn, and publish the monitoring event stream as SSE. It also holds the runtime composition root, the memory-lifecycle adapters (recall, writeback, compaction) that implement interfaces `internal/agent/` declares, and two `message.Sink` implementations no production path constructs — #508 and #503 own those. | `gateway.md` |
| `internal/agent/` | Run the reasoning loop (frame, recall, act, writeback) for the agent's one rolling session: the tool-call loop and its budgets, the writeback block's parsing, the event vocabulary the gateway publishes, and the instructions composed from the operator's ego file and the platform contract. Declares the model, tool-client and lifecycle-hook interfaces the other packages implement (ADR 0004), and ships the scripted model the mock provider uses. | `agent.md` |
| `internal/agent/openai/` | The chat-model implementation of the agent's model interface, over the chat-completions API at the operator-configured base URL (ADR 0004). Holds the strict-mode projection of each tool's schema that the wire request carries — a provider-local view, not the tool boundary. | `agent.md` |
| `internal/tool/` | The platform tool client: read manifests from the operator's `tools/` directory, spawn the executable, validate inputs and outputs against the manifest's JSON Schemas, hold each manifest hash against the operator's pin set to refuse drift at startup, and fail closed when a required tool is missing (ADR 0017, ADR 0009). | `agent.md`, `environment.md` |
| `internal/tool/tooltest/` | Test guards for the shipped tools' contracts: read `tools/<name>/manifest.json`, split its input and output schemas, check each parses as a JSON Schema whose `required` names properties that exist, and check a schema against the Go struct it must match. Shared by the tests of the packages that own those structs. | — |
| `internal/memory/` | The agent's long-term memory as a domain: the memory types, the one ranking function and its tunables, hybrid search (embedding cosine merged with the store's keyword ranks), and the write and maintenance operations — store, update, delete, compress, consolidate, forget, retain — behind the `Service` interface. Declares the `Store`, `Embedder` and `Summarizer` interfaces, and ships the offline defaults: the hashing embedder and the concatenating and threshold summarizers. | `memory.md` |
| `internal/memory/openai/` | The memory-side provider implementations over the operator-configured base URL (ADR 0004): the embedder against the embeddings API and the summarizer against chat completions. | `memory.md` |
| `internal/memory/sqlite/` | The `memory.Store` implementation and the only package that opens `memory.db` (ADR 0002): the schema — active and archive tables, the FTS5 index and its triggers — applied at open, and the keyword ranks hybrid search merges. Embeddings are stored as an opaque blob; the cosine runs in `internal/memory/`. | `memory.md` |
| `internal/message/` | The outbound message type — channel, target, content — and the `Sink` interface that delivers it. The `message_send` tool that produces one ships separately under `tools/message_send/` (ADR 0017). | `gateway.md` |
| `internal/workspace/` | The gRPC data plane: proto, generated stubs, server wrapping the workspace filesystem backend for Read/Write/List and `internal/workspace/shell.Executor` for Exec (ADR 0017). It relabels every filesystem error as `InvalidArgument` and turns an executor timeout into a normal response. No tool hosting. | `environment.md` |
| `internal/workspace/shell/` | The workspace's exec backend behind the Exec RPC: fork+exec+setuid dropped to the exec UID (ADR 0008), scrubbed child environment (ADR 0003), per-request network isolation, and the trust class that decides whether resource caps bind at all (ADR 0010); two fail-closed sentinels, `agent_uid unavailable` and `resource limits unavailable`. It owns two things that are not behind the RPC: `ProtectEnviron`, which marks the workspace process itself non-dumpable (ADR 0003), and the `exec-limits` shim — an argv verb of the platform binary, dispatched in `cmd/gagent/` before the command tree, that caps itself and execs the child. | `environment.md` |
| `internal/workspace/files/` | The workspace's filesystem backend (single-consumer of the sidecar; lives here per the layout rules): an FS rooted at the workspace directory which enforces the lexical confinement itself, and the ownership handling exec children need (ADR 0008, ADR 0010) — handing the root to the exec account with a shared-group write bit, writing through a temp file and an atomic rename so a file that account owns can still be replaced, and propagating ownership to the directories it creates. | `environment.md` |
| `tools/` | The tool set the operator installed. Each tool is a directory `tools/<name>/` holding `manifest.json` (the contract) and a Go package (the implementation); `make build-tools` compiles the executable the platform spawns into that directory, where `.gitignore` keeps it out of the tree. First-party tools ship here. | `agent.md`, `environment.md` |
| `tests/` | Integration and end-to-end scenarios wiring multiple domains, as the layout rules place them: the binary runs that drive two real processes, and the in-process tests that substitute an external system or an executor. | — |
