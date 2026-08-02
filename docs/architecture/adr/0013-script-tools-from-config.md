# ADR 0013: Script tools registered from the platform config

> Status: accepted (the platform-UID execution decision is superseded by ADR 0014)
> Date: 2026-08-02
> Amends: ADR 0010's scope (see Consequences).

## Context

ADR 0010 removed every external tool loader (MCP), leaving Go built-ins as the only tools: adding a capability meant adding Go code and rebuilding the platform. Operator-driven capabilities (e.g. Playwright browser automation) need to join an environment without a platform rebuild. The `tool.Tool` contract — name, description, input/output JSON Schema, `Call` — is format-agnostic: a subprocess speaking JSON on stdin/stdout satisfies it. This decision landed in code (PRs #117, #119, #121) before its ADR; this document records it retroactively.

## Decision

One universal subprocess adapter (`internal/tool/script/`) runs any executable as a tool: JSON input on stdin, JSON output on stdout, per-call timeout. The platform config's `tools:` section registers script tools alongside the built-ins; a script-tool name colliding with a built-in fails platform startup with `tool.ErrDuplicateTool`.

## Consequences

- ADR 0010 is amended: "no external tool loaders" now means no in-process plugin or network (MCP) loaders. The config-registered subprocess is the single external tool source; the 3-component structure is unchanged because a script tool is still just a `tool.Tool`.
- Adding a capability is either a config change (script tool) or Go code (built-in); there is no third path.
- Script tools run as the platform UID with no privilege drop — unlike `shell_exec` (ADR 0012). They are operator-supplied trusted executables, not agent-supplied code; `security.md` records the trust domains.
- An invalid `tools:` entry (built-in collision, unreadable schema) must fail platform startup, not the first message. The implementation gap is tracked in issue #124.
- `tools/playwright/` is the reference example: browser automation registered by config only.
