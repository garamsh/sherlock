# ADR 0001: Go stack and single-module layout

> Status: accepted
> Date: 2026-07-25

## Context

`docs/PLAN.md` §6 fixes the language as Go and the working language as English. PLAN §5.1 names a top-level structure of `gateway/`, `agent/`, `tools/`, while `docs/convention/stack-go.md` mandates Layout A (`cmd/<binary>` + `internal/<domain>`) with a banned-name list. The two must be reconciled without adding layers.

## Decision

One Go module at the repository root. Components map to packages, not top-level folders:

- Binaries live under `cmd/` — `cmd/gagent/` is one binary whose subcommands are the two pod processes (ADR 0018): `agent` and `sidecar`.
- The reasoning loop → `internal/agent/`; ingress and monitoring → `internal/gateway/`; the tool contract → `internal/tool/`, with one package per tool implementation (`internal/memory/`, `internal/files/`, `internal/message/`, `internal/shell/`).
- End-to-end tests that wire multiple domains live in `tests/` at the module root, per stack-go.md §7.

## Consequences

- Tool implementations are ordinary domain packages that import the `internal/tool/` contract; no plugin system or extra layer is introduced.
- Conventions take precedence over PLAN folder names (stack-go.md).
