# PLAN.md — File-based agent system prompt with embedded default and merged platform contract

> Status: in progress · Owner: worker agent · Created: 2026-07-26 · Last updated: 2026-07-26 · Related: issue #39 · Stack reference: docs/convention/stack-go.md

## 1. Goal

The agent's system prompt is `persona + platform contract`: the persona resolves from `--agent-prompt <path>` or an embedded `internal/agent/AGENT.md` default, the contract is always appended by the platform, and the config file resolves via `--config <path>` > `./gagent.yaml` > XDG (`os.UserConfigDir()/gagent/config.yaml`).

## 2. Context

Today `internal/agent/instructions.go` holds one `DefaultInstructions` string constant mixing persona and protocol; `Options.Instructions` overrides it wholesale, so a custom prompt can break the memory writeback loop. Config search is `./gagent.yaml` then `~/.gagent/` (hand-rolled, non-XDG). Owner decisions (issue #39): single agent, contract auto-merge, `--config` flag with XDG default, embedded AGENT.md persona, prompt path is flag-only (no config key).

## 3. Non-Goals

- Per-agent definition files, multiple personas, tools whitelists, a config key for the prompt path.
- Hot-reloading the persona file (restart-to-reload).
- Changes to the contract text itself.
- ADR (externalization of an existing mechanism, not a decision change).

## 4. Proposed Solution

### 4.0 Library & Tooling Choices

| Slot | Chosen | Rationale |
|---|---|---|
| Embedded default | `go:embed` (stdlib) | Issue mandates file-as-default, no new dep |
| Config search | viper `SetConfigFile` / `SetConfigName` + `AddConfigPath` (existing dep) | Issue mandates viper built-ins, no hand-rolled path plumbing |

### 4.1 Approach

- Split `DefaultInstructions` into persona (identity/behavior) and contract (tools, writeback format, reply conventions).
- Persona moves to `internal/agent/AGENT.md`, embedded via `go:embed`; contract stays a Go constant versioned with the code.
- Session composes `persona + contract` once at construction; injection point (first system message of a fresh session) is unchanged.
- CLI resolves the persona file at startup (fail fast on missing/unreadable) and threads it through `gateway.Factory` into `agent.Options.Persona`.
- Config resolution switches the home-directory search path from `~/.gagent/` to the XDG config dir and adds the `--config` explicit-path flag.

### 4.2 Interfaces

```go
// internal/agent/instructions.go
//go:embed AGENT.md
var DefaultPersona string
const Contract = `...` // tool use, writeback format, reply conventions
func ComposeInstructions(persona string) string // empty persona -> DefaultPersona; always appends Contract

// internal/agent/service.go
type Options struct { ...; Persona string; ... } // replaces Instructions

// internal/gateway/runtime.go
type Factory struct { ...; Persona string } // passed to Options.Persona

// cmd/gagent/persona.go
func resolvePersona(cmd *cobra.Command) (string, error) // "" flag -> "" (embedded default downstream)
```

Flags (root persistent): `--config <path>`, `--agent-prompt <path>`.

### 4.3 Data Model

N/A — no schema change; `gagent.yaml` keys are unchanged.

### 4.4 Implementation Steps

1. `internal/agent/AGENT.md` + rewrite `instructions.go` (embed, Contract, ComposeInstructions).
2. `service.go`: `Options.Persona`, composed instructions injected on fresh session; update `service_test.go`, add `instructions_test.go`.
3. `internal/gateway/runtime.go`: `Factory.Persona` wired to `Options.Persona`.
4. `cmd/gagent/config.go`: `--config` flag, XDG search chain; update `config_test.go`.
5. `cmd/gagent/persona.go` + root flag; thread persona through `agent.go`/`gateway.go`; add `persona_test.go`.
6. Docs: `docs/architecture/agent.md`, root `README.md`.
7. `make ci`, commit, PR.

### 4.5 Edge Cases & Error Handling

- `--agent-prompt` missing/unreadable → wrapped error at startup, before any model call.
- `--config` pointing at a missing file → error (viper non-NotFound path); absent file in the search chain is not an error.
- Empty persona file → composed instructions are effectively the contract only (accepted; no extra validation per issue).
- `os.UserConfigDir()` failing → XDG path skipped, cwd search still works.

## 5. Alternatives Considered

- Keep `~/.gagent/` alongside XDG — rejected: issue specifies the XDG chain only; two legacy paths rot.
- Config key for the prompt path — rejected explicitly by the issue (persona is a separately addressed asset).

## 6. Cross-Cutting Concerns

- Security: persona file is read at startup only; no secrets in it (same trust level as the config file). Contract non-overridable keeps the writeback loop tamper-proof from prompts.
- Observability: startup failure surfaces via the returned error (existing CLI error path).
- Compatibility: `~/.gagent/gagent.yaml` is no longer searched — called out in the PR description.
- Performance / privacy / reliability: N/A.

## 7. Test & Verification Plan

### 7.1 Automated Tests

- `internal/agent`: compose (default persona, custom persona, contract always present); session injects composed instructions once, on a fresh session only.
- `cmd/gagent`: config order (flag > cwd > XDG), `--config` missing file errors; persona resolution (flag file read, missing file errors, empty flag -> empty).

### 7.2 Manual Verification

`make ci` — pass.

### 7.3 Definition of Done

All acceptance criteria on issue #39 checked; docs updated; `make ci` green.

## 8. Risks & Open Questions

- `os.UserConfigDir()` behavior differs by OS; tests pin it via `XDG_CONFIG_HOME` (Linux target per issue).
- Dropping `~/.gagent/` search is a behavior change; flagged in the PR, not in an ADR (issue says none needed).

## 9. Deviations Log

None yet.

## Revision Log

- 2026-07-26: initial plan.
