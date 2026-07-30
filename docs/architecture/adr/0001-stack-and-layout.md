# ADR 0001: Go stack and single-module layout

> Status: accepted
> Date: 2026-07-25

## Context

`docs/PLAN.md` §6 fixes the language as Go and the working language as English. PLAN §5.1 names a top-level structure of `gateway/`, `agent/`, `tools/`, while `docs/convention/stack-go.md` mandates Layout A (`cmd/<binary>` + `internal/<domain>`) with a banned-name list. The two must be reconciled without adding layers beyond Gateway / Agent / Tool.

## Decision

One Go module at the repository root. The three components map to packages, not top-level folders:

- Gateway → `cmd/gateway/main.go` (thin entrypoint) + `internal/gateway/`
- Agent → `internal/agent/`
- Tool → `internal/tool/` (shared contract and registry) with one package per tool implementation (`internal/memory/`, `internal/files/`, `internal/message/`)

## Consequences

- The logical 3-component architecture of PLAN §2 is preserved; PLAN §5.1's folder names are interpreted as component names, following stack-go.md (conventions take precedence).
- Tool implementations are ordinary domain packages that import the `internal/tool/` contract; no plugin system or extra layer is introduced.
- End-to-end tests that wire multiple domains live in `tests/` at the module root, per stack-go.md §7.
