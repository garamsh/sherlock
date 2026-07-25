# gagent

gagent is an autonomous agent platform written in Go. It runs AI agents that can reason over multi-turn conversations, call tools, execute code, and manage their own long-term memory.

## Principles

- **Three components only: Gateway / Agent / Tool.** The Gateway normalizes external channels (web, API, messengers, CLI) into a single entry point. The Agent runs the reasoning loop and decides which tools to call. Every capability — memory, sandboxed execution, external service connectors — is a Tool behind one common interface. No extra layers.
- **The agent owns its execution environment.** Each agent runs in its own container with full authority inside it; the orchestration layer manages the boundary (network egress, mounts, resource quotas, lifecycle).
- **Memory is agent-managed, not just stored.** Memory is RAG-based, and the agent actively maintains it: storing, updating, compressing, and forgetting — like human memory rather than a plain store/search index.

## Who it is for

Developers who want to build and operate autonomous agents on a minimal, explicit architecture — without framework lock-in or hidden subsystems.

## Running

The gateway serves HTTP message ingress and an SSE monitoring stream:

```sh
go run ./cmd/gateway -addr :8080 -data-root data/envs
```

- `POST /envs/{env-id}/messages` with `{"content":"..."}` — queues a message for the environment (created on first use) and returns `202` with a message ID.
- `GET /envs/{env-id}/events` — server-sent events stream of the environment's agent loop (turn started, tool call, writeback, turn finished).
- Agent replies (`message_send`) land as JSON files in `data/envs/<env-id>/outbox/`.

**Warning: the gateway has no authentication.** Run it on trusted networks only — an exposed gateway is a remote shell into the environment.

The binary wires the scripted mock model (no LLM provider yet), which answers each environment's first message with a canned reply; `sandbox_exec` needs a Docker daemon. To work on the codebase you need Go and GNU Make, and the standard checks are:

```sh
make ci      # lint + format check + test + build (same as CI)
```

Individual targets: `make lint`, `make format`, `make test`, `make build`.

## Development

Run `make hooks` once after cloning to install the lefthook git hooks (pre-commit lint/format, pre-push test).

This repository is developed by AI agents under written conventions — see `AGENTS.md` for the role model and `docs/convention/` for the rules all code follows. Internal structure and design decisions live in `docs/architecture/`.
