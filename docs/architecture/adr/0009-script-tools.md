# ADR 0009: Script tools — the tool definition shape

> Status: accepted
> Date: 2026-08-02

## Context

Capabilities must join without Go changes to the platform core: browser automation, a GitHub client, a one-off utility. The tool contract — name, description, input/output JSON Schema, call — is format-agnostic: an executable speaking JSON on stdin/stdout satisfies it.

## Decision

A tool is defined by a descriptor (name, description, input/output JSON Schemas) plus a configured executable with a per-call timeout. One universal adapter runs the executable as a tool: JSON input on stdin, JSON output on stdout.

Definitions live where the implementation runs: the sidecar's config declares the computer's tools (ADR 0018), and the agent discovers them at runtime — there is no central registry to configure twice. An invalid entry (name collision, unreadable schema, empty command) fails the tool server's startup with an error naming the tool, never the first call.

## Consequences

- Adding a capability is a configuration change on the container that owns the implementation; no Go changes, no agent changes.
- `tools/playwright/` is the reference example: browser automation as a computer configuration.
- The shape carries forward from the pre-pod platform config (`tools:` section), whose registration model this record replaces.
