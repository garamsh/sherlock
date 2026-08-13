# ADR 0001: Go stack and single-module layout

> Status: accepted
> Date: 2026-07-25 (revised in place 2026-08-13)

## Context

`docs/PLAN.md` §6 fixes the language as Go and the working language as English. PLAN §5.1 names a top-level structure of `gateway/`, `agent/`, `tools/`, while `docs/convention/stack-go.md` mandates Layout A (`cmd/<binary>` + `internal/<domain>`) with a banned-name list. The two must be reconciled without adding layers.

## Decision

One Go module at the repository root. Components map to packages and binaries, not extra top-level folders:

- Platform binaries live under `cmd/` — `cmd/gagent/` is one binary whose subcommands are the two pod processes (ADR 0018): `agent` and `workspace`.
- First-party tool binaries live under `cmd/<tool>/`, sibling of `cmd/gagent/` (ADR 0018, ADR 0009). Each is an independent tool server; the agent binary does not import them.
- The reasoning loop → `internal/agent/`; ingress and monitoring → `internal/gateway/`; the platform tool client → `internal/tool/`. Domain machinery that is not a tool stays in `internal/<domain>/` (`internal/memory/`, `internal/workspace/`, `internal/workspace/shell/`, `internal/workspace/files/`, `internal/workspace/client/`).
- End-to-end tests that wire multiple domains live in `tests/` at the module root, per stack-go.md §7.

## Consequences

- The platform binary (`cmd/gagent`) holds no tool behavior. Tool implementations are sibling binaries the platform reaches through the protocol; a capability is added by installing a tool server, not by a Go import.
- The platform's tool client (`internal/tool`) is the only place the platform knows about tools, and what it knows is the protocol — discovery, call, boundary validation, permission policy, drift pinning.
- Conventions take precedence over PLAN folder names (stack-go.md).
