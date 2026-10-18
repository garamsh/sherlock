# ADR 0028: `sherlock workspace mcp` is a stdio MCP server whose tools each run one workspace CLI operation as a child process

> Status: accepted
> Date: 2026-10-18

Drafted for #1112, step 2 of the owner's direction relayed in #1110. Facts about Sherlock hold at `dev` `499108d2bdb884bb648bfebf266381415a486383`.

## Context

The owner's direction (#1110, item 3): "build an MCP server that drives that CLI, so any MCP-capable agent can operate a Sherlock workspace with the same isolation", with Claude Code with its local tools disabled as the example. The safety property is that the agent operates a separate workspace, not the machine it runs on.

Facts this has to fit:

- **The CLI exists and its contract is fixed.** `sherlock workspace read|write|list|exec` each make one call on the workspace listener (ADR 0027 §3). The result goes to stdout and a failure to stderr as one JSON object. The exit status is the class: 0 performed, 1 the CLI's own failure, 2 the workspace refused, 3 unreachable or no answer (ADR 0027 §4; `cmd/sherlock/workspace_client.go:59-74`). #1112 changes neither that contract nor the workspace contract.
- **ADR 0027 §5 already chose the CLI's shape for this server**: one process per call with no session, a process contract any language can drive, and exit 2 and exit 3 as the two answers the server gives (`docs/architecture/adr/0027-workspace-operations-are-sherlock-workspace-subcommands.md:69-74`).
- **`sherlock workspace` serves, and refuses a positional argument** (`cmd/sherlock/workspace.go:29`). Its operation subcommands are added at `cmd/sherlock/workspace.go:39`.
- **The listener is loopback-only and unauthenticated** (ADR 0012). Nothing here may widen who can reach it.
- **No Go MCP implementation is in the module.** `go.mod` at the revision above requires none.

## Decision

`sherlock workspace mcp` serves the Model Context Protocol over its standard input and output. It offers four tools — `read`, `write`, `list` and `exec` — and each call runs the matching `sherlock workspace` operation subcommand once, as a child process of the same binary. It maps the child's exit class onto the MCP result. The server does nothing else on its own host.

### 1. Transport: stdio only

- **The client starts the server and talks to it over stdin and stdout**, one JSON-RPC message per line, as the MCP stdio transport defines. That is how an MCP client such as Claude Code runs a local server, from a command line in its configuration.
- **No listener is opened.** A stdio server is reachable only by the process that started it, so it adds no network surface and needs no authentication. Who can reach the workspace stays decided by the listener's bind gate (ADR 0012).
- **stdout carries the protocol and nothing else.** A child's stdout and stderr are read by the server and never pass through to it, and the server logs only to stderr.

### 2. Where it lives

- **A subcommand of the `sherlock` binary, `sherlock workspace mcp`**, beside the four operations. It keeps ADR 0027 §1's reason: one noun for the workspace, whether a caller serves it, operates it, or serves it to an MCP client.
- **`mcp` is not an operation.** It makes no call on the listener itself, and ADR 0027 §3-§5 do not apply to it. ADR 0027 is updated in place to say so.
- **It takes no positional argument**, and plain `sherlock workspace` still serves the gRPC listener exactly as before.
- **No new package.** The server is code in `cmd/sherlock/`. Its one caller is the subcommand, and a package between them would hide nothing (`simplicity.md`).

### 3. It runs the CLI; it does not share the CLI's code in-process

- **Each tool call execs `os.Executable()`** with `workspace <operation> --workspace-addr <addr>` and the call's arguments, and waits for it. The binary that serves MCP is the one that runs the operation, so no `PATH` lookup can substitute another build or another program.
- **This is "drives that CLI" (#1110) as ADR 0027 §5 shaped it.** The CLI's exit class is the one classification of a call, made in one place. The server reads it and does not repeat it.
- **The server's only effect on its own host is running the CLI.** It links no workspace stubs and opens no gRPC connection, so what it can do is what the four operations can do.
- **The server's one setting is the address it passes on**: `--workspace-addr`, then `SHERLOCK_WORKSPACE_ADDR`, then `127.0.0.1:8081`, resolved as the operations resolve it, with no config file. The child gets it as `--workspace-addr`, so the child resolves nothing differently. The child inherits the server's environment, and the CLI forwards none of it to the workspace (ADR 0027 §2).
- **A process per call costs a fork and exec**, a few milliseconds against a gRPC round trip and the command itself. No connection outlives a call.

### 4. The tools

| Tool | Input | Runs | Result on exit 0 |
|---|---|---|---|
| `read` | `path` | `workspace read <path>` | the file as text content when it is UTF-8, otherwise as an embedded resource holding the bytes |
| `write` | `path`, `content` (string) | `workspace write <path>`, `content` on stdin | `{"bytes_written":n}` as text and as structured content |
| `list` | `path` | `workspace list <path>` | `{"entries":[…]}` as text and as structured content |
| `exec` | `command`; optional `args` (strings), `stdin` (string), `timeout` (a Go duration such as `30s`) | `workspace exec [--timeout <timeout>] [--stdin] -- <command> [<args>…]`, `--stdin` only when `stdin` is given | `{"stdout","stderr","exit_code","timed_out"}` as text and as structured content |

- **One tool per operation, and none beyond them** (#1112 criterion 2). No tool reads or writes the server's host, and none passes a flag the operation lacks.
- **`exec`'s mapping is the CLI's**: no `args` runs `command` through `sh -c`, and `args` runs `command` as the executable with them. The child's isolation is the CLI's, fixed: no network, the resource caps, the scrubbed environment, the workspace directory (ADR 0027 §3).
- **`write` takes text.** MCP tool arguments are JSON, so `content` is a string; the CLI's stdin carries its UTF-8 bytes.

### 5. Exit classes in the MCP result

| CLI exit | MCP answer | Carries |
|---|---|---|
| 0 | a tool result | the result above |
| 2 | a tool result with `isError: true` | the CLI's stderr object as text: class `workspace`, the gRPC code, the message |
| 3 | a JSON-RPC error, code `-32001` | message `workspace unreachable: <message>`, and the CLI's stderr object as `data` |
| 1, or any other exit, or a child that could not start | a JSON-RPC error, code `-32603` (internal error) | the CLI's stderr object as `data` when it printed one |

- **Exit 2 goes to the model.** A refusal — a path outside the workspace, an executable the workspace cannot start — is something the model can correct, and MCP's tool-error result is how a model sees one.
- **Exit 3 goes to the client as the server's error.** No change to the arguments fixes a workspace that is down. A distinct code lets a client tell it from every other failure without parsing the message.
- **Exit 1 is a fault of the server or the CLI, not the model.** The server builds every argv from validated input, so a usage failure means the two disagree. A failed write of the result after the workspace performed the operation (ADR 0027 §4) ends here too. The `data` carries the class, so a client can tell which.

### 6. The dependency

- **`github.com/modelcontextprotocol/go-sdk`, at `v1.8.0`, using its `mcp` package.** It is the MCP project's own Go SDK. It gives the server the stdio transport, protocol version negotiation and input-schema validation. It also gives the tests a client that is not this server's own code, which a hand-written protocol subset could not offer.
- **Version basis.** `go list -m -versions github.com/modelcontextprotocol/go-sdk`, run on 2026-10-18, ends `… v1.7.0 v1.8.0-pre.1 v1.8.0-pre.2 v1.8.0`. `go list -m -json github.com/modelcontextprotocol/go-sdk@latest` reports `v1.8.0` with time `2026-09-04T08:08:52Z`. Its `go.mod` requires `go 1.25.0`, below this module's `go 1.26.5`.
- **What it links.** `go list -deps` on a program importing only `mcp` lists these modules outside the standard library and the SDK:
  - `github.com/google/jsonschema-go` v0.4.3;
  - `github.com/segmentio/encoding` v0.5.4 and `github.com/segmentio/asm` v1.1.3;
  - `github.com/yosida95/uritemplate/v3` v3.0.2;
  - `golang.org/x/oauth2`, v0.35.0 in the SDK's requirements, selected here at v0.36.0 by minimal version selection;
  - `golang.org/x/sync`, v0.20.0 in the SDK's requirements, selected here at v0.23.0;
  - `golang.org/x/time` v0.15.0;
  - `golang.org/x/sys`, at v0.41.0 in the SDK's requirements, already required here at a later version (v0.48.0 at the revision above, v0.49.0 since #1125), which minimal version selection keeps.
  
  The SDK's `auth` and `oauthex` packages are linked through `mcp`. Nothing in this decision configures them, and the stdio transport carries no credential.
- **Advisory check.** `govulncheck -show verbose ./...` (scanner v1.7.0, DB updated 2026-10-08 22:31:09 UTC) on that program, under `go 1.26.5`, reports nothing in the SDK or the modules it adds. It reports one finding outside the standard library: GO-2026-5024 in `golang.org/x/sys` v0.41.0, fixed in v0.44.0, Windows-only and not called. It does not apply here, since the module resolves `x/sys` to v0.48.0 or later. The standard-library findings are the `go1.26.5` toolchain's. `govulncheck ./...` on this module at `8719dea396ca476fc9d45aa4cb49f8b1cd9eac84`, without the SDK, reports 17 of them already.
- **Pinning and moving.** The version is pinned by `go.mod` and checked against `go.sum` and the checksum database. `.github/dependabot.yml`'s weekly `gomod` entry (#1120) proposes bumps for the SDK as for every module in `go.mod`, and each bump is reviewed as its own pull request.

## Rejected alternatives

- **The streamable HTTP transport, or HTTP first.** It opens a listener, which is a second unauthenticated route onto the workspace beside the gRPC one. It would need its own bind gate and authentication before it could leave loopback. No client named in #1110 needs it: Claude Code runs stdio servers.
- **Calling the CLI's code in-process.** It would save a fork per call, and that is all. The server would then hold the gRPC client and classify failures itself, a second copy of what ADR 0027 §4 does in the CLI. That is the reading of "drives that CLI" which #1110's wording and ADR 0027 §5 exclude.
- **A separate executable** (`cmd/sherlock-mcp/`). It costs a build target and a place in the images, as in ADR 0027's rejection. It would also have to find the `sherlock` binary at run time, which `os.Executable()` makes unnecessary.
- **A top-level `sherlock mcp`.** It names the protocol and not what is served, and splits the workspace across two nouns.
- **A hand-written MCP subset** (initialize, tools/list, tools/call over newline-delimited JSON-RPC). It adds no module, but protocol upkeep becomes this project's. Tests would check the server against the project's own reading of the spec, with no independent client.
- **Every failure as a tool result with `isError`.** The model would be told a workspace outage as if it were a mistake in its arguments. ADR 0027 §5 separates the two for that reason.
- **Tools beyond the four**, such as a git tool. git runs through `exec` under its isolation (ADR 0003, ADR 0010), and a new operation belongs in the workspace contract first (#840).

## Consequences

- An MCP-capable agent with no local tools can operate a Sherlock workspace by configuring one command, `sherlock workspace mcp --workspace-addr <addr>`. Everything it does happens in the workspace, through the CLI and the contract.
- The listener, the contract and ADR 0027's CLI contract are unchanged. A future CLI operation needs a matching tool here, and a future CLI exit class needs a row in §5.
- The module gains one direct dependency and the modules in §6. A new SDK release arrives as a Dependabot pull request from the weekly `gomod` entry, like every Go module here, and is reviewed as its own change.
- **Records updated in place with this one** (`docs/architecture/README.md` §Rules, "ADR records are living"): ADR 0027 §1, which now says `mcp` is a subcommand under the same noun and not one of the operations its §3-§5 govern.
- **Responsibility documents paired with this record** (`docs/architecture/README.md` §Rules, "Decisions land in pairs"): `environment.md` (the MCP server, its transport and tools, the exit-class mapping, and the rewritten open question on a remote tool form), `packages.md` (`cmd/sherlock/` as the home of the server, which drives the CLI and links no workspace stubs).

## Signs the decision was wrong

- MCP clients that matter cannot start a local process, so the stdio transport leaves them out and the HTTP route has to be built with its own gate after all.
- The per-call fork shows in latency or load, which would mean in-process calls were worth the second copy of the classification.
- Clients surface exit 3's JSON-RPC error worse than a tool error, so models stall instead of reporting the outage, which would mean the outage belongs in the tool result with a marker.
- The SDK's releases break the server or pull in modules faster than they are reviewed, which would mean the hand-written subset was the cheaper path.
- A tool is asked for that the CLI lacks, which would mean the CLI, and the contract behind it, need the operation first.
