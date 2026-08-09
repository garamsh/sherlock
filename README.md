# gagent

gagent is an autonomous agent platform written in Go. It runs AI agents that can reason over multi-turn conversations, call tools, and manage their own long-term memory.

## Principles

- **Two processes per agent: self and workspace.** The agent process holds the gateway (message ingress and the monitoring stream), the reasoning loop, long-term memory, the mouth (outbound messaging), and the tools (Go packages the agent imports at compile time). The workspace process is the agent's data plane: a filesystem-only gRPC server (Read, Write, List). Tools are agent-side Go packages — the plugin concept is dropped (ADR 0018).
- **The workspace enforces permissions at the action site.** Agent-influenced file writes run under the workspace's own UID, and workspace ownership is kernel-enforced (ADR 0008, ADR 0010). Native process exec is the agent's own capability (ADR 0018), surfaced to the model as the `shell_exec` tool: children drop to the configured exec UID and run in the shared workspace directory — the same real directory the workspace serves — so exec'd programs see the files the tools see. The orchestration layer manages the pod boundary (network egress, mounts, lifecycle).
- **Memory is agent-managed, not just stored.** Memory is RAG-based, and the agent actively curates it through a per-turn writeback — keeping, correcting, and retracting — while the platform consolidates and evicts on thresholds (ADR 0011), like human memory rather than a plain store/search index.

## Who it is for

Developers who want to build and operate autonomous agents on a minimal, explicit architecture — without framework lock-in or hidden subsystems.

## Running

One CLI binary, `gagent`, runs both processes of an agent pod (ADR 0018): the workspace and the agent. Build or run it from source:

```sh
go run ./cmd/gagent <subcommand>   # or: go build -o gagent ./cmd/gagent
```

The agent reaches the workspace over the gRPC data plane, so start the workspace first. With the default mock model this runs fully offline. The repository-root example configs are picked up automatically and must be owner-only first:

```sh
chmod 0600 gagent.yaml workspace.yaml     # config files must be owner-only
gagent workspace &                        # serves the workspace on 127.0.0.1:8081
gagent agent --message "hello"            # runs one turn and exits
gagent agent                             # or: serve the agent's gateway
```

To try the platform in Docker instead (agent and workspace images debian-slim, local compose setup), see `deploy/local/`.

### gagent agent

Runs the agent's self: the rolling session, memory, the mouth, and the HTTP gateway for the one agent this process serves (`--agent-id`, default `default`). Without `--message` it serves until interrupted:

- `POST /agents/{agent-id}/messages` with `{"content":"..."}` — queues a message for the agent and returns `202` with a message ID. A path naming any other agent ID gets 404: one process serves one agent.
- `GET /agents/{agent-id}/events` — server-sent events stream of the agent's loop (turn started, tool call, writeback, turn finished). Every run ends with exactly one terminal event — `turn_finished` on success, `turn_failed` with the error on failure.
- Agent replies (`message_send`) land as JSON files in `outbox/` next to the `memory-path` file.

With `--message` the agent runs one turn against the same compile-time tool set and exits; outbound `message_send` content and the final response print to stdout (logs go to stderr):

```sh
gagent agent --agent-id my-agent --message "hello"
```

**Warning: an unauthenticated gateway is a remote shell into the agent.** The gateway authenticates callers with mTLS when the `tls:` config section is set (server certificate/key plus a client CA — callers must present a certificate from that CA; ADR 0013). Without `tls:` the gateway serves loopback binds only, and a non-loopback `--addr` fails startup. Certificate issuance and CA operation are operator scope; rotation is a restart with new files.

### gagent workspace

Serves the agent's workspace (ADR 0018) — a filesystem-only gRPC data plane over loopback. The workspace owns the workspace directory; it holds no tools, no process executor, and no plugin loader. The agent reaches it through the workspace gRPC client it dials from `--workspace-addr`.

```sh
gagent workspace
```

- The workspace owns the workspace directory (`workspace` key, created on first use) under its own account; it spawns no children. When privileged it chowns the directory to the agent's exec UID (`exec-uid` key), so files written through the gRPC contract and by the agent's exec'd children share one owner.
- The listener carries no authentication in this phase, so it serves loopback binds only — a non-loopback `--addr` fails startup (ADR 0013, same posture as the gateway).
- Its config file resolves in order: `--config <path>`, then `./workspace.yaml`, then the XDG config path (`~/.config/gagent/workspace.yaml` on Linux). Keys: `addr` (default `127.0.0.1:8081`), `workspace` (default `data/workspace`), `exec-uid` (default `1001`). There is no `tools:` config — the workspace is the data plane only.

### Configuration

Settings resolve in order: **flags > environment variables > config file > defaults**. Environment variables use the `GAGENT_` prefix (`GAGENT_MEMORY_PATH`, `GAGENT_WORKSPACE_ADDR`, …). The agent's config file resolves in order: `--config <path>`, then `./gagent.yaml`, then the XDG config path (`~/.config/gagent/config.yaml` on Linux); the workspace's resolves the same way with `workspace.yaml`. A missing file is an error only when `--config` names it explicitly. A loaded config file must be owner-only (`0600` or `0400`); a group/other-readable file fails startup (docs/architecture/security.md).

Agent keys (`gagent agent`; see `gagent.yaml` for an example):

| Key | Flag | Default |
|---|---|---|
| `addr` | `--addr` | `127.0.0.1:8080` (gateway listen address; loopback-only unless `tls:` is set) |
| `memory-path` | `--memory-path` | `data/memory.db` (the agent's SQLite memory file, created on first use) |
| `workspace-addr` | `--workspace-addr` | `127.0.0.1:8081` (gRPC endpoint of the agent's workspace; the agent dials it once at startup and passes the client to the tools it imports) |
| `exec-uid` | `--exec-uid` | `1001` (UID `shell_exec` children run as; single-account mode: set to the agent's own UID — any other unprivileged value fails closed with `agent_uid unavailable`) |
| `exec-timeout` | `--exec-timeout` | `30s` (per-call ceiling for `shell_exec` commands) |
| `workspace-path` | `--workspace-path` | `data/workspace` (shared directory `shell_exec` children work in — the same directory the workspace serves, at the same path) |
| `model.provider` | — | `mock` (scripted, offline) or `openai` |
| `model.base-url` | — | `https://api.openai.com/v1` (any OpenAI-compatible endpoint, e.g. Ollama/vLLM) |
| `model.model` | — | `gpt-4o-mini` |
| `model.api-key-env` | — | `OPENAI_API_KEY` (name of the env var holding the key — the key itself never goes in the config file) |
| `model.timeout` | — | `60s` (one chat-completions request; raise for slow local endpoints like CPU-served Ollama/vLLM) |
| `embedder.provider` | — | `hashing` (default, offline) or `openai` (any OpenAI-compatible embeddings endpoint) |
| `embedder.base-url` | — | `https://api.openai.com/v1` |
| `embedder.model` | — | `text-embedding-3-small` |
| `embedder.api-key-env` | — | `OPENAI_API_KEY` (same env-var indirection as the chat model) |
| `embedder.timeout` | — | `60s` (one embeddings request) |
| `tls.cert` | — | unset (gateway server certificate, PEM; all three `tls:` keys together enable mTLS) |
| `tls.key` | — | unset (server certificate's private key, PEM) |
| `tls.client-ca` | — | unset (CA bundle, PEM, that client certificates are verified against) |
| `console.url` | — | unset (console endpoint every `message_send` is POSTed to over mTLS, reusing the `tls:` material; unset keeps the file outbox, which is also the delivery-failure fallback) |

Workspace keys (`gagent workspace`; see `workspace.yaml` for an example):

| Key | Flag | Default |
|---|---|---|
| `addr` | `--addr` | `127.0.0.1:8081` (workspace gRPC listen address; loopback-only) |
| `workspace` | `--workspace` | `data/workspace` (workspace directory, created on first use) |
| `exec-uid` | `--exec-uid` | `1001` (the agent's exec UID; a privileged workspace chowns the workspace directory to it) |

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

### Adding tools

Tools are agent-side Go packages the agent imports at compile time (ADR 0018). To add a tool:

- write a Go package implementing `tool.Tool` (`Name`, `Description`, `InputSchema`, `OutputSchema`, `Call`), and wire it into the agent's registry in `cmd/gagent/agent.go` (or a `tools/` package the agent imports). A tool that needs files uses the workspace gRPC client (via `internal/workspace/client`); a tool that needs an external API calls it directly. There is no runtime plugin loader and no `tools:` workspace config — the workspace is the data plane only.

Example configs in the repository root: `gagent.yaml` (agent) and `workspace.yaml` (workspace).

With the default `mock` provider the agent wires the scripted mock model, which answers the first message with a canned run of the full loop: it computes through `shell_exec` in the shared workspace directory, replies via `message_send`, and writes the outcome back as a new episodic memory. The script covers the first message per process; a later message fails its turn with `script exhausted` (a `turn_failed` event) — the mock is a wiring demo, not a conversation partner. With `provider: openai` it runs against a real chat model; a missing API key env var fails at startup. To work on the codebase you need Go and GNU Make, and the standard checks are:

```sh
make ci      # lint + format check + test + build (same as CI)
```

Individual targets: `make lint`, `make format`, `make test`, `make build`.

## Development

Beyond Go and GNU Make, the toolchain is:

- **golangci-lint** — `make lint` (`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2`; the version is pinned in `ci.yml` and `.golangci.yml`).
- **mockery** — `make generate` regenerates the mocks under `internal/tool/mocks/` (config `.mockery.yml`).
- **lefthook** — `make hooks` installs the git hooks once after cloning (pre-commit lint/format, pre-push test).

This repository is developed by AI agents under written conventions — see `AGENTS.md` for the role model and `docs/convention/` for the rules all code follows. Internal structure and design decisions live in `docs/architecture/`.
