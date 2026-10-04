# ADR 0023: The system is partitioned by domain, and a domain depends on a sibling one way through its declared surface

> Status: accepted
> Date: 2026-10-04

## Context

The convention template moved three structural rules out of `stack-go.md` — what a unit is named for, what a unit may reference across units, and when shared code is extracted — into a project's `structure.md`, written from the shape the project chose (#939). The template (convention-driven-project `8e32768`, `docs/architecture/structures/`) offers three shapes: domain, layered and hexagonal.

Sherlock already had a shape before it had a record of one. ADR 0001 maps components to `internal/<domain>/` packages rather than technical layers, and `packages.md` cuts every package by pod, then domain, then implementation. A layered partition would cut by technical responsibility — interface, logic, persistence — across subjects that change independently: the gateway, the reasoning loop, memory and the workspace. A hexagonal one makes the port the unit around one application, while Sherlock holds several subjects, each already reaching its technologies through interfaces it declares (`agent.Model`, `memory.Storer`).

Domain partitioning leaves one point open: whether a domain may depend on a sibling at all, from sharing code (Shared Kernel) to no reference whatsoever (Separate Ways) in *Domain-Driven Design* (Evans, 2003). The import graph answers it for most pairs: `internal/gateway/` imports `internal/agent/` and `internal/message/`, `internal/agent/` imports `internal/tool/`, and none of them imports back. The agent and memory domains are the exception — `internal/agent/memory/` imports `internal/memory/` and `internal/memory/sqlite/` imports `internal/agent/` — and that pair breaks the one-way relation decided below; it is a known violation, not an exception this record makes.

## Decision

The system is partitioned by domain: a top-level `internal/<name>/` package is the unit, and its structural rules live in `structure.md`. A domain may depend on a sibling one way, through the sibling's declared interface and published value types, never its concrete type.

## Consequences

- The rules a reviewer applies to packages are `structure.md`'s, and `stack-go.md` spells them in Go without restating them.
- A change to one subject usually lands in one `internal/<name>/` tree; a capability lands as a tool under `tools/<name>/` and touches no domain (ADR 0009).
- A domain cannot reach into a sibling's internals or concrete types, so a sibling's implementation can be replaced behind its interface without touching its consumers.
- Code no single domain owns has no home except a package below the domains, so shared code has to be named and justified rather than collected.

## Signs the decision was wrong

- Most changes touch several domains at once, and the pair that always move together cannot be released independently.
- The same fix is applied once per domain, and a domain keeps being missed.
- The shared code every domain imports is growing faster than any domain.
- Which domain a new file belongs to cannot be settled without asking someone.
- A domain's public surface has grown until it names what the domain keeps inside.
