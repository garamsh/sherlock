# Packages

Governs the package partition: which `stack-go.md` §1 layout this project is under, what makes a boundary exist, which way imports cross it, and where a new package goes. What each package holds is `README.md` §Modules; this file is how the line around it was drawn. Rationale: ADR 0001, ADR 0017, ADR 0004, ADR 0009.

## The layout this project is under

`stack-go.md` §1 offers three layouts and marks Layout A the default. This project is under **Layout B — domain-rich service**, which is why the choice is recorded here rather than left to that default. Both criteria the file offers agree, and they do not carry equal weight.

- **Shape decides it.** The two layouts list the same implementation at different paths: Layout A as a file in the domain package, Layout B promoted to a sub-package of it, the in-memory implementation promoted the same way. The tree here is Layout B's — `internal/memory/sqlite/` behind `memory.Storer`, `internal/agent/openai/` and `internal/agent/anthropic/` behind `agent.Model`, and `internal/agent/memory/` behind `agent.Recaller`, `agent.WritebackSink` and `agent.Compactor`. `find internal -mindepth 2 -type d` lists the sub-packages, these four among them. Layout A reaches that tree only by taking its own §Single file vs package exception once per implementation; Layout B states the promotion as the shape, and that is the thing the two layouts differ on.
- **Size agrees and settles nothing.** 7,639 shipped Go lines under `cmd/` and `internal/`, excluding tests and the files carrying the `// Code generated` marker; 17,071 counting tests. Layout B's stated band is 5k–30k. Neither of the other two names a module size at all: Layout A's `~300 LoC` sizes one implementation, not the module. So a measurement inside B's band confirms B and excludes neither of the others, which is why it corroborates the shape above and cannot stand in for it. Reproduce with `find cmd internal -name '*.go' ! -name '*_test.go' | xargs grep -L '^// Code generated' | xargs cat | wc -l`.

The choice decides a rule's scope rather than a style, which is why it is written down: **`service.go`**. Layout A carries it as a bullet every `internal/<domain>/` has; under Layout B it arrives with business behaviour coordinating several dependencies, so a domain that is only types and persistence has none. `stack-go.md` §1 states that condition under Layout B and owns it — a domain package here that has no `service.go` is not, on that fact alone, incomplete.

## Current decisions

Three cuts produce every package under `cmd/` and `internal/`, applied in that order.

- **The pod split cuts first** (ADR 0017). `internal/workspace/` and its sub-packages are the sidecar's; `internal/agent/`, `internal/gateway/`, `internal/memory/`, `internal/tool/` and `internal/message/` are the agent container's, and `internal/config/` resolves the settings both subcommands start from. No agent-side package imports the workspace sub-tree, and the sub-tree imports no other package in this module. Its only importers elsewhere are `tools/files/` and `tools/shell_exec/`, separate executables rather than code either container links — which is the import graph saying what ADR 0017 decided: the agent side reaches the workspace by spawning a tool. `cmd/gagent/` holds both sides because one binary carries both subcommands (ADR 0001), as two commands and not as a caller and a callee.
- **A domain cuts second.** A top-level `internal/<name>/` holds one domain's types and the interfaces its consumers are written against. Size decides none of them: `internal/message/` is one four-field struct and still a package, because `internal/gateway/` and the separately built `tools/message_send/` must agree on the JSON; `internal/workspace/` is the wire alone, so a tool links it without linking a backend (#525).
- **An implementation cuts third, and downward.** A sub-package holds either an implementation of an interface the package above declares — `internal/memory/sqlite/` for `memory.Storer`, `internal/agent/openai/` and `internal/agent/anthropic/` for `agent.Model`, `internal/agent/memory/` for `agent.Recaller`, `agent.WritebackSink` and `agent.Compactor` — or a backend nothing outside the sub-tree uses, which is `internal/workspace/files/` and `internal/workspace/shell/` behind `internal/workspace/server/`.

The direction is the rule: **no package's shipped code imports its own sub-package.** A sub-package imports upward (`internal/memory/sqlite/` to `internal/memory/`) or sideways inside its own sub-tree (`internal/workspace/server/` composing `files/` and `shell/`), never the reverse, and outside `cmd/gagent/` nothing crosses the pod split. `go list -f '{{.ImportPath}} {{.Imports}}' ./...` is the check.

Tests do cross, deliberately. `internal/agent/`'s tests import `internal/agent/memory/`, `internal/memory/`'s import `internal/memory/sqlite/`, `internal/message/`'s import `internal/tool/tooltest/`, and `tests/` wires both sides of the pod. The seam is a property of the shipped binaries; a test binary is where the halves are put back together on purpose.

A capability is not a package. Tools arrive as `tools/<name>/` directories the platform spawns from their manifests, so adding one changes no import (ADR 0009, ADR 0001).

## Two boundaries the names obscure

- **`internal/memory/` and `internal/agent/memory/` are not one thing split in two.** `internal/memory/` is the memory domain — address, operation, salience, `Storer`, `Service` — and it imports nothing else in the module; it stands with no agent around it. `internal/agent/memory/` is three hooks named for the interfaces they satisfy, and it imports both. The shared word names the domain the hooks reach, not a parent and a child; the arrows run the other way from what nesting suggests. Moving those files into `internal/agent/` would put `internal/memory` in the loop's import set, and the loop is written against `agent.Recalled` and `agent.WritebackOp`, never `memory.Memory` — `internal/agent/memory/recall.go` is where the two vocabularies convert. `internal/agent/`'s only shipped internal import is `internal/tool`, and keeping that true is what the sub-package is for.
- **The provider clients are packages because they stopped fitting as files.** `stack-go.md` §1 Single file vs package sizes this: a file while an implementation stays under roughly 300 lines with no private helpers worth isolating, a package past that. The same interface already ships as a file here — `internal/agent/scripted.go`, sixty lines of `agent.Model`. Both provider clients are past the threshold and hold private helpers besides: their own wire types, and for OpenAI the strict-mode schema projection. Names settle what size leaves open: both declare `Config`, `New`, `toWireMessages` and `toWireTools`, so one package cannot hold both without renaming half of one. Neither is imported by the loop it serves; `cmd/gagent/model_provider.go` selects one from the configured protocol (ADR 0004).

## Where a new package goes

Answer in order. The first yes places it.

1. Does the sidecar run it? It goes under `internal/workspace/`, and no agent-side package may import it.
2. Does it implement an interface an existing package declares? It goes with that package — a file in it, or a sub-package of it once `stack-go.md` §1 Single file vs package says so.
3. Must two separately built programs agree on it? It is a top-level `internal/<name>/` holding the shape and no behaviour, as `internal/message/` and `internal/workspace/` do.
4. Is it a domain — types, sentinels, and the interfaces its consumers are written against? It is a top-level `internal/<name>/`, and its implementations nest beneath it.
5. Otherwise it is a file in the package that uses it. One consumer and no interface is not a boundary.

A new capability usually answers no to all five, because it is a tool and not Go code the platform links.

## Open questions

- **The composition root sits outside `cmd/`.** `internal/gateway/runtime.go` calls `sqlite.Open`, `memory.NewService` and `agent.NewSession` and constructs the three `internal/agent/memory/` hooks, which `stack-go.md` §Project envelope reserves for the binary's entry point. `README.md` §Modules states it as the current shape; #508 held it as a criterion and closed as completed on 2026-08-16 with that criterion deferred, so no issue tracks it now.
- **`internal/tool/tooltest/` is produced by none of the three cuts.** It implements no interface `internal/tool/` declares and its consumers are elsewhere — the tests of `internal/gateway/` and `internal/message/`. It nests under the package whose manifests it reads, which is a fourth reason for a sub-package that the rule above does not state.
- **No ADR states the pod-split import rule.** ADR 0017 decides the two containers. That no agent-side package may import `internal/workspace/` follows from it, and is written down only here and in the import graph.
