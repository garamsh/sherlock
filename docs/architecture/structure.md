# Structure

Governs the structural rules of the shape this project is partitioned into: what the unit is and what it is named for, what may reference what across units, where concrete implementations are constructed, where shared code goes, and when a new unit is earned. The shape is domain partitioning (ADR 0023). Which packages these rules produce here — the pod split, the domains, the implementations beneath them — is `packages.md`'s, under "Three cuts produce every package".

## Current decisions

- **A domain is the unit.** A top-level `internal/<name>/` package is one domain: it holds that domain's types and sentinels, and the interfaces its consumers are written against. Every Go file under `internal/` belongs to exactly one domain, the domain whose directory holds it.
- **A unit is named for the subject it owns, never for a role or a catch-all.** `utils`, `helpers`, `common`, `ext`, `adapter`, `driver`, `platform`, `infra`, `kit` and `repo` name no subject, so none of them is a package or file name at any level.
- **A domain is reached only through what it declares public.** Its exported types and interfaces are its surface; an unexported name another domain would need is a sign the surface is missing something, not a reason to move code across.
- **The application has one composition site.** It is the one place that constructs concrete implementations, and it decides which implementation each declared abstraction receives.
- **Every other unit receives what it depends on and constructs none of it**, whatever the shape makes the unit — a domain, a layer, or an adapter.
- **The composition site stays thin.** It constructs and passes; it decides nothing else.
- **A consumer depends on the producer's declared interface, never its concrete type.** `internal/gateway/` runs a turn against `agent.Session` and `agent.Model`, the interfaces `internal/agent/` declares, and not against the structs behind them. A value type a domain publishes as part of its surface — `tool.Descriptor`, `message.Message` — is that surface, not a concrete implementation. Where the composition site sits is `stack-go.md` §Project envelope and the open question in `packages.md`. The compiler does not enforce this rule, so it is a review matter.
- **A domain may depend on a sibling, one way.** Siblings are neither isolated nor sharing code: a consumer imports the producer's declared surface, and the producer never imports the consumer. This is the point the domain shape leaves to the project (README §Rules, "A point the structural rules leave to the project is settled in an ADR"), and ADR 0023 records it.
- **Shared code sits below every domain, never beside one.** What two domains both need is extracted into a package that imports none of them — `internal/message/` holds the outbound message shape the gateway and `tools/message_send/` agree on and imports nothing in this module — or it is owned by one domain and reached through that domain's surface. It never becomes a sibling folder no domain owns.
- **A cycle between domains means the split is wrong**, and so does a file that belongs in two of them. Go rejects an import cycle at compile time; a file that belongs in two domains is found by reading.
- **A new domain is earned** by subject matter with its own lifecycle and vocabulary, which something outside it depends on, and whose public surface can stay stable while its inside changes. Short of that, the code is a file in the domain that uses it (`packages.md`, "Where a new package goes").

## Rationale

- ADR 0023 — why the system is partitioned by domain, the sibling relation it takes, and what would show the choice was wrong.
- ADR 0001 — one module, components as packages under `internal/` rather than top-level folders.
- ADR 0017 — the pod split, which `packages.md` applies before any domain is cut.

## Open questions

- **The composition site sits outside `cmd/`.** `internal/gateway/runtime.go` constructs concrete types for the turn it runs, which the concrete-type rule above leaves to the composition site alone. `packages.md` holds the question under "The composition root sits outside `cmd/`" (#710).
