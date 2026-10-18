# Environment

Governs the environment model: what an environment is, its topology, and its trust boundaries. Rationale: ADR 0017, ADR 0008, ADR 0009, ADR 0010, ADR 0027, ADR 0028.

## Current decisions

- The environment is split into two layers: the workspace (the agent's data plane) and the tools (the agent's capabilities). The workspace is a dedicated sidecar container in the agent's pod; the tools are scripts the operator installs under `tools/<name>/` and the platform runs over stdio (ADR 0017, ADR 0009). One pod per agent; one workspace per agent to start.
- The sidecar is the computer: it serves the gRPC workspace contract — Read, Write, List, Exec — over a loopback listener. It holds no tool servers and no `tools:` config; its executor (`internal/workspace/shell`) runs agent-influenced processes in the workspace directory (ADR 0008). The agent runs no exec'd command — the only processes it starts are the installed tool executables (ADR 0021); the model reaches exec through the `shell_exec` script, which calls the workspace's Exec RPC (ADR 0017).
- Tools are scripts with manifests (ADR 0009). The platform specifies only the contract shape and the call protocol; the tool owns its description, validation, behavior, and domain security. Adding a capability is dropping a `tools/<name>/` directory: a `manifest.json` and an executable. No Go import, no protocol implementation, no sidecar config change.
- Installed tools run inside the agent environment and inherit it whole, including every credential the agent holds (ADR 0009, ADR 0017). There is no per-tool credential scope or platform secret dispatcher. This trust posture is separate from the workspace's scrubbed Exec environment (ADR 0003, `security.md`).
- The workspace is configured from outside, like a real working volume: the workspace directory is created on first use, the listener bind address is operator-supplied, and the agent learns the listener's address from its own config (`--workspace-addr`, default `127.0.0.1:8081`).
- The workspace directory is the workspace's own real directory: the workspace serves the typed file API over it and execs children in it, so exec'd programs see workspace files natively — no shared mount and no path-equality trick. The agent holds no workspace-path setting and does not dial the workspace at all: it publishes the address to the tools it spawns, and those that need the workspace dial it themselves (§Current state).
- Permission enforcement lives at the data plane — the action site for files and processes. Workspace writes run under the sidecar's own UID and workspace ownership is kernel-enforced (DAC), not filtered (ADR 0008, ADR 0010). A privileged workspace chowns the workspace directory to the exec UID (`exec-uid`, default 1001), keeps the directory's write bit for its own group, and gives every file it writes over the contract to the same UID, so files written by tools through the gRPC contract and by exec'd children share one owner; an unprivileged workspace owns the directory itself. A platform that cannot run a child as the configured exec UID reports `agent_uid unavailable` instead of silently upgrading (ADR 0008).
- Exec children run under the workspace's trust boundary (ADR 0008, ADR 0010): dropped to the exec UID (workspace key `exec-uid`), bounded per call (`exec-timeout`, default 30s) and by rlimits, with a scrubbed environment and a fresh, empty network namespace. The child applies its own rlimits before it execs the command, so they need no capability and bind from its first instruction; a cap that cannot be applied fails the call instead of running the child uncapped. The per-call timeout is the one bound that is the executor's to enforce rather than the child's to carry, and enforcing it across the drop needs CAP_KILL: without it the call fails closed rather than running unbounded (#612). The unprivileged shape applies unchanged: exec-uid equal to the workspace's own UID needs no privilege, and needs two host requirements for network isolation — `user.max_user_namespaces` above 0 and a seccomp profile admitting the user+network clone, which `RuntimeDefault` does not; any other unprivileged value fails closed. It is supported beside the privileged drop, and what it does not separate is ADR 0008's (`deployment.md` §Volumes and ownership, §Capabilities).
- Untrusted exec has a 2 GiB **per-process virtual address-space** ceiling (`DefaultRlimits` in `internal/workspace/shell/process_linux.go`). This includes runtime reservations, not just resident memory: the former 512 MiB ceiling prevented the local Go runtime from starting (#833). The larger ceiling admits local Go compilation and tests while retaining a hard address-space bound; it also permits more resident allocation per process. It is not an aggregate memory budget for a process tree. Operators must bound aggregate memory with the workspace container's resource policy; CPU, file-size, open-file, dedicated-UID process-count and per-call time bounds remain in force.
- Local Go testing requires an installed toolchain and locally available dependencies: the production workspace image does not include Go (`build/workspace.Dockerfile`). Downloads remain unavailable inside the empty network namespace.
- Egress control lives at the pod network layer — the deployment-level boundary (the infrastructure layer's mechanism, not an artifact this repo ships). Process-level network isolation for exec children is the per-request netns above.
- Self-destruction is bounded by construction: files live on the workspace, and the agent's self (a separate container) is unreachable from it. A broken workspace is recreated.
- A shared workspace serving several agents — one volume with several user accounts — is a later step, not the starting shape.
- Anything that can run a command operates the workspace through its contract with the `sherlock workspace` operation subcommands — `read`, `write`, `list`, `exec` — and reaches nothing on its own host (ADR 0027). Each makes one call on the listener at `--workspace-addr`, then `SHERLOCK_WORKSPACE_ADDR`, then `127.0.0.1:8081`, and reads no config file. They change neither the contract nor who can reach the listener (ADR 0012). §Workspace CLI contract states what they print and how they exit.
- Plain `sherlock workspace` serves the workspace as before and takes no positional argument, so a mistyped operation refuses instead of starting a server (ADR 0027).
- An MCP-capable agent operates the workspace through `sherlock workspace mcp`, a stdio MCP server whose tools `read`, `write`, `list` and `exec` each run the matching operation subcommand once as a child process of the same binary (ADR 0028). It opens no listener and links no workspace stubs, so its only effect on its own host is running the CLI. `mcp` is not an operation: §Workspace CLI contract governs the four operations, and §Workspace MCP server governs it.

## Current state

The pod split is two processes today. `sherlock workspace` serves one workspace (config key, created on first use) over the gRPC contract — Read, Write, List, Exec. It owns no tools and no script-tool runtime, but it owns the executor: Exec children run in the workspace directory, dropped to the exec UID, and when privileged the workspace chowns the directory to the same UID so both writers share one owner. The shipped images make this concrete: the agent image hosts the reasoning loop, gateway, memory, and the tool client — it runs no exec'd command and no tool's behavior in its own process, and the only processes it starts are the installed tool executables, spawned directly, so it stays distroless nonroot with no shell; the sidecar image hosts the workspace gRPC server with a shell (Exec children are `sh -c` sub-processes) and an exec account at uid 1001 (`build/workspace.Dockerfile`); the deployment sets the exec UID children drop to and grants exactly the capabilities the executor needs — SETUID, SETGID, CHOWN, SYS_ADMIN, KILL (`deploy/local/`).

`sherlock agent` holds the reasoning loop, gateway, memory, and the platform tool client (`internal/tool`). The agent's tool set is the operator's `tools/` directory — each tool is a `manifest.json` plus an executable. The platform's stdio runner spawns the executable on demand. Tools that need the workspace (file_read, file_write, file_list, shell_exec) dial it and hold the gRPC client themselves. One binary serves both roles, so it imports `internal/workspace/server` for the `sherlock workspace` subcommand — it is the server. `sherlock agent` never calls the workspace as a client, so the agent side reaches the workspace only through a tool it spawned (ADR 0017). The `sherlock workspace` operation subcommands (`cmd/sherlock/workspace_client.go`) are the binary's only client code for the listener: each dials it once per invocation, through the wire in `internal/workspace/` (ADR 0027). The agent's only workspace setting is `workspace-addr` — the agent keeps no exec machinery and no workspace-path.

## File operation contracts

- `file_read` takes a path and returns content; `file_list` takes a directory path and returns entries. `file_write` takes a path and required string content and returns the number of bytes written. It creates parent directories and replaces existing content; an empty string deliberately creates or truncates an empty file. Missing or null content is rejected before writing, including at the executable's stdin boundary.
- These are the file tools installed under `tools/`; the operation-dependent `files` tool is removed. Operators replace that directory and refresh manifest pins. There is no compatibility write path. The workspace's confinement, ownership and atomic replacement remain the action boundary.
- Each operation has only required inputs, so OpenAI's optional-to-nullable projection adds no absent-content alternative to writes. Provider projection and registry validation remain unchanged. Each tool now has its own six-call allowance per turn; comparisons must report calls and budget refusals by tool, not treat terminal success as evidence of a schema-only effect.
- Issue #811's retained baseline requests show nullable content in the exposed schema, null write arguments, and subsequent `content is required for write` results. The optional manifest property admitted absence while the executable required nonempty content. This establishes a misleading contract, not why the model chose null repeatedly or that the new contract makes it choose usable content. The later PM preparation failures corroborate the symptom; missing terminal reconciliation is outside this file boundary.

## Workspace CLI contract

What the `sherlock workspace` operation subcommands send, print and exit with (ADR 0027).

| Command | Sends | Prints on success (stdout) |
|---|---|---|
| `read <path>` | Read | the file's bytes, unchanged |
| `write <path>` | Write, with the content read whole from stdin | `{"bytes_written":<n>}` |
| `list <path>` | List | `{"entries":[{"name":"…","dir":<bool>}]}` |
| `exec [--timeout <duration>] [--stdin] -- <command> [<arg>…]` | Exec | `{"stdout":"…","stderr":"…","exit_code":<n>,"timed_out":<bool>}` |

- `exec` with one positional runs it through `sh -c`; with more, the first is the executable and the rest its arguments, run without a shell. `--timeout` sets the call's timeout, and without it `exec-timeout` decides; `--stdin` forwards standard input, and without it none is read.
- `exec` always asks for an empty network namespace and never for the trusted class, an environment or a working directory: the child runs exactly as a `shell_exec` child does. The command's exit code and a workspace timeout are results, so the CLI exits 0 for them.
- Every failure prints `{"error":"<class>","code":"<gRPC code>","message":"<text>"}` on stderr, and nothing on stdout beyond the part of a result whose write failed:

| Exit | `error` | Meaning |
|---|---|---|
| 0 | — | The workspace performed the operation |
| 1 | `usage` | The CLI's own failure: it sent nothing (a bad argument or flag, a refused `--config` or `--strict-config`, an unreadable stdin, an unusable address), or it could not write the result after the workspace performed the operation, whose effect then happened and whose result stdout may hold in part |
| 2 | `workspace` | The workspace refused the call: any status it answered with, today `InvalidArgument` for a filesystem refusal such as a path outside the workspace, or `Internal` for an executor failure |
| 3 | `unreachable` | No answer: `Unavailable` (the connection failed) or `DeadlineExceeded` (the CLI's bound passed first). After a Write or an Exec was sent, its effect is unknown |

- Every call is bounded, so a workspace that stopped answering ends in exit 3: Read, Write and List at a fixed bound, Exec at `--timeout` plus a dispatch margin or, without one, at one hour, as `shell_exec` does; an `exec-timeout` above an hour ends such a call in exit 3.
- Every other `sherlock` command keeps exit 1 for any error.

## Workspace MCP server

What `sherlock workspace mcp` offers and answers (ADR 0028).

- Transport: MCP over stdin and stdout, started by the client. No listener; stdout carries only the protocol, and logs go to stderr. Its one setting is the address it passes to each child as `--workspace-addr`: the flag, then `SHERLOCK_WORKSPACE_ADDR`, then `127.0.0.1:8081`, with no config file. It takes no positional argument.

| Tool | Input | Runs | Result on exit 0 |
|---|---|---|---|
| `read` | `path` | `workspace read <path>` | text content when the file is UTF-8, otherwise an embedded resource holding the bytes |
| `write` | `path`, `content` | `workspace write <path>` with `content` on stdin | `{"bytes_written":n}`, as text and structured content |
| `list` | `path` | `workspace list <path>` | `{"entries":[…]}`, as text and structured content |
| `exec` | `command`; optional `args`, `stdin`, `timeout` | `workspace exec [--timeout] [--stdin] -- <command> [<args>…]` | `{"stdout","stderr","exit_code","timed_out"}`, as text and structured content |

| CLI exit | MCP answer |
|---|---|
| 0 | tool result |
| 2 | tool result with `isError: true`, the CLI's stderr object as text |
| 3 | JSON-RPC error `-32001`, `workspace unreachable: …`, the CLI's stderr object as `data` |
| 1, any other exit, or a child that could not start | JSON-RPC error `-32603`, the CLI's stderr object as `data` when it printed one |

## GitHub operation contracts

- `github_push` takes owner, repo, branch and sha; `github_open_issue` takes owner, repo, title, body and labels; `github_open_pull_request` takes owner, repo, head, base, title and body; `github_comment` takes owner, repo, number and message. Every input is required: `body` may be an empty string and `labels` an empty list, and the boundary rejects a missing or null title or body, a field another tool takes, and a comment number below 1.
- These replace the operation-dependent `github` tool, the shape the file tools above took for the same reason (#811). Operators replace `tools/github/` with the four directories and refresh manifest pins and required tool lists. There is no compatibility alias.
- Each tool has only required inputs, so the OpenAI-compatible projection promotes nothing and offers the model neither a null nor another operation's field. Provider projection and registry validation are unchanged.
- Issue #831's live calls (`https://api.minimax.io/v1`, `dev` at `e1b8dcf`, the shipped manifest through `internal/agent/openai` and `internal/tool`) show MiniMax-M2.5-highspeed sending null for every property the projection made nullable, title and body included, in 4 of 4 calls. Sent as hand-built requests to the same endpoint, it did the same in 3 of 3 calls with the null spelled as `anyOf` or listed first, and filled title and body in 3 of 3 when they were not nullable. MiniMax-M3 filled fields of other operations; the issue records `number: 0`, which the shared schema's `minimum: 1` rejected. Against the four tools, both models sent a complete `github_open_issue` input that the registry accepted, 3 of 3 each, on a fake GitHub endpoint and with no token. The 401 in the issue was a local credential and is not addressed here.

## Open questions

- The agent↔sidecar authentication (pod-local workspace listener today) — with the contract protocol work.
- Whether that contract reports the ceilings the workspace applies. A `shell_exec` call carrying no `timeout_nanos` runs under `exec-timeout`, which no tool in the agent container can read, so the tool bounds such a call against a workspace that stopped answering and nothing else — an hour, and the queue makes that every later message for the agent (ADR 0007). #740 asked for that path to become unreachable and is answered with a measurement instead of a change: filling the timeout agent-side and requiring it in the manifest each hand the command's ceiling to the caller, which is the regression #732 fixed, so the only shape leaving `exec-timeout` to decide is a method here reporting what the workspace will apply — a fifth beside Read, Write, List and Exec, and a decision about this contract rather than an implementation of it. With the contract protocol work above.
- The shared-sidecar account model — with the multi-tenancy work.
- A remote tool form for the agent's own tool client (an HTTP tool, or the agent calling a hosted MCP server) when an external integration needs one. The current tool shape is stdio-only; an external transport (HTTP, gRPC) lands when the first real use case arrives, not before. The workspace's MCP server (ADR 0028) runs the other way — it offers the workspace to other agents, over stdio — and settles nothing here.