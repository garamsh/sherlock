# Architecture

Current architecture of the system and the decisions behind it. This file is the index; workers read it before touching code, the PM keeps it honest.

One pod per agent, two containers (ADR 0017): the agent container is the agent's self (gateway, reasoning loop, memory, mouth, tool client); the sidecar container is the agent's workspace — the computer: the gRPC data plane serving files and process execution. Tools are scripts the operator installs under `tools/<name>/` with a `manifest.json` and an executable; the platform owns the manifest format, the stdio call protocol, the boundary validation, and the drift pin. The code ships one binary with two subcommands (`gagent agent`, `gagent workspace`); first-party and third-party tools ship the same way, as `tools/<name>/` directories. Deployment artifacts live at `build/agent.Dockerfile`, `build/workspace.Dockerfile` and `build/tools.Dockerfile` (one image each, built by `make build-images`) and `deploy/local/`; the third holds the tool tree an operator mounts into an agent, and is a filesystem rather than a process.

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
- `deployment.md` — what has to be true to run the workload as a pod: listeners and the bind gate, volumes, capabilities, startup failures, shutdown, images, and the facts an external consumer depends on
- `agent.md` — the agent is the self: reasoning loop, organs, platform tool client, model access
- `memory.md` — durable current work and long-term facts: the agent's organ — store, reads, lifecycle
- `gateway.md` — the agent's ears and eyes: ingress, outbound, monitoring
- `security.md` — trust domains, secret custody, egress, tool-trust posture
- `packages.md` — the package partition: which `stack-go.md` §1 layout this project is under, the three cuts that produce every package, which way imports run across them, and where a new package goes

### Decision log

- `0001` — Go stack and single-module layout
- `0002` — per-agent memory in a single SQLite file
- `0003` — secret-free execution domain
- `0004` — model access is one consumer-owned interface, and the wire protocol is configuration
- `0005` — tools own security; guard models are layers, not boundaries
- `0006` — memory lifecycle: source-authoritative current work, subordinate progress, recall, act, writeback
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
- `0019` — a question becomes an address before the store is read, and a read that finds nothing there widens to the subject
- `0020` — the image repositories are immutable, and a consumer references an image by digest

### Modules

| Path | Responsibility | Topic file |
|---|---|---|
| `cmd/gagent/` | CLI entry point (`gagent agent`, `gagent workspace`): build the command tree, resolve settings through `internal/config/`, and hold the provider wiring that turns them into concrete implementations. It keeps the startup decisions that read a resolved setting and belong to no one boundary — where logs go, whether an address may be served at all under ADR 0012, and how long the tool registry lives. | — |
| `internal/config/` | Resolve the settings both entry points run on, in one precedence — flags, `GAGENT_*` environment, config file, defaults — and register the flags that are its own top layer, so a caller cannot reach the resolution with the wrong flag set. Holds the resolved types the commands pass down, the ego file's reading, the gateway's TLS material (ADR 0012), and the two startup gates gating on a resolved value: the owner-only rule for operator files, and the loopback-bind predicate both listeners refuse a non-loopback address with. | — |
| `internal/gateway/` | Accept normalized inbound messages over HTTP, serialize the one agent's runs through a single queue that drains a batch per turn, and publish the monitoring event stream as SSE. It runs the HTTP listener and its shutdown, and runs the one-shot turn for a caller that is not serving — reading the outbox to report what the turn delivered, and collecting the turn's failed tool calls so the caller's exit status can carry them. It also holds the runtime composition root: the factory that opens the agent's memory store, wires the session's lifecycle hooks, and assembles the rolling session a turn runs on. `cmd/gagent/` builds that factory from the resolved settings, but the store, the hooks and the session are constructed here — so concrete construction is split across two packages rather than held in the entry point `stack-go.md` §Project envelope reserves it for. | `gateway.md` |
| `internal/agent/` | Run the reasoning loop (frame, recall, act, writeback) for the agent's one rolling session: the source-authoritative current-work lifecycle and individual effect reconciliation, the tool-call loop and its budgets, the writeback block's parsing, the event vocabulary the gateway publishes, and the instructions composed from the operator's ego file and the platform contract. Declares the model, tool-client and lifecycle-hook interfaces the other packages implement (ADR 0004), and ships the scripted model the mock provider uses. | `agent.md` |
| `internal/agent/anthropic/` | The chat-model implementation of the agent's model interface, over Anthropic's Messages API at the operator-configured base URL (ADR 0004). A second wire protocol rather than a second vendor: it absorbs what that protocol places differently — the system prompt, the carrier a tool result travels on, and a token ceiling the protocol requires — and sends each tool's schema unmodified, so it holds no strict-mode projection. | `agent.md` |
| `internal/agent/memory/` | The session's context hooks (ADR 0006), all three implemented: the writeback that stores what a turn stated at its address; the compactor, which keeps the newest messages and writes nothing because an addressed store has nowhere to put a summary of the rest (ADR 0018); and the recall hook, the read step that resolves the turn's task into an address, reads it, widens to the subject where that address held nothing, and reports the outcome it reached (ADR 0019). It sits under the package declaring those interfaces, so fact conversion stays outside the loop; the session consumes the separate current-work persistence contract directly. | `agent.md`, `memory.md` |
| `internal/agent/openai/` | The chat-model implementation of the agent's model interface, over the chat-completions API at the operator-configured base URL (ADR 0004). Holds the strict-mode projection of each tool's schema that the wire request carries — a provider-local view, not the tool boundary. | `agent.md` |
| `internal/tool/` | The platform tool client: read manifests from the operator's `tools/` directory, spawn the executable, validate inputs and outputs against the manifest's JSON Schemas, hold each manifest hash against the operator's pin set to refuse drift at startup, and fail closed when a required tool is missing (ADR 0017, ADR 0009). It also names the environment variables the spawn contract carries to every tool, and publishes their values; a tool's credentials are not among them (ADR 0003). | `agent.md`, `environment.md` |
| `internal/tool/tooltest/` | Test guards for the shipped tools' contracts: read `tools/<name>/manifest.json`, split its input and output schemas, check each parses as a JSON Schema whose `required` names properties that exist, and check a schema against the Go struct it must match. Shared by the tests of the packages that own those structs. | — |
| `internal/memory/` | The agent's long-term memory as a domain: the address a fact is held at, the operation a write states about it (ADR 0018), the salience tiers and the eviction order, the addressed reads, and the one write behind the `Service` interface. Declares the fact `Storer` interface and the current-work types and persistence contract. It has no embedder, no cosine and no threshold: an addressed store computes where a fact is instead of scoring what it looks like. | `memory.md` |
| `internal/memory/sqlite/` | The fact and current-work store implementation and the only package that opens `memory.db` (ADR 0002): the schema — active and archive fact tables, the address index and the revisioned current-work singleton — applied and validated at open, the three reads that index carries, and the refusal to open a store written before the addressed schema. | `memory.md` |
| `internal/message/` | The outbound message type — channel, target, content. It is the shape of the JSON the `message_send` tool writes into the outbox directory and the shape a reader of that directory decodes; the tool that writes one ships separately under `tools/message_send/` (ADR 0017). The package carries no delivery code, because delivery is that write. | `gateway.md` |
| `internal/workspace/` | The gRPC data plane's wire: the proto and the generated stubs, and nothing else — a tool that needs the workspace imports this package and links no backend (#525). | `environment.md` |
| `internal/workspace/server/` | The data plane's server side: the `Server` wrapping the workspace filesystem backend for Read/Write/List and `internal/workspace/shell.Executor` for Exec (ADR 0017), and the listener's lifecycle — bind, serve, graceful stop, the single-account environ protection and the root handover to the exec account. It relabels every filesystem error as `InvalidArgument` and turns an executor timeout into a normal response. No tool hosting. | `environment.md` |
| `internal/workspace/shell/` | The workspace's exec backend behind the Exec RPC: fork+exec+setuid dropped to the exec UID (ADR 0008), scrubbed child environment (ADR 0003), per-request network isolation, and the trust class that decides whether resource caps bind at all (ADR 0010); two fail-closed sentinels, `agent_uid unavailable` and `resource limits unavailable`. It owns two things that are not behind the RPC: `ProtectEnviron`, which marks the workspace process itself non-dumpable (ADR 0003), and the `exec-limits` shim — an argv verb of the platform binary, dispatched in `cmd/gagent/` before the command tree, that caps itself and execs the child. | `environment.md` |
| `internal/workspace/files/` | The workspace's filesystem backend (single-consumer of the sidecar; lives here per the layout rules): an FS rooted at the workspace directory which enforces the lexical confinement itself, and the ownership handling exec children need (ADR 0008, ADR 0010) — handing the root to the exec account with a shared-group write bit, writing through a temp file and an atomic rename so a file that account owns can still be replaced, and propagating ownership to the directories it creates. | `environment.md` |
| `tools/` | The tool set the operator installed. Each tool is a directory `tools/<name>/` holding `manifest.json` (the contract) and a Go package (the implementation); `make build-tools` compiles the executable the platform spawns into that directory, where `.gitignore` keeps it out of the tree. First-party tools ship here, and `build/tools.Dockerfile` compiles the same set into the image an operator mounts as an agent's tree (`deployment.md` §Images and tags). | `agent.md`, `environment.md` |
| `tests/` | Integration and end-to-end scenarios wiring multiple domains, as the layout rules place them: the binary runs that drive two real processes, and the in-process tests that substitute an external system or an executor. It also holds the experiment run that drives those two processes against a live model endpoint, and the metering proxy that bounds what such a run may spend; the run needs an opt-in environment variable and a key, and `make ci` sets neither. | — |
