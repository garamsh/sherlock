# Architecture overview

Governs what the system is made of: the module list and the entry point to the topic files. Current truth per responsibility lives in the topic files; decision history lives in `adr/`.

> Status: partial — `internal/tool/` and `internal/environment/` exist; the remaining modules are planned.

## System shape

Three components only — Gateway, Agent, Tool (PLAN §2). Everything else is a tool implementation or a property of the environment. The environment is the agent's body: one container per environment, subdivided into trust domains by Unix UIDs.

## Modules by responsibility

| Path | Responsibility | Topic file |
|---|---|---|
| `cmd/gateway/main.go` | Construct concrete implementations and wire them; nothing else. | — |
| `internal/gateway/` | Accept normalized inbound messages, route them to environments, and publish the monitoring event stream. | `gateway.md` |
| `internal/agent/` | Run the reasoning loop (frame, recall, act, writeback) for one rolling session per environment. | `agent.md` |
| `internal/tool/` | Define the Tool contract (name, input schema, output schema) and the registry every tool joins. | — |
| `internal/environment/` | Own the environment data layout: create-on-first-use and teardown of `data/envs/<env-id>/`. | `environment.md` |
| `internal/memory/` | Own one environment's long-term memory: the `memory.db` store, hybrid search, and the five memory operations. | `memory.md` |
| `internal/sandbox/` | Execute agent-supplied commands inside the agent's own environment (`sandbox_exec`). | `environment.md` |
| `internal/files/` | Read and write files inside the environment's workspace. | `environment.md` |
| `internal/message/` | Deliver agent-decided outbound messages to channels and peers (`message_send`). | `gateway.md` |
| `tests/` | End-to-end scenarios wiring multiple modules together. | — |

## Environment data layout

Each environment owns one directory, `data/envs/<env-id>/`, containing its workspace and `memory.db`. The directory is the isolation unit: teardown, migration, and backup are directory operations.

## Topic files

- `environment.md` — the environment model: body, topology, trust domains.
- `memory.md` — long-term memory: store, search, lifecycle.
- `security.md` — trust domains, secret custody, egress.
- `gateway.md` — ingress, routing, outbound, monitoring.
- `agent.md` — reasoning loop, tool use, model access.

Open questions are tracked in each topic file. Decision rationale and rejected alternatives are in `adr/` (immutable once merged).
