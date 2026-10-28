# sherlock

sherlock is an autonomous agent platform written in Go. It runs AI agents that can reason over multi-turn conversations, call tools, and manage their own long-term memory.

## Contents
- Principles
- Who it is for
- Running
- Development

## Principles

- **Two processes per agent: self and workspace.** The agent process holds the gateway (message ingress and the monitoring stream), the reasoning loop, long-term memory, the mouth (outbound messaging), and the script-tool runtime (`internal/tool`) that loads each tool from `tools/<name>/manifest.json` and dispatches calls as one-shot stdio processes (ADR 0017, ADR 0009). The workspace process is the computer: a gRPC server for the filesystem (Read, Write, List) and process execution (Exec) in the workspace directory. Tools are stand-alone executables behind a fixed stdio contract; the agent binary hosts no tool behavior.
- **The workspace enforces permissions at the action site.** Agent-influenced file writes run under the workspace's own UID, and workspace ownership is kernel-enforced (ADR 0008, ADR 0010). Process exec is the workspace's capability (ADR 0017), surfaced to the model as the `shell_exec` tool: the tool calls the workspace's Exec RPC, and children drop to the configured exec UID and run in the workspace directory, so exec'd programs see the files the tools see. The agent executes nothing. The orchestration layer manages the pod boundary (network egress, mounts, lifecycle).
- **Memory is source-linked.** Completed turns save visible user and assistant messages before synthesis. Short memory is the rolling recent conversation; each full four-message batch produces a mid episode stored with its summary's embedding. Recall combines two recent episodes with up to five more most similar to the task and pending source previews, with no model call; the agent can inspect saved source messages when it needs exact wording. See [Memory](docs/architecture/memory.md) for recall and context compaction.

## Who it is for

Developers who want to build and operate autonomous agents on a minimal, explicit architecture — without framework lock-in or hidden subsystems.

## Running

One CLI binary, `sherlock`, runs both processes of an agent pod (ADR 0017): the workspace and the agent. Build or run it from source:

```sh
go run ./cmd/sherlock <subcommand>   # or: go build -o sherlock ./cmd/sherlock
```

Start the workspace first: the agent's file tools and `shell_exec` tools dial it when the model first calls one, and a tool that finds nothing listening fails that call. With the default mock model this runs fully offline. The example configs under `configs/` are picked up automatically when running from the repository root and must be owner-only first:

```sh
chmod 0600 configs/agent.yaml configs/workspace.yaml     # config files must be owner-only
sherlock workspace &                        # serves the workspace on 127.0.0.1:8081
sherlock agent --agent-id my-agent --message "hello"   # runs one turn and exits
sherlock agent --agent-id my-agent                     # or: serve the agent's gateway
```

To try the platform in Docker instead: `make build-images` builds both images — distroless agent (`build/agent.Dockerfile`, carrying the first-party tool executables at `/opt/sherlock/tools/`) and debian-slim workspace (`build/workspace.Dockerfile`) — and `deploy/local/` holds the local compose setup.

### The `agent` subcommand

Runs the agent's self: the rolling session, memory, the mouth, and the HTTP gateway for the one agent this process serves (`--agent-id`, required: an absent or empty value refuses startup, and the value is used verbatim, so a GRN is a valid ID). `--assignment-epoch` optionally carries the launcher's assignment epoch; each serving start also mints a fresh generation, a runtime start nonce. The agent logs both and checks neither. Without `--message` it serves until interrupted:

- `POST /agents/{agent-id}/messages` with `{"content":"..."}` — queues a message for the agent and returns `202` with a message ID. A path naming any other agent ID gets 404: one process serves one agent.
- `GET /agents/{agent-id}/status` — what this process loaded: `agent_id`, `assignment_epoch`, `generation`, `config_revision` (the config file's `revision` key) and `started_at`, plus `state`, which reads `draining` once `SIGTERM` has arrived. On `SIGTERM` the agent answers new posts 503, finishes the turn in flight, and then exits (`docs/architecture/deployment.md` §Shutdown and the grace period).
- `GET /agents/{agent-id}/events` — server-sent events stream of the agent's loop (turn started, tool call, writeback, turn finished). Every run ends with exactly one terminal event — `turn_finished` on success, `turn_failed` with the error on failure.
- Agent replies (`message_send`) land as JSON files in `outbox/` next to the `memory-path` file. That write is the delivery: the `message_send` tool writes the file and nothing forwards it anywhere, so a reader of that directory is reading everything the agent sent.

With `--message` the agent runs one turn against the same operator-installed tool list and exits; outbound `message_send` content and the final response print to stdout (logs go to stderr):

```sh
sherlock agent --agent-id my-agent --message "hello"
```

A one-shot run exits non-zero if any tool call in the turn failed, naming the tools that failed on stderr — the turn's output still prints first. A failed tool call does not end the turn (the model is told and gets another iteration), so the exit status is where a caller that is no longer watching learns the run was not clean. The serving path differs on purpose: there the failure goes to the model and the `tool_call` event, the turn still ends `turn_finished`, and the gateway keeps serving.

**Warning: an unauthenticated gateway is a remote shell into the agent.** The gateway authenticates callers with mTLS when the `tls:` config section is set (server certificate/key plus a client CA — callers must present a certificate from that CA; ADR 0012). Without `tls:` the gateway serves loopback binds only, and a non-loopback `--addr` fails startup. Certificate issuance and CA operation are operator scope; rotation is a restart with new files.

### The `workspace` subcommand

Serves the agent's workspace (ADR 0017) — the computer: a gRPC data plane over loopback serving the filesystem (Read, Write, List) and process execution (Exec) in the workspace directory. The workspace owns the workspace directory and the executor; it holds no tools and no plugin loader. Nothing in the agent process dials it — the tools that need files or exec dial it themselves (§Adding tools), so the agent itself executes nothing.

```sh
sherlock workspace
```

- The workspace owns the workspace directory (`workspace` key, created on first use); Exec children work in it by default, so they see workspace files natively. Exec children drop to the exec UID (`exec-uid` key, default 1001; single-account mode: the workspace's own UID — any other unprivileged value fails closed with `agent_uid unavailable` — which the executor runs and which is not a supported deployment shape, ADR 0008). When privileged the workspace chowns the directory to the exec UID, so files written through the gRPC contract and by exec'd children share one owner.
- The listener carries no authentication in this phase, so it serves loopback binds only — a non-loopback `--addr` fails startup (ADR 0012, same posture as the gateway).
- Its config file resolves in order: `--config <path>`, then `./configs/workspace.yaml`, then the XDG config path (`~/.config/sherlock/workspace.yaml` on Linux). Keys: `addr` (default `127.0.0.1:8081`), `workspace` (default `data/workspace`), `exec-uid` (default `1001`), `exec-timeout` (default `30s`). There is no `tools:` config — tools live in the agent.
- A positional argument that names no operation below is refused rather than ignored, so `sherlock workspace raed` fails instead of starting a server.

### Operating a workspace

The `sherlock workspace` subcommands `read`, `write`, `list` and `exec` each make one call on a running workspace's gRPC contract and nothing else on the caller's host, so a person, a script or an MCP server operates the workspace with the isolation the agent's own tools get (ADR 0027).

```sh
sherlock workspace list .                          # {"entries":[{"name":"notes.txt","dir":false}]}
echo hello | sherlock workspace write notes.txt    # {"bytes_written":6}
sherlock workspace read notes.txt                  # the file's bytes, unchanged
sherlock workspace exec -- 'ls -la; exit 3'        # {"stdout":"…","stderr":"","exit_code":3,"timed_out":false}
sherlock workspace exec --stdin -- wc -c < notes.txt
```

- The address is `--workspace-addr`, then `SHERLOCK_WORKSPACE_ADDR`, then `127.0.0.1:8081`. No config file is read, and `--config` or `--strict-config` is refused.
- `exec` runs one positional through `sh -c`, and more than one as an executable with its arguments. It always runs the child without network, under the resource caps, with the scrubbed environment, in the workspace directory. `--timeout` sets the command's timeout, and without it the workspace's `exec-timeout` applies. `--stdin` forwards standard input.
- The command's own exit code is part of the result. The CLI's exit status is about the call: `0` for performed, `1` for the CLI's own failure (refused before any call, or unable to write the result of an operation the workspace performed), `2` for refused by the workspace (a path outside it, an executable it cannot start), and `3` for an unreachable workspace or one that did not answer. Every failure prints one JSON object on stderr, `{"error":"usage|workspace|unreachable","code":"<gRPC code>","message":"…"}` (`docs/architecture/environment.md` §Workspace CLI contract).

### Serving a workspace to an MCP client

`sherlock workspace mcp` is a stdio MCP server whose tools `read`, `write`, `list` and `exec` each run the matching operation above once, as a child process of the same binary (ADR 0028). An MCP-capable agent with its local tools disabled then has these four as its only tools, so the workspace is what its tool calls act on; what that leaves running on its own host is listed below. The client starts it from a command line, for example in an MCP configuration file:

```json
{"mcpServers": {"sherlock-workspace": {"command": "sherlock", "args": ["workspace", "mcp", "--workspace-addr", "127.0.0.1:8081"]}}}
```

- Its one setting is the address, resolved as the operations resolve it. It opens no listener and takes no positional argument.
- A workspace refusal (exit 2) comes back as a tool result marked as an error; an unreachable workspace (exit 3) as JSON-RPC error `-32001`; any other failure as `-32603` (`docs/architecture/environment.md` §Workspace MCP server).

#### Claude Code as the client

Checked against Claude Code 2.1.302, the version `claude --version` printed; each flag's quoted effect is from that version's `claude --help`. With a workspace serving on `127.0.0.1:8081` and `sherlock` on `PATH`:

```sh
claude -p "List the workspace's top directory, then run pwd in it." \
  --tools "" \
  --strict-mcp-config \
  --mcp-config '{"mcpServers": {"sherlock-workspace": {"command": "sherlock", "args": ["workspace", "mcp", "--workspace-addr", "127.0.0.1:8081"]}}}' \
  --allowedTools 'mcp__sherlock-workspace__*'
```

- `--tools ""` turns off Claude Code's built-in tools: the flag specifies "the list of available tools from the built-in set", and `""` is to "disable all tools".
- `--strict-mcp-config` makes the server above the only one: "Only use MCP servers from --mcp-config, ignoring all other MCP configurations".
- `--mcp-config` loads that server from a JSON string, the configuration shown earlier in this section.
- `--allowedTools` allows the server's tools without a permission prompt, which a `-p` run has nobody to answer. `mcp__<server>__*` matching every tool of one server is from Claude Code's permissions documentation (`https://code.claude.com/docs/en/permissions`, "To match every tool from an MCP server").

#### What this setup does not cover

- **Mechanisms the client runs on its own host that are not tools.** `--tools ""` names the built-in tool set only. Claude Code's `--help` names hooks "defined in settings and by installed plugins" (under `--bare`), and its permissions documentation describes hooks as "custom shell commands" Claude Code runs (§Extend permissions with hooks). Turn those off, or review what they run, before relying on this setup: `claude --help` states that `--bare` skips them, and that it also changes how Claude Code authenticates. For any other client, find and disable its equivalents; this section states nothing about them.
- **The workspace's isolation is the workspace's own.** An `exec` child gets no network, the resource caps, the scrubbed environment and the workspace directory (ADR 0027 §3, "Exec runs under the isolation the agent's own exec gets"), and the MCP server's only effect on the client's host is running the operation CLI (ADR 0028 §3, "The server's only effect on its own host is running the CLI"). Nothing here adds isolation beyond that.
- **A workspace run directly on the host separates less than one in its own container.** `docs/architecture/deployment.md` §Volumes and ownership rests the separation of an `exec` child from the agent's files on the workspace container's mounts and on the `exec-uid`. A workspace started as a host process has no such mount boundary, and the executor gives an `exec` child a new network namespace (and a user namespace when unprivileged) but no mount namespace or changed root (`internal/workspace/shell/process_linux.go`), so its `exec` children read and write whatever files their `exec-uid` can on that host.

### Configuration

Settings resolve in order: **flags > environment variables > config file > defaults**. Environment variables use the `SHERLOCK_` prefix (`SHERLOCK_MEMORY_PATH`, `SHERLOCK_WORKSPACE_ADDR`, …). The agent's config file resolves in order: `--config <path>`, then `./configs/agent.yaml`, then the XDG config path (`~/.config/sherlock/config.yaml` on Linux); the workspace's resolves the same way with `workspace.yaml`. A missing file is an error only when `--config` names it explicitly. A loaded config file must be owner-only (`0600` or `0400`); a group/other-readable file fails startup (docs/architecture/security.md). A key the file carries that no setting reads — a typo, a renamed key, a block left from a removed feature — is reported at startup by a WARN naming the key and the file, and does not stop the process: the file keeps working when a key is renamed, and `tools.pin` for `tools.pins` says so instead of reading as an absent pin set.

Agent keys (`sherlock agent`; see `configs/agent.yaml` for an example):

| Key | Flag | Default |
|---|---|---|
| `addr` | `--addr` | `127.0.0.1:8080` (gateway listen address; loopback-only unless `tls:` is set) |
| `memory-path` | `--memory-path` | `data/memory.db` (the agent's SQLite memory file, created on first use) |
| `workspace-addr` | `--workspace-addr` | `127.0.0.1:8081` (gRPC endpoint of the agent's workspace. The agent does not dial it; it publishes the value as `SHERLOCK_WORKSPACE_ADDR` to every tool it spawns, and the tools that need files or exec dial it themselves. A wrong address surfaces at the first tool call, not at startup — the agent's only workspace setting) |
| `tools-dir` | — | `./tools` (the directory manifests are read from at startup) |
| `revision` | — | unset (an opaque name for this configuration, set by whatever writes the file; echoed as `config_revision` on the status route and never computed. Unset means unknown) |
| `tools.pins` | — | unset (the accepted tool set, one manifest SHA-256 per tool name — see §Adding tools. Unset loads the tree unpinned and warns; present and naming no tool fails startup. Config file only: one variable carries one value, so `SHERLOCK_TOOLS_PINS` is refused at startup rather than read) |
| `model.provider` | — | `mock` (scripted, offline), `openai-compatible` (the chat-completions protocol) or `anthropic-compatible` (the Messages protocol). It names the wire protocol the endpoint speaks, not the vendor: one value serves every endpoint speaking it, and `model.base-url` is what picks between them |
| `model.base-url` | — | `https://api.openai.com/v1` (any OpenAI-compatible endpoint, e.g. MiniMax/Ollama/vLLM — see §Endpoints, and how far each is verified) |
| `model.model` | — | `gpt-4o-mini` |
| `model.api-key-env` | — | `OPENAI_API_KEY` (name of the env var holding the key — the key itself never goes in the config file) |
| `model.api-key-file` | — | unset (path of a file holding the key, for a short-lived token the platform rewrites while the agent runs: read again for every request, and once more to retry a request answered with 401. When set, `model.api-key-env` is not read, and setting both fails startup — see §A gateway that issues short-lived tokens) |
| `model.timeout` | — | `60s` (one chat-completions request; raise for slow local endpoints like CPU-served Ollama/vLLM) |
| `embedding.base-url` | — | unset (an OpenAI-compatible `/embeddings` API root, hosted or local — see §Embeddings. Required unless `model.provider` is `mock`, which embeds offline and reads no `embedding:` key) |
| `embedding.model` | — | unset (embedding model name, e.g. `BAAI/bge-base-en-v1.5`) |
| `embedding.api-key-env` | — | unset (name of the env var holding the embeddings key; unset sends no key, as a local server needs none) |
| `tls.cert` | — | unset (gateway server certificate, PEM; all three `tls:` keys together enable mTLS) |
| `tls.key` | — | unset (server certificate's private key, PEM) |
| `tls.client-ca` | — | unset (CA bundle, PEM, that client certificates are verified against) |

Workspace keys (`sherlock workspace`; see `configs/workspace.yaml` for an example):

| Key | Flag | Default |
|---|---|---|
| `addr` | `--addr` | `127.0.0.1:8081` (workspace gRPC listen address; loopback-only) |
| `workspace` | `--workspace` | `data/workspace` (workspace directory, created on first use; Exec children work here by default) |
| `exec-uid` | `--exec-uid` | `1001` (UID Exec children run as; single-account mode, not a supported deployment shape (ADR 0008): set to the workspace's own UID — any other unprivileged value fails closed with `agent_uid unavailable`; a privileged workspace also chowns the workspace directory to it) |
| `exec-timeout` | `--exec-timeout` | `30s` (per-call ceiling for Exec commands) |

### Endpoints, and how far each is verified

`model.provider` names a wire protocol and `model.base-url` picks the vendor speaking it, so a new vendor on an existing protocol needs configuration and no code. What differs between protocols, and how far each has actually been exercised, is below — the gap between "it is compatible" and "this part was run" is where an agent breaks quietly.

#### MiniMax, on `openai-compatible`

MiniMax speaks the OpenAI chat-completions protocol, so it needs no `model.provider` value of its own:

```yaml
model:
  provider: openai-compatible
  base-url: https://api.minimax.io/v1
  model: MiniMax-M2
  api-key-env: MINIMAX_KEY
```

The key never goes in the file — `api-key-env` names the environment variable holding it (ADR 0003).

Models the endpoint lists: `MiniMax-M3`, `MiniMax-M2.7`, `MiniMax-M2.7-highspeed`, `MiniMax-M2.5`, `MiniMax-M2.5-highspeed`, `MiniMax-M2.1`, `MiniMax-M2.1-highspeed`, `MiniMax-M2`. `https://api.minimaxi.com/v1` and `https://api.minimax.chat/v1` both answer 401; only `api.minimax.io` authenticates.

**One asymmetry an operator has to know, because "OpenAI-compatible" does not imply it.** On this endpoint a *tool* schema is enforced — a call returns the declared function with well-formed JSON arguments — while a *response* schema is not: `response_format: json_schema` is accepted with HTTP 200 and ignored. Tool calling holds; the response schema is decoration. The agent no longer sends a response format at all: the memory writeback travels as a reserved tool call and a turn does not end until the model makes it (#615), so this asymmetry costs an operator nothing here. It is recorded because it is the kind of difference that is met after a turn rather than at the config line. Probed 2026-08-17 on `MiniMax-M2`, `MiniMax-M2.1`, `MiniMax-M2.5-highspeed` and `MiniMax-M3`; re-check against the endpoint before relying on either half.

#### A gateway that issues short-lived tokens

An endpoint that authenticates the agent with a token that expires — garam's AI gateway is the case this was built for — takes the token from a file rather than from an environment variable, because a variable cannot change under a running process:

```yaml
model:
  provider: openai-compatible
  base-url: https://ai.garam.sh/v1
  model: auto
  api-key-file: /var/run/sherlock/model-token
```

The platform rewrites the file, and should replace it atomically (a new file renamed over the old one). The agent reads it for every model request, so a new token takes effect on the next request without a restart, and a request answered with 401 reads it once more and is sent once more. A file that cannot be read, or holds nothing, fails startup; one that goes missing later fails the model call with an error naming the file, and the agent keeps running (ADR 0029). Mount it into the agent container only, never the workspace's (ADR 0003).

#### Embeddings

Recall ranks mid episodes by embedding similarity, so every provider but `mock` also needs an `embedding:` section naming an OpenAI-compatible `/embeddings` endpoint. It is separate from `model:` because the chat endpoint need not serve embeddings, and the embedder speaks only the OpenAI-compatible protocol whatever `model.provider` names. The model the replication and the parity run measured, `BAAI/bge-base-en-v1.5`, runs locally in Hugging Face's text-embeddings-inference server (`research/mid-memory-rag/README.md` §Production parity (issue #858)):

```yaml
embedding:
  base-url: http://127.0.0.1:8090/v1
  model: BAAI/bge-base-en-v1.5
```

Keep one embedding model per memory file: stored vectors belong to the model that wrote them (`docs/architecture/memory.md` §Existing databases).

#### Anthropic, on `anthropic-compatible`

Anthropic's Messages API is a different protocol rather than a different vendor, so it takes its own value:

```yaml
model:
  provider: anthropic-compatible
  base-url: https://api.anthropic.com/v1
  model: claude-sonnet-4-5
  api-key-env: ANTHROPIC_API_KEY
```

**This configuration has never been run against the live API.** There is no Anthropic key in the environment this was built in, so no request has left the machine: the client is written to the protocol's documented contract and tested against a local fake that asserts the request it emits. Those tests confirm the client sends what this implementation believes the API wants; **they cannot confirm the API agrees.** The first real call is the thing that would establish that, and it has not happened. Treat the block above as unexercised until someone runs it.

Three differences from the chat-completions protocol are handled in the client rather than by the operator, and are listed because they are what a first live call would exercise: the system prompt moves to a top-level field instead of a message role, a tool result is a block on a user message instead of a role of its own, and `max_tokens` is required rather than optional (`internal/agent/anthropic`, default 4096 — a response that hits the ceiling is returned as an error rather than as a truncated document).

#### What each protocol gives the agent

| | `openai-compatible`, OpenAI | `openai-compatible`, MiniMax | `anthropic-compatible`, Anthropic |
|---|---|---|---|
| Tool calling | enforced | enforced (probed) | not run |
| Response schema on the reply | enforced via `response_format` | accepted and ignored (#615) | no such field in the protocol |
| Verified by | in use | probed 2026-08-17 | nothing yet — no key, no live call |

The response-schema row decided how much the writeback could rely on the wire until it stopped travelling that way. The writeback is a reserved tool call (#615), so it rests on the tool-calling row above instead; the response-schema row records a protocol difference the agent no longer depends on.

### Agent ego

The agent's instructions are composed of up to three parts, in this order: an **ego** (identity and behavior guidance) you own, optional **operator instructions** (what your deployment expects of the agent, such as how to answer a transport's message envelope) you also own, and a **platform contract** (tool use, memory writeback format, reply conventions) the platform always appends last — neither of your files can disable the memory loop (ADR 0024).

The ego is a Markdown file, resolved in order: `--ego-file <path>`, then the embedded default (`internal/agent/ego.md`). The operator instructions are a second file, `--instructions-file <path>`, left out when not named; naming one keeps the ego, embedded or yours. Neither has a config key, and a missing or unreadable file fails at startup. Changes apply on restart; neither file is hot-reloaded.

```sh
sherlock agent --agent-id my-agent --ego-file ./EGO.md --instructions-file ./INSTRUCTIONS.md --message "hello"
```

An ego file describes who the agent is and how it behaves — not the protocol. Example:

```markdown
You are Nimbus, an autonomous site-reliability agent running one continuous
session per environment. You prefer small, reversible changes and explain
every destructive action before taking it.
```

### Adding tools

Tools are stand-alone executables behind the script-tool contract (ADR 0017, ADR 0009). The agent binary does not import them; the script-tool runtime (`internal/tool`) reads each tool's `manifest.json` from the `tools/` directory at startup and spawns the binary as a one-shot stdio process. To add a tool:

- drop a directory under `tools/<name>/` containing `manifest.json` (name, description, `command` path relative to the manifest's dir, and the `input_schema` and `output_schema` JSON Schemas) and the executable the manifest names. First-party tools ship in place: `file_read`, `file_write`, `file_list`, `shell_exec`, `message_send`, `github_push`, `github_open_issue`, `github_open_pull_request`, `github_comment` — `make build-tools` rebuilds them. A tool that needs files or exec dials the workspace's gRPC listener itself at `SHERLOCK_WORKSPACE_ADDR`, which the agent sets for every tool it spawns; a tool that needs an external API calls it directly. The default install is the nine shipped tools; override the directory with the `tools-dir` key (or `SHERLOCK_TOOLS_DIR`, the same setting through the environment; default `./tools`) or remove a manifest to drop a tool from the agent's surface. The script-tool runtime validates the required tool set at startup and refuses to load a tool whose manifest hash drifts from the pin you gave it.

Tools inherit the agent's environment, including its credentials; see [Security](docs/architecture/security.md) for the tool trust boundary.

The pin set is yours to write, because only you can say which manifest you reviewed. Put it under `tools:` in the agent config, one SHA-256 per tool name:

```yaml
tools:
  pins:
    message_send: 3f0a…
    file_read: 8c21…
```

Each hash is on the `tool manifest loaded` line the agent logs for that tool, and a refusal quotes the hash you would pin to accept what is on disk. Once the section exists it is the whole accepted set: a manifest that changed under a pin is refused, and so is a tool directory the set does not name — a drop you did not review is otherwise indistinguishable from one you did. Adding a tool is then a directory drop plus its hash. Leave the section out and the agent loads the tree unpinned, warning at startup that it has no record to check drift against.

Example configs under `configs/`: `configs/agent.yaml` (agent) and `configs/workspace.yaml` (workspace).

With the default `mock` provider the agent wires the scripted mock model, which answers the first message with a canned run of the full loop: it computes through `shell_exec` (the call crosses the wire to the workspace's Exec RPC and runs in the workspace directory), reads the result back through `file_read`, and replies via `message_send`. Its terminal response carries the reply and an empty note. The completed turn is saved as source-linked conversation memory. The script covers the first message per process; a later message fails its turn with `script exhausted` (a `turn_failed` event) — the mock is a wiring demo, not a conversation partner. With `provider: openai-compatible` it runs against a real chat model; a missing API key env var fails at startup. To work on the codebase you need Go and GNU Make — `make test` runs the Go tests. The standard checks are:

```sh
make ci      # lint + format check + test + build (same as CI)
```

Individual targets: `make lint`, `make format`, `make test`, `make build`.

`make test-privileged` requires root and the capabilities authored in
`deploy/local/docker-compose.yml`. It narrows the run to that set and exercises
both the shell tests and shipped-workspace Exec with a distinct exec UID.
The binary cases verify credentials, shared file ownership, environment and
network isolation, resource limits, and deadlines; missing privilege fails
instead of skipping. The privileged CI job invokes the same target on PRs to
`main`.

## Development

Beyond Go and GNU Make, the toolchain is:

- **golangci-lint** — `make lint` runs it from your PATH, with its cache in `.cache/golangci-lint` inside the checkout, so a result recorded by another checkout never reaches it. CI installs `v2.12.2` through `golangci-lint-action` (`.github/workflows/ci.yml`), which is the one place the version is enforced; `.golangci.yml` carries it in a comment beside the config-schema version it declares.
- **buf** — `make proto` lints the proto files, regenerates the workspace gRPC stubs (`buf.yaml` + `buf.gen.yaml`), and fails if a regenerated stub is uncommitted; the CLI version is pinned in `.github/workflows/ci.yml` (1.72.0).
- **lefthook** — `make hooks` installs the git hooks once after cloning (pre-commit lint/format, pre-push test).

Every target exports `GOTOOLCHAIN` as a minimum built from `go.mod`'s `go` directive, so a go command run through `make` builds each tool it fetches with at least that toolchain — `go install tool@version` otherwise takes the toolchain from the tool's own `go.mod` and can hand you a binary that cannot load this module's packages. A `go install` typed outside `make` is not covered by it and takes the floor from your own environment.

This repository is developed by AI agents under written conventions — see `AGENTS.md` for what a contributor is held to and `docs/convention/` for the rules all code follows. Internal structure and design decisions live in `docs/architecture/`.
