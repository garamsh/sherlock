# ADR 0001: Go stack and single-module layout

> Status: accepted
> Date: 2026-07-25 (revised in place 2026-08-13, again 2026-08-14, again 2026-08-26)

## Context

The system's components — gateway, agent, tools — each invite a top-level folder of their own, while `docs/convention/stack-go.md` §1 offers three layouts, the two service ones shaped `cmd/<binary>` + `internal/<domain>`, under a banned-name list. The two shapes must be reconciled without adding layers.

## Decision

One Go module at the repository root. Components map to packages and binaries, not extra top-level folders:

- Platform binaries live under `cmd/` — `cmd/sherlock/` is one binary whose subcommands are the two pod processes (ADR 0017): `agent` and `workspace`. After Phase 4 this directory contains only `cmd/sherlock/`.
- First-party tools live under `tools/<name>/`, each a `manifest.json` plus an executable the manifest names (ADR 0009 §Decision). The platform reads the directory through the script-tool runtime in `internal/tool/` (`NewRegistry`); the agent binary does not import any tool code.
- The reasoning loop → `internal/agent/`; ingress and monitoring → `internal/gateway/`; the platform tool client → `internal/tool/`. Domain machinery that is not a tool stays in `internal/<domain>/` (`internal/memory/`, `internal/workspace/`, `internal/workspace/shell/`, `internal/workspace/files/`).
- End-to-end tests that wire multiple domains live in `tests/` at the module root, per stack-go.md §7.

## Consequences

- The platform binary (`cmd/sherlock`) holds no tool behavior. A capability is added by installing a tool under `tools/<name>/`, not by a Go import.
- The platform's tool runtime (`internal/tool`) is the only place the platform knows about tools, and what it knows is the manifest contract — manifest load, schema-validated call boundary, drift pin, descriptor projection.
- Conventions take precedence over component-shaped folder names (stack-go.md).
