# ADR 0027: The workspace's operations are `sherlock workspace` subcommands that reach it only through its listener

> Status: accepted
> Date: 2026-10-18

Drafted for #1111, step 1 of the owner's direction relayed in #1110. Facts about Sherlock hold at `dev` `4709de335d66a14b8884a8a7e6f28445892e12cf`.

## Context

The owner's direction (#1110, item 3): the safety property is that an agent operates a separate workspace, not the machine it runs on. Step 1 exposes the workspace's operations as a workspace CLI; step 2 (#1112) is an MCP server that drives that CLI, so any MCP-capable agent operates a Sherlock workspace with the same isolation.

Facts this has to fit:

- **The contract is four RPCs.** `Workspace` serves Read, Write, List and Exec over a loopback gRPC listener (`internal/workspace/workspace.proto:12-17`). #1111 changes no RPC and no message. git has no RPC; it runs through Exec, under Exec's isolation (ADR 0003, ADR 0010).
- **Every refusal the workspace sends carries one of two codes.** Read, Write and List relabel every filesystem error, a path outside the workspace among them, as `InvalidArgument` (`internal/workspace/server/server.go:35`, `:42`, `:50`). Exec answers a timeout as a normal response with `timed_out` set (`server.go:80`) and any other executor failure, such as `agent_uid unavailable`, as `Internal` (`server.go:85`). `rg -n "codes\.|status\.(Error|Errorf|New)" internal/workspace cmd/sherlock -g '!*_test.go' -g '!*.pb.go'` finds those four sites and no other; the generated stubs answer `Unimplemented` only for a method the server lacks, and it implements all four.
- **`sherlock workspace` is the server, and deployments run it bare.** `build/workspace.Dockerfile:54` is `CMD ["workspace"]` and `deploy/local/docker-compose.yml:83` is `command: ["workspace"]`. The command sets no argument validator (`cmd/sherlock/workspace.go:23-34`), and cobra v1.10.2's default accepts any positional argument on a non-root command with no subcommands (`legacyArgs` in its `args.go`), so today `sherlock workspace raed` starts serving.
- **The only clients are the four tool executables.** `tools/file_read`, `tools/file_write`, `tools/file_list` and `tools/shell_exec` each speak the manifest stdio protocol and dial `SHERLOCK_WORKSPACE_ADDR`, the variable the spawn contract names (`internal/tool/spawnenv.go:18`). `shell_exec` always sets `no_network` and never `trusted` (`tools/shell_exec/shell_exec.go:97-100`).
- **The binary exits 1 on every error** (`cmd/sherlock/main.go:25-27`), and cobra prints its own error and usage text to stderr.
- **Settings resolve in `internal/config/`**, flags over `SHERLOCK_*` environment over a config file over defaults; the agent's file is `agent.yaml` and the server's is `workspace.yaml`, where `addr` is the bind address (`internal/config/workspace.go:29-36`).

## Decision

Four subcommands of the existing `sherlock workspace` command — `read`, `write`, `list` and `exec` — each make exactly one call on the workspace contract, at the address `--workspace-addr` or `SHERLOCK_WORKSPACE_ADDR` names, and do nothing else on the caller's host. Each prints its result on stdout, prints a failure as one JSON object on stderr, and exits 0 when the workspace performed the operation, 2 when the workspace refused it, and 3 when the workspace could not be reached or did not answer.

### 1. Where the commands live

- **Subcommands of the `sherlock` binary, under `sherlock workspace`.** The binary already ships in both images and is what `README.md` tells a user to build; one noun names the workspace whether a caller serves it or operates it.
- **Plain `sherlock workspace` keeps starting the server exactly as today**, with the same flags, environment and config file. The subcommands add nothing to its flag set, and its flags stay its own: `sherlock workspace read --exec-uid 5 x` fails as an unknown flag.
- **A typo cannot start a server.** The server command takes no positional argument: `sherlock workspace raed` exits non-zero with cobra's `unknown command "raed" for "sherlock workspace"` and binds nothing. Today that argument is silently ignored and the server starts (§Context). No deployment in this repository passes one: the two command lines in §Context are what `rg -n 'workspace' build/workspace.Dockerfile deploy/local/ | rg -i 'cmd|entrypoint|command|args'` finds.
- **No new package.** The client is code in `cmd/sherlock/` over the generated stubs in `internal/workspace/`, its one caller. A client package would stand between one caller and the stubs and hide nothing.

### 2. The address and what the CLI reads

- **The listener's address is the CLI's one setting**: `--workspace-addr`, then `SHERLOCK_WORKSPACE_ADDR`, then `127.0.0.1:8081`, resolved in `internal/config/` like every entry point's settings. The flag is the name the agent already uses for the same address, and the variable is the one every tool the agent spawns already holds.
- **No config file is read.** `agent.yaml` is the agent's and `workspace.yaml` is the server's, where `addr` means the bind; reading either ties a client to a process it is not, and a caller outside both containers has neither. One setting with a flag and a variable needs no file. The root's shared `--config` and `--strict-config` are refused when given to a subcommand, rather than ignored.
- **The CLI dials what it is given and gates nothing.** Who can reach the listener is the listener's bind gate (ADR 0012), which this decision leaves as it is: loopback only, unauthenticated.
- **No credential reaches the CLI or passes through it.** It reads no secret and forwards no environment to the workspace.

### 3. What each command sends and prints

| Command | Sends | Prints on success (stdout) |
|---|---|---|
| `read <path>` | `ReadRequest{path}` | the file's bytes, unchanged |
| `write <path>` | `WriteRequest{path, content}`, `content` read whole from stdin | `{"bytes_written":<n>}` |
| `list <path>` | `ListRequest{path}` | `{"entries":[{"name":"…","dir":<bool>}]}` |
| `exec [--timeout <duration>] [--stdin] -- <command> [<arg>…]` | `ExecRequest` (below) | `{"stdout":"…","stderr":"…","exit_code":<n>,"timed_out":<bool>}` |

- **Read and Write move the file's bytes as they are.** A file is not necessarily UTF-8, and a JSON string cannot carry arbitrary bytes without an encoding the caller must undo. Write's result has the field name `file_write`'s output already uses.
- **Exec sends one command.** A single positional is the command, run by `sh -c` as `shell_exec` runs one; more than one sends the first as the executable and the rest as `args`, run without a shell. `--timeout` sets `timeout_nanos`; without it the workspace's `exec-timeout` decides. `--stdin` sends the CLI's standard input as `stdin`; without it nothing is read, so a caller with an open terminal never blocks.
- **Exec runs under the isolation the agent's own exec gets.** The CLI always sets `no_network` and never sets `trusted`, `env` or `workdir`, as `shell_exec` does: the child gets an empty network namespace, the resource caps, the scrubbed environment and the workspace directory.
- **The command's exit code is a result, not the CLI's status.** A command that exits 1, or one the workspace timed out, is an operation the workspace performed: the CLI exits 0 and the result says `exit_code` and `timed_out`.

### 4. Failures and exit status

Every failure prints one JSON object on stderr, `{"error":"<class>","code":"<gRPC code>","message":"<text>"}`, and nothing on stdout. cobra's own error and usage text is silenced for these commands.

| Exit | `error` | When | `code` |
|---|---|---|---|
| 0 | — | The workspace performed the operation | — |
| 1 | `usage` | The CLI sent nothing: a bad argument or flag, a refused `--config`, an unreadable stdin | empty |
| 2 | `workspace` | The workspace answered the call with an error status: a path outside the workspace, a missing file, an executor failure | the status's code, today `InvalidArgument` or `Internal` |
| 3 | `unreachable` | No answer: the connection failed, or the CLI's deadline passed first | `Unavailable` or `DeadlineExceeded` |

- **Class 3 is exactly `Unavailable` and `DeadlineExceeded`; every other status is class 2.** The workspace sends neither code itself (§Context), so either one is the client's report that no answer came.
- **Class 3 does not say the operation did not happen.** A deadline that passes after a Write or an Exec was sent leaves its effect unknown; a caller retrying a write or a command owns that.
- **The CLI bounds every call**, so a workspace that stopped answering ends in exit 3 and not a hang: Read, Write and List at a fixed bound, as the file tools do; Exec at `--timeout` plus a margin for dispatch, or, without `--timeout`, at a stall bound that clears any `exec-timeout`, as `shell_exec` does.
- **Exit 1 stays the binary's status for every other command's error**, so nothing an existing caller of `sherlock agent` or `sherlock workspace` reads changes.

### 5. Why this shape suits the MCP server (#1112)

- **One process per call, no session.** An MCP tool call maps to one invocation: the tool's arguments to argv and stdin, its result to stdout. The server holds no connection and no state between calls.
- **The two failure classes are the two answers an MCP server gives.** Exit 2 is a refusal the calling model can act on — a wrong path, a command the workspace cannot run — and goes back as a tool result marked as an error. Exit 3 means the workspace is down, which no change to the arguments fixes, and goes back as the server's own error.
- **The contract is a process contract.** argv, stdin, stdout, stderr and exit status can be driven from any language, so #1112 needs neither the generated stubs nor gRPC, and the one client of the contract outside the tools stays here.
- **The isolation carries through.** The MCP server's only effect is to run the CLI, and the CLI's only effect is a call on the listener, so an agent driving it — Claude Code with its local tools disabled is #1110's example — reaches the workspace and nothing on its own host.

## Rejected alternatives

- **A separate executable** (a new `cmd/<name>/` linking only `internal/workspace/`). Its import graph would show by itself that it links no executor. It costs a second build target, a place in both images, and a second artifact to version and ship, for a property the subcommands keep by making nothing but the call; and #1112 drives a process either way.
- **A new top-level noun** (`sherlock ws`, `sherlock client`). Two names for one workspace, where `sherlock workspace <operation>` reads as operating it.
- **Driving the existing tool executables.** They speak the manifest protocol to the agent's tool runner, take no args or stdin for Exec, and #1111 keeps them unchanged.
- **One JSON object for Read's content.** It needs base64 or lossy UTF-8 for a file that is not text; the bytes are the result.
- **JSON input on stdin for every command.** Write's content and Exec's stdin are bytes already on stdin, and a path or a command is an argument a person types.
- **Exposing every `ExecRequest` field.** A network namespace, an uncapped trust class or an environment the caller supplies would give a CLI caller more than the agent's own exec has, and `env` is a path for credentials into the workspace.
- **Propagating the command's exit code as the CLI's status.** It collides with the CLI's own classes: a command exiting 3 would read as an unreachable workspace.
- **Mapping each gRPC code to its own exit status.** The caller needs to tell a refusal from no answer, and the code is in the JSON for anyone who needs more.
- **Reading `agent.yaml`'s `workspace-addr`.** Inside the agent container the variable already holds it, and outside it there is no such file.

## Consequences

- The `sherlock` binary becomes a client of the workspace listener, through these four subcommands only. `sherlock agent` still never dials it; the agent reaches the workspace by spawning a tool (ADR 0017).
- A person, a script or an MCP server can operate a workspace through its contract. The listener's exposure, the contract and the tools are unchanged.
- `sherlock workspace <anything>` that is not a subcommand now refuses instead of serving.
- A future workspace that sends `Unavailable` or `DeadlineExceeded` itself would blur classes 2 and 3; such a change revisits §4.
- **Records updated in place with this one** (`docs/architecture/README.md` §Rules, "ADR records are living"): ADR 0017, whose rejected alternative said the binary never calls the workspace gRPC client.
- **Responsibility documents paired with this record** (`docs/architecture/README.md` §Rules, "Decisions land in pairs"): `environment.md` (the CLI, its surface and its output and exit-status contract), `deployment.md` (who dials the workspace), `packages.md` (`cmd/sherlock/` as a caller of the wire).

## Signs the decision was wrong

- Callers or #1112 retry on exit 3 and duplicate writes or commands, which would mean the class needs to separate a failed connection from a deadline after sending.
- #1112 cannot be built without linking the stubs or keeping a connection, which would mean the process contract is the wrong layer for it.
- Operators ask for `exec` with network or without caps from the CLI, which would mean the agent's isolation is the wrong default for other callers.
- A deployment or script passed a positional argument to `sherlock workspace` and breaks on the new refusal.
- The binary's size or start-up cost matters to an MCP caller, which would mean the separate executable was worth its build target.
