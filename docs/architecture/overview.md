# Architecture overview

Governs what the system is made of: the module list and the responsibility assigned to each module. The *why* lives in `adr/`.

> Status: planned — no source code exists yet. This is the structure the first code PRs create.

## System shape

Three components only — Gateway, Agent, Tool (PLAN §2). Everything else is a tool implementation or a property of the environment. The environment is the agent's body (ADR 0002): one container per environment, subdivided into trust domains by Unix UIDs (ADR 0006).

## Modules by responsibility

| Path | Responsibility |
|---|---|
| `cmd/gateway/main.go` | Construct concrete implementations and wire them; nothing else. |
| `internal/gateway/` | Accept normalized inbound messages, route them to environments, and publish the monitoring event stream. |
| `internal/agent/` | Run the reasoning loop (frame, recall, act, writeback) for one rolling session per environment. |
| `internal/tool/` | Define the Tool contract (name, input schema, output schema) and the registry every tool joins. |
| `internal/memory/` | Own one environment's long-term memory: the `memory.db` store, hybrid search, and the five memory operations. |
| `internal/sandbox/` | Execute agent-supplied commands inside the agent's own environment (`sandbox_exec`). |
| `internal/files/` | Read and write files inside the environment's workspace. |
| `internal/message/` | Deliver agent-decided outbound messages to channels and peers (`message_send`). |
| `tests/` | End-to-end scenarios wiring multiple modules together. |

## Cross-cutting responsibilities

| Concern | Owner | Rule |
|---|---|---|
| Session continuity | `internal/agent/` | One rolling session per environment; no resets; overflow drains into memory (ADR 0008, ADR 0009). |
| Message ordering | `internal/gateway/` | Serialized execution per environment; mid-run messages queue as followups (ADR 0009). |
| Live visibility | `internal/gateway/` | Read-only subscription event stream, separate from the message path (ADR 0009). |
| Memory lifecycle | `internal/memory/` | Recall on task frame, writeback every turn, supersede-not-overwrite, capped index, archive-based eviction (ADR 0008). |
| Model access | `internal/agent/`, `internal/memory/` | Each consumes a narrow interface (model, embedder); MVP implementations are local mocks (ADR 0005). |
| Security policy | Each tool, for its own domain | Code-executing tools hold no secrets; secret-holding tools never execute agent code (ADR 0007). |
| Secret custody | Egress path | Secret references resolve only at the egress tool under (secret, destination, header) bindings with response redaction (ADR 0004). |
| Environment boundary | Orchestration side | Egress, quotas, mounts, and lifecycle are injected at environment creation; the agent cannot change them (ADR 0002). |

## Environment data layout

Each environment owns one directory, `data/envs/<env-id>/`, containing its workspace and `memory.db`. The directory is the isolation unit: teardown, migration, and backup are directory operations (ADR 0003).

## Decisions

- `adr/0001-stack-and-layout.md` — Go, single module, Layout A.
- `adr/0002-environment-is-agent-body.md` — the environment is the agent's body.
- `adr/0003-memory-per-environment.md` — per-environment `memory.db` (SQLite).
- `adr/0004-secret-free-environment.md` — secret values never materialize in the execution domain.
- `adr/0005-model-interface-only.md` — LLM access behind interfaces; mock for MVP.
- `adr/0006-single-container-uid-domains.md` — one container, UID trust domains.
- `adr/0007-tool-owned-security.md` — tools own security; guards are layers, not boundaries.
- `adr/0008-memory-lifecycle.md` — recall, act, writeback.
- `adr/0009-gateway-io-model.md` — async ingress, tool-based outbound, separate monitoring.

## Open decisions

- Memory snapshot/backup outside the environment — default post-MVP (ADR 0003).
- Cross-environment shared memory — default deferred (ADR 0003).
- Gateway authentication — deferred; trusted networks only until decided (ADR 0009).
- UID separation mechanics (setuid supervisor) — target per ADR 0006; MVP runs without it.
- Environment image definition and hygiene checklist — required by ADR 0006, not yet written.
- LLM provider selection — deferred (ADR 0005).
