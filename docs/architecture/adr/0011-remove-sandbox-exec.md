# ADR 0011: Remove `sandbox_exec` — the agent has no shell

> Status: accepted
> Date: 2026-07-30

## Context

PLAN §2.3 listed `sandbox_exec` as one of two required MVP tools (alongside `memory`). The original PLAN §4 even described it as a Docker-based "create/exec/destroy" sub-sandbox tier.

ADR-0002 then **rejected** the two-tier sandbox model: "The environment is the agent's body. Code the agent runs shares fate with the agent — there is no separate sandbox container tier." But the same ADR kept `sandbox_exec` as a tool, redefining it as "execute a command inside my own environment" — i.e. the agent's own container, not a sub-container.

The two halves of ADR-0002 are in tension:

1. "No separate sandbox tier" — the principle.
2. "sandbox_exec means execute in my own environment" — the escape hatch that let the tool stay in the codebase as "just an internal tool".

The risk ADR-0002 itself flagged but did not eliminate was **"secret exposure through a shell tool"**. ADR-0007 papered over it with the trust-domain invariant ("a CodeExecuting tool runs with no reachable secrets"), enforced only at design review and at the container-network-policy layer (NetworkMode("none")). The user's review surfaces that this is the wrong shape: a generic shell inside the agent's body is the most dangerous tool in the surface (any code path can reach network, package managers, the filesystem outside `workspace/`), and the only ways to keep it safe are

- (a) extensive container-level restrictions that already limit what the agent can do anyway, **and**
- (b) the trust-domain review that has to be repeated for every future tool

A general-purpose agent in this architecture needs three kinds of capability, not four: persistent memory (`memory`), in-workspace file operations (`files`), and outbound messaging (`message_send`). All three are typed and narrow. The fourth — arbitrary shell execution inside the environment — does not fit the tool-centric discipline established by ADR-0010 ("Tool is the only abstraction"), and it is the only one that needs the heavyweight Docker SDK + UID trust domain + egress allowlist scaffolding to be safe.

The user has confirmed the call: remove `sandbox_exec` entirely.

## Decision

The four built-in tools become three. `sandbox_exec` is removed.

- Delete `internal/sandbox/` in full.
- Drop the `github.com/docker/docker` SDK (only used by `sandbox/docker.go`). This removes the runtime dependency on a Docker daemon; the platform binary no longer needs Docker at runtime to do anything agent-facing.
- Drop the `sandbox.Runtime`, `sandbox.Policy`, `sandbox.Config`, `sandbox.Result` types and the `DockerRuntime` / `ExecTool` implementations.
- Drop `Factory.Sandbox`, `Factory.Image`, `Factory.Policy`, `Factory.ExecTimeout` fields in `internal/gateway/runtime.go::Factory`.
- Drop the corresponding CLI / config keys: `--image`, `--exec-timeout`, `--cpu-limit`, `--memory-limit` (and their config-file keys). These were only meaningful in the spawned-sandbox-container model. With no spawned container, they have no effect; leaving them as inert config would be a footgun.
- Update `cmd/gagent/mock_model.go` to drive the canned MVP scenario through `message_send` + `memory` only (no `sandbox_exec` call).
- Update `internal/agent/instructions.go::Contract` and `internal/agent/EGO.md` to remove the `sandbox_exec` line; the example memory block stays.
- Update `tests/e2e_test.go` and `tests/gateway_test.go` to drive the canned MVP scenario without `sandbox_exec`.

What stays:

- The three remaining built-in tools: `memory`, `files`, `message_send`.
- The `Environment` directory layout (`internal/environment/`): the agent's body is the directory, not a container.
- `BuiltinToolNames()` returns `["memory", "files", "message_send"]`.
- The container / UID trust-domain model (ADR-0006) is **not** undone — it still describes what the deployment environment should look like (the platform runs in a distroless nonroot container; the agent's body inside that container is its directory). What changes is that there is no inner container that the platform spawns on the agent's behalf.
- The egress proxy (ADR-0004) stays deferred per `security.md` open questions. With `sandbox_exec` gone there is one fewer shell that could exfiltrate; the egress concern is even smaller than before.
- The mock provider (`internal/agent/scripted.go`) stays as the default offline model; the canned script (now in `cmd/gagent/mock_model.go`) is updated, not removed.

## Consequences

- **One fewer tool class to trust.** The agent's surface is now `memory` + `files` + `message_send`, all narrow and typed. The dangerous generic shell is gone.
- **No Docker at runtime.** The platform binary no longer talks to a Docker daemon. The `Dockerfile` at the repo root still builds the platform image; `deploy/local/docker-compose.yml` still runs that image. But the agent inside the running platform never spawns containers.
- **Smaller dependency surface.** `github.com/docker/docker` (~hundreds of MB of transitive deps including `errdefs`, `moby`, `containerd/*`) is gone from `go.mod`/`go.sum`. `internal/sandbox/docker_test.go` and `internal/sandbox/exec_test.go` are gone with the package.
- **Documentation that referenced `sandbox_exec` is updated.** README config table drops the four sandbox-related keys; `deploy/local/README.md` no longer mentions `sandbox_exec` in its mock-script example; `architecture/README.md` removes the `internal/sandbox/` row from the modules table; the five responsibility docs (`environment.md`, `agent.md`, `gateway.md`, `security.md`, `architecture/README.md`) drop `sandbox_exec` references; ADR-0001 drops `internal/sandbox/` from the tool-implementation list.
- **The MVP "PLAN §5.7 scenario"** (the end-to-end "question → memory → sandbox → memory" loop in PLAN §5.7) is re-shaped to "question → memory → files / message_send → memory". The mock provider's canned script updates accordingly.
- **ADR-0002's wording is tightened.** The bullet "sandbox_exec means 'execute a command inside my own environment'" is removed (there is no such tool); the bullet "Self-destruction is bounded by construction" stays because it now refers to file-system destruction within `workspace/`, which is what remains.
- **Tests shrink, not grow.** The 3 daemon-gated scenarios in `internal/sandbox/{docker,exec}_test.go` are deleted. The two MVP scenarios in `tests/{e2e,gateway}_test.go` are rewritten to drop the `sandbox_exec` step.

## Rejected alternatives

- **Keep `sandbox_exec` and harden it further** — add a command allowlist, dry-run mode, etc. Each hardening layer is another piece of surface to review and test; the tool's value (a generic shell) does not justify the cost. Removing the tool is cheaper and safer than hardening it.
- **Replace `sandbox_exec` with a typed compute tool** — e.g. a `python_exec` tool that runs a Python script in a sub-process. Same risk profile as `sandbox_exec` (a shell is a shell); same Docker requirement; same trust-domain concerns. Defer until a concrete use case appears.
- **Make `sandbox_exec` opt-in via a config flag** — turns a default tool into a configuration step. Inverts the principle: tools should be safe by default; removing the unsafe one is the simpler expression of that.

## What this ADR does NOT change

- **ADR-0001** — tool implementations under `internal/<domain>/`; the entry for `internal/sandbox/` is removed, the others stay.
- **ADR-0002** — the body model and the rejection of two-tier sandbox. The body model stands; only the `sandbox_exec` bullet is removed.
- **ADR-0003** — per-environment memory in `memory.db`. Untouched.
- **ADR-0004** — secret-free execution domain. With `sandbox_exec` gone there is no in-body shell for secrets to leak through; the principle is now enforced by absence rather than by the trust-domain review. The proxy and reference-resolution work stays deferred.
- **ADR-0005** — model interface only. Untouched.
- **ADR-0006** — single-container UID trust domains. The deployment context (the platform runs as nonroot in distroless) stays; the only thing removed is the platform spawning a second container for `sandbox_exec`.
- **ADR-0007** — tools own security, trust-domain invariant. The invariant still applies to the three remaining tools: `files` is `TrustCodeExecuting`, `memory` and `message_send` are `TrustSecretHolding`. The note about MCP server processes was already removed by ADR-0010.
- **ADR-0008** — memory lifecycle. Untouched.
- **ADR-0009** — gateway I/O. Untouched.
- **ADR-0010** — Tool is the only abstraction. Strengthened: the four tools become three, all narrow.

## Open questions

- None. If a future use case needs compute, it lands as a new typed tool (e.g. a `package_json_query` tool that runs `jq` on a workspace file), reviewed for trust domain and added to the registry like any other. No shell.
