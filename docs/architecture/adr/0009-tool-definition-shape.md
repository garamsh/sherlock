# ADR 0009: The tool definition shape — a descriptor plus a Go implementation, imported at compile time

> Status: accepted
> Date: 2026-08-02

## Context

Capabilities must join the agent without a platform-owned plugin mechanism: browser automation, a GitHub client, a one-off utility. The tool contract — name, description, input/output JSON Schema, call — is a Go interface; whatever satisfies it is a tool. ADR 0018 settled where tools live: agent-side Go packages the agent imports at compile time.

## Decision

A tool is defined by a descriptor (name, description, input/output JSON Schemas) plus a Go implementation of the `tool.Tool` contract. Tool packages live under `tools/`; the agent binary imports them at compile time and constructs them at startup with the workspace gRPC client it dials from `--workspace-addr` (ADR 0018). There is no runtime discovery, no plugin loader, and no config-registered executables — the compile-time registry is the only registry, so there is nothing to configure twice.

## Consequences

- Adding a capability is a code change in the agent's build: a new package under `tools/`, imported and registered by `cmd/gagent/agent.go`. The agent and the capability move together.
- `tools/playwright/` is the reference example: browser automation as a Go package in the agent's build.
- The shape replaces the former config-registered script-tool model; that history lives in git.
