# gagent

gagent is an autonomous agent platform written in Go. It runs AI agents that can reason over multi-turn conversations, call tools, execute code, and manage their own long-term memory.

## Principles

- **Three components only: Gateway / Agent / Tool.** The Gateway normalizes external channels (web, API, messengers, CLI) into a single entry point. The Agent runs the reasoning loop and decides which tools to call. Every capability — memory, sandboxed execution, external service connectors — is a Tool behind one common interface. No extra layers.
- **The agent owns its execution environment.** Each agent runs in its own container with full authority inside it; the orchestration layer manages the boundary (network egress, mounts, resource quotas, lifecycle).
- **Memory is agent-managed, not just stored.** Memory is RAG-based, and the agent actively maintains it: storing, updating, compressing, and forgetting — like human memory rather than a plain store/search index.

## Who it is for

Developers who want to build and operate autonomous agents on a minimal, explicit architecture — without framework lock-in or hidden subsystems.

## Running

One CLI binary, `gagent`, runs the platform. Build or run it from source:

```sh
go run ./cmd/gagent <subcommand>   # or: go build -o gagent ./cmd/gagent
```

To try the platform in Docker instead (distroless image, local compose setup), see `deploy/local/`.

### gagent gateway

Serves HTTP message ingress and an SSE monitoring stream:

```sh
gagent gateway
```

- `POST /envs/{env-id}/messages` with `{"content":"..."}` — queues a message for the environment (created on first use) and returns `202` with a message ID.
- `GET /envs/{env-id}/events` — server-sent events stream of the environment's agent loop (turn started, tool call, writeback, turn finished).
- Agent replies (`message_send`) land as JSON files in `data/envs/<env-id>/outbox/`.

**Warning: the gateway has no authentication.** Run it on trusted networks only — an exposed gateway is a remote shell into the environment.

### gagent agent

Runs one turn of an agent directly and prints the result:

```sh
gagent agent --env my-env --message "hello"
```

- `--message` is required; `--env` defaults to `default`.
- Outbound `message_send` content and the final response print to stdout (logs go to stderr).

### Configuration

Settings resolve in order: **flags > environment variables > config file > defaults**. Environment variables use the `GAGENT_` prefix (`GAGENT_DATA_ROOT`, `GAGENT_EXEC_TIMEOUT`, …). The config file resolves in order: `--config <path>`, then `./gagent.yaml`, then the XDG config path (`~/.config/gagent/config.yaml` on Linux). A missing file is an error only when `--config` names it explicitly.

| Key | Flag | Default |
|---|---|---|
| `addr` | `--addr` | `:8080` (used by `gateway` only) |
| `data-root` | `--data-root` | `data/envs` |
| `image` | `--image` | `alpine:3.21` |
| `exec-timeout` | `--exec-timeout` | `30s` |
| `cpu-limit` | `--cpu-limit` | `1` (CPU quota per environment container, in CPUs; must be positive) |
| `memory-limit` | `--memory-limit` | `512m` (memory quota per environment container; at least `6m`, Docker's floor) |
| `model.provider` | — | `mock` (scripted, offline) or `openai` |
| `model.base-url` | — | `https://api.openai.com/v1` (any OpenAI-compatible endpoint, e.g. Ollama/vLLM) |
| `model.model` | — | `gpt-4o-mini` |
| `model.api-key-env` | — | `OPENAI_API_KEY` (name of the env var holding the key — the key itself never goes in the config file) |
| `model.timeout` | — | `60s` (one chat-completions request; raise for slow local endpoints like CPU-served Ollama/vLLM) |
| `mcp.servers` | — | `[]` (external MCP servers; see below) |

### Agent ego

The agent's instructions are composed of two parts: an **ego** (identity and behavior guidance) you own, and a **platform contract** (tool use, memory writeback format, reply conventions) the platform always appends — a custom ego can never disable the memory loop.

The ego is a Markdown file, resolved in order: `--ego-file <path>`, then the embedded default (`internal/agent/EGO.md`). There is no config key for it, and a missing or unreadable file fails at startup. Changes apply on restart; the file is not hot-reloaded.

```sh
gagent agent --ego-file ./EGO.md --message "hello"
```

An ego file describes who the agent is and how it behaves — not the protocol. Example:

```markdown
You are Nimbus, an autonomous site-reliability agent running one continuous
session per environment. You prefer small, reversible changes and explain
every destructive action before taking it.
```

### External tools (MCP)

External tools are loaded through the [Model Context Protocol](https://modelcontextprotocol.io): each configured server starts as a subprocess at boot (stdio transport), its tools are discovered and registered alongside the built-ins, and calls are forwarded. Adding a tool never requires recompiling the platform.

```yaml
mcp:
  servers:
    - name: time                     # label used in logs and errors (required)
      command: /usr/local/bin/time-mcp  # executable (required)
      args: ["--tz", "UTC"]          # optional argv
      env: [SOME_TOKEN]              # platform env var names forwarded to the server
      timeout: 10s                   # per tool call; default 30s
```

Behavior notes:

- The server process environment is scrubbed: only the `env`-allowlisted variables are passed through, so platform secrets never leak into server processes.
- A tool name colliding with a built-in (`memory`, `files`, `sandbox_exec`, `message_send`) or another MCP tool fails startup — nothing is silently overridden.
- A server that fails to start is logged and skipped; the rest still load. A server that dies mid-run fails its tool calls with a typed error; other servers are unaffected. Dead servers are not restarted automatically.

Example `gagent.yaml`:

```yaml
addr: ":9090"
data-root: /var/lib/gagent/envs
image: alpine:3.21
exec-timeout: 45s
cpu-limit: 1.5
memory-limit: 1g
model:
  provider: openai
  base-url: http://localhost:11434/v1   # Ollama, for example
  model: llama3.2
  api-key-env: OLLAMA_API_KEY
mcp:
  servers:
    - name: time
      command: /usr/local/bin/time-mcp
```

With the default `mock` provider the binary wires the scripted mock model, which answers each environment's first message with a canned run of the full loop: it computes in the sandbox via `sandbox_exec`, replies via `message_send`, and writes the result back to memory. With `provider: openai` it runs against a real chat model; a missing API key env var fails at startup. `sandbox_exec` (and therefore the mock script) needs a Docker daemon. To work on the codebase you need Go and GNU Make, and the standard checks are:

```sh
make ci      # lint + format check + test + build (same as CI)
```

Individual targets: `make lint`, `make format`, `make test`, `make build`.

## Development

Run `make hooks` once after cloning to install the lefthook git hooks (pre-commit lint/format, pre-push test).

This repository is developed by AI agents under written conventions — see `AGENTS.md` for the role model and `docs/convention/` for the rules all code follows. Internal structure and design decisions live in `docs/architecture/`.
